package term

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ZMODEM (as used by lrzsz "sz"/"rz"), implemented from the public protocol
// description by Chuck Forsberg.

const (
	zPAD  = '*'
	zDLE  = 0x18
	zBIN  = 'A'
	zHEX  = 'B'
	zBIN3 = 'C'

	zRQINIT = 0
	zRINIT  = 1
	zSINIT  = 2
	zACK    = 3
	zFILE   = 4
	zSKIP   = 5
	zNAK    = 6
	zABORT  = 7
	zFIN    = 8
	zRPOS   = 9
	zDATA   = 10
	zEOF    = 11
	zFERR   = 12
	zCAN    = 16

	zCRCE = 'h'
	zCRCG = 'i'
	zCRCQ = 'j'
	zCRCW = 'k'
	zRUB0 = 'l'
	zRUB1 = 'm'

	canFDX  = 0x01
	canOVIO = 0x02
	canFC32 = 0x20
)

var errZAbort = errors.New("transfer aborted by remote")

// zAbortSeq cancels a ZMODEM session on the other side.
var zAbortSeq = []byte{zDLE, zDLE, zDLE, zDLE, zDLE, zDLE, zDLE, zDLE, 8, 8, 8, 8, 8, 8, 8, 8}

type zHeader struct {
	typ byte
	p   [4]byte
}

func (h zHeader) pos() uint32 { return binary.LittleEndian.Uint32(h.p[:]) }

func posHeader(typ byte, pos uint32) zHeader {
	h := zHeader{typ: typ}
	binary.LittleEndian.PutUint32(h.p[:], pos)
	return h
}

// zio wraps the byte stream with ZMODEM framing helpers.
type zio struct {
	rest    *[]byte // receives bytes after the session ("OO" excluded)
	ctx     context.Context
	in      <-chan byte
	write   func([]byte) error
	crc32   bool // use 32-bit CRC for binary headers/data we send
	pending []byte
}

func (z *zio) get(d time.Duration) (byte, error) {
	if len(z.pending) > 0 {
		b := z.pending[0]
		z.pending = z.pending[1:]
		return b, nil
	}
	b, err := readByte(z.ctx, z.in, d)
	if err != nil && z.ctx.Err() == nil {
		return 0, errors.New("timeout waiting for the other side")
	}
	return b, err
}

// zesc appends b ZDLE-escaped.
func zesc(out []byte, b byte) []byte {
	switch b {
	case zDLE, 0x10, 0x11, 0x13, 0x90, 0x91, 0x93:
		return append(out, zDLE, b^0x40)
	case 0x7f:
		return append(out, zDLE, zRUB0)
	case 0xff:
		return append(out, zDLE, zRUB1)
	}
	return append(out, b)
}

func crc16x(data []byte) uint16 { return crc16(data) }

func (z *zio) sendHex(h zHeader) error {
	raw := append([]byte{h.typ}, h.p[:]...)
	c := crc16x(raw)
	s := fmt.Sprintf("%c%c%c%c%02x%02x%02x%02x%02x%04x\r\x8a", zPAD, zPAD, zDLE, zHEX, h.typ, h.p[0], h.p[1], h.p[2], h.p[3], c)
	b := []byte(s)
	if h.typ != zFIN && h.typ != zACK {
		b = append(b, 0x11) // XON
	}
	return z.write(b)
}

func (z *zio) sendBin(h zHeader) error {
	raw := append([]byte{h.typ}, h.p[:]...)
	out := []byte{zPAD, zDLE}
	if z.crc32 {
		out = append(out, zBIN3)
	} else {
		out = append(out, zBIN)
	}
	for _, b := range raw {
		out = zesc(out, b)
	}
	if z.crc32 {
		var c [4]byte
		binary.LittleEndian.PutUint32(c[:], crc32.ChecksumIEEE(raw))
		for _, b := range c {
			out = zesc(out, b)
		}
	} else {
		c := crc16x(raw)
		out = zesc(zesc(out, byte(c>>8)), byte(c))
	}
	return z.write(out)
}

func (z *zio) sendData(data []byte, end byte) error {
	out := make([]byte, 0, len(data)*2+16)
	for _, b := range data {
		out = zesc(out, b)
	}
	out = append(out, zDLE, end)
	if z.crc32 {
		var c [4]byte
		binary.LittleEndian.PutUint32(c[:], crc32.ChecksumIEEE(append(append([]byte(nil), data...), end)))
		for _, b := range c {
			out = zesc(out, b)
		}
	} else {
		c := crc16x(append(append([]byte(nil), data...), end))
		out = zesc(zesc(out, byte(c>>8)), byte(c))
	}
	if end == zCRCW {
		out = append(out, 0x11)
	}
	return z.write(out)
}

