package fileserve

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTest(t *testing.T, write bool) (*Manager, string, *recorder) {
	t.Helper()
	dir := t.TempDir()
	m := New()
	rec := &recorder{}
	m.OnTransfer = rec.add
	m.Configure(Config{Folder: dir, Address: "127.0.0.1", TFTPWrite: write, FTPWrite: write, FTPUser: "tw", FTPPassword: "s3cret-pass", Idle: time.Hour})
	t.Cleanup(m.StopAll)
	return m, dir, rec
}

type recorder struct {
	mu sync.Mutex
	l  []Transfer
}

func (r *recorder) add(x Transfer) { r.mu.Lock(); r.l = append(r.l, x); r.mu.Unlock() }
func (r *recorder) final(state string) []Transfer {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Transfer
	for _, x := range r.l {
		if x.State == state {
			out = append(out, x)
		}
	}
	return out
}

func port(t *testing.T, m *Manager, kind string) int {
	for _, s := range m.State().Servers {
		if s.Kind == kind && s.Running {
			return s.Port
		}
	}
	t.Fatalf("%s not running", kind)
	return 0
}

func randBytes(n int) []byte { b := make([]byte, n); rand.Read(b); return b }

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{"a.bin": "a.bin", "/a.bin": "a.bin", "sub/x.cfg": filepath.FromSlash("sub/x.cfg"), `sub\x.cfg`: filepath.FromSlash("sub/x.cfg"), "/": "", "./a": "a"} {
		if got, err := CleanName(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"../x", "/../../etc/passwd", `..\..\x`, "a/../../b", "a\x00b"} {
		if _, err := CleanName(in); err == nil {
			t.Errorf("%q should be refused", in)
		}
	}
}

// --- a tiny TFTP client ---

func tftpReqPkt(op uint16, name string, opts ...string) []byte {
	b := []byte{0, byte(op)}
	b = append(append(b, name...), 0)
	b = append(append(b, "octet"...), 0)
	for _, o := range opts {
		b = append(append(b, o...), 0)
	}
	return b
}

func tftpGet(t *testing.T, p int, name string, opts ...string) ([]byte, map[string]string, error) {
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	srv := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: p}
	c.WriteToUDP(tftpReqPkt(opRRQ, name, opts...), srv)
	blk := 512
	var out bytes.Buffer
	oack := map[string]string{}
	buf := make([]byte, 70000)
	var want uint16 = 1
	for {
		c.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			return nil, nil, err
		}
		switch binary.BigEndian.Uint16(buf) {
		case opERROR:
			return nil, nil, fmt.Errorf("error %d: %s", binary.BigEndian.Uint16(buf[2:]), strings.TrimRight(string(buf[4:n]), "\x00"))
		case opOACK:
			parts := bytes.Split(buf[2:n], []byte{0})
			for i := 0; i+1 < len(parts); i += 2 {
				oack[string(parts[i])] = string(parts[i+1])
			}
			if v, ok := oack["blksize"]; ok {
				blk, _ = strconv.Atoi(v)
			}
			c.WriteToUDP([]byte{0, opACK, 0, 0}, from)
		case opDATA:
			b := binary.BigEndian.Uint16(buf[2:])
			if b == want {
				out.Write(buf[4:n])
				want++
			}
			c.WriteToUDP([]byte{0, opACK, byte(b >> 8), byte(b)}, from)
			if n-4 < blk {
				return out.Bytes(), oack, nil
			}
		}
	}
}

func tftpPut(t *testing.T, p int, name string, data []byte, opts ...string) error {
	c, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	defer c.Close()
	srv := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: p}
	c.WriteToUDP(tftpReqPkt(opWRQ, name, opts...), srv)
	buf := make([]byte, 2048)
	blk := 512
	var block uint16
	var to *net.UDPAddr
	for {
		c.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		to = from
		switch binary.BigEndian.Uint16(buf) {
		case opERROR:
			return fmt.Errorf("error %d: %s", binary.BigEndian.Uint16(buf[2:]), strings.TrimRight(string(buf[4:n]), "\x00"))
		case opOACK:
			if i := bytes.Index(buf[:n], []byte("blksize\x00")); i >= 0 {
				v := bytes.Split(buf[i+8:n], []byte{0})[0]
				blk, _ = strconv.Atoi(string(v))
			}
		case opACK:
			if binary.BigEndian.Uint16(buf[2:]) != block {
				continue
			}
			if int(block)*blk > len(data) {
				return nil
			}
		}
		start := int(block) * blk
		end := min(start+blk, len(data))
		block++
		pkt := append([]byte{0, opDATA, byte(block >> 8), byte(block)}, data[start:end]...)
		c.WriteToUDP(pkt, to)
	}
}

