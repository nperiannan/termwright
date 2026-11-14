package main

import (
	"os"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
	"path/filepath"
	"strings"
	"testing"
)

// A firmware becomes an image of the board's flash size: a .bin is padded (erased after it), an ESP-IDF build
// folder is merged from its flash_args.
func TestEmuAddFirmware(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = false, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	bin := filepath.Join(t.TempDir(), "micropython-s3.bin")
	_ = os.WriteFile(bin, append([]byte{0xE9}, make([]byte, 3<<20)...), 0o644) // 3 MB: an 4 MB flash
	v, err := a.EmuAddFirmware("esp32s3", bin, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var got bool
	for _, im := range v.Images {
		if im.Board == "esp32s3" && im.FlashMB == 4 && im.Size == 4<<20 && im.Name == "micropython-s3" && strings.HasSuffix(im.FileName, "-4mb.bin") {
			b, _ := os.ReadFile(im.File)
			got = len(b) == 4<<20 && b[0] == 0xE9 && b[len(b)-1] == 0xFF
		}
	}
	if !got {
		t.Fatalf("no 4 MB esp32s3 image: %+v", v.Images)
	}
	// asked for a larger flash
	if v, err = a.EmuAddFirmware("esp32s3", bin, "big", 16); err != nil || !func() bool {
		for _, im := range v.Images {
			if im.Name == "big" && im.FlashMB == 16 && im.Size == 16<<20 {
				return true
			}
		}
		return false
	}() {
		t.Fatalf("16 MB: %v", err)
	}
	// not an ESP image, an unknown board
	junk := filepath.Join(t.TempDir(), "x.bin")
	_ = os.WriteFile(junk, []byte("hello"), 0o644)
	if _, err := a.EmuAddFirmware("esp32s3", junk, "", 0); err == nil || !strings.Contains(err.Error(), "not an ESP flash image") {
		t.Fatalf("junk: %v", err)
	}
	if _, err := a.EmuAddFirmware("arduino", bin, "", 0); err == nil {
		t.Fatal("unknown board")
	}
	// a build folder for the C3
	build := t.TempDir()
	_ = os.WriteFile(filepath.Join(build, "boot.bin"), []byte{0xE9, 7}, 0o644)
	_ = os.WriteFile(filepath.Join(build, "flash_args"), []byte("--flash_size 4MB\n0x0 boot.bin\n"), 0o644)
	if v, err = a.EmuAddFirmware("esp32c3", build, "hello", 0); err != nil {
		t.Fatal(err)
	}
	for _, im := range v.Images {
		if im.Name == "hello" && (im.Board != "esp32c3" || im.FlashMB != 4 || im.DeviceType != "ESP32-C3") {
			t.Fatalf("c3: %+v", im)
		}
	}
}
