package pdf

import (
	"bytes"
	"strings"
	"testing"
)

func TestDoc(t *testing.T) {
	d := New("Run (1)")
	d.Text(40, 800, Bold, 18, Black, "Hello (world) ✓ café")
	d.Rect(40, 700, 100, 20, RGB{0, 0.6, 0.3})
	d.AddPage()
	d.Line(40, 40, 200, 40, 1, Black)
	b := d.Bytes()
	if !bytes.HasPrefix(b, []byte("%PDF-1.4")) || !strings.Contains(string(b), "/Count 2") || !strings.Contains(string(b), `Hello \(world\) v caf\351`) || !bytes.HasSuffix(b, []byte("%%EOF\n")) {
		t.Fatalf("%s", b)
	}
	// the xref offsets point at "N 0 obj"
	s := string(b)
	i := strings.Index(s, "xref\n0 ")
	lines := strings.Split(s[i:], "\n")
	for n, l := range lines[3:] {
		if !strings.HasSuffix(l, " n ") {
			break
		}
		var off int
		for _, c := range l[:10] {
			off = off*10 + int(c-'0')
		}
		want := strings.TrimSpace(strings.Split(s[off:], " ")[0])
		if want != itoa(n+1) {
			t.Fatalf("object %d at %d: %q", n+1, off, s[off:off+10])
		}
	}
	if w := Wrap(Courier, 10, 60, "aaaa bbbb cccc dddd"); len(w) < 2 {
		t.Fatal(w)
	}
}

func itoa(n int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0) + func() string {
		b := []byte{}
		for n > 0 {
			b = append([]byte{byte('0' + n%10)}, b...)
			n /= 10
		}
		return string(b)
	}())
}
