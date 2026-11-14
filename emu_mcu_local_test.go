package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/session"
)

func emuTestApp(t *testing.T) *App {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	emuLab.loaded, emuLab.f = false, emuFile{}
	t.Cleanup(func() { emuLab.loaded, emuLab.f = false, emuFile{} })
	return &App{cfg: cfg, store: st}
}

// MicroPython comes from micropython.org once: the newest release (not a preview), padded to the flash its header asks for.
func TestEmuSampleFirmware(t *testing.T) {
	a := emuTestApp(t)
	var gets atomic.Int32
	fw := append([]byte{0xE9, 3, 2, 0x3F}, make([]byte, 1<<20)...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		switch r.URL.Path {
		case "/download/ESP32_GENERIC_S3/":
			fmt.Fprint(w, `<a href="/resources/firmware/ESP32_GENERIC_S3-20250101-v1.24.1.bin">old</a>
<a href="/resources/firmware/ESP32_GENERIC_S3-20251101-v1.27.0-preview.5.gabc.bin">preview</a>
<a href="/resources/firmware/ESP32_GENERIC_S3-20250415-v1.25.0.bin">new</a>`)
		case "/resources/firmware/ESP32_GENERIC_S3-20250415-v1.25.0.bin":
			_, _ = w.Write(fw)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := emuMicroPythonURL
	emuMicroPythonURL = srv.URL + "/download/"
	defer func() { emuMicroPythonURL = old }()
	v, err := a.EmuSampleFirmware("esp32s3")
	if err != nil {
		t.Fatal(err)
	}
	var im *EmuImageView
	for i := range v.Images {
		if v.Images[i].Board == "esp32s3" {
			im = &v.Images[i]
		}
	}
	if im == nil || im.Name != "MicroPython 1.25.0" || im.FlashMB != 8 || im.NICs != 2 || im.Source != "firmware ESP32_GENERIC_S3-20250415-v1.25.0.bin" {
		t.Fatalf("image: %+v", im)
	}
	n := gets.Load()
	if _, err := a.EmuSampleFirmware("esp32s3"); err != nil || gets.Load() != n {
		t.Fatalf("the second time it is there: %v, %d requests", err, gets.Load()-n)
	}
	if _, err := a.EmuSampleFirmware("esp32h2"); err == nil || !strings.Contains(err.Error(), "no build") {
		t.Fatalf("h2: %v", err)
	}
	if _, err := a.EmuSampleFirmware("esp32c3"); err == nil { // the page is not there
		t.Fatal("c3 without a page")
	}
}

// Espressif's QEMU for this computer: the release's builds for its system, checked and unpacked once.
func TestEmuMCUToolsLocal(t *testing.T) {
	triple := map[string]string{"linux/amd64": "x86_64-linux-gnu", "linux/arm64": "aarch64-linux-gnu", "darwin/arm64": "aarch64-apple-darwin", "darwin/amd64": "x86_64-apple-darwin"}[runtime.GOOS+"/"+runtime.GOARCH]
	if triple == "" {
		t.Skip("no build for this system")
	}
	a := emuTestApp(t)
	t.Setenv("HOME", t.TempDir())
	tgz := func(arch string) []byte {
		var b strings.Builder
		gz := gzip.NewWriter(&b)
		tw := tar.NewWriter(gz)
		body := "#!/bin/sh\necho " + arch + "\n"
		_ = tw.WriteHeader(&tar.Header{Name: "qemu/bin/qemu-system-" + arch, Mode: 0o755, Size: int64(len(body))})
		_, _ = tw.Write([]byte(body))
		tw.Close()
		gz.Close()
		return []byte(b.String())
	}
	files := map[string][]byte{"xtensa": tgz("xtensa"), "riscv32": tgz("riscv32")}
	bad := false
	var gets atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		if r.URL.Path == "/latest" {
			var as []map[string]string
			for arch, b := range files {
				sum := sha256.Sum256(b)
				d := hex.EncodeToString(sum[:])
				if bad {
					d = strings.Repeat("0", 64)
				}
				as = append(as, map[string]string{"name": "qemu-" + arch + "-softmmu-esp_develop_9.2.2_20261109-" + triple + ".tar.gz", "browser_download_url": srv.URL + "/dl/" + arch + ".tar.gz", "digest": "sha256:" + d})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "esp-develop-9.2.2-20261109", "assets": as})
			return
		}
		_, _ = w.Write(files[strings.TrimSuffix(filepath.Base(r.URL.Path), ".tar.gz")])
	}))
	defer srv.Close()
	old := emuMCUReleaseURL
	emuMCUReleaseURL = srv.URL + "/latest"
	defer func() { emuMCUReleaseURL = old }()
	bad = true
	if err := a.emuMCUToolsLocal("termwright-lab"); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("a wrong checksum: %v", err)
	}
	bad = false
	if err := a.emuMCUToolsLocal("termwright-lab"); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	q := filepath.Join(home, "termwright-lab/tools/qemu-esp/current/bin/qemu-system-riscv32")
	if st, err := os.Stat(q); err != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("not unpacked: %v", err)
	}
	if l, _ := os.Readlink(filepath.Join(home, "termwright-lab/tools/qemu-esp/current")); l != "esp-develop-9.2.2-20261109" {
		t.Fatalf("current -> %q", l)
	}
	n := gets.Load()
	if err := a.emuMCUToolsLocal("termwright-lab"); err != nil || gets.Load() != n {
		t.Fatalf("the second time it is there: %v", err)
	}
}

