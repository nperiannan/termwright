package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func exportFixture(t *testing.T) *Store {
	t.Helper()
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	save := func(s Session) {
		if _, err := st.Save(s, ""); err != nil {
			t.Fatal(err)
		}
	}
	jump := Defaults()
	jump.Name, jump.Folder, jump.Hostname, jump.Username, jump.Port = "jump station", "Cloud", "203.0.113.5", "ops", 2222
	save(jump)
	ssh := Defaults()
	ssh.Name, ssh.Folder, ssh.Hostname, ssh.Username, ssh.Port = "core-sw1", "Datacenter/Core", "10.10.0.1", "admin", 22
	ssh.IdentityFile, ssh.Keepalive, ssh.KeepaliveSec, ssh.AgentForward = "/home/me/.ssh/id_ed25519", true, 30, true
	ssh.JumpSession = "Cloud/jump station"
	ssh.DeviceType = "VOSS"
	ssh.Description = []string{"core A"}
	ssh.PortForwards = []PortForward{{Type: "local", BindPort: 8443, DestHost: "10.10.0.1", DestPort: 443}, {Type: "dynamic", BindPort: 1080}}
	ssh.MAC = "00:04:96:12:34:56"
	save(ssh)
	tel := Defaults()
	tel.Name, tel.Folder, tel.Protocol, tel.Hostname, tel.Port = "legacy router", "Lab", ProtoTelnet, "192.168.50.1", 2003
	save(tel)
	ser := Defaults()
	ser.Name, ser.Folder, ser.Protocol, ser.ComPort, ser.BaudRate, ser.Hostname, ser.Port = "console", "Lab", ProtoSerial, "COM3", 115200, "", 0
	ser.SerialFlow, ser.StopBits, ser.Parity = "rtscts", 2, 2
	save(ser)
	rdp := Defaults()
	rdp.Name, rdp.Protocol, rdp.Hostname, rdp.Port = "dc01", ProtoRDP, "10.30.0.10", 3389
	save(rdp)
	return st
}

func find(list []Session, name string) *Session {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

func TestExportPuTTYRoundTrip(t *testing.T) {
	st := exportFixture(t)
	dir := t.TempDir()
	for _, f := range []string{"putty-reg", "kitty-reg"} {
		dest := filepath.Join(dir, f+".reg")
		rep, err := st.ExportOther(nil, dest, f, nil)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Count != 4 || len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0], "dc01") {
			t.Fatalf("%s: %+v", f, rep)
		}
		b, _ := os.ReadFile(dest)
		if b[0] != 0xFF || b[1] != 0xFE {
			t.Fatal("no UTF-16 BOM")
		}
		list := ParsePuTTYReg(decodeText(b))
		if len(list) != 4 {
			t.Fatalf("%s: re-imported %d", f, len(list))
		}
		c := find(list, "core-sw1")
		if c == nil || c.Folder != "Datacenter/Core" || c.Hostname != "10.10.0.1" || c.Username != "admin" || !c.AgentForward || c.KeepaliveSec != 30 ||
			len(c.PortForwards) != 2 || c.PortForwards[0].DestPort != 443 || c.IdentityFile != "/home/me/.ssh/id_ed25519" {
			t.Fatalf("%s: %+v", f, c)
		}
		s := find(list, "console")
		if s == nil || s.ComPort != "COM3" || s.BaudRate != 115200 || s.SerialFlow != "rtscts" || s.StopBits != 2 || s.Parity != 2 {
			t.Fatalf("%s serial: %+v", f, s)
		}
		if l := find(list, "legacy router"); l == nil || l.Port != 2003 || l.Protocol != ProtoTelnet || l.Folder != "Lab" {
			t.Fatalf("%s telnet: %+v", f, l)
		}
		if !strings.Contains(strings.Join(rep.Notes, "\n"), "jump host written as an SSH proxy") {
			t.Fatalf("notes %v", rep.Notes)
		}
		if f == "putty-reg" && !strings.Contains(decodeText(b), "\"ProxyHost\"=\"203.0.113.5\"") {
			t.Fatal("jump host not written")
		}
	}
}