// readEsc reads one ZDLE-decoded byte. frameEnd is set for ZCRCx markers.
func (z *zio) readEsc(d time.Duration) (b byte, frameEnd bool, err error) {
	for {
		c, err := z.get(d)
		if err != nil {
			return 0, false, err
		}
		switch c {
		case 0x11, 0x13, 0x91, 0x93:
			continue // flow control noise
		case zDLE:
		default:
			return c, false, nil
		}
		cancels := 1
		for {
			c, err = z.get(d)
			if err != nil {
				return 0, false, err
			}
			if c == 0x11 || c == 0x13 || c == 0x91 || c == 0x93 {
				continue
			}
			if c == zDLE {
				cancels++
				if cancels >= 5 {
					return 0, false, errZAbort
				}
				continue
			}
			break
		}
		switch c {
		case zCRCE, zCRCG, zCRCQ, zCRCW:
			return c, true, nil
		case zRUB0:
			return 0x7f, false, nil
		case zRUB1:
			return 0xff, false, nil
		}
		return c ^ 0x40, false, nil
	}
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// readHeader skips garbage until a valid header arrives.
func (z *zio) readHeader(d time.Duration) (zHeader, bool, error) {
	deadline := time.Now().Add(d)
	cancels := 0
	for time.Now().Before(deadline) {
		c, err := z.get(time.Until(deadline))
		if err != nil {
			return zHeader{}, false, err
		}
		if c == zDLE { // a run of CANs outside a frame aborts the session
			if cancels++; cancels >= 5 {
				return zHeader{}, false, errZAbort
			}
			continue
		}
		cancels = 0
		if c != zPAD {
			continue
		}
		for c == zPAD {
			if c, err = z.get(time.Second); err != nil {
				return zHeader{}, false, err
			}
		}
		if c != zDLE {
			continue
		}
		kind, err := z.get(time.Second)
		if err != nil {
			return zHeader{}, false, err
		}
		var h zHeader
		var raw []byte
		switch kind {
		case zHEX:
			for i := 0; i < 7; i++ {
				hi, err1 := z.get(time.Second)
				lo, err2 := z.get(time.Second)
				if err1 != nil || err2 != nil {
					return h, false, errors.New("timeout in header")
				}
				a, ok1 := hexVal(hi)
				b, ok2 := hexVal(lo)
				if !ok1 || !ok2 {
					raw = nil
					break
				}
				raw = append(raw, a<<4|b)
			}
			if len(raw) != 7 || crc16x(raw[:5]) != uint16(raw[5])<<8|uint16(raw[6]) {
				continue
			}
			// swallow CR LF (and XON) after hex headers
			for i := 0; i < 2; i++ {
				if b, err := z.get(200 * time.Millisecond); err != nil || (b != '\r' && b != '\n' && b != 0x8a && b != 0x8d) {
					break
				}
			}
			h.typ = raw[0]
			copy(h.p[:], raw[1:5])
			return h, false, nil
		case zBIN, zBIN3:
			n := 7
			if kind == zBIN3 {
				n = 9
			}
			for i := 0; i < n; i++ {
				b, end, err := z.readEsc(time.Second)
				if err != nil {
					return h, false, err
				}
				if end {
					break
				}
				raw = append(raw, b)
			}
			if len(raw) != n {
				continue
			}
			if kind == zBIN3 {
				if crc32.ChecksumIEEE(raw[:5]) != binary.LittleEndian.Uint32(raw[5:9]) {
					continue
				}
			} else if crc16x(raw[:5]) != uint16(raw[5])<<8|uint16(raw[6]) {
				continue
			}
			h.typ = raw[0]
			copy(h.p[:], raw[1:5])
			return h, kind == zBIN3, nil
		}
	}
	return zHeader{}, false, errors.New("timeout waiting for ZMODEM header")
}

// readData reads one data subpacket. crc32 tells which CRC the sender uses.
func (z *zio) readData(use32 bool, max int) ([]byte, byte, error) {
	var data []byte
	for {
		b, end, err := z.readEsc(10 * time.Second)
		if err != nil {
			return nil, 0, err
		}
		if end {
			n := 2
			if use32 {
				n = 4
			}
			var crc []byte
			for i := 0; i < n; i++ {
				c, _, err := z.readEsc(time.Second)
				if err != nil {
					return nil, 0, err
				}
				crc = append(crc, c)
			}
			chk := append(append([]byte(nil), data...), b)
			if use32 {
				if crc32.ChecksumIEEE(chk) != binary.LittleEndian.Uint32(crc) {
					return nil, 0, errors.New("CRC error")
				}
			} else if crc16x(chk) != uint16(crc[0])<<8|uint16(crc[1]) {
				return nil, 0, errors.New("CRC error")
			}
			return data, b, nil
		}
		data = append(data, b)
		if len(data) > max {
			return nil, 0, errors.New("data subpacket too long")
		}
	}
}

// zmodemReceive receives files (remote ran "sz") into dir.
func zmodemReceive(ctx context.Context, in <-chan byte, write func([]byte) error, dir string, progress func(file string, done, total int64), rest ...*[]byte) ([]string, error) {
	z := &zio{ctx: ctx, in: in, write: write}
	if len(rest) > 0 {
		z.rest = rest[0]
	}
	rinit := zHeader{typ: zRINIT, p: [4]byte{0, 0, 0, canFDX | canOVIO | canFC32}}
	if err := z.sendHex(rinit); err != nil {
		return nil, err
	}
	var saved []string
	var f *os.File
	var name string
	var size, pos int64
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	errors0 := 0
	for {
		h, is32, err := z.readHeader(20 * time.Second)
		if err != nil {
			_ = write(zAbortSeq)
			return saved, err
		}
		switch h.typ {
		case zRQINIT:
			_ = z.sendHex(rinit)
		case zSINIT:
			// attention string follows; read and acknowledge
			if _, _, err := z.readData(false, 64); err == nil {
				_ = z.sendHex(zHeader{typ: zACK})
			}
		case zFILE:
			info, _, err := z.readData(is32, 4096)
			if err != nil {
				// maybe 16-bit CRC: ask again
				_ = z.sendHex(zHeader{typ: zNAK})
				continue
			}
			parts := bytes.SplitN(info, []byte{0}, 2)
			name = filepath.Base(strings.ReplaceAll(string(parts[0]), "\\", "/"))
			size = -1
			if len(parts) > 1 {
				if fs := strings.Fields(string(parts[1])); len(fs) > 0 {
					size, _ = strconv.ParseInt(fs[0], 10, 64)
				}
			}
			target := uniqueFile(filepath.Join(dir, name))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return saved, err
			}
			if f, err = os.Create(target); err != nil {
				_ = z.sendHex(zHeader{typ: zSKIP})
				f = nil
				continue
			}
			name, pos = target, 0
			_ = z.sendHex(posHeader(zRPOS, 0))
		case zDATA:
			if f == nil {
				_ = z.sendHex(zHeader{typ: zSKIP})
				continue
			}
			if int64(h.pos()) != pos {
				_ = z.sendHex(posHeader(zRPOS, uint32(pos)))
				continue
			}
			for {
				data, end, err := z.readData(is32, 8192)
				if err != nil {
					errors0++
					if errors0 > 20 || errors.Is(err, errZAbort) || ctx.Err() != nil {
						_ = write(zAbortSeq)
						return saved, err
					}
					_ = z.sendHex(posHeader(zRPOS, uint32(pos)))
					break
				}
				if _, err := f.Write(data); err != nil {
					_ = write(zAbortSeq)
					return saved, err
				}
				pos += int64(len(data))
				progress(name, pos, size)
				if end == zCRCW || end == zCRCQ {
					_ = z.sendHex(posHeader(zACK, uint32(pos)))
				}
				if end == zCRCE || end == zCRCW {
					break
				}
			}
		case zEOF:
			if f == nil || int64(h.pos()) != pos {
				continue
			}
			f.Close()
			f = nil
			saved = append(saved, name)
			_ = z.sendHex(rinit)
		case zFIN:
			_ = z.sendHex(zHeader{typ: zFIN})
			// sender ends with "OO"; anything else is already terminal output
			for i := 0; i < 2; i++ {
				b, err := z.get(500 * time.Millisecond)
				if err != nil {
					break
				}
				if b != 'O' {
					z.pending = append([]byte{b}, z.pending...)
					break
				}
			}
			if z.rest != nil {
				*z.rest = append(*z.rest, z.pending...)
			}
			return saved, nil
		case zCAN, zABORT, zFERR:
			return saved, errZAbort
		}
	}
}