// This computer as a host: checked without a session or a login, and for microcontrollers only.
func TestEmuLocalHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("macOS and Linux")
	}
	a := emuTestApp(t)
	t.Setenv("HOME", t.TempDir())
	defer func() {
		if c := emuLab.conns[emu.LocalHost]; c != nil {
			c.close()
			delete(emuLab.conns, emu.LocalHost)
		}
	}()
	h, err := a.EmuCheckHost(emu.LocalHost)
	if err != nil {
		t.Fatal(err)
	}
	if h.Name != "This computer" || h.Info.User == "" || !h.Info.OK() || h.Info.CPUs == 0 || h.Info.MemTotal == 0 {
		t.Fatalf("info: %+v", h)
	}
	emuLab.mu.Lock()
	emuLab.f.Images = []emu.Image{{ID: "sw", Name: "switch", NICs: 4}}
	emuLab.mu.Unlock()
	if _, err := a.EmuAdd(EmuRequest{Host: emu.LocalHost, Groups: []EmuGroup{{Image: "sw", Count: 1}}}); !errors.Is(err, errLocalMCU) {
		t.Fatalf("a switch on this computer: %v", err)
	}
	if _, err := a.EmuAddComputer(emu.LocalHost, false); !errors.Is(err, errLocalMCU) {
		t.Fatalf("a computer on this computer: %v", err)
	}
	if _, err := a.EmuSetupHost(emu.LocalHost, ""); err == nil {
		t.Fatal("set up of this computer")
	}
}

// fakeEspQemu stands in for Espressif's QEMU: it stays in the background (its pid in the pid file) and serves a
// MicroPython banner on the console port, as the firmware's UART would.
const fakeEspQemu = `#!/bin/sh
pid= port= all="$*"
while [ $# -gt 0 ]; do
  case "$1" in
    -pidfile) pid=$2; shift ;;
    -chardev) port=$(echo "$2" | sed -n 's/.*port=\([0-9]*\).*/\1/p'); shift ;;
  esac
  shift
done
echo "$all" > "$pid.args"
( python3 -c 'import socket,sys,threading
s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1); s.bind(("127.0.0.1",int(sys.argv[1]))); s.listen(4)
def h(c):
  c.sendall(b"MicroPython v1.25.0 on 2025-04-15; Generic ESP32S3 module with ESP32S3\r\n>>> "); buf=b""
  while True:
    d=c.recv(1024)
    if not d: return
    for ch in d:
      if ch==3: c.sendall(b"\r\nKeyboardInterrupt: \r\n>>> ")
      elif ch==13:
        line=buf.decode(); buf=b""; out=""
        if line.startswith("print(") and line.endswith(")"): out=str(eval(line[6:-1]))+"\r\n"
        c.sendall((line+"\r\n"+out+">>> ").encode())
      elif 32<=ch<127: buf+=bytes([ch])
while True:
  c,_=s.accept(); threading.Thread(target=h,args=(c,),daemon=True).start()' "$port" & while :; do sleep 1; done ) >/dev/null 2>&1 &
echo $! > "$pid"
`