func TestTFTP(t *testing.T) {
	m, dir, rec := newTest(t, false)
	img := randBytes(512*3 + 0) // an exact multiple: ends with an empty block
	os.WriteFile(filepath.Join(dir, "image.xos"), img, 0o644)
	os.MkdirAll(filepath.Join(dir, "lic"), 0o755)
	os.WriteFile(filepath.Join(dir, "lic", "a.xlic"), []byte("licence"), 0o644)
	if err := m.Start(TFTP); err != nil {
		t.Fatal(err)
	}
	p := port(t, m, TFTP)

	got, _, err := tftpGet(t, p, "image.xos")
	if err != nil || !bytes.Equal(got, img) {
		t.Fatalf("plain get: %v (%d bytes)", err, len(got))
	}
	big := randBytes(1_000_000)
	os.WriteFile(filepath.Join(dir, "big.bin"), big, 0o644)
	got, oack, err := tftpGet(t, p, "/big.bin", "blksize", "1428", "tsize", "0")
	if err != nil || !bytes.Equal(got, big) || oack["tsize"] != "1000000" || oack["blksize"] != "1428" {
		t.Fatalf("get with options: %v %v (%d bytes)", err, oack, len(got))
	}
	if got, _, err := tftpGet(t, p, `lic\a.xlic`); err != nil || string(got) != "licence" {
		t.Fatalf("sub folder with backslash: %v %q", err, got)
	}
	if _, _, err := tftpGet(t, p, "../../etc/passwd"); err == nil || !strings.Contains(err.Error(), "error 2") {
		t.Fatalf("traversal: %v", err)
	}
	if _, _, err := tftpGet(t, p, "missing.bin"); err == nil || !strings.Contains(err.Error(), "error 1") {
		t.Fatalf("missing: %v", err)
	}
	if err := tftpPut(t, p, "cfg.txt", []byte("x")); err == nil || !strings.Contains(err.Error(), "uploads are turned off") {
		t.Fatalf("put with uploads off: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cfg.txt")); err == nil {
		t.Fatal("file written with uploads off")
	}
	// uploads on (read at every request)
	c := m.config()
	c.TFTPWrite = true
	m.Configure(c)
	cfg := randBytes(70_000)
	if err := tftpPut(t, p, "sw1.cfg", cfg, "blksize", "1024", "tsize", "70000"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if b, _ := os.ReadFile(filepath.Join(dir, "sw1.cfg")); !bytes.Equal(b, cfg) {
		t.Fatalf("uploaded %d bytes", len(b))
	}
	if len(rec.final("done")) != 4 || len(rec.final("refused")) != 3 {
		t.Fatalf("log: %d done, %d refused", len(rec.final("done")), len(rec.final("refused")))
	}
	if tr := m.Transfers(); tr[0].File != "sw1.cfg" || !tr[0].Put || tr[0].Bytes != 70000 {
		t.Fatalf("newest transfer: %+v", tr[0])
	}
}

// --- FTP ---

func ftpLogin(t *testing.T, p int, user, pass string) (*textproto.Conn, int, string) {
	c, err := textproto.Dial("tcp", "127.0.0.1:"+strconv.Itoa(p))
	if err != nil {
		t.Fatal(err)
	}
	c.ReadResponse(220)
	c.Cmd("USER %s", user)
	c.ReadResponse(331)
	c.Cmd("PASS %s", pass)
	code, msg, _ := c.ReadResponse(0)
	return c, code, msg
}

func ftpPasv(t *testing.T, c *textproto.Conn) net.Conn {
	c.Cmd("EPSV")
	_, msg, err := c.ReadResponse(229)
	if err != nil {
		t.Fatal(err)
	}
	p := msg[strings.Index(msg, "|||")+3 : strings.LastIndex(msg, "|")]
	d, err := net.Dial("tcp", "127.0.0.1:"+p)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestFTP(t *testing.T) {
	m, dir, _ := newTest(t, false)
	img := randBytes(300_000)
	os.WriteFile(filepath.Join(dir, "fw.bin"), img, 0o644)
	if err := m.Start(FTP); err != nil {
		t.Fatal(err)
	}
	p := port(t, m, FTP)
	if c, code, _ := ftpLogin(t, p, "tw", "wrong"); code != 530 {
		t.Fatalf("wrong password: %d", code)
	} else {
		c.Cmd("LIST")
		if code, _, _ := c.ReadResponse(0); code != 530 {
			t.Fatalf("LIST before login: %d", code)
		}
		c.Close()
	}
	c, code, _ := ftpLogin(t, p, "tw", "s3cret-pass")
	if code != 230 {
		t.Fatalf("login: %d", code)
	}
	defer c.Close()
	c.Cmd("SIZE fw.bin")
	if _, msg, _ := c.ReadResponse(213); msg != "300000" {
		t.Fatalf("SIZE %q", msg)
	}
	d := ftpPasv(t, c)
	c.Cmd("RETR fw.bin")
	c.ReadResponse(150)
	got, _ := io.ReadAll(d)
	if _, _, err := c.ReadResponse(226); err != nil || !bytes.Equal(got, img) {
		t.Fatalf("RETR: %v %d", err, len(got))
	}
	// resume
	d = ftpPasv(t, c)
	c.Cmd("REST 299000")
	c.ReadResponse(350)
	c.Cmd("RETR fw.bin")
	c.ReadResponse(150)
	got, _ = io.ReadAll(d)
	c.ReadResponse(226)
	if !bytes.Equal(got, img[299000:]) {
		t.Fatalf("REST: %d bytes", len(got))
	}
	d = ftpPasv(t, c)
	c.Cmd("LIST -la")
	c.ReadResponse(150)
	ls, _ := io.ReadAll(d)
	c.ReadResponse(226)
	if !strings.Contains(string(ls), "300000") || !strings.Contains(string(ls), "fw.bin") {
		t.Fatalf("LIST: %q", ls)
	}
	c.Cmd("RETR ../../etc/passwd")
	if code, _, _ := c.ReadResponse(0); code != 550 {
		t.Fatalf("traversal: %d", code)
	}
	c.Cmd("CWD ../..")
	c.ReadResponse(0)
	c.Cmd("PWD")
	if _, msg, _ := c.ReadResponse(257); !strings.HasPrefix(msg, `"/"`) {
		t.Fatalf("CWD above the folder: %q", msg)
	}
	c.Cmd("STOR up.cfg")
	if code, _, _ := c.ReadResponse(0); code != 553 {
		t.Fatalf("STOR with uploads off: %d", code)
	}
	// active mode to another address is refused (no FTP bounce)
	c.Cmd("PORT 10,1,2,3,4,5")
	if code, _, _ := c.ReadResponse(0); code != 504 {
		t.Fatalf("PORT to another host: %d", code)
	}
	// active mode to ourselves, with uploads on
	cf := m.config()
	cf.FTPWrite = true
	m.Configure(cf)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	ap := ln.Addr().(*net.TCPAddr).Port
	c.Cmd("PORT 127,0,0,1,%d,%d", ap>>8, ap&255)
	c.ReadResponse(200)
	c.Cmd("STOR up.cfg")
	c.ReadResponse(150)
	dc, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	dc.Write([]byte("config text\n"))
	dc.Close()
	ln.Close()
	if _, _, err := c.ReadResponse(226); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "up.cfg")); string(b) != "config text\n" {
		t.Fatalf("uploaded %q", b)
	}
	c.Cmd("DELE fw.bin")
	if code, _, _ := c.ReadResponse(0); code != 550 {
		t.Fatalf("DELE: %d", code)
	}
}

