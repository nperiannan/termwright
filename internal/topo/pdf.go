package topo

import (
	"bytes"
	"errors"
	"fmt"
	"image/jpeg"
)

// JPEGToPDF wraps a JPEG picture in a one-page PDF whose page is the picture's size (a pixel is 0.75 pt, as
// at 96 dpi), so the map prints at its own size. title goes in the document information.
func JPEGToPDF(jpg []byte, title string) ([]byte, error) {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(jpg))
	if err != nil {
		return nil, errors.New("not a JPEG picture: " + err.Error())
	}
	cs := "/DeviceRGB"
	if cfg.ColorModel != nil {
		switch fmt.Sprintf("%T", cfg.ColorModel) {
		case "color.GrayModel":
			cs = "/DeviceGray"
		}
	}
	w, h := float64(cfg.Width)*0.75, float64(cfg.Height)*0.75
	var b bytes.Buffer
	var off []int
	obj := func(body string) {
		off = append(off, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(off), body)
	}
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj("<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>", w, h))
	off = append(off, b.Len())
	fmt.Fprintf(&b, "4 0 obj\n<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n", cfg.Width, cfg.Height, cs, len(jpg))
	b.Write(jpg)
	b.WriteString("\nendstream\nendobj\n")
	content := fmt.Sprintf("q %.2f 0 0 %.2f 0 0 cm /Im0 Do Q", w, h)
	obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	obj("<< /Title " + pdfString(title) + " /Producer (Termwright) >>")
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(off)+1)
	for _, o := range off {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info 6 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(off)+1, xref)
	return b.Bytes(), nil
}

// pdfString is a PDF literal string (ASCII; anything else is dropped).
func pdfString(s string) string {
	var o bytes.Buffer
	o.WriteByte('(')
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			o.WriteByte('\\')
			o.WriteRune(r)
		case r >= 0x20 && r < 0x7f:
			o.WriteRune(r)
		}
	}
	o.WriteByte(')')
	return o.String()
}
