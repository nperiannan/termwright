package emu

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestISO9660Structure(t *testing.T) {
	files := SeedFiles(Seed{User: "root", Password: "pw", Host: "h1"})
	img, err := ISO9660("cidata", files)
	if err != nil {
		t.Fatal(err)
	}
	if len(img)%isoSector != 0 || len(img) < 22*isoSector {
		t.Fatalf("size %d", len(img))
	}
	pvd := img[16*isoSector:]
	if pvd[0] != 1 || string(pvd[1:6]) != "CD001" {
		t.Fatal("no primary volume descriptor")
	}
	if got := strings.TrimSpace(string(pvd[40:72])); got != "CIDATA" {
		t.Fatalf("label %q", got)
	}
	if n := binary.LittleEndian.Uint32(pvd[80:]); int(n)*isoSector != len(img) {
		t.Fatalf("space size %d sectors, image %d bytes", n, len(img))
	}
	if img[17*isoSector] != 255 {
		t.Fatal("no terminator")
	}
	// the root folder lists both files with Rock Ridge names and their data
	dir := img[20*isoSector : 21*isoSector]
	for name, body := range files {
		i := bytes.Index(dir, append([]byte{'N', 'M', byte(5 + len(name)), 1, 0}, name...))
		if i < 0 {
			t.Fatalf("no Rock Ridge name %q", name)
		}
		// the record that holds it starts before: find the record start by walking
		off := 0
		found := false
		for off < isoSector && dir[off] != 0 {
			l := int(dir[off])
			if i >= off && i < off+l {
				at := int(binary.LittleEndian.Uint32(dir[off+2:]))
				size := int(binary.LittleEndian.Uint32(dir[off+10:]))
				if got := string(img[at*isoSector : at*isoSector+size]); got != body {
					t.Fatalf("%s holds %q", name, got)
				}
				found = true
			}
			off += l
		}
		if !found {
			t.Fatalf("record of %s not found", name)
		}
	}
	if !bytes.Contains(dir, []byte{'S', 'P', 7, 1, 0xBE, 0xEF, 0}) {
		t.Fatal("no SUSP indicator in the root")
	}
}

func TestISO9660Rejects(t *testing.T) {
	if _, err := ISO9660("", nil); err == nil {
		t.Fatal("empty label accepted")
	}
	if _, err := ISO9660("x", map[string]string{"a/b": "1"}); err == nil {
		t.Fatal("path accepted")
	}
}

func TestSeedFilesHaveTheAccount(t *testing.T) {
	f := SeedFiles(Seed{Password: "s3cret"})
	if !strings.HasPrefix(f["user-data"], "#cloud-config\n") || !strings.Contains(f["user-data"], "s3cret") || !strings.Contains(f["user-data"], "name: root") {
		t.Fatalf("user-data: %s", f["user-data"])
	}
	if !strings.Contains(f["meta-data"], "instance-id:") {
		t.Fatal("meta-data without an instance id")
	}
}