// --- HTTP ---

func TestHTTP(t *testing.T) {
	m, dir, rec := newTest(t, true)
	img := randBytes(200_000)
	os.WriteFile(filepath.Join(dir, "fw.bin"), img, 0o644)
	os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.txt"), []byte("no"), 0o644)
	if err := m.Start(HTTP); err != nil {
		t.Fatal(err)
	}
	base := "http://127.0.0.1:" + strconv.Itoa(port(t, m, HTTP))
	r, err := http.Get(base + "/fw.bin")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if !bytes.Equal(b, img) {
		t.Fatalf("GET %d bytes", len(b))
	}
	req, _ := http.NewRequest("GET", base+"/fw.bin", nil)
	req.Header.Set("Range", "bytes=100-199")
	r, _ = http.DefaultClient.Do(req)
	b, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 206 || !bytes.Equal(b, img[100:200]) {
		t.Fatalf("range: %d %d", r.StatusCode, len(b))
	}
	r, _ = http.Post(base+"/x.cfg", "text/plain", strings.NewReader("x"))
	if r.StatusCode != 405 {
		t.Fatalf("POST: %d", r.StatusCode)
	}
	// a raw request, so no client cleans the path first
	conn, _ := net.Dial("tcp", strings.TrimPrefix(base, "http://"))
	fmt.Fprint(conn, "GET /../secret.txt HTTP/1.0\r\n\r\n")
	resp, _ := http.ReadResponse(bufio.NewReader(conn), nil)
	conn.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("traversal: %d", resp.StatusCode)
	}
	r, _ = http.Get(base + "/")
	b, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(b), "fw.bin") {
		t.Fatal("folder list")
	}
	if len(rec.final("done")) != 2 || len(rec.final("refused")) != 2 {
		t.Fatalf("log: %d done, %d refused", len(rec.final("done")), len(rec.final("refused")))
	}
}