func TestExportPuTTYDir(t *testing.T) {
	st := exportFixture(t)
	dir := filepath.Join(t.TempDir(), "sessions")
	if _, err := st.ExportOther(nil, dir, "putty-dir", nil); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 4 {
		t.Fatalf("%d files", len(ents))
	}
	b, _ := os.ReadFile(filepath.Join(dir, "Lab%2Flegacy%20router"))
	s, ok := ParsePuTTYFile("Lab%2Flegacy%20router", string(b))
	if !ok || s.Hostname != "192.168.50.1" || s.Port != 2003 || s.Name != "legacy router" || s.Folder != "Lab" {
		t.Fatalf("%+v", s)
	}
}

func TestExportMobaRoundTrip(t *testing.T) {
	st := exportFixture(t)
	dest := filepath.Join(t.TempDir(), "s.mxtsessions")
	rep, err := st.ExportOther(nil, dest, "moba", nil)
	if err != nil || rep.Count != 4 {
		t.Fatal(err, rep)
	}
	b, _ := os.ReadFile(dest)
	list, skipped := ParseMobaXterm(string(b))
	if len(list) != 4 || len(skipped) != 0 {
		t.Fatalf("%d %v\n%s", len(list), skipped, b)
	}
	c := find(list, "core-sw1")
	if c == nil || c.Hostname != "10.10.0.1" || c.Username != "admin" || c.Folder != "Datacenter/Core" || c.IdentityFile == "" ||
		len(c.Description) == 0 || c.Description[0] != "core A" {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(strings.Join(c.Description, " "), "203.0.113.5") {
		t.Fatalf("gateway lost: %v", c.Description)
	}
	if s := find(list, "console"); s == nil || s.ComPort != "COM3" || s.BaudRate != 115200 {
		t.Fatalf("%+v", s)
	}
	if l := find(list, "legacy router"); l == nil || l.Port != 2003 {
		t.Fatalf("%+v", l)
	}
}

func TestExportTabby(t *testing.T) {
	st := exportFixture(t)
	dest := filepath.Join(t.TempDir(), "tabby.yaml")
	rep, err := st.ExportOther(nil, dest, "tabby", nil)
	if err != nil || rep.Count != 4 {
		t.Fatal(err, rep)
	}
	b, _ := os.ReadFile(dest)
	var cfg struct {
		Profiles []struct {
			Type, Name, Group, ID string
			Options               map[string]any
		}
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	var core, jump map[string]any
	for _, p := range cfg.Profiles {
		if p.Name == "core-sw1" {
			core = p.Options
			if p.Group != "Datacenter/Core" || p.Type != "ssh" {
				t.Fatalf("%+v", p)
			}
		}
		if p.Name == "jump station" {
			jump = map[string]any{"id": p.ID}
		}
	}
	if core["host"] != "10.10.0.1" || core["jumpHost"] != jump["id"] || core["forwardedPorts"] == nil {
		t.Fatalf("%v / %v", core, jump)
	}
}

func TestExportSSHConfig(t *testing.T) {
	st := exportFixture(t)
	dest := filepath.Join(t.TempDir(), "config")
	rep, err := st.ExportOther(nil, dest, "ssh-config", nil)
	if err != nil || rep.Count != 2 || len(rep.Skipped) != 3 {
		t.Fatal(err, rep)
	}
	b, _ := os.ReadFile(dest)
	for _, want := range []string{"Host core-sw1\n  HostName 10.10.0.1\n  User admin", "ProxyJump jump-station", "Host jump-station", "Port 2222",
		"LocalForward 8443 10.10.0.1:443", "DynamicForward 1080", "ServerAliveInterval 30", "ForwardAgent yes"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing %q in\n%s", want, b)
		}
	}
}

func TestExportCSVRoundTrip(t *testing.T) {
	st := exportFixture(t)
	dest := filepath.Join(t.TempDir(), "s.csv")
	rep, err := st.ExportOther(nil, dest, "csv", nil)
	if err != nil || rep.Count != 5 {
		t.Fatal(err, rep)
	}
	b, _ := os.ReadFile(dest)
	list, err := ParseCSV(b)
	if err != nil {
		t.Fatal(err)
	}
	c := find(list, "core-sw1")
	if c == nil || c.Folder != "Datacenter/Core" || c.DeviceType != "VOSS" || c.Hostname != "10.10.0.1" ||
		c.MAC != "00:04:96:12:34:56" || c.JumpSession != "Cloud/jump station" {
		t.Fatalf("%+v", c)
	}
	if s := find(list, "console"); s == nil || s.ComPort != "COM3" || s.BaudRate != 115200 {
		t.Fatalf("%+v", s)
	}
	if !strings.Contains(string(b), "00:04:96:12:34:56") {
		t.Fatal("MAC missing")
	}
}

func TestPuttyEscape(t *testing.T) {
	for _, n := range []string{"Lab/legacy router", "a%b*c?", ".hidden", "Ümlaut"} {
		if got := puttyUnescape(puttyEscape(n)); got != n {
			t.Fatalf("%q -> %q", n, got)
		}
	}
}

// Termwright's own formats (folder, .zip, XML) keep everything, Termwright
// keys included: device type, MAC address, environment, recording, smart card…
func TestNativeExportRoundTrip(t *testing.T) {
	st := exportFixture(t)
	c, _ := st.Get("Datacenter/Core/core-sw1")
	c.Environment, c.RecordSession, c.Timestamps = "production", true, true
	if _, err := st.Save(c, "Datacenter/Core/core-sw1"); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"xml", "zip", "folder"} {
		dest := filepath.Join(t.TempDir(), "out")
		if format != "folder" {
			dest += "." + format
		}
		if _, err := st.Export(nil, dest, format); err != nil {
			t.Fatal(format, err)
		}
		other, _ := NewStore(t.TempDir())
		if _, err := other.Import(dest, ImportOptions{}); err != nil {
			t.Fatal(format, err)
		}
		got, err := other.Get("Datacenter/Core/core-sw1")
		if err != nil || got.DeviceType != "VOSS" || got.MAC != "00:04:96:12:34:56" || got.Environment != "production" ||
			!got.RecordSession || !got.Timestamps || got.JumpSession != "Cloud/jump station" || len(got.PortForwards) != 2 {
			t.Fatalf("%s: %+v %v", format, got, err)
		}
	}
}

