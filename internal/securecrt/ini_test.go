package securecrt

import (
	"bytes"
	"os"
	"testing"
)

func TestParseRoundTrip(t *testing.T) {
	data, err := os.ReadFile("testdata/switch.ini")
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !f.BOM || !f.CRLF {
		t.Fatalf("expected BOM and CRLF, got %v %v", f.BOM, f.CRLF)
	}
	if got := f.Str("Hostname", ""); got != "192.0.2.10" {
		t.Fatalf("hostname %q", got)
	}
	if got := f.DWord("[SSH2] Port", 0); got != 22 {
		t.Fatalf("port %d", got)
	}
	if d := f.List("Description"); len(d) != 2 || d[1] != "Rack 4" {
		t.Fatalf("description %#v", d)
	}
	if b := f.Get("Color Scheme Overrides").Bin; len(b) != 20 || b[19] != 0x13 {
		t.Fatalf("binary %v", b)
	}
	if l := f.List("Port Forward Table V2"); l == nil || len(l) != 0 {
		t.Fatalf("empty list %#v", l)
	}
	if out := f.Bytes(); !bytes.Equal(out, data) {
		t.Fatalf("round trip differs:\n%q\n%q", out, data)
	}
}

func TestSetters(t *testing.T) {
	f := NewFile()
	f.SetStr("Hostname", "a\nb")
	f.SetDWord("Port", 23)
	f.SetList("Description", []string{"x"})
	f.SetBool("Flag", true)
	g, err := Parse(f.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if g.Str("Hostname", "") != "a b" || g.DWord("Port", 0) != 23 || !g.Bool("Flag", false) || g.List("Description")[0] != "x" {
		t.Fatalf("unexpected %q", f.Bytes())
	}
	g.Delete("Port")
	if g.Get("Port") != nil {
		t.Fatal("delete failed")
	}
}

func TestXMLRoundTrip(t *testing.T) {
	data, _ := os.ReadFile("testdata/switch.ini")
	f, _ := Parse(data)
	var buf bytes.Buffer
	if err := WriteXML(&buf, XMLSessions{"Lab/Core/switch": f, "router": NewFile()}); err != nil {
		t.Fatal(err)
	}
	got, err := ParseXML(&buf)
	if err != nil {
		t.Fatal(err)
	}
	g := got["Lab/Core/switch"]
	if g == nil {
		t.Fatalf("missing session, got %v", keys(got))
	}
	if g.Str("Hostname", "") != "192.0.2.10" || g.DWord("[SSH2] Port", 0) != 22 || len(g.List("Description")) != 2 {
		t.Fatalf("bad values %q", g.Bytes())
	}
	if len(g.Get("Color Scheme Overrides").Bin) != 20 {
		t.Fatal("binary lost")
	}
}

func keys(m XMLSessions) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	return k
}
