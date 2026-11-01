package twcore

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
)

// A Termwright .zip export comes back as the app's sessions, jump host included.
func TestImportTermwrightZip(t *testing.T) {
	dir := t.TempDir()
	st, _ := session.NewStore(filepath.Join(dir, "s"))
	nas := session.Defaults()
	nas.Name, nas.Folder, nas.Protocol, nas.Hostname, nas.Username = "nas", "Home", session.ProtoSSH2, "192.168.1.5", "admin"
	mac := session.Defaults()
	mac.Name, mac.Folder, mac.Protocol, mac.Hostname = "macbook", "Home", session.ProtoWOL, "192.168.1.40"
	mac.MAC, mac.WakeKnock, mac.JumpSession = "aa:bb:cc:dd:ee:ff, 11:22:33:44:55:66", "22, 5900", "Home/nas"
	mac.WakeSchedule, mac.WakeAt = "weekdays", "07:00"
	for _, s := range []session.Session{nas, mac} {
		if _, err := st.Save(s, ""); err != nil {
			t.Fatal(err)
		}
	}
	zip := filepath.Join(dir, "export.zip")
	if _, err := st.Export([]string{"Home/nas", "Home/macbook"}, zip, "zip"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(zip)
	// the name may have lost its .zip on the way to the phone
	js, err := ImportSessions(t.TempDir(), "Termwright export", data)
	if err != nil {
		t.Fatal(err)
	}
	var got []appSession
	_ = json.Unmarshal([]byte(js), &got)
	if len(got) != 2 {
		t.Fatalf("%s", js)
	}
	m := got[0]
	if m.Name != "macbook" || m.Protocol != session.ProtoWOL || m.Jump != "Home/nas" || !strings.Contains(m.MAC, "11:22") || m.Knock != "22, 5900" || m.Folder != "Home" ||
		m.WakeSchedule != "weekdays" || m.WakeAt != "07:00" {
		t.Fatalf("%+v", m)
	}
}

func TestBroadcastAndNetwork(t *testing.T) {
	d := appSession{Host: "192.168.1.40"}
	if b := broadcastOf(d, []string{"192.168.1.23/23"}); b != "192.168.1.255" {
		t.Fatal(b)
	}
	if b := broadcastOf(appSession{WOLNetwork: "10.0.0.0/22"}, nil); b != "10.0.3.255" {
		t.Fatal(b)
	}
	if !onNetwork(d, []string{"192.168.1.23/24"}) || onNetwork(d, []string{"10.1.1.5/24"}) {
		t.Fatal("onNetwork")
	}
}

// Away from the device's network: the packet goes from the jump host over SSH,
// its key is returned the first time and checked after that.
func TestWakeThroughJump(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if c.User() == "admin" && string(p) == "pw" {
			return nil, nil
		}
		return nil, os.ErrPermission
	}}
	cfg.AddHostKey(signer)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	cmds := make(chan string, 8)
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					ch, rq, _ := nch.Accept()
					go func() {
						for r := range rq {
							if r.Type == "exec" {
								cmds <- string(r.Payload[4:])
								r.Reply(true, nil)
								ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
								ch.Close()
							}
						}
					}()
				}
			}()
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	var p int
	for _, c := range port {
		p = p*10 + int(c-'0')
	}
	req := wakeRequest{Device: appSession{MAC: "aa:bb:cc:dd:ee:ff", Host: "192.168.1.40", Knock: "22"},
		LocalNets: []string{"10.20.0.7/24"}, Jump: &jumpHost{Host: host, Port: p, User: "admin", Password: "pw"}}
	b, _ := json.Marshal(req)
	out, err := Wake(string(b))
	if err != nil {
		t.Fatal(err)
	}
	var res wakeResult
	_ = json.Unmarshal([]byte(out), &res)
	if res.Via != host || !strings.HasPrefix(res.HostKey, "SHA256:") || res.Knocked != "192.168.1.40: 22" {
		t.Fatalf("%+v", res)
	}
	select {
	case c := <-cmds:
		if !strings.Contains(c, "192.168.1.255") || !strings.Contains(c, "perl -e") {
			t.Fatalf("command %q", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no command")
	}
	// a different key than the one accepted is refused
	req.Jump.HostKey = "SHA256:other"
	b, _ = json.Marshal(req)
	if _, err := Wake(string(b)); err == nil || !strings.HasPrefix(err.Error(), HostKeyChanged) {
		t.Fatalf("changed key not refused: %v", err)
	}
}

// On the device's network the packet goes from the phone.
func TestWakeLocal(t *testing.T) {
	ln, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	port := ln.LocalAddr().(*net.UDPAddr).Port
	req := wakeRequest{Device: appSession{MAC: "aa:bb:cc:dd:ee:ff", Host: "127.0.0.9", Broadcast: "127.0.0.1", WOLPort: port}, LocalNets: []string{"127.0.0.2/8"}}
	b, _ := json.Marshal(req)
	if _, err := Wake(string(b)); err != nil {
		t.Fatal(err)
	}
	_ = ln.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 200)
	if n, _, err := ln.ReadFromUDP(buf); err != nil || n != 102 {
		t.Fatalf("%d %v", n, err)
	}
}