// Everything added in 0.21 (accounts, Files panel, SFTP opens on, Wake-on-LAN
// options and Wake-on-LAN sessions) survives Termwright's own export formats.
func TestNativeExportKeeps021Fields(t *testing.T) {
	st := exportFixture(t)
	c, _ := st.Get("Datacenter/Core/core-sw1")
	c.Account, c.FilesPanel, c.WOLBroadcast, c.WOLPort, c.WOLFrom = "lab-admin", true, "10.10.0.255", 7, "local"
	if _, err := st.Save(c, "Datacenter/Core/core-sw1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(Session{Name: "office-pc", Folder: "Office", Protocol: ProtoWOL, MAC: "3c:7c:3f:1a:22:90",
		WOLNetwork: "192.168.50.0/24", WOLScans: 6, WOLScanSecs: 20, WOLPingSecs: -1}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(Session{Name: "dl2", Protocol: ProtoSSH2, Hostname: "gw", UseRemoteCommand: true, RemoteCommand: "ssh -t dl2", SFTPOn: "jump", Account: "-"}, ""); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"xml", "zip", "folder"} {
		dest := filepath.Join(t.TempDir(), "out")
		if format != "folder" {
			dest += "." + format
		}
		if _, err := st.Export(nil, dest, format); err != nil {
			t.Fatal(format, err)
		}
		other, _ := NewStore(t.TempDir())
		if _, err := other.Import(dest, ImportOptions{}); err != nil {
			t.Fatal(format, err)
		}
		got, _ := other.Get("Datacenter/Core/core-sw1")
		if got.Account != "lab-admin" || !got.FilesPanel || got.WOLBroadcast != "10.10.0.255" || got.WOLPort != 7 || got.WOLFrom != "local" {
			t.Errorf("%s core-sw1: %+v", format, got)
		}
		w, err := other.Get("Office/office-pc")
		if err != nil || w.Protocol != ProtoWOL || w.MAC != "3c:7c:3f:1a:22:90" || w.WOLNetwork != "192.168.50.0/24" || w.WOLScans != 6 || w.WOLScanSecs != 20 || w.WOLPingSecs != -1 {
			t.Errorf("%s office-pc: %+v %v", format, w, err)
		}
		d, _ := other.Get("dl2")
		if d.SFTPOn != "jump" || d.Account != "-" {
			t.Errorf("%s dl2: sftpOn %q account %q", format, d.SFTPOn, d.Account)
		}
	}
}

