package emu

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Microcontrollers: Espressif chips run in Espressif's QEMU (qemu-system-xtensa for the ESP32-S3,
// qemu-system-riscv32 for the C and H series) from their flash image. No KVM is needed (software
// emulation, tens of MB each). The UART is the console (a telnet port on the host, as a switch's);
// there is no Wi-Fi or Bluetooth in the emulator.

// Board is a microcontroller Termwright emulates.
type Board struct {
	Key     string `json:"key"`     // the image's board ("esp32s3")
	Name    string `json:"name"`    // "ESP32-S3"
	Arch    string `json:"arch"`    // xtensa or riscv32: which QEMU runs it
	Machine string `json:"machine"` // QEMU's -machine
	CPU     string `json:"cpu"`     // what it is, for the lists
	Flash   []int  `json:"flash"`   // flash sizes it takes, MB (the image is padded to one of them)
	DefMB   int    `json:"defMb"`   // the default flash size
	PSRAM   []int  `json:"psram"`   // PSRAM sizes it takes, MB (0 = none)
}

// Boards are the microcontrollers there are (the C6 and H2 have initial support in Espressif's QEMU).
var Boards = []Board{
	{Key: "esp32s3", Name: "ESP32-S3", Arch: "xtensa", Machine: "esp32s3", CPU: "Xtensa LX7 ×2, 240 MHz", Flash: []int{2, 4, 8, 16, 32}, DefMB: 8, PSRAM: []int{0, 2, 4, 8}},
	{Key: "esp32c3", Name: "ESP32-C3", Arch: "riscv32", Machine: "esp32c3", CPU: "RISC-V, 160 MHz", Flash: []int{2, 4, 8, 16}, DefMB: 4},
	{Key: "esp32c6", Name: "ESP32-C6", Arch: "riscv32", Machine: "esp32c6", CPU: "RISC-V, 160 MHz", Flash: []int{2, 4, 8, 16}, DefMB: 4},
	{Key: "esp32h2", Name: "ESP32-H2", Arch: "riscv32", Machine: "esp32h2", CPU: "RISC-V, 96 MHz", Flash: []int{2, 4, 8, 16}, DefMB: 4},
}

// BoardOf finds a board by its key ("" or unknown: nil).
func BoardOf(key string) *Board {
	for i := range Boards {
		if strings.EqualFold(Boards[i].Key, key) {
			return &Boards[i]
		}
	}
	return nil
}

// FlashSize is the size an image of n bytes is padded to: the smallest the board takes that holds it.
func (b *Board) FlashSize(n int64) (int, error) {
	for _, mb := range b.Flash {
		if int64(mb)<<20 >= n {
			return mb, nil
		}
	}
	return 0, fmt.Errorf("the firmware (%d MB) is larger than the %s's largest flash (%d MB)", (n+(1<<20)-1)>>20, b.Name, b.Flash[len(b.Flash)-1])
}

// MCUTools is where Espressif's QEMU goes on a host (under the lab folder).
func MCUTools(labDir string) string { return labDir + "/tools/qemu-esp" }

// MCUQemu is the QEMU binary for a board on a host.
func MCUQemu(labDir string, b *Board) string {
	return MCUTools(labDir) + "/current/bin/qemu-system-" + b.Arch
}

