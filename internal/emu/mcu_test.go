package emu

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoardsAndFlash(t *testing.T) {
	b := BoardOf("ESP32S3")
	if b == nil || b.Arch != "xtensa" || BoardOf("esp32c6").Arch != "riscv32" || BoardOf("esp32h2") == nil || BoardOf("nope") != nil {
		t.Fatal("boards")
	}
	img, mb, err := PadFlash([]byte{1, 2, 3}, b)
	if err != nil || mb != 2 || len(img) != 2<<20 || img[3] != 0xFF || img[2] != 3 {
		t.Fatalf("pad: %v %d %d", err, mb, len(img))
	}
	// the bootloader's header asks for 8 MB (0x3_): padded to 8, not the 2 the file fits in
	hdr := append([]byte{0xE9, 3, 2, 0x3F}, make([]byte, 100)...)
	if HeaderFlashMB(hdr) != 8 || HeaderFlashMB([]byte{1, 2, 3}) != 0 {
		t.Fatalf("header %d", HeaderFlashMB(hdr))
	}
	if img, mb, err := PadFlash(hdr, b); err != nil || mb != 8 || len(img) != 8<<20 {
		t.Fatalf("header pad: %d %v", mb, err)
	}
	if _, mb, _ := PadFlash(hdr, BoardOf("esp32c3")); mb != 8 {
		t.Fatalf("c3 header pad: %d", mb)
	}
	if _, _, err := PadFlash(make([]byte, 33<<20), b); err == nil {
		t.Fatal("too big")
	}
	// an ESP-IDF build folder: flash_args puts the binaries at their offsets, the rest is erased
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "bootloader"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bootloader", "bootloader.bin"), []byte{0xE9, 1}, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "app.bin"), []byte{0xE9, 2, 3}, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "flash_args"), []byte("--flash_mode dio --flash_freq 80m --flash_size 4MB\n0x0 bootloader/bootloader.bin\n0x10000 app.bin\n"), 0o644)
	m, err := MergeFlash(dir, 4)
	if err != nil || len(m) != 4<<20 || m[0] != 0xE9 || m[1] != 1 || m[2] != 0xFF || m[0x10000] != 0xE9 || m[0x10002] != 3 || m[len(m)-1] != 0xFF {
		t.Fatalf("merge: %v", err)
	}
	if _, err := MergeFlash(t.TempDir(), 4); err == nil {
		t.Fatal("no flash_args")
	}
}

func TestMCUArgsAndStart(t *testing.T) {
	l := &Lab{ID: "ab", Dir: "termwright-lab", Base: 20000}
	v := VM{ID: "m1", Name: "esp32s3-1", Index: 2, Image: "fw", RAM: 8, GDB: true}
	im := &Image{ID: "fw", FileName: "micropython-s3.bin", Board: "esp32s3", FlashMB: 8, NICs: 2}
	a := strings.Join(l.MCUArgs(v, im, BoardOf("esp32s3")), " ")
	li, se := l.TesterPorts(v, 1)
	for _, w := range []string{"-machine esp32s3", "file=termwright-lab/lab-ab/m1.flash.bin,if=mtd,format=raw", "-m 8M", "port=20009", "telnet:127.0.0.1:20010", "-gdb tcp:127.0.0.1:20011", "-daemonize",
		fmt.Sprintf("-nic socket,model=open_eth,mac=%s,udp=127.0.0.1:%d,localaddr=127.0.0.1:%d", l.PortMAC(v, im, 1), li, se)} {
		if !strings.Contains(a, w) {
			t.Errorf("args lack %q: %s", w, a)
		}
	}
	// cabled to a switch's port 3: its Ethernet sends to that port's socket
	sw0 := VM{ID: "s1", Name: "sw", Index: 1, Image: "exos"}
	l.VMs = []VM{v, sw0}
	l.Cables = []Cable{{A: "m1", APort: 1, B: "s1", BPort: 3}}
	a = strings.Join(l.MCUArgs(v, im, BoardOf("esp32s3")), " ")
	if w := fmt.Sprintf("udp=127.0.0.1:%d,localaddr=127.0.0.1:%d", l.udpPort(sw0, 3), l.udpPort(v, 1)); !strings.Contains(a, w) {
		t.Errorf("cabled: args lack %q: %s", w, a)
	}
	if a := strings.Join(l.MCUArgs(v, &Image{Board: "esp32s3"}, BoardOf("esp32s3")), " "); strings.Contains(a, "open_eth") {
		t.Errorf("an image without ports has no Ethernet: %s", a)
	}
	l.VMs, l.Cables = nil, nil
	sw := VM{ID: "s1", Name: "sw", Index: 1, Image: "exos"}
	s := l.StartScript([]VM{v, sw}, map[string]*Image{"fw": im, "exos": {ID: "exos", FileName: "exos.qcow2", Disk: "ide", NICs: 2}})
	if !strings.Contains(s, "qemu-esp/current/bin/qemu-system-xtensa") || !strings.Contains(s, "cp termwright-lab/images/micropython-s3.bin termwright-lab/lab-ab/m1.flash.bin") || !strings.Contains(s, "qemu-system-x86_64 -name sw ") || strings.Contains(s, "qemu-system-x86_64 -name esp32s3-1") {
		t.Fatalf("start script:\n%s", s)
	}
	if out, err := exec.Command("sh", "-n", "-c", s).CombinedOutput(); err != nil {
		t.Fatalf("the script does not parse: %v %s", err, out)
	}
}

