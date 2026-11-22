// Package pdf writes simple PDF documents - text in the standard fonts, lines and filled boxes, several
// A4 pages - with no dependencies (reports that open anywhere).
package pdf

import (
	"bytes"
	"fmt"
	"strings"
)

// Fonts (the PDF standard 14; nothing embedded).
const (
	Helvetica = "F1"
	Bold      = "F2"
	Courier   = "F3"
)

// A4 in points.
const (
	PageW = 595.28
	PageH = 841.89
)

// RGB is a colour (0..1 each).
type RGB struct{ R, G, B float64 }

var Black = RGB{0, 0, 0}

// Doc is a document being written.
type Doc struct {
	pages []*bytes.Buffer
	cur   *bytes.Buffer
	Title string
}

// New starts a document with one empty page.
func New(title string) *Doc {
	d := &Doc{Title: title}
	d.AddPage()
	return d
}

// AddPage starts a new page.
func (d *Doc) AddPage() {
	d.cur = &bytes.Buffer{}
	d.pages = append(d.pages, d.cur)
}

// Pages is how many pages there are.
func (d *Doc) Pages() int { return len(d.pages) }

// esc makes text safe for a PDF string in WinAnsi (Latin-1); other characters become ?.
func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '✓':
			b.WriteString("v")
		case r == '✗':
			b.WriteString("x")
		case r == '…':
			b.WriteString("...")
		case r == '→':
			b.WriteString("->")
		case r == '\t':
			b.WriteString("    ")
		case r < 32:
		case r < 128:
			b.WriteRune(r)
		case r < 256:
			fmt.Fprintf(&b, "\\%03o", r)
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

// Text writes s at x, y (from the bottom left) in a font and size.
func (d *Doc) Text(x, y float64, font string, size float64, c RGB, s string) {
	fmt.Fprintf(d.cur, "BT /%s %.1f Tf %.3f %.3f %.3f rg %.2f %.2f Td (%s) Tj ET\n", font, size, c.R, c.G, c.B, x, y, esc(s))
}

// Rect fills a box.
func (d *Doc) Rect(x, y, w, h float64, c RGB) {
	fmt.Fprintf(d.cur, "%.3f %.3f %.3f rg %.2f %.2f %.2f %.2f re f\n", c.R, c.G, c.B, x, y, w, h)
}

// Line draws a line.
func (d *Doc) Line(x1, y1, x2, y2, width float64, c RGB) {
	fmt.Fprintf(d.cur, "%.3f %.3f %.3f RG %.2f w %.2f %.2f m %.2f %.2f l S\n", c.R, c.G, c.B, width, x1, y1, x2, y2)
}

// Width estimates the width of s in points (Courier exactly, Helvetica on average).
func Width(font string, size float64, s string) float64 {
	n := float64(len([]rune(s)))
	switch font {
	case Courier:
		return n * size * 0.6
	case Bold:
		return n * size * 0.56
	}
	return n * size * 0.52
}

// Wrap splits s into lines that fit width.
func Wrap(font string, size, width float64, s string) []string {
	var out []string
	for _, para := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		max := int(width / (Width(font, size, "n")))
		if max < 8 {
			max = 8
		}
		r := []rune(para)
		if len(r) == 0 {
			out = append(out, "")
			continue
		}
		for len(r) > max {
			cut := max
			for i := max; i > max/2; i-- {
				if r[i] == ' ' {
					cut = i
					break
				}
			}
			out = append(out, string(r[:cut]))
			r = []rune(strings.TrimLeft(string(r[cut:]), " "))
		}
		out = append(out, string(r))
	}
	return out
}

// Bytes writes the PDF file.
func (d *Doc) Bytes() []byte {
	var b bytes.Buffer
	var offs []int
	obj := func(body string) int {
		offs = append(offs, b.Len())
		n := len(offs)
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", n, body)
		return n
	}
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	// 1 catalog, 2 pages (written later as fixed numbers), 3-5 fonts
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	offs = append(offs, 0) // 2: the page tree, written after the pages
	f1 := obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	f2 := obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	f3 := obj("<< /Type /Font /Subtype /Type1 /BaseFont /Courier /Encoding /WinAnsiEncoding >>")
	var kids []string
	for _, p := range d.pages {
		c := p.Bytes()
		cn := obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(c), c))
		pn := obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Contents %d 0 R /Resources << /Font << /F1 %d 0 R /F2 %d 0 R /F3 %d 0 R >> >> >>", PageW, PageH, cn, f1, f2, f3))
		kids = append(kids, fmt.Sprintf("%d 0 R", pn))
	}
	offs[1] = b.Len()
	fmt.Fprintf(&b, "2 0 obj\n<< /Type /Pages /Kids [%s] /Count %d >>\nendobj\n", strings.Join(kids, " "), len(kids))
	info := obj(fmt.Sprintf("<< /Title (%s) /Producer (Termwright) >>", esc(d.Title)))
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offs)+1, info, xref)
	return b.Bytes()
}