func uniqueFile(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p
	}
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for i := 1; ; i++ {
		c := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(c); err != nil {
			return c
		}
	}
}

// zmodemSend sends files to a receiver (remote ran "rz").
func zmodemSend(ctx context.Context, in <-chan byte, write func([]byte) error, files []string, progress func(file string, done, total int64)) error {
	z := &zio{ctx: ctx, in: in, write: write}
	// wait for ZRINIT (the receiver normally already sent one; ask again)
	_ = z.sendHex(zHeader{typ: zRQINIT})
	var rinit zHeader
	for {
		h, _, err := z.readHeader(30 * time.Second)
		if err != nil {
			return err
		}
		if h.typ == zRINIT {
			rinit = h
			break
		}
		if h.typ == zCAN || h.typ == zABORT {
			return errZAbort
		}
	}
	z.crc32 = rinit.p[3]&canFC32 != 0
	const block = 1024
	for _, file := range files {
		fh, err := os.Open(file)
		if err != nil {
			return err
		}
		st, _ := fh.Stat()
		size := st.Size()
		info := fmt.Sprintf("%s\x00%d %o %o 0 0 0", filepath.Base(file), size, st.ModTime().Unix(), 0o100644)
		var start int64 = -1
	fileInfo:
		for try := 0; try < 10; try++ {
			if err := z.sendBin(zHeader{typ: zFILE}); err != nil {
				fh.Close()
				return err
			}
			if err := z.sendData(append([]byte(info), 0), zCRCW); err != nil {
				fh.Close()
				return err
			}
			for {
				h, _, err := z.readHeader(30 * time.Second)
				if err != nil {
					fh.Close()
					return err
				}
				switch h.typ {
				case zRPOS:
					start = int64(h.pos())
					break fileInfo
				case zSKIP:
					break fileInfo
				case zRINIT:
					continue fileInfo
				case zCAN, zABORT, zFERR:
					fh.Close()
					return errZAbort
				}
			}
		}
		if start < 0 { // skipped
			fh.Close()
			continue
		}
		pos := start
		buf := make([]byte, block)
	sendLoop:
		for {
			if _, err := fh.Seek(pos, io.SeekStart); err != nil {
				fh.Close()
				return err
			}
			if err := z.sendBin(posHeader(zDATA, uint32(pos))); err != nil {
				fh.Close()
				return err
			}
			for {
				if ctx.Err() != nil {
					_ = write(zAbortSeq)
					fh.Close()
					return ctx.Err()
				}
				n, rerr := io.ReadFull(fh, buf)
				end := byte(zCRCG)
				last := rerr == io.EOF || rerr == io.ErrUnexpectedEOF
				if last {
					end = zCRCE
				} else if rerr != nil {
					fh.Close()
					return rerr
				}
				if err := z.sendData(buf[:n], end); err != nil {
					fh.Close()
					return err
				}
				pos += int64(n)
				progress(file, pos, size)
				// check for a ZRPOS (receiver error) without blocking
				select {
				case c := <-in:
					if c == zPAD {
						z.pending = append(z.pending, c)
						h, _, err := z.readHeader(2 * time.Second)
						if err == nil && h.typ == zRPOS {
							pos = int64(h.pos())
							continue sendLoop
						}
						if err == nil && (h.typ == zCAN || h.typ == zABORT) {
							fh.Close()
							return errZAbort
						}
					}
				default:
				}
				if last {
					break
				}
			}
			if err := z.sendBin(posHeader(zEOF, uint32(pos))); err != nil {
				fh.Close()
				return err
			}
			for {
				h, _, err := z.readHeader(30 * time.Second)
				if err != nil {
					fh.Close()
					return err
				}
				switch h.typ {
				case zRINIT:
					break sendLoop
				case zRPOS:
					pos = int64(h.pos())
					continue sendLoop
				case zCAN, zABORT, zFERR:
					fh.Close()
					return errZAbort
				}
			}
		}
		fh.Close()
	}
	// finish
	for try := 0; try < 5; try++ {
		if err := z.sendHex(zHeader{typ: zFIN}); err != nil {
			return err
		}
		h, _, err := z.readHeader(10 * time.Second)
		if err == nil && h.typ == zFIN {
			return z.write([]byte("OO"))
		}
	}
	return errors.New("receiver did not confirm the end of the session")
}
