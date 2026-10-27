// Package sessionfile reads and writes Termwright's plain-text session files
// (one .ini file per session, S:/D:/B:/Z: typed keys). The same layout is used
// to import and export session libraries of other terminal programs.
//
// The format was implemented from scratch by inspecting user-owned session
// files; no third-party code is used.
//
// File layout (one entry per line, continuation lines start with a space):
//
//	S:"Hostname"=192.0.2.10              string
//	D:"[SSH2] Port"=00000016             32-bit value, 8 hex digits
//	B:"Some Binary"=00000004             binary, byte count in hex, then
//	 01 02 03 04                          hex bytes on continuation lines
//	Z:"Description"=00000002             string list, count in hex, then
//	 first line                           one item per continuation line
//	 second line
//
// Unknown entry types and keys are preserved verbatim so that a file read and
// written by Termwright stays loadable by the original application.
package sessionfile

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Entry is one key in a session file.
type Entry struct {
	Type  byte     // 'S', 'D', 'B', 'Z' or any other type letter (kept raw)
	Name  string   // key name without quotes
	Str   string   // S value
	DWord uint32   // D value
	Bin   []byte   // B value
	List  []string // Z value
	Raw   []string // original lines for unknown types (first line + continuations)
}

// File is an ordered list of entries plus formatting details needed to write
// the file back in the same style.
type File struct {
	Entries []*Entry
	BOM     bool
	CRLF    bool
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Parse decodes a session .ini file.
func Parse(data []byte) (*File, error) {
	f := &File{}
	if bytes.HasPrefix(data, utf8BOM) {
		f.BOM = true
		data = data[len(utf8BOM):]
	}
	f.CRLF = bytes.Contains(data, []byte("\r\n"))
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var cur *Entry
	var binHex strings.Builder
	finishBin := func() error {
		if cur != nil && cur.Type == 'B' {
			b, err := hex.DecodeString(binHex.String())
			if err != nil {
				return fmt.Errorf("binary %q: %w", cur.Name, err)
			}
			cur.Bin = b
			binHex.Reset()
		}
		return nil
	}
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.HasPrefix(line, " ") && cur != nil {
			cont := line[1:]
			switch cur.Type {
			case 'B':
				binHex.WriteString(strings.ReplaceAll(cont, " ", ""))
			case 'Z':
				cur.List = append(cur.List, cont)
			default:
				cur.Raw = append(cur.Raw, line)
			}
			continue
		}
		if err := finishBin(); err != nil {
			return nil, err
		}
		cur = nil
		if strings.TrimSpace(line) == "" {
			continue
		}
		e, err := parseHeader(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		f.Entries = append(f.Entries, e)
		cur = e
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if err := finishBin(); err != nil {
		return nil, err
	}
	return f, nil
}

func parseHeader(line string) (*Entry, error) {
	// T:"Name"=value
	if len(line) < 4 || line[1] != ':' || line[2] != '"' {
		return &Entry{Type: '?', Raw: []string{line}}, nil
	}
	end := strings.Index(line[3:], "\"=")
	if end < 0 {
		return &Entry{Type: '?', Raw: []string{line}}, nil
	}
	e := &Entry{Type: line[0], Name: line[3 : 3+end]}
	val := line[3+end+2:]
	switch e.Type {
	case 'S':
		e.Str = val
	case 'D':
		v, err := strconv.ParseUint(strings.TrimSpace(val), 16, 32)
		if err != nil {
			return nil, fmt.Errorf("dword %q: %w", e.Name, err)
		}
		e.DWord = uint32(v)
	case 'B', 'Z':
		// value is a hex length/count; data follows on continuation lines
		if _, err := strconv.ParseUint(strings.TrimSpace(val), 16, 32); err != nil {
			return nil, fmt.Errorf("size %q: %w", e.Name, err)
		}
		if e.Type == 'B' {
			e.Bin = []byte{}
		} else {
			e.List = []string{}
		}
	default:
		e.Raw = []string{line}
	}
	return e, nil
}

// Bytes encodes the file.
func (f *File) Bytes() []byte {
	var b bytes.Buffer
	nl := "\n"
	if f.CRLF {
		nl = "\r\n"
	}
	if f.BOM {
		b.Write(utf8BOM)
	}
	for _, e := range f.Entries {
		switch e.Type {
		case 'S':
			fmt.Fprintf(&b, "S:\"%s\"=%s%s", e.Name, e.Str, nl)
		case 'D':
			fmt.Fprintf(&b, "D:\"%s\"=%08x%s", e.Name, e.DWord, nl)
		case 'B':
			fmt.Fprintf(&b, "B:\"%s\"=%08x%s", e.Name, len(e.Bin), nl)
			for i := 0; i < len(e.Bin); i += 16 {
				j := i + 16
				if j > len(e.Bin) {
					j = len(e.Bin)
				}
				for _, c := range e.Bin[i:j] {
					fmt.Fprintf(&b, " %02x", c)
				}
				b.WriteString(nl)
			}
		case 'Z':
			fmt.Fprintf(&b, "Z:\"%s\"=%08x%s", e.Name, len(e.List), nl)
			for _, s := range e.List {
				b.WriteString(" " + s + nl)
			}
		default:
			for _, l := range e.Raw {
				b.WriteString(l + nl)
			}
		}
	}
	return b.Bytes()
}

// NewFile returns an empty file (Windows line ends and a UTF-8 BOM, the usual style).
func NewFile() *File { return &File{BOM: true, CRLF: true} }

// Get returns the entry with the given name, or nil.
func (f *File) Get(name string) *Entry {
	for _, e := range f.Entries {
		if e.Name == name && e.Type != '?' {
			return e
		}
	}
	return nil
}

// Str returns a string value or def.
func (f *File) Str(name, def string) string {
	if e := f.Get(name); e != nil && e.Type == 'S' {
		return e.Str
	}
	return def
}

// DWord returns a numeric value or def.
func (f *File) DWord(name string, def uint32) uint32 {
	if e := f.Get(name); e != nil && e.Type == 'D' {
		return e.DWord
	}
	return def
}

// Bool returns a D value interpreted as a boolean.
func (f *File) Bool(name string, def bool) bool {
	d := uint32(0)
	if def {
		d = 1
	}
	return f.DWord(name, d) != 0
}

// List returns a Z value or nil.
func (f *File) List(name string) []string {
	if e := f.Get(name); e != nil && e.Type == 'Z' {
		return append([]string{}, e.List...)
	}
	return nil
}

func (f *File) upsert(t byte, name string) *Entry {
	if e := f.Get(name); e != nil {
		if e.Type != t {
			*e = Entry{Type: t, Name: name}
		}
		return e
	}
	e := &Entry{Type: t, Name: name}
	f.Entries = append(f.Entries, e)
	return e
}

// SetStr sets a string value, creating the key if needed.
func (f *File) SetStr(name, v string) {
	// strings cannot span lines in an S entry
	v = strings.NewReplacer("\r", "", "\n", " ").Replace(v)
	f.upsert('S', name).Str = v
}

// SetDWord sets a numeric value.
func (f *File) SetDWord(name string, v uint32) { f.upsert('D', name).DWord = v }

// SetBool sets a boolean as 0/1 dword.
func (f *File) SetBool(name string, v bool) {
	var d uint32
	if v {
		d = 1
	}
	f.SetDWord(name, d)
}

// SetList sets a string list.
func (f *File) SetList(name string, v []string) {
	clean := make([]string, 0, len(v))
	for _, s := range v {
		clean = append(clean, strings.NewReplacer("\r", "", "\n", " ").Replace(s))
	}
	f.upsert('Z', name).List = clean
}

// SetBin sets a binary value.
func (f *File) SetBin(name string, v []byte) { f.upsert('B', name).Bin = append([]byte(nil), v...) }

// Delete removes a key.
func (f *File) Delete(name string) {
	out := f.Entries[:0]
	for _, e := range f.Entries {
		if e.Name != name || e.Type == '?' {
			out = append(out, e)
		}
	}
	f.Entries = out
}

// Clone deep-copies the file.
func (f *File) Clone() *File {
	n := &File{BOM: f.BOM, CRLF: f.CRLF}
	for _, e := range f.Entries {
		c := *e
		c.Bin = append([]byte(nil), e.Bin...)
		c.List = append([]string(nil), e.List...)
		c.Raw = append([]string(nil), e.Raw...)
		n.Entries = append(n.Entries, &c)
	}
	return n
}