func TestCSVKeeps021Fields(t *testing.T) {
	st := exportFixture(t)
	c, _ := st.Get("Datacenter/Core/core-sw1")
	c.Account, c.FilesPanel = "lab-admin", true
	_, _ = st.Save(c, "Datacenter/Core/core-sw1")
	_, _ = st.Save(Session{Name: "office-pc", Folder: "Office", Protocol: ProtoWOL, MAC: "3c:7c:3f:1a:22:90", WOLNetwork: "192.168.50.0/24"}, "")
	_, _ = st.Save(Session{Name: "dc01", Protocol: ProtoRDP, Hostname: "10.30.0.10", Port: 3389}, "")
	dest := filepath.Join(t.TempDir(), "s.csv")
	if _, err := st.ExportOther(nil, dest, "csv", nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dest)
	list, err := ParseCSV(b)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Session{}
	for _, s := range list {
		by[s.Name] = s
	}
	if s := by["core-sw1"]; s.Account != "lab-admin" || !s.FilesPanel {
		t.Errorf("core-sw1: %+v", s)
	}
	if s := by["office-pc"]; s.Protocol != ProtoWOL || s.MAC != "3c:7c:3f:1a:22:90" || s.WOLNetwork != "192.168.50.0/24" {
		t.Errorf("office-pc: %+v", s)
	}
	if s := by["dc01"]; s.Protocol != ProtoRDP {
		t.Errorf("dc01 protocol %q", s.Protocol)
	}
}

func TestOtherFormatsSkipWOL(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	_, _ = st.Save(Session{Name: "office-pc", Protocol: ProtoWOL, MAC: "3c:7c:3f:1a:22:90", WOLNetwork: "192.168.50.0/24"}, "")
	_, _ = st.Save(Session{Name: "sw", Protocol: ProtoSSH2, Hostname: "10.0.0.1", Port: 22, Account: "lab"}, "")
	for _, f := range []string{"putty-reg", "kitty-reg", "putty-dir", "moba", "tabby", "ssh-config"} {
		dest := filepath.Join(t.TempDir(), "out")
		rep, err := st.ExportOther(nil, dest, f, nil)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if rep.Count != 1 || len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0], "office-pc") {
			t.Errorf("%s: count %d skipped %v", f, rep.Count, rep.Skipped)
		}
	}
}

// A zip or folder export carries extra files (Termwright's settings) that
// ReadExportFile finds again; the sessions still import as before and the
// import reports where each session went.
func TestExportCarriesSettingsFile(t *testing.T) {
	st := exportFixture(t)
	for _, format := range []string{"zip", "folder"} {
		dest := filepath.Join(t.TempDir(), "out")
		if format != "folder" {
			dest += "." + format
		}
		if _, err := st.ExportWith(nil, dest, format, map[string][]byte{"termwright-settings.json": []byte(`{"format":"x"}`)}); err != nil {
			t.Fatal(format, err)
		}
		srcs := []string{dest}
		if format == "folder" {
			srcs = append(srcs, filepath.Join(dest, "Sessions"))
		}
		for _, src := range srcs {
			if data, ok := ReadExportFile(src, "termwright-settings.json"); !ok || string(data) != `{"format":"x"}` {
				t.Fatalf("%s: settings file not found from %s", format, src)
			}
		}
		other, _ := NewStore(t.TempDir())
		res, err := other.Import(dest, ImportOptions{TargetFolder: "In"})
		if err != nil || len(res.Errors) > 0 {
			t.Fatal(format, err, res.Errors)
		}
		if res.Paths["Datacenter/Core/core-sw1"] != "In/Datacenter/Core/core-sw1" {
			t.Fatalf("%s paths %v", format, res.Paths)
		}
	}
	if _, ok := ReadExportFile(filepath.Join(t.TempDir(), "none.zip"), "termwright-settings.json"); ok {
		t.Fatal("found a file in a missing export")
	}
}
