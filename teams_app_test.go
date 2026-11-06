package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strconv"
	"testing"
)

func TestTeamSetUpHelpers(t *testing.T) {
	if s := teamSlug("  Switching Lab B / Ünit 3 "); s != "switching-lab-b-nit-3" {
		t.Errorf("slug %q", s)
	}
	if shQuote("it's") != `'it'\''s'` {
		t.Error("shQuote")
	}
	lst := "0.0.0.0:22\n[::]:80\n127.0.0.1:8080\n*:7443\n0.0.0.0:7444->7443/tcp, :::7444->7443/tcp\n"
	used := map[int]bool{}
	for _, m := range portRe.FindAllStringSubmatch(lst, -1) {
		p, _ := strconv.Atoi(m[1])
		used[p] = true
	}
	for _, p := range []int{22, 80, 8080, 7443, 7444} {
		if !used[p] {
			t.Errorf("port %d not seen as used", p)
		}
	}
	if used[7445] || freePort(used) != 7445 {
		t.Errorf("free port %d", freePort(used))
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ n, b string }{{"termwright-team/INSTALL.md", "read me"}, {"termwright-team/termwright-team", "ELF…"}} {
		_ = tw.WriteHeader(&tar.Header{Name: f.n, Mode: 0o755, Size: int64(len(f.b)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(f.b))
	}
	_ = tw.Close()
	_ = gz.Close()
	if b, err := teamBinaryFromTarGz(&buf); err != nil || string(b) != "ELF…" {
		t.Errorf("binary %q %v", b, err)
	}
}
