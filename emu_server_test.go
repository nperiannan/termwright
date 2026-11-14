package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/sftp"
	"github.com/zalando/go-keyring"
	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/localssh"
	"github.com/termwright/termwright/internal/secrets"
)

// fakeDropbear is a server's SSH as Termwright uses it: the user's password, sftp with / mapped to root, and
// commands (the status script) run with sh.
func fakeDropbear(t *testing.T, ln net.Listener, user, pw, root string) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(key)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if c.User() == user && string(p) == pw {
			return nil, nil
		}
		return nil, io.EOF
	}}
	cfg.AddHostKey(signer)
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			_, chans, reqs, err := ssh.NewServerConn(c, cfg)
			if err != nil {
				return
			}
			go ssh.DiscardRequests(reqs)
			for nc := range chans {
				ch, rq, err := nc.Accept()
				if err != nil {
					continue
				}
				go func() {
					for r := range rq {
						switch r.Type {
						case "subsystem":
							_ = r.Reply(true, nil)
							go func() {
								// the server's /srv is root/srv here
								srv := sftp.NewRequestServer(ch, sftp.Handlers{FileGet: fsRoot(root), FilePut: fsRoot(root), FileCmd: fsRoot(root), FileList: fsRoot(root)})
								_ = srv.Serve()
								ch.Close()
							}()
						case "exec":
							var p struct{ Cmd string }
							_ = ssh.Unmarshal(r.Payload, &p)
							_ = r.Reply(true, nil)
							out, _ := exec.Command("sh", "-c", p.Cmd).CombinedOutput()
							_, _ = ch.Write(out)
							_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{0}))
							ch.Close()
						default:
							_ = r.Reply(false, nil)
						}
					}
				}()
			}
		}()
	}
}

// fsRoot serves a folder as the root of an sftp server.
type fsRoot string

func (f fsRoot) p(r *sftp.Request) string { return filepath.Join(string(f), filepath.FromSlash(r.Filepath)) }
func (f fsRoot) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	return os.Open(f.p(r))
}
func (f fsRoot) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	return os.OpenFile(f.p(r), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
}
func (f fsRoot) Filecmd(r *sftp.Request) error {
	switch r.Method {
	case "Remove":
		return os.Remove(f.p(r))
	case "Rmdir":
		return os.Remove(f.p(r))
	case "Mkdir":
		return os.Mkdir(f.p(r), 0o755)
	case "Setstat":
		return nil
	}
	return sftp.ErrSSHFxOpUnsupported
}

type listAt []os.FileInfo

func (l listAt) ListAt(out []os.FileInfo, off int64) (int, error) {
	if off >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(out, l[off:])
	return n, nil
}

func (f fsRoot) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch r.Method {
	case "List":
		es, err := os.ReadDir(f.p(r))
		if err != nil {
			return nil, err
		}
		var l listAt
		for _, e := range es {
			if fi, err := e.Info(); err == nil {
				l = append(l, fi)
			}
		}
		return l, nil
	case "Stat", "Lstat":
		fi, err := os.Stat(f.p(r))
		if err != nil {
			return nil, err
		}
		return listAt{fi}, nil
	}
	return nil, sftp.ErrSSHFxOpUnsupported
}

