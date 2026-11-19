package topo

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"testing"
)

func TestJPEGToPDF(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for x := 0; x < 400; x++ {
		for y := 0; y < 200; y++ {
			img.Set(x, y, color.RGBA{uint8(x / 2), uint8(y), 120, 255})
		}
	}
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, img, nil); err != nil {
		t.Fatal(err)
	}
	pdf, err := JPEGToPDF(jb.Bytes(), "Network (map) \\ test é")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) || !bytes.HasSuffix(pdf, []byte("%%EOF\n")) {
		t.Fatal("header or trailer")
	}
	// the page is the picture's size at 96 dpi: 300 x 150 pt
	if !bytes.Contains(pdf, []byte("/MediaBox [0 0 300.00 150.00]")) {
		t.Fatal("page size")
	}
	// every xref entry points at "N 0 obj"
	m := regexp.MustCompile(`startxref\n(\d+)`).FindSubmatch(pdf)
	xr, _ := strconv.Atoi(string(m[1]))
	if !bytes.HasPrefix(pdf[xr:], []byte("xref\n0 7\n")) {
		t.Fatalf("startxref points at %q", pdf[xr:xr+12])
	}
	rows := regexp.MustCompile(`(\d{10}) 00000 n `).FindAllSubmatch(pdf[xr:], -1)
	if len(rows) != 6 {
		t.Fatalf("%d xref rows", len(rows))
	}
	for i, r := range rows {
		o, _ := strconv.Atoi(string(r[1]))
		if !bytes.HasPrefix(pdf[o:], []byte(fmt.Sprintf("%d 0 obj", i+1))) {
			t.Fatalf("object %d is not at %d: %q", i+1, o, pdf[o:o+10])
		}
	}
	if !bytes.Contains(pdf, []byte(`/Title (Network \(map\) \\ test )`)) {
		t.Fatal("title escaping")
	}
	if _, err := JPEGToPDF([]byte("nope"), "x"); err == nil {
		t.Fatal("a non-JPEG must be refused")
	}
	// a real PDF reader opens it, when one is installed
	if p, err := exec.LookPath("pdfinfo"); err == nil {
		f := t.TempDir() + "/m.pdf"
		_ = os.WriteFile(f, pdf, 0o600)
		out, err := exec.Command(p, f).CombinedOutput()
		if err != nil || !regexp.MustCompile(`Page size:\s+300 x 150 pts`).Match(out) {
			t.Fatalf("pdfinfo: %v\n%s", err, out)
		}
		if pp, err := exec.LookPath("pdftoppm"); err == nil { // the picture is really on the page: the left edge is dark-red-ish, the right edge more red
			ppm := t.TempDir() + "/o"
			if out, err := exec.Command(pp, "-r", "96", "-singlefile", "-png", f, ppm).CombinedOutput(); err != nil {
				t.Fatalf("pdftoppm: %v %s", err, out)
			}
			fh, _ := os.Open(ppm + ".png")
			defer fh.Close()
			im, _, err := image.Decode(fh)
			if err != nil || im.Bounds().Dx() != 400 || im.Bounds().Dy() != 200 {
				t.Fatalf("rendered page: %v %v", err, im.Bounds())
			}
			r0, _, _, _ := im.At(5, 100).RGBA()
			r1, _, _, _ := im.At(395, 100).RGBA()
			if r1>>8 < r0>>8+100 {
				t.Fatalf("the picture is not on the page: red %d at the left, %d at the right", r0>>8, r1>>8)
			}
		}
	} else {
		t.Log("pdfinfo not installed: structure checked only")
	}
}