// MCUArgs is the QEMU command line of a microcontroller (its flash is a copy of the image of its own, so
// what the firmware writes to it stays; its UART is the console port, its monitor the monitor port, and the
// spare port of its slot is the GDB server when asked for).
func (l *Lab) MCUArgs(v VM, b *Board) []string {
	dir := l.VMDir()
	a := []string{"-name", strings.ReplaceAll(v.Name, ",", ",,"), "-machine", b.Machine,
		"-drive", "file=" + dir + "/" + v.ID + ".flash.bin,if=mtd,format=raw"}
	if v.RAM > 0 && containsInt(b.PSRAM, v.RAM) { // PSRAM
		a = append(a, "-m", strconv.Itoa(v.RAM)+"M")
	}
	a = append(a,
		"-chardev", fmt.Sprintf("socket,id=con0,host=127.0.0.1,port=%d,server=on,wait=off,telnet=on,logfile=%s/%s.log", l.ConsolePort(v), dir, v.ID),
		"-serial", "chardev:con0",
		"-monitor", fmt.Sprintf("telnet:127.0.0.1:%d,server,nowait", l.MonitorPort(v)),
		"-display", "none", "-daemonize", "-pidfile", dir+"/"+v.ID+".pid")
	if v.GDB {
		a = append(a, "-gdb", fmt.Sprintf("tcp:127.0.0.1:%d", l.GDBPort(v)))
	}
	return a
}

// GDBPort is a microcontroller's GDB server port on its host (the spare port of its slot).
func (l *Lab) GDBPort(v VM) int { return l.Base + v.Index*4 + 3 }

// mcuStart is the start script's part for a microcontroller: its flash copy, Espressif's QEMU, then QEMU.
func (l *Lab) mcuStart(v VM, im *Image, b *Board) string {
	var s strings.Builder
	dir := Q(l.VMDir())
	flash := dir + "/" + v.ID + ".flash.bin"
	fmt.Fprintf(&s, "if %s; then echo 'up %s'; else\n", qemuUp(dir+"/"+v.ID+".pid"), v.ID)
	// its own flash, from the image the first time (Flash Firmware removes it, so it comes again)
	fmt.Fprintf(&s, " [ -f %s ] || cp %s %s || { echo 'fail %s the firmware %s is not on this host'; }\n", flash, Q(l.ImageDir()+"/"+im.FileName), flash, v.ID, im.FileName)
	fmt.Fprintf(&s, " q=%s; [ -x \"$q\" ] || { %s; }\n", Q(MCUQemu(l.Dir, b)), MCUToolScript(l.Dir))
	fmt.Fprintf(&s, " if [ ! -x \"$q\" ]; then echo 'fail %s no Espressif QEMU on this host: it could not be downloaded (see the log)'; else\n", v.ID)
	fmt.Fprintf(&s, " : > %s/%s.log\n", dir, v.ID)
	q := "\"$q\" " + joinArgs(l.MCUArgs(v, b))
	q = strings.ReplaceAll(q, "file="+l.Dir+"/", "file=$HOME/"+l.Dir+"/")
	fmt.Fprintf(&s, " if out=$( { %s; } 2>&1); then echo 'started %s'; else m=$(echo $out | tr '\\n' ' '); echo \"fail %s ${m:-QEMU stopped at once and said nothing}\"; fi\n fi\nfi\n", q, v.ID, v.ID)
	return s.String()
}