// A microcontroller on this computer: its firmware copied into the lab folder in the home folder, Espressif's
// QEMU started there, its console reached without a jump host.
func TestEmuLocalMCURuns(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the fake QEMU is for Linux")
	}
	a := emuTestApp(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	defer func() {
		_ = exec.Command("pkill", "-f", filepath.Join(home, "termwright-lab")).Run()
		if c := emuLab.conns[emu.LocalHost]; c != nil {
			c.close()
			delete(emuLab.conns, emu.LocalHost)
		}
	}()
	bin := filepath.Join(home, "termwright-lab/tools/qemu-esp/current/bin")
	_ = os.MkdirAll(bin, 0o755)
	for _, arch := range []string{"xtensa", "riscv32"} {
		_ = os.WriteFile(filepath.Join(bin, "qemu-system-"+arch), []byte(fakeEspQemu), 0o755)
	}
	fw := filepath.Join(t.TempDir(), "fw.bin")
	_ = os.WriteFile(fw, append([]byte{0xE9, 3, 2, 0x2F}, make([]byte, 1000)...), 0o644)
	v, err := a.EmuAddFirmware("esp32s3", fw, "fw", 0)
	if err != nil {
		t.Fatal(err)
	}
	imgID := v.Images[0].ID
	if _, err := a.EmuAdd(EmuRequest{Host: emu.LocalHost, Groups: []EmuGroup{{Image: imgID, Count: 1, Names: "esp-{n}"}}, Cabling: "none"}); err != nil {
		t.Fatal(err)
	}
	emuLab.mu.Lock()
	id := emuLab.f.Lab.VMs[0].ID
	l := a.emuFor(emu.LocalHost)
	emuLab.mu.Unlock()
	a.emuStart(emu.LocalHost, []string{id})
	if e := emuLab.errs[id]; e != "" {
		t.Fatalf("start: %s", e)
	}
	args, err := os.ReadFile(filepath.Join(home, l.VMDir(), id+".pid.args"))
	if err != nil || !strings.Contains(string(args), "-machine esp32s3") || !strings.Contains(string(args), "model=open_eth") {
		t.Fatalf("args %q %v", args, err)
	}
	if b, err := os.ReadFile(filepath.Join(home, l.VMDir(), id+".flash.bin")); err != nil || len(b) != 4<<20 {
		t.Fatalf("its flash: %d %v", len(b), err)
	}
	up := a.emuPollFresh().Up
	if _, ok := up[id]; !ok {
		t.Fatalf("not up: %+v", up)
	}
	cl, err := a.emuClient(context.Background(), emu.LocalHost)
	if err != nil {
		t.Fatal(err)
	}
	var c net.Conn
	for i := 0; i < 50; i++ {
		if c, err = cl.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(l.ConsolePort(emuLab.f.Lab.VMs[0])))); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 200)
	n, _ := c.Read(buf)
	c.Close()
	if !strings.Contains(string(buf[:n]), "MicroPython") {
		t.Fatalf("console %q", buf[:n])
	}
	// the connector: list, and Python on its console
	out, err := a.emuMCP("lab_microcontrollers", map[string]any{"action": "list"})
	if err != nil || !strings.Contains(out, "This computer is a host") || !strings.Contains(out, "esp-1 | ESP32S3 | fw |") {
		t.Fatalf("list: %v\n%s", err, out)
	}
	out, err = a.emuMCP("lab_console", map[string]any{"switch": "esp-1", "commands": []any{"print(6*7)", "x = 1"}, "timeout": 30})
	if err != nil || !strings.Contains(out, "print(6*7)\n42\n") || !strings.Contains(out, "x = 1\n") {
		t.Fatalf("console: %v\n%q", err, out)
	}
	if out, err := a.emuMCP("lab_microcontrollers", map[string]any{"action": "add", "board": "ESP32-S3", "names": "esp-b"}); err != nil || out != "Added on this computer: esp-b - not started (lab_power start)" {
		t.Fatalf("add: %v %q", err, out)
	}
	if _, err := a.emuMCP("lab_microcontrollers", map[string]any{"action": "add", "board": "h2"}); err == nil || !strings.Contains(err.Error(), "MicroPython has none") {
		t.Fatalf("h2 without a firmware: %v", err)
	}
	if _, err := a.EmuStop([]string{id}); err != nil {
		t.Fatal(err)
	}
}
