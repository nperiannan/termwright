package main

import (
	"archive/zip"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/sim"
)

// demoVM zips the engine's demo pack into a .vm file.
func demoVM(t *testing.T, dir string) string {
	t.Helper()
	out := filepath.Join(dir, "demo.vm")
	f, _ := os.Create(out)
	zw := zip.NewWriter(f)
	src := "internal/sim/testdata/demo"
	_ = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		w, _ := zw.Create(filepath.ToSlash(rel))
		b, _ := os.ReadFile(p)
		_, _ = w.Write(b)
		return nil
	})
	zw.Close()
	f.Close()
	return out
}

func resetLab() { simLab.lab, simLab.key, simLab.meta, simLab.pack = nil, nil, labFile{}, nil }

func TestLabPackConsentAddStartReload(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	resetLab()
	defer func() { a.labShutdown(); resetLab() }()

	if _, err := a.LabAdd(LabRequest{Groups: []sim.Group{{Model: "D100-24T-4X", Count: 1}}}); err == nil || !strings.Contains(err.Error(), "device pack") {
		t.Fatalf("added without a pack: %v", err)
	}
	vm := demoVM(t, dir)
	info, err := a.LabPackInfo(vm)
	if err != nil || info.ID != "demo" || info.Hash == "" || info.Models != 2 {
		t.Fatalf("info %+v %v", info, err)
	}
	if _, err := a.LabPackAccept(vm, "wrong"); err == nil {
		t.Fatal("accepted without the shown hash")
	}
	v, err := a.LabPackAccept(vm, info.Hash)
	if err != nil || v.Pack == nil || v.Pack.Accepted == "" || len(v.Models) != 2 || v.Models[0].Layout != "24 1G copper, 4 10G fiber" {
		t.Fatalf("accept %+v %v", v, err)
	}
	v, err = a.LabAdd(LabRequest{Groups: []sim.Group{{Model: "D100-24T-4X", Count: 2, Names: "sw{n}"}}, Cabling: sim.CableChain, Start: true})
	if err != nil || len(v.Devices) != 2 || len(v.Links) != 1 || !v.Running {
		t.Fatalf("add %v %+v", err, v)
	}
	s, err := st.Get("Simulated Lab/sw1")
	if err != nil || s.Hostname != "127.0.0.1" || s.DeviceType != "Demo" {
		t.Fatalf("session %+v %v", s, err)
	}
	cb, _ := knownhosts.New(filepath.Join(dir, "known_hosts"))
	c, err := ssh.Dial("tcp", "127.0.0.1:"+strconv.Itoa(s.Port), &ssh.ClientConfig{User: "admin", Auth: []ssh.AuthMethod{ssh.Password("x")}, HostKeyCallback: cb})
	if err != nil {
		t.Fatal(err)
	}
	ss, _ := c.NewSession()
	out, _ := ss.Output("show system")
	c.Close()
	if !strings.Contains(string(out), "Name    : sw1") {
		t.Fatalf("show system: %s", out)
	}
	// a deleted session comes back when the lab starts; serials in the pack's format
	if err := st.Delete("Simulated Lab/sw1"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.LabStart(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(v.Sessions[v.Devices[0].ID]); err != nil {
		t.Fatalf("session not made again: %v", err)
	}
	if v2, err := a.LabNewSerials(nil); err != nil || !strings.HasPrefix(v2.Devices[0].Serial, "DM") || v2.Devices[0].Serial == v.Devices[0].Serial {
		t.Fatalf("new serials %v %+v", err, v2.Devices)
	}
	// serial numbers, outputs
	if _, err := a.LabSetSerials([]string{v.Devices[0].ID, v.Devices[1].ID}, []string{"A1", "A1"}); err == nil {
		t.Fatal("duplicate serials")
	}
	if _, err := a.LabSetOutputs([]sim.Output{{OS: "demo", Command: "show  thing", Text: "Serial {serial}"}}); err != nil {
		t.Fatal(err)
	}
	// a new Termwright start reads it back; the pack file is checked
	resetLab()
	v, err = a.LabState()
	if err != nil || len(v.Devices) != 2 || v.Pack == nil || v.Pack.Error != "" || len(v.Outputs) != 1 {
		t.Fatalf("reload %v %+v", err, v)
	}
	if out := simLab.lab.Show(v.Devices[0].ID, "show thing"); !strings.Contains(out, "Serial ") {
		t.Fatalf("output %q", out)
	}
	// a changed pack file is not loaded, and the switches are kept for it
	_ = os.WriteFile(filepath.Join(dir, "lab", "packs", v.Pack.File), []byte("changed"), 0o600)
	resetLab()
	v, _ = a.LabState()
	if v.Pack == nil || v.Pack.Error == "" || len(v.Devices) != 0 || v.Waiting != 2 {
		t.Fatalf("changed pack %+v", v)
	}
	_, _ = a.LabStop(nil)
	resetLab()
	if v, _ = a.LabState(); v.Waiting != 2 {
		t.Fatalf("switches lost while the pack was missing: %+v", v)
	}
	if _, err := a.LabPackRemove(); err == nil {
		t.Fatal("pack removed with switches")
	}
	// taking the pack again brings the switches back
	if v, err = a.LabPackAccept(vm, info.Hash); err != nil || len(v.Devices) != 2 {
		t.Fatalf("again %v %+v", err, v)
	}
	if v, err = a.LabRemove([]string{v.Devices[0].ID}, true); err != nil || len(v.Devices) != 1 {
		t.Fatalf("remove %v", err)
	}
	if _, err := st.Get("Simulated Lab/sw1"); err == nil {
		t.Fatal("session kept")
	}
}
