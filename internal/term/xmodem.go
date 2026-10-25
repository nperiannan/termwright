package term

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	xSOH = 0x01
	xSTX = 0x02
	xEOT = 0x04
	xACK = 0x06
	xNAK = 0x15
	xCAN = 0x18
	xC   = 'C'
	xSUB = 0x1a
)

func crc16(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// SendXmodem uploads a file over a connected tab (typically Serial) with
// XMODEM-CRC / XMODEM-1K, e.g. to a device boot loader. blockSize is 128 or 1024.
func (m *Manager) SendXmodem(tabID, file string, blockSize int) (string, error) {
	t, err := m.get(tabID)
	if err != nil {
		return "", err
	}
	if !t.isConnected() {
		return "", errors.New("tab is not connected")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	if blockSize != 1024 {
		blockSize = 128
	}
	in, err := m.beginCapture(t, nil)
	if err != nil {
		return "", err
	}
	tr := &Transfer{ID: newID(), Direction: "xmodem", File: filepath.Base(file), Total: int64(len(data)), FilesTotal: 1, State: "running"}
	m.runCaptured(t, tr, in, func(ctx context.Context, in chan byte) error {
		err := xmodemSend(ctx, t.rawWrite, in, data, blockSize, func(n int64) {
			tr.Bytes = n
			m.emit.Emit("xfer:progress", *tr)
		})
		if err == nil {
			tr.FilesDone, tr.Bytes = 1, tr.Total
		}
		return err
	})
	return tr.ID, nil
}

func readByte(ctx context.Context, in <-chan byte, d time.Duration) (byte, error) {
	select {
	case b := <-in:
		return b, nil
	case <-time.After(d):
		return 0, errors.New("timeout")
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func xmodemSend(ctx context.Context, write func([]byte) error, in <-chan byte, data []byte, blockSize int, progress func(int64)) error {
	// wait up to 60 s for the receiver to ask for data
	useCRC := false
	deadline := time.Now().Add(60 * time.Second)
	for {
		if time.Now().After(deadline) {
			return errors.New("receiver did not start (no 'C' or NAK within 60 s)")
		}
		b, err := readByte(ctx, in, time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			continue
		}
		if b == xC {
			useCRC = true
			break
		}
		if b == xNAK {
			break
		}
		if b == xCAN {
			return errors.New("cancelled by receiver")
		}
	}
	if !useCRC {
		blockSize = 128 // classic checksum mode only supports 128-byte blocks
	}
	blk := byte(1)
	for off := 0; off < len(data); off += blockSize {
		chunk := make([]byte, blockSize)
		n := copy(chunk, data[off:])
		for i := n; i < blockSize; i++ {
			chunk[i] = xSUB
		}
		hdr := byte(xSOH)
		if blockSize == 1024 {
			hdr = xSTX
		}
		pkt := append([]byte{hdr, blk, ^blk}, chunk...)
		if useCRC {
			c := crc16(chunk)
			pkt = append(pkt, byte(c>>8), byte(c))
		} else {
			var sum byte
			for _, x := range chunk {
				sum += x
			}
			pkt = append(pkt, sum)
		}
		acked := false
		for try := 0; try < 10 && !acked; try++ {
			if err := write(pkt); err != nil {
				return err
			}
			for {
				b, err := readByte(ctx, in, 10*time.Second)
				if err != nil {
					if ctx.Err() != nil {
						return err
					}
					break // timeout -> resend
				}
				if b == xACK {
					acked = true
					break
				}
				if b == xNAK {
					break
				}
				if b == xCAN {
					return errors.New("cancelled by receiver")
				}
			}
		}
		if !acked {
			return fmt.Errorf("block %d not acknowledged after 10 tries", blk)
		}
		blk++
		end := off + blockSize
		if end > len(data) {
			end = len(data)
		}
		progress(int64(end))
	}
	for try := 0; try < 10; try++ {
		if err := write([]byte{xEOT}); err != nil {
			return err
		}
		if b, err := readByte(ctx, in, 10*time.Second); err == nil && b == xACK {
			return nil
		}
	}
	return errors.New("no ACK for end of transmission")
}

// xmodemReceive receives one file with XMODEM-CRC (128 or 1K blocks).
// Trailing SUB (0x1A) padding of the last block is removed.
func xmodemReceive(ctx context.Context, write func([]byte) error, in <-chan byte, outPath string, progress func(int64)) error {
	var data []byte
	expect := byte(1)
	started := false
	errs := 0
	for {
		if !started {
			if err := write([]byte{xC}); err != nil {
				return err
			}
		}
		b, err := readByte(ctx, in, 3*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			if errs++; errs > 20 {
				return errors.New("sender did not start or stopped responding")
			}
			if started {
				_ = write([]byte{xNAK})
			}
			continue
		}
		switch b {
		case xEOT:
			_ = write([]byte{xACK})
			for len(data) > 0 && data[len(data)-1] == xSUB {
				data = data[:len(data)-1]
			}
			return os.WriteFile(outPath, data, 0o644)
		case xCAN:
			return errors.New("cancelled by sender")
		case xSOH, xSTX:
			started = true
			size := 128
			if b == xSTX {
				size = 1024
			}
			pkt := make([]byte, size+4) // blk, ~blk, data, crc hi, crc lo
			ok := true
			for i := range pkt {
				c, err := readByte(ctx, in, 2*time.Second)
				if err != nil {
					ok = false
					break
				}
				pkt[i] = c
			}
			if !ok || pkt[0] != ^pkt[1] {
				_ = write([]byte{xNAK})
				continue
			}
			chunk := pkt[2 : 2+size]
			c := crc16(chunk)
			if pkt[2+size] != byte(c>>8) || pkt[3+size] != byte(c) {
				_ = write([]byte{xNAK})
				continue
			}
			switch pkt[0] {
			case expect:
				data = append(data, chunk...)
				expect++
				progress(int64(len(data)))
			case expect - 1: // duplicate of the previous block
			default:
				_ = write([]byte{xCAN, xCAN, xCAN})
				return fmt.Errorf("block sequence error (got %d, want %d)", pkt[0], expect)
			}
			errs = 0
			_ = write([]byte{xACK})
		}
	}
}