// MCUToolScript downloads Espressif's QEMU (both CPUs: xtensa and riscv32) for this host from the newest
// release of github.com/espressif/qemu into the lab's tools folder, checks it against the release's SHA-256
// and points "current" at it. It prints "mcu-tools <version>" or "mcu-tools-fail <why>".
func MCUToolScript(labDir string) string {
	t := Q(MCUTools(labDir))
	py := `import json,os,platform,sys,urllib.request
m={'x86_64':'x86_64-linux-gnu','amd64':'x86_64-linux-gnu','aarch64':'aarch64-linux-gnu','arm64':'aarch64-linux-gnu'}.get(platform.machine().lower())
if sys.platform=='darwin': m={'arm64':'aarch64-apple-darwin','x86_64':'x86_64-apple-darwin'}.get(platform.machine())
if not m: print('mcu-tools-fail no Espressif QEMU for '+platform.machine()); sys.exit()
r=json.load(urllib.request.urlopen('https://api.github.com/repos/espressif/qemu/releases/latest',timeout=30))
out=[]
for arch in ('xtensa','riscv32'):
  a=[x for x in r.get('assets',[]) if x['name'].startswith('qemu-'+arch+'-softmmu') and m in x['name'] and x['name'].endswith(('.tar.xz','.tar.gz'))]
  if not a: print('mcu-tools-fail the release '+r.get('tag_name','')+' has no '+arch+' build for '+m); sys.exit()
  out.append(a[0]['browser_download_url']+' '+(a[0].get('digest') or '').replace('sha256:',''))
print('mcu-tag '+r['tag_name']); [print('mcu-url '+o) for o in out]`
	return fmt.Sprintf(`(mkdir -p %[1]s && cd %[1]s && python3 -c %[2]s > urls.txt 2>&1; tag=$(sed -n 's/^mcu-tag //p' urls.txt); `+
		`if [ -z "$tag" ]; then echo "mcu-tools-fail $(tail -c 300 urls.txt | tr '\n' ' ')"; else mkdir -p "$tag"; grep '^mcu-url ' urls.txt > dl.txt; ok=1; `+
		`while read -r _ u h; do f="${u##*/}"; curl -fsSL -o "$f" "$u" || { echo "mcu-tools-fail could not download $f"; ok=; break; }; `+
		`if [ -n "$h" ] && ! echo "$h  $f" | sha256sum -c - >/dev/null 2>&1; then echo "mcu-tools-fail $f does not match its SHA-256"; ok=; break; fi; `+
		`tar -xf "$f" -C "$tag" --strip-components=1 || { echo "mcu-tools-fail could not unpack $f"; ok=; break; }; rm -f "$f"; done < dl.txt; `+
		`[ -n "$ok" ] && ln -sfn "$tag" current && echo "mcu-tools $tag"; fi)`, t, Q(py))
}

// MergeFlash makes a flash image of sizeMB from an ESP-IDF build folder: its flash_args file lists the binaries
// and where they go ("0x0 bootloader/bootloader.bin"); the rest of the flash is erased (0xFF), as esptool's
// merge_bin --fill-flash-size does.
func MergeFlash(buildDir string, sizeMB int) ([]byte, error) {
	f, err := os.Open(filepath.Join(buildDir, "flash_args"))
	if err != nil {
		return nil, errors.New("no flash_args in the folder: choose the build folder of an ESP-IDF project (after idf.py build)")
	}
	defer f.Close()
	img := bytes.Repeat([]byte{0xFF}, sizeMB<<20)
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		w := strings.Fields(sc.Text())
		for i := 0; i+1 < len(w); i++ {
			if !strings.HasPrefix(strings.ToLower(w[i]), "0x") {
				continue
			}
			off, err := strconv.ParseInt(w[i][2:], 16, 64)
			if err != nil {
				continue
			}
			bin, err := os.ReadFile(filepath.Join(buildDir, w[i+1]))
			if err != nil {
				return nil, fmt.Errorf("%s (in flash_args): %w", w[i+1], err)
			}
			if off+int64(len(bin)) > int64(len(img)) {
				return nil, fmt.Errorf("%s at %#x does not fit in %d MB of flash", w[i+1], off, sizeMB)
			}
			copy(img[off:], bin)
			n++
			i++
		}
	}
	if n == 0 {
		return nil, errors.New("flash_args lists no binaries")
	}
	return img, sc.Err()
}

// PadFlash makes a firmware file (a merged image, from offset 0) the size of the board's flash it fits in,
// erased (0xFF) after it.
func PadFlash(bin []byte, b *Board) ([]byte, int, error) {
	mb, err := b.FlashSize(int64(len(bin)))
	if err != nil {
		return nil, 0, err
	}
	if len(bin) == mb<<20 {
		return bin, mb, nil
	}
	return append(append([]byte{}, bin...), bytes.Repeat([]byte{0xFF}, mb<<20-len(bin))...), mb, nil
}

func containsInt(l []int, n int) bool {
	for _, x := range l {
		if x == n {
			return true
		}
	}
	return false
}