// The tools script picks the release's builds for this host, checks their SHA-256 and unpacks them.
func TestMCUToolScript(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("no tar")
	}
	home, bin, src := t.TempDir(), t.TempDir(), t.TempDir()
	mk := func(arch string) (string, string) {
		d := filepath.Join(src, "qemu-"+arch, "qemu", "bin")
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, "qemu-system-"+arch), []byte("#!/bin/sh\necho "+arch+"\n"), 0o755)
		f := filepath.Join(src, "qemu-"+arch+"-softmmu-esp_develop_9.2.2_20261109-x86_64-linux-gnu.tar.gz")
		if out, err := exec.Command("tar", "-czf", f, "-C", filepath.Join(src, "qemu-"+arch), "qemu").CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		b, _ := os.ReadFile(f)
		h := sha256.Sum256(b)
		return f, hex.EncodeToString(h[:])
	}
	fx, hx := mk("xtensa")
	fr, hr := mk("riscv32")
	// a python3 that answers as the release would
	py := "#!/bin/sh\necho 'mcu-tag esp-develop-9.2.2-20261109'\necho 'mcu-url file://" + fx + " " + hx + "'\necho 'mcu-url file://" + fr + " " + hr + "'\n"
	_ = os.WriteFile(filepath.Join(bin, "python3"), []byte(py), 0o755)
	cmd := exec.Command("sh", "-c", "cd "+home+" && "+MCUToolScript("termwright-lab"))
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(out, []byte("mcu-tools esp-develop-9.2.2-20261109")) {
		t.Fatalf("%v %s", err, out)
	}
	for _, a := range []string{"xtensa", "riscv32"} {
		if o, err := exec.Command(filepath.Join(home, "termwright-lab/tools/qemu-esp/current/bin/qemu-system-"+a)).Output(); err != nil || strings.TrimSpace(string(o)) != a {
			t.Fatalf("%s: %v %s", a, err, o)
		}
	}
	// a file that does not match its SHA-256 is refused
	py = strings.Replace(py, hx, strings.Repeat("0", 64), 1)
	_ = os.WriteFile(filepath.Join(bin, "python3"), []byte(py), 0o755)
	home2 := t.TempDir()
	cmd = exec.Command("sh", "-c", "cd "+home2+" && "+MCUToolScript("termwright-lab"))
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "HOME="+home2)
	out, _ = cmd.CombinedOutput()
	if !bytes.Contains(out, []byte("does not match its SHA-256")) || bytes.Contains(out, []byte("mcu-tools esp")) {
		t.Fatalf("checksum: %s", out)
	}
}
