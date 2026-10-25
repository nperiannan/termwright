package term

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// YMODEM batch transfer (XMODEM-1K with a file header block 0).

func ymodemBlock(num byte, data []byte, size int) []byte {
	chunk := make([]byte, size)
	n := copy(chunk, data)
	pad := byte(xSUB)
	if num == 0 {
		pad = 0
	}
	for i := n; i < size; i++ {
		chunk[i] = pad
	}
	hdr := byte(xSOH)
	if size == 1024 {
		hdr = xSTX
	}
	c := crc16(chunk)
	return append(append([]byte{hdr, num, ^num}, chunk...), byte(c>>8), byte(c))
}

// waitFor reads until one of want arrives (returns it) or timeout.
func waitByte(ctx context.Context, in <-chan byte, d time.Duration, want ...byte) (byte, error) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		b, err := readByte(ctx, in, time.Until(deadline))
		if err != nil {
			return 0, err
		}
		if b == xCAN {
			if b2, err := readByte(ctx, in, time.Second); err == nil && b2 == xCAN {
				return 0, errors.New("cancelled by the other side")
			}
		}
		for _, w := range want {
			if b == w {
				return b, nil
			}
		}
	}
	return 0, errors.New("timeout")
}

// sendBlockAck sends a packet until ACKed (10 tries).
func sendBlockAck(ctx context.Context, write func([]byte) error, in <-chan byte, pkt []byte) error {
	for try := 0; try < 10; try++ {
		if err := write(pkt); err != nil {
			return err
		}
		b, err := waitByte(ctx, in, 10*time.Second, xACK, xNAK, xC)
		if err != nil {
			if ctx.Err() != nil || strings.Contains(err.Error(), "cancelled") {
				return err
			}
			continue
		}
		if b == xACK {
			return nil
		}
	}
	return errors.New("block not acknowledged after 10 tries")
}

// ymodemSend sends files in one YMODEM batch.
func ymodemSend(ctx context.Context, write func([]byte) error, in <-chan byte, files []string, progress func(file string, done, total int64)) error {
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		st, _ := os.Stat(f)
		if _, err := waitByte(ctx, in, 60*time.Second, xC); err != nil {
			return fmt.Errorf("receiver did not start: %w", err)
		}
		hdr := fmt.Sprintf("%s\x00%d %o 100644", filepath.Base(f), len(data), st.ModTime().Unix())
		size := 128
		if len(hdr) > 127 {
			size = 1024
		}
		if err := sendBlockAck(ctx, write, in, ymodemBlock(0, []byte(hdr), size)); err != nil {
			return err
		}
		if _, err := waitByte(ctx, in, 10*time.Second, xC); err != nil {
			return err
		}
		num := byte(1)
		for off := 0; off < len(data); off += 1024 {
			end := off + 1024
			if end > len(data) {
				end = len(data)
			}
			bs := 1024
			if end-off <= 128 {
				bs = 128
			}
			if err := sendBlockAck(ctx, write, in, ymodemBlock(num, data[off:end], bs)); err != nil {
				return fmt.Errorf("%s: %w", filepath.Base(f), err)
			}
			num++
			progress(f, int64(end), int64(len(data)))
		}
		acked := false
		for try := 0; try < 10 && !acked; try++ {
			if err := write([]byte{xEOT}); err != nil {
				return err
			}
			b, err := waitByte(ctx, in, 10*time.Second, xACK, xNAK)
			acked = err == nil && b == xACK
		}
		if !acked {
			return errors.New("no ACK for end of file")
		}
	}
	// empty header ends the batch
	if _, err := waitByte(ctx, in, 30*time.Second, xC); err != nil {
		return err
	}
	return sendBlockAck(ctx, write, in, ymodemBlock(0, nil, 128))
}

// readBlock reads the rest of a block after its header byte.
func readBlock(ctx context.Context, in <-chan byte, hdr byte) (byte, []byte, bool) {
	size := 128
	if hdr == xSTX {
		size = 1024
	}
	pkt := make([]byte, size+4)
	for i := range pkt {
		c, err := readByte(ctx, in, 2*time.Second)
		if err != nil {
			return 0, nil, false
		}
		pkt[i] = c
	}
	if pkt[0] != ^pkt[1] {
		return 0, nil, false
	}
	data := pkt[2 : 2+size]
	c := crc16(data)
	if pkt[2+size] != byte(c>>8) || pkt[3+size] != byte(c) {
		return 0, nil, false
	}
	return pkt[0], data, true
}

// ymodemReceive receives a YMODEM batch into dir and returns the saved paths.
func ymodemReceive(ctx context.Context, write func([]byte) error, in <-chan byte, dir string, progress func(file string, done, total int64)) ([]string, error) {
	var saved []string
	for {
		// request the header block
		var hdr byte
		for try := 0; ; try++ {
			if try > 20 {
				return saved, errors.New("sender did not start")
			}
			if err := write([]byte{xC}); err != nil {
				return saved, err
			}
			b, err := waitByte(ctx, in, 3*time.Second, xSOH, xSTX, xEOT)
			if err == nil {
				if b == xEOT { // stray EOT from a previous file
					_ = write([]byte{xACK})
					continue
				}
				hdr = b
				break
			}
			if ctx.Err() != nil || strings.Contains(err.Error(), "cancelled") {
				return saved, err
			}
		}
		num, data, ok := readBlock(ctx, in, hdr)
		if !ok || num != 0 {
			_ = write([]byte{xNAK})
			continue
		}
		name := string(bytes.SplitN(data, []byte{0}, 2)[0])
		if name == "" { // end of batch
			_ = write([]byte{xACK})
			return saved, nil
		}
		size := int64(-1)
		if parts := bytes.SplitN(data, []byte{0}, 2); len(parts) == 2 {
			if f := strings.Fields(string(bytes.TrimRight(parts[1], "\x00"))); len(f) > 0 {
				size, _ = strconv.ParseInt(f[0], 10, 64)
			}
		}
		_ = write([]byte{xACK, xC})
		var buf []byte
		expect := byte(1)
		eots := 0
		for {
			b, err := waitByte(ctx, in, 10*time.Second, xSOH, xSTX, xEOT)
			if err != nil {
				return saved, err
			}
			if b == xEOT {
				if eots++; eots == 1 {
					_ = write([]byte{xNAK})
					continue
				}
				_ = write([]byte{xACK})
				break
			}
			n, chunk, ok := readBlock(ctx, in, b)
			switch {
			case !ok:
				_ = write([]byte{xNAK})
				continue
			case n == expect:
				buf = append(buf, chunk...)
				expect++
				progress(name, int64(len(buf)), size)
			case n == expect-1: // duplicate
			default:
				_ = write([]byte{xCAN, xCAN})
				return saved, fmt.Errorf("block sequence error")
			}
			_ = write([]byte{xACK})
		}
		if size >= 0 && int64(len(buf)) > size {
			buf = buf[:size]
		} else {
			buf = bytes.TrimRight(buf, "\x1a")
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return saved, err
		}
		target := uniqueFile(filepath.Join(dir, filepath.Base(name)))
		if err := os.WriteFile(target, buf, 0o644); err != nil {
			return saved, err
		}
		saved = append(saved, target)
	}
}
