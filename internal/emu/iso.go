package emu

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Seed is the data a cloud image (cloud-init, NoCloud) reads from a small
// CD-ROM labelled "cidata": who can log in and the instance's identity.
type Seed struct {
	User     string // the account whose password is set ("root")
	Password string
	Host     string // the hostname it boots with
}

// SeedFiles are the files of the cidata CD-ROM for a cloud image: the account
// can log in on the serial console and over SSH with a password.
func SeedFiles(s Seed) map[string]string {
	user := orStr(s.User, "root")
	pw := orStr(s.Password, "termwright")
	host := orStr(s.Host, "cloud")
	ud := "#cloud-config\n" +
		"disable_root: false\n" +
		"ssh_pwauth: true\n" +
		"chpasswd:\n  expire: false\n  users:\n    - {name: " + user + ", password: \"" + pw + "\", type: text}\n" +
		"runcmd:\n  - [ sh, -c, \"echo '" + user + ":" + pw + "' | chpasswd\" ]\n"
	md := "instance-id: termwright-seed\nlocal-hostname: " + host + "\n"
	return map[string]string{"user-data": ud, "meta-data": md}
}

const isoSector = 2048

// ISO9660 builds a small CD-ROM image with the given label and files in its
// top folder: ISO 9660 level 1 names with Rock Ridge names (so "user-data"
// keeps its hyphen and case when Linux mounts it), no sub-folders. It is what
// a cloud-init seed needs, and nothing more.
func ISO9660(label string, files map[string]string) ([]byte, error) {
	if label == "" || len(label) > 32 {
		return nil, errors.New("the label must be 1 to 32 characters")
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	const root = 20 // sector of the root folder
	type ent struct {
		name, id string
		data     []byte
		at       int
	}
	var es []ent
	next := root + 1
	used := map[string]bool{}
	for _, n := range names {
		if n == "" || strings.ContainsAny(n, "/\x00") || len(n) > 200 {
			return nil, fmt.Errorf("file name %q cannot be used", n)
		}
		d := []byte(files[n])
		id := isoID(n, 0)
		for k := 1; used[id]; k++ {
			id = isoID(n, k)
		}
		used[id] = true
		es = append(es, ent{name: n, id: id, data: d, at: next})
		next += (len(d) + isoSector - 1) / isoSector
		if len(d) == 0 {
			next++
		}
	}
	img := make([]byte, next*isoSector)
	both32 := func(b []byte, v uint32) {
		binary.LittleEndian.PutUint32(b, v)
		binary.BigEndian.PutUint32(b[4:], v)
	}
	both16 := func(b []byte, v uint16) {
		binary.LittleEndian.PutUint16(b, v)
		binary.BigEndian.PutUint16(b[2:], v)
	}
	// a directory record (ECMA-119 9.1) with its system use area (Rock Ridge)
	rec := func(at, size int, dir bool, id string, su []byte) []byte {
		n := 33 + len(id)
		if n%2 == 1 {
			n++
		}
		n += len(su)
		if n%2 == 1 {
			n++
		}
		r := make([]byte, n)
		r[0] = byte(n)
		both32(r[2:], uint32(at))
		both32(r[10:], uint32(size))
		copy(r[18:], []byte{124, 1, 1, 0, 0, 0, 0}) // 2024-01-01 00:00:00 UTC
		if dir {
			r[25] = 2
		}
		both16(r[28:], 1)
		r[32] = byte(len(id))
		copy(r[33:], id)
		off := 33 + len(id)
		if off%2 == 1 {
			off++
		}
		copy(r[off:], su)
		return r
	}
	nm := func(name string) []byte { // Rock Ridge: the flags entry (RR: names follow), then the alternate name
		b := []byte{'R', 'R', 5, 1, 0x08, 'N', 'M', byte(5 + len(name)), 1, 0}
		return append(b, name...)
	}
	// the root folder
	sp := []byte{'S', 'P', 7, 1, 0xBE, 0xEF, 0, 'R', 'R', 5, 1, 0}
	sp = append(sp, 'E', 'R', 8+10+4, 1, 10, 4, 0, 1)
	sp = append(sp, "RRIP_1991ARRIP"...) // the extension id and its description
	var dir []byte
	dir = append(dir, rec(root, isoSector, true, "\x00", sp)...)
	dir = append(dir, rec(root, isoSector, true, "\x01", nil)...)
	for _, e := range es {
		r := rec(e.at, len(e.data), false, e.id, nm(e.name))
		if len(dir)+len(r) > isoSector {
			return nil, errors.New("too many files for one folder")
		}
		dir = append(dir, r...)
	}
	copy(img[root*isoSector:], dir)
	for _, e := range es {
		copy(img[e.at*isoSector:], e.data)
	}
	// the path table (one entry: the root), little and big endian
	pt := func(le bool) []byte {
		b := make([]byte, 10)
		b[0] = 1
		if le {
			binary.LittleEndian.PutUint32(b[2:], root)
			binary.LittleEndian.PutUint16(b[6:], 1)
		} else {
			binary.BigEndian.PutUint32(b[2:], root)
			binary.BigEndian.PutUint16(b[6:], 1)
		}
		return b
	}
	copy(img[18*isoSector:], pt(true))
	copy(img[19*isoSector:], pt(false))
	// the volume descriptors
	pvd := img[16*isoSector : 17*isoSector]
	pvd[0] = 1
	copy(pvd[1:], "CD001")
	pvd[6] = 1
	pad := func(off, n int, s string) {
		for i := 0; i < n; i++ {
			pvd[off+i] = ' '
		}
		copy(pvd[off:], strings.ToUpper(s))
	}
	pad(8, 32, "LINUX")
	pad(40, 32, label)
	both32(pvd[80:], uint32(next))
	both16(pvd[120:], 1)
	both16(pvd[124:], 1)
	both16(pvd[128:], isoSector)
	both32(pvd[132:], 10)
	binary.LittleEndian.PutUint32(pvd[140:], 18)
	binary.BigEndian.PutUint32(pvd[148:], 19)
	copy(pvd[156:], rec(root, isoSector, true, "\x00", nil))
	pad(190, 128, "")
	pad(318, 128, "")
	pad(446, 128, "TERMWRIGHT")
	pad(574, 128, "TERMWRIGHT")
	pad(702, 37, "")
	pad(739, 37, "")
	pad(776, 37, "")
	for _, off := range []int{813, 830, 864} {
		copy(pvd[off:], "2024010100000000")
		pvd[off+16] = 0
	}
	copy(pvd[847:], "0000000000000000")
	pvd[881] = 1
	end := img[17*isoSector:]
	end[0] = 255
	copy(end[1:], "CD001")
	end[6] = 1
	return img, nil
}

// isoID makes a level 1 file name (8.3, capitals, digits and _) of a file name, unique by its number.
func isoID(name string, n int) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := b.String()
	if len(s) > 8 {
		s = s[:8]
	}
	if n > 0 {
		t := fmt.Sprintf("%d", n)
		s = s[:min(len(s), 8-len(t))] + t
	}
	return s + ".;1"
}
