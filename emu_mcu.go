package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	im := emu.Image{ID: randHex(4), Name: name, File: file, FileName: fn, Size: size, SHA256: sum, DeviceType: b.Name, CPUs: 1, NICs: 2, // port 1: its Ethernet
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

// EmuSetGDB turns a microcontroller's GDB server on or off (from its next start: QEMU's -gdb on the spare port
// of its slot, on its host; "target remote" goes there through the host).
func (a *App) EmuSetGDB(id string, on bool) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	var v *emu.VM
	if emuLab.f.Lab != nil {
		v = emuLab.f.Lab.VMByID(id)
	}
	if v == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such microcontroller")
	}
	v.GDB = on
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// Downloads for microcontrollers (tests point them at their own server).
var (
	emuMCUReleaseURL  = "https://api.github.com/repos/espressif/qemu/releases/latest"
	emuMicroPythonURL = "https://micropython.org/download/"
	emuMCUHTTP        = &http.Client{Timeout: 10 * time.Minute}
)

// emuGet downloads a URL to w (at most max bytes), returning its SHA-256.
func emuGet(url string, w io.Writer, max int64) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Termwright")
	resp, err := emuMCUHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", url, resp.Status)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, max+1))
	if err != nil {
		return "", err
	}
	if n > max {
		return "", fmt.Errorf("%s is larger than expected", url)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// emuMCUToolsLocal gets Espressif's QEMU for this computer when it is not there yet: the newest release's two
// builds for this system and CPU (github.com/espressif/qemu), each checked against the release's SHA-256,
// unpacked into the lab's tools folder in the home folder ("current" points at it). On a KVM host the start
// script does the same.
func (a *App) emuMCUToolsLocal(labDir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, emu.MCUTools(labDir))
	have := func() bool {
		for _, arch := range []string{"xtensa", "riscv32"} {
			if st, err := os.Stat(filepath.Join(dir, "current", "bin", "qemu-system-"+arch)); err != nil || st.Mode()&0o111 == 0 {
				return false
			}
		}
		return true
	}
	if have() {
		return nil
	}
	var rel bytes.Buffer
	if _, err := emuGet(emuMCUReleaseURL, &rel, 4<<20); err != nil {
		return fmt.Errorf("Espressif's QEMU could not be found: %w", err)
	}
	tag, assets, err := emu.MCUAssets(rel.Bytes(), runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	if strings.ContainsAny(tag, "/\\") || tag == "." || tag == ".." {
		return errors.New("unexpected release name " + tag)
	}
	emuLab.mu.Lock()
	emuEvOn(emu.LocalHost, "", "info", "downloading Espressif's QEMU "+tag+" for this computer (once)")
	emuLab.mu.Unlock()
	to := filepath.Join(dir, tag)
	if err := os.MkdirAll(to, 0o755); err != nil {
		return err
	}
	for _, as := range assets {
		f := filepath.Join(dir, filepath.Base(as.URL))
		out, err := os.Create(f)
		if err != nil {
			return err
		}
		sum, err := emuGet(as.URL, out, 1<<30)
		out.Close()
		if err == nil && as.SHA256 != "" && !strings.EqualFold(sum, as.SHA256) {
			err = fmt.Errorf("%s does not match its SHA-256", filepath.Base(as.URL))
		}
		if err == nil {
			if o, e := exec.Command("tar", "-xf", f, "-C", to, "--strip-components=1").CombinedOutput(); e != nil {
				err = fmt.Errorf("could not unpack %s: %s", filepath.Base(as.URL), strings.TrimSpace(string(o)))
			}
		}
		_ = os.Remove(f) // the download itself: unpacked (or not usable)
		if err != nil {
			return err
		}
	}
	cur := filepath.Join(dir, "current")
	_ = os.Remove(cur)
	if err := os.Symlink(tag, cur); err != nil {
		return err
	}
	if !have() {
		return errors.New("Espressif's QEMU was unpacked but its programs are not where expected")
	}
	emuLab.mu.Lock()
	emuEvOn(emu.LocalHost, "", "ok", "Espressif's QEMU "+tag+" is on this computer")
	emuLab.mu.Unlock()
	return nil
}

// EmuSampleFirmware adds MicroPython as a board's firmware: its newest release from micropython.org, downloaded
// once - the next time it is there. The ESP32-C3's only: the S3 and C6 builds (1.29) stop in Espressif's QEMU.
func (a *App) EmuSampleFirmware(board string) (EmuView, error) {
	b := emu.BoardOf(board)
	if b == nil {
		return EmuView{}, fmt.Errorf("unknown microcontroller %q", board)
	}
	if b.Sample == "" {
		return EmuView{}, fmt.Errorf("there is no MicroPython sample for the %s (MicroPython's builds for the S3 and C6 stop right after their bootloader in Espressif's QEMU; the H2 has none): add a firmware of your own (Add Firmware), or use an ESP32-C3", b.Name)
	}
	emuLab.mu.Lock()
	a.emuLoad()
	for _, im := range emuLab.f.Images {
		if im.Board == b.Key && strings.HasPrefix(im.Source, "firmware "+b.Sample+"-") {
			emuLab.mu.Unlock()
			return a.EmuState()
		}
	}
	emuLab.mu.Unlock()
	var page bytes.Buffer
	if _, err := emuGet(emuMicroPythonURL+b.Sample+"/", &page, 4<<20); err != nil {
		return EmuView{}, fmt.Errorf("MicroPython's download page could not be read: %w", err)
	}
	url, ver := emu.MicroPythonLatest(page.String(), b.Sample)
	if url == "" {
		return EmuView{}, fmt.Errorf("micropython.org lists no release for %s", b.Sample)
	}
	if strings.HasPrefix(url, "/") { // a path on the site
		if u, err := neturl.Parse(emuMicroPythonURL); err == nil {
			url = u.Scheme + "://" + u.Host + url
		}
	}
	if strings.HasPrefix(emuMicroPythonURL, "https://") && !strings.HasPrefix(url, "https://micropython.org/") {
		return EmuView{}, errors.New("unexpected download address " + url)
	}
	var bin bytes.Buffer
	if _, err := emuGet(url, &bin, 16<<20); err != nil {
		return EmuView{}, fmt.Errorf("MicroPython could not be downloaded: %w", err)
	}
	dir := a.emuDownloadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return EmuView{}, err
	}
	file := filepath.Join(dir, filepath.Base(url)) // as micropython.org names it (the firmware's name comes from it)
	if err := os.WriteFile(file, bin.Bytes(), 0o644); err != nil {
		return EmuView{}, err
	}
	return a.EmuAddFirmware(b.Key, file, "MicroPython "+ver, 0)
}