func TestIdleStopAndErrors(t *testing.T) {
	old := idleTick
	idleTick = 20 * time.Millisecond
	defer func() { idleTick = old }()
	m, _, _ := newTest(t, false)
	c := m.config()
	c.Idle = 50 * time.Millisecond
	m.Configure(c)
	var states []State
	var mu sync.Mutex
	m.OnState = func(s State) { mu.Lock(); states = append(states, s); mu.Unlock() }
	if err := m.Start(HTTP); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if m.Running() || !strings.Contains(m.State().Stopped, "without transfers") {
		t.Fatalf("not stopped on its own: %+v", m.State())
	}
	// a port in use gives a plain message
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	c.HTTPPort = ln.Addr().(*net.TCPAddr).Port
	m.Configure(c)
	if err := m.Start(HTTP); err == nil || !strings.Contains(err.Error(), "used by another program") {
		t.Fatalf("port in use: %v", err)
	}
	if s := m.State().Servers[2]; s.Error == "" || s.Running {
		t.Fatalf("state after the error: %+v", s)
	}
	c.FTPPassword = ""
	m.Configure(c)
	if err := m.Start(FTP); err == nil {
		t.Fatal("FTP without a password started")
	}
}

// TestRealClients uses curl (when it has TFTP and FTP) as a device would.
func TestRealClients(t *testing.T) {
	out, err := exec.Command("curl", "-V").Output()
	if err != nil || !strings.Contains(string(out), "tftp") {
		t.Skip("curl with TFTP not found")
	}
	m, dir, _ := newTest(t, true)
	img := randBytes(3_000_000)
	os.WriteFile(filepath.Join(dir, "summitX.xos"), img, 0o644)
	for _, k := range []string{TFTP, FTP, HTTP} {
		if err := m.Start(k); err != nil {
			t.Fatal(err)
		}
	}
	up := filepath.Join(t.TempDir(), "sw.cfg")
	os.WriteFile(up, []byte("configure snmp sysName sw1\n"), 0o644)
	tp, fp, hp := port(t, m, TFTP), port(t, m, FTP), port(t, m, HTTP)
	run := func(name string, args ...string) []byte {
		t.Helper()
		b, err := exec.Command("curl", append([]string{"-sS", "--max-time", "60"}, args...)...).Output()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return b
	}
	cases := map[string][]string{
		"tftp":         {fmt.Sprintf("tftp://127.0.0.1:%d/summitX.xos", tp)},
		"tftp blksize": {"--tftp-blksize", "1468", fmt.Sprintf("tftp://127.0.0.1:%d/summitX.xos", tp)},
		"ftp passive":  {fmt.Sprintf("ftp://tw:s3cret-pass@127.0.0.1:%d/summitX.xos", fp)},
		"ftp active":   {"-P", "127.0.0.1", fmt.Sprintf("ftp://tw:s3cret-pass@127.0.0.1:%d/summitX.xos", fp)},
		"http":         {fmt.Sprintf("http://127.0.0.1:%d/summitX.xos", hp)},
	}
	for name, args := range cases {
		if b := run(name, args...); !bytes.Equal(b, img) {
			t.Errorf("%s: %d bytes", name, len(b))
		}
	}
	run("tftp put", "-T", up, fmt.Sprintf("tftp://127.0.0.1:%d/tftp-sw.cfg", tp))
	run("ftp put", "-T", up, fmt.Sprintf("ftp://tw:s3cret-pass@127.0.0.1:%d/ftp-sw.cfg", fp))
	for _, n := range []string{"tftp-sw.cfg", "ftp-sw.cfg"} {
		if b, _ := os.ReadFile(filepath.Join(dir, n)); string(b) != "configure snmp sysName sw1\n" {
			t.Errorf("%s: %q", n, b)
		}
	}
}
