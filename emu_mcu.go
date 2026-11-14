package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// Microcontrollers in the emulated lab: a firmware is an image whose file is the board's whole flash; a
// microcontroller is a VM made from it, run by Espressif's QEMU on its host (internal/emu/mcu.go).

// EmuBoards lists the microcontrollers Termwright emulates.
func (a *App) EmuBoards() []emu.Board { return emu.Boards }

// EmuAddFirmware adds a microcontroller firmware as an image: p is a flash image (.bin, from offset 0: it is
// padded to the board's flash size, erased after it) or an ESP-IDF build folder (merged from its flash_args
// to flashMB, or the board's default). name "" = from the file or folder.
func (a *App) EmuAddFirmware(board, p, name string, flashMB int) (EmuView, error) {
	defer emuAs("user")()
	b := emu.BoardOf(board)
	if b == nil {
		return EmuView{}, fmt.Errorf("unknown microcontroller %q", board)
	}
	st, err := os.Stat(p)
	if err != nil {
		return EmuView{}, err
	}
	var img []byte
	mb := flashMB
	if st.IsDir() {
		if mb == 0 {
			mb = b.DefMB
		}
		if !emuIntIn(b.Flash, mb) {
			return EmuView{}, fmt.Errorf("the %s takes %v MB of flash, not %d", b.Name, b.Flash, mb)
		}
		if img, err = emu.MergeFlash(p, mb); err != nil {
			return EmuView{}, err
		}
	} else {
		if st.Size() > 64<<20 {
			return EmuView{}, errors.New("that is not a flash image (larger than 64 MB)")
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return EmuView{}, err
		}
		if len(raw) == 0 || raw[0] != 0xE9 && !(len(raw) > 0x1000 && raw[0x1000] == 0xE9) {
			return EmuView{}, errors.New("that is not an ESP flash image: it does not start with a bootloader (0xE9) - give the merged image (esptool merge_bin) or the ESP-IDF build folder")
		}
		if img, mb, err = emu.PadFlash(raw, b); err != nil {
			return EmuView{}, err
		}
		if flashMB > mb && emuIntIn(b.Flash, flashMB) { // a larger flash than the image needs, asked for: erased
			img, mb = append(img, bytes.Repeat([]byte{0xFF}, (flashMB-mb)<<20)...), flashMB
		}
	}
	base := filepath.Base(strings.TrimRight(p, "/"))
	if name = strings.TrimSpace(name); name == "" {
		name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	dir := a.emuDownloadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return EmuView{}, err
	}
	fn := fmt.Sprintf("%s-%s-%dmb.bin", b.Key, emuSafeName(name), mb)
	file := filepath.Join(dir, fn)
	if err := os.WriteFile(file, img, 0o644); err != nil {
		return EmuView{}, err
	}
	sum, size, err := fileSHA256(file)
	if err != nil {
		return EmuView{}, err
	}
	im := emu.Image{ID: randHex(4), Name: name, File: file, FileName: fn, Size: size, SHA256: sum, DeviceType: b.Name, CPUs: 1, NICs: 0,
		Board: b.Key, FlashMB: mb, Version: versionIn(base), Source: "firmware " + base}
	emuLab.mu.Lock()
	a.emuLoad()
	emuLab.f.Images = append(emuLab.f.Images, im)
	err = a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuFlashFirmware gives a microcontroller another firmware (an image of its board): its flash is made again
// from that image, so what the old firmware wrote to flash goes, and it starts again.
func (a *App) EmuFlashFirmware(id, imageID string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var v *emu.VM
	if l != nil {
		v = l.VMByID(id)
	}
	im := emuImage(imageID)
	if v == nil || im == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such microcontroller or firmware")
	}
	old := emuImage(v.Image)
	if old == nil || old.Board == "" || old.Board != im.Board {
		emuLab.mu.Unlock()
		return EmuView{}, fmt.Errorf("%s is for the %s: choose a firmware of the same board", im.Name, orStr(strings.ToUpper(im.Board), "other board"))
	}
	v.Image = imageID
	host, lab := emuHostOf(*v), a.emuFor(emuHostOf(*v))
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	if _, err := a.EmuStop([]string{id}); err != nil {
		return EmuView{}, err
	}
	if lab != nil {
		if _, err := a.emuRun(host, "rm -f "+emu.Q(lab.VMDir()+"/"+id+".flash.bin"), 30*time.Second); err != nil {
			return EmuView{}, err
		}
	}
	emuEv(id, "info", "flashed "+im.Name)
	return a.EmuStart([]string{id})
}

func emuIntIn(l []int, n int) bool {
	for _, x := range l {
		if x == n {
			return true
		}
	}
	return false
}

// emuSafeName keeps letters, digits, . _ - of a name for a file name.
func emuSafeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-.")
}
