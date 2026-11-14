package emu

import (
	"os/exec"
	"strings"
	"testing"
)

func TestServerSettings(t *testing.T) {
	s := ServerDefaults("10.0.0.10/24")
	if s.From != "10.0.0.100" || s.To != "10.0.0.199" || s.DNS != "10.0.0.10" || !s.HTTP || !s.DHCP || s.User != "lab" {
		t.Fatalf("defaults %+v", s)
	}
	if err := s.Check("10.0.0.10/24"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		s    func(*Server)
		addr string
		want string
	}{
		{func(s *Server) {}, "dhcp", "fixed address"},
		{func(s *Server) { s.From = "10.1.0.5" }, "10.0.0.10/24", "not in 10.0.0.0/24"},
		{func(s *Server) { s.From, s.To = "10.0.0.150", "10.0.0.120" }, "10.0.0.10/24", "backwards"},
		{func(s *Server) { s.BootFile = "a b" }, "10.0.0.10/24", "not a file name"},
		{func(s *Server) { s.User = "root" }, "10.0.0.10/24", "not a user name"},
		{func(s *Server) { s.Gateway = "x" }, "10.0.0.10/24", "not an address"},
	} {
		x := ServerDefaults("10.0.0.10/24")
		c.s(&x)
		if err := x.Check(c.addr); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v %s: %v", x, c.addr, err)
		}
	}
	off := Server{HTTP: true}
	if err := off.Check(""); err != nil { // no DHCP: no address needed
		t.Fatal(err)
	}
}

func TestServerScript(t *testing.T) {
	s := ServerDefaults("10.0.0.10/24")
	s.Gateway, s.BootFile, s.BootFrom = "10.0.0.1", "summit.xos", "10.0.0.10"
	sc := ServerScript(s, "pa'ss", "52:54:00:AB:CD:01")
	for _, w := range []string{"apk add -q " + ServerPackages, "adduser -D -h /srv -s /bin/sh lab", `'pa'\''ss'`, "bb httpd -p 80 -h /srv", "ftpd -w /srv", "tftpd -c -u lab /srv",
		"/52:54:00:ab:cd:01/", "printf 'interface %s\\n' \"${i:-eth1}\" > /etc/udhcpd.conf", "start 10.0.0.100\nend 10.0.0.199", "option router 10.0.0.1", "boot_file summit.xos", "siaddr 10.0.0.10", "bb udhcpd /etc/udhcpd.conf", "echo tw-server-ok"} {
		if !strings.Contains(sc, w) {
			t.Errorf("script lacks %q", w)
		}
	}
	s.HTTP, s.DHCP, s.TFTPWrite = false, false, false
	sc2 := ServerScript(s, "x", "")
	if strings.Contains(sc2, "httpd -p") || strings.Contains(sc2, "udhcpd.conf <<") || strings.Contains(sc2, "tftpd -c") {
		t.Errorf("services that are off: %s", sc2)
	}
	for _, x := range []string{sc, sc2, ServerOffScript(), ServerStatusScript} {
		if out, err := exec.Command("sh", "-n", "-c", x).CombinedOutput(); err != nil {
			t.Fatalf("does not parse: %v %s\n%s", err, out, x)
		}
	}
}

func TestParseServerStatus(t *testing.T) {
	st := ParseServerStatus("on http\r\non ftp\non dhcp\non ssh\nlease 52:54:00:3c:7e:22 10.0.0.102 pc-1 00:41:12\nlease 00:04:96:9A:12:01 10.0.0.101 00:58:03\ndf 1015632 980112\n")
	if strings.Join(st.On, " ") != "http ftp dhcp ssh" || st.DiskKB != 1015632 || st.FreeKB != 980112 || len(st.Leases) != 2 {
		t.Fatalf("%+v", st)
	}
	if l := st.Leases[0]; l.IP != "10.0.0.101" || l.MAC != "00:04:96:9a:12:01" || l.Name != "" || l.Expires != "00:58:03" {
		t.Fatalf("first lease %+v", l)
	}
	if l := st.Leases[1]; l.Name != "pc-1" || l.Expires != "00:41:12" {
		t.Fatalf("second lease %+v", l)
	}
}