// A server: added with its address and settings, its files copied in and out over SSH/SFTP through its host,
// deleted, its status and leases read; settings checked before they are kept.
func TestEmuServer(t *testing.T) {
	keyring.MockInit()
	a := emuTestApp(t)
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "srv"), 0o755)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	// the KVM host is this computer (the SSH inside Termwright): the server's SSH is forwarded to a port on it
	cl, err := localssh.Dial(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if emuLab.conns == nil {
		emuLab.conns = map[string]*emuConn{}
	}
	if emuLab.ready == nil {
		emuLab.ready = map[string]bool{}
	}
	emuLab.conns["kvm"] = &emuConn{c: cl, close: func() { cl.Close() }}
	defer delete(emuLab.conns, "kvm")
	s := emu.ServerDefaults("10.0.0.10/24")
	emuLab.mu.Lock()
	emuLab.loaded = true
	emuLab.f.Images = []emu.Image{{ID: "alp", Name: "Alpine Linux Virt", PortAddr: []string{"ip addr add {addr} dev {port}"}}}
	emuLab.f.Lab = &emu.Lab{ID: "t1", Dir: "termwright-lab", Mgmt: "forward", Base: port - 4, VMs: []emu.VM{{ID: "s1", Name: "srv-1", Index: 1, Image: "alp", Server: &s, Addrs: map[int]string{1: "10.0.0.10/24"}}}}
	emuLab.f.Host = "kvm"
	emuLab.mu.Unlock()
	_ = secrets.Set(emuServerKey("s1"), "pw1")
	go fakeDropbear(t, ln, "lab", "pw1", root)

	// a file from this computer in, into a folder, back out, deleted
	src := filepath.Join(t.TempDir(), "summit.xos")
	_ = os.WriteFile(src, []byte("an EXOS image"), 0o644)
	if _, err := a.EmuServerMkdir("s1", "images"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EmuServerUpload("s1", []string{src}, "images"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "srv/images/summit.xos")); string(b) != "an EXOS image" {
		t.Fatalf("uploaded %q", b)
	}
	back := filepath.Join(t.TempDir(), "back.xos")
	if err := a.EmuServerDownload("s1", "images/summit.xos", back); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(back); string(b) != "an EXOS image" {
		t.Fatalf("downloaded %q", b)
	}
	if _, err := a.EmuServerUpload("s1", []string{src}, "../etc"); err == nil {
		t.Fatal("outside the shared folder")
	}
	if _, err := a.EmuServerDelete("s1", []string{"/"}); err == nil {
		t.Fatal("the shared folder itself")
	}
	// its state while it runs: the files and what the status script prints (sh here)
	emuLab.mu.Lock()
	emuLab.status.Up = map[string]float64{"s1": 1}
	emuLab.ready["s1"] = true
	emuLab.mu.Unlock()
	sv, err := a.EmuServerState("s1")
	if err != nil || sv.Error != "" {
		t.Fatalf("%v %s", err, sv.Error)
	}
	if len(sv.Files) != 2 || sv.Files[0].Path != "images" || !sv.Files[0].Dir || sv.Files[1].Path != "images/summit.xos" || sv.Files[1].Size != 13 || sv.Addr != "10.0.0.10/24" {
		t.Fatalf("state %+v", sv)
	}
	if _, err := a.EmuServerDelete("s1", []string{"images"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "srv/images")); !os.IsNotExist(err) {
		t.Fatalf("not deleted: %v", err)
	}
	// settings: checked, kept (not running now: nothing typed)
	emuLab.mu.Lock()
	emuLab.status.Up = nil
	emuLab.mu.Unlock()
	bad := s
	bad.From = "10.9.9.9"
	if _, err := a.EmuSetServer("s1", bad); err == nil || !strings.Contains(err.Error(), "not in 10.0.0.0/24") {
		t.Fatalf("bad range: %v", err)
	}
	good := s
	good.TFTP, good.BootFile = false, "summit.xos"
	if _, err := a.EmuSetServer("s1", good); err != nil {
		t.Fatal(err)
	}
	if v := emuLab.f.Lab.VMByID("s1"); v.Server.TFTP || v.Server.BootFile != "summit.xos" {
		t.Fatalf("not kept: %+v", v.Server)
	}
	// the connector
	if out, err := a.emuMCP("lab_server", map[string]any{"action": "set", "server": "srv-1", "tftp": true, "lease_min": float64(30)}); err != nil || !strings.Contains(out, "they apply when it starts") {
		t.Fatalf("lab_server set: %v %q", err, out)
	}
	if v := emuLab.f.Lab.VMByID("s1"); !v.Server.TFTP || v.Server.Lease != 1800 {
		t.Fatalf("lab_server set kept %+v", v.Server)
	}
	if out, err := a.emuMCP("lab_server", map[string]any{"action": "show", "server": "srv-1"}); err != nil || !strings.Contains(out, `"addr": "10.0.0.10/24"`) {
		t.Fatalf("lab_server show: %v %q", err, out)
	}
	if pw, err := a.EmuServerPassword("s1"); err != nil || pw != "pw1" {
		t.Fatalf("password %q %v", pw, err)
	}
}
