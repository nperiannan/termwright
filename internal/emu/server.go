package emu

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

// A Server is a Linux computer (Alpine) that serves the lab: HTTP, FTP and TFTP from one shared folder (/srv)
// and DHCP on a data port, all from BusyBox (busybox-extras), and SSH/SFTP (Dropbear) for files from this
// computer. Its services answer on every address it has: the management network too (files from this computer,
// switches fetching over VR-Mgmt); DHCP serves its data port only, never the network the management is on.

// Server is a server's settings.
type Server struct {
	HTTP      bool   `json:"http"`
	HTTPPort  int    `json:"httpPort,omitempty"` // 0 = 80
	FTP       bool   `json:"ftp"`
	FTPPort   int    `json:"ftpPort,omitempty"` // 0 = 21
	TFTP      bool   `json:"tftp"`
	TFTPWrite bool   `json:"tftpWrite"`           // uploads by TFTP (a switch saving its configuration)
	DHCP      bool   `json:"dhcp"`                // on DHCPPort
	DHCPPort  int    `json:"dhcpPort,omitempty"`  // the data port it serves (0 = 1)
	From      string `json:"from,omitempty"`      // the first address it gives
	To        string `json:"to,omitempty"`        // the last
	Gateway   string `json:"gateway,omitempty"`   // option router ("" = none)
	DNS       string `json:"dns,omitempty"`       // option dns ("" = none)
	Lease     int    `json:"lease,omitempty"`     // seconds (0 = 3600)
	BootFile  string `json:"bootFile,omitempty"`  // option 67 (a file in the shared folder)
	BootFrom  string `json:"bootFrom,omitempty"`  // the TFTP server it names (siaddr, option 66; "" = itself)
	User      string `json:"user,omitempty"`      // the FTP and SSH user ("" = lab); its password is kept in the keychain
	Installed bool   `json:"installed,omitempty"` // its packages are in its disk
}

// ServerPackages are the Alpine packages a server needs (well under 1 MB with Dropbear).
const ServerPackages = "busybox-extras dropbear dropbear-scp openssh-sftp-server"

// ServerDefaults are a new server's settings: every service on, DHCP for 10.0.0.100-199 on port 1 (where it has
// addr, e.g. 10.0.0.10/24).
func ServerDefaults(addr string) Server {
	s := Server{HTTP: true, FTP: true, TFTP: true, TFTPWrite: true, DHCP: true, DHCPPort: 1, Lease: 3600, User: "lab"}
	if ip, n, err := net.ParseCIDR(addr); err == nil && ip.To4() != nil {
		b := n.IP.To4()
		if ones, _ := n.Mask.Size(); ones <= 24 {
			s.From = fmt.Sprintf("%d.%d.%d.100", b[0], b[1], b[2])
			s.To = fmt.Sprintf("%d.%d.%d.199", b[0], b[1], b[2])
		}
		s.DNS = ip.String()
	}
	return s
}

func orPort(p, d int) int {
	if p <= 0 || p > 65535 {
		return d
	}
	return p
}

// Check says what is wrong with a server's settings (its address on the DHCP port is addr, "" = none).
func (s Server) Check(addr string) error {
	for _, p := range []int{s.HTTPPort, s.FTPPort} {
		if p < 0 || p > 65535 {
			return fmt.Errorf("port %d is not a port", p)
		}
	}
	if s.User != "" && !userRe(s.User) {
		return fmt.Errorf("%q is not a user name (letters, digits, - and _)", s.User)
	}
	if strings.ContainsAny(s.BootFile, "'\"\\$`\n ") || strings.Contains(s.BootFile, "..") {
		return fmt.Errorf("%q is not a file name", s.BootFile)
	}
	if !s.DHCP {
		return nil
	}
	if addr == "" || strings.EqualFold(addr, "dhcp") {
		return fmt.Errorf("DHCP needs a fixed address on port %d (Network)", orPort(s.DHCPPort, 1))
	}
	_, n, err := net.ParseCIDR(addr)
	if err != nil {
		return fmt.Errorf("port %d: %v", orPort(s.DHCPPort, 1), err)
	}
	for _, x := range []struct{ name, v string }{{"the first address", s.From}, {"the last address", s.To}} {
		ip := net.ParseIP(x.v)
		if ip == nil || ip.To4() == nil {
			return fmt.Errorf("%s %q is not an IPv4 address", x.name, x.v)
		}
		if !n.Contains(ip) {
			return fmt.Errorf("%s %s is not in %s (the server's network on port %d)", x.name, x.v, n, orPort(s.DHCPPort, 1))
		}
	}
	if ipToInt(net.ParseIP(s.From)) > ipToInt(net.ParseIP(s.To)) {
		return fmt.Errorf("the range %s - %s is backwards", s.From, s.To)
	}
	for _, x := range []struct{ name, v string }{{"the gateway", s.Gateway}, {"the DNS server", s.DNS}, {"the TFTP server", s.BootFrom}} {
		if x.v != "" && net.ParseIP(x.v) == nil {
			return fmt.Errorf("%s %q is not an address", x.name, x.v)
		}
	}
	return nil
}

func userRe(u string) bool {
	if len(u) == 0 || len(u) > 32 || u == "root" {
		return false
	}
	for _, r := range u {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func ipToInt(ip net.IP) uint32 {
	b := ip.To4()
	if b == nil {
		return 0
	}
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// ServerScript sets a server up on its console (as root): its packages when they are not in its disk (that
// needs internet once: a Server image has them), its user and password, the shared folder, and a start script
// (/etc/local.d) that starts the services it has on at every boot - then runs it. It prints tw-server-ok. mac is
// its DHCP port's MAC address (its interface is found by it).
func ServerScript(s Server, password, mac string) string {
	user := s.User
	if user == "" {
		user = "lab"
	}
	ifc := "eth" + strconv.Itoa(orPort(s.DHCPPort, 1)) // when its MAC is not found
	var st strings.Builder // the start script
	st.WriteString("#!/bin/sh\n# Termwright: the server's services (written by Termwright; changes in Termwright)\n")
	st.WriteString(serverStop)
	// an applet from busybox-extras, else from busybox
	st.WriteString("bb() { if busybox-extras --list 2>/dev/null | grep -qx \"$1\"; then busybox-extras \"$@\"; else busybox \"$@\"; fi; }\n")
	st.WriteString("mkdir -p /srv /var/lib/udhcpd; touch /var/lib/udhcpd/udhcpd.leases\n")
	st.WriteString("rc-service dropbear start >/dev/null 2>&1 || dropbear -R\n")
	if s.HTTP {
		fmt.Fprintf(&st, "bb httpd -p %d -h /srv\n", orPort(s.HTTPPort, 80))
	}
	if s.FTP {
		fmt.Fprintf(&st, "bb tcpsvd -vE 0.0.0.0 %d $(command -v busybox-extras || echo busybox) ftpd -w /srv >/var/log/tw-ftpd.log 2>&1 &\n", orPort(s.FTPPort, 21))
	}
	if s.TFTP {
		w := ""
		if s.TFTPWrite {
			w = "-c "
		}
		fmt.Fprintf(&st, "bb udpsvd -vE 0.0.0.0 69 $(command -v busybox-extras || echo busybox) tftpd %s-u %s /srv >/var/log/tw-tftpd.log 2>&1 &\n", w, user)
	}
	if s.DHCP {
		var c strings.Builder
		fmt.Fprintf(&c, "start %s\nend %s\nlease_file /var/lib/udhcpd/udhcpd.leases\nauto_time 10\n", s.From, s.To)
		if s.Gateway != "" {
			fmt.Fprintf(&c, "option router %s\n", s.Gateway)
		}
		if s.DNS != "" {
			fmt.Fprintf(&c, "option dns %s\n", s.DNS)
		}
		fmt.Fprintf(&c, "option lease %d\n", orPort(s.Lease, 3600))
		if s.BootFile != "" {
			fmt.Fprintf(&c, "boot_file %s\noption bootfile %s\n", s.BootFile, s.BootFile)
		}
		if s.BootFrom != "" {
			fmt.Fprintf(&c, "siaddr %s\noption tftp %s\n", s.BootFrom, s.BootFrom)
		}
		fmt.Fprintf(&st, "i=$(ip -o link | awk -F': ' 'tolower($0) ~ /%s/ {sub(/@.*/, \"\", $2); print $2; exit}')\n", strings.ToLower(mac))
		fmt.Fprintf(&st, "printf 'interface %%s\\n' \"${i:-%s}\" > /etc/udhcpd.conf\ncat >> /etc/udhcpd.conf <<'TWDHCP'\n%sTWDHCP\nbb udhcpd /etc/udhcpd.conf\n", ifc, c.String())
	}
	var b strings.Builder
	b.WriteString("set -e\n")
	fmt.Fprintf(&b, "for p in %s; do apk info -e $p >/dev/null 2>&1 || need=1; done\n", ServerPackages)
	fmt.Fprintf(&b, "if [ -n \"$need\" ]; then apk add -q %s >/dev/null 2>&1 || { echo 'tw-server-fail its packages could not be installed (it needs internet once: %s)'; exit 1; }; fi\n", ServerPackages, ServerPackages)
	fmt.Fprintf(&b, "id %s >/dev/null 2>&1 || adduser -D -h /srv -s /bin/sh %s\n", user, user)
	fmt.Fprintf(&b, "printf '%%s:%%s\\n' %s %s | chpasswd >/dev/null\n", Q(user), Q(password))
	fmt.Fprintf(&b, "mkdir -p /srv && chown -R %s /srv && chmod 755 /srv\n", user)
	b.WriteString("[ -f /srv/index.html ] || printf '<h1>Termwright lab server</h1>\\n' > /srv/index.html\n")
	b.WriteString("rc-update add dropbear default >/dev/null 2>&1 || true; rc-update add local default >/dev/null 2>&1 || true\n")
	b.WriteString("mkdir -p /etc/local.d\ncat > /etc/local.d/tw-server.start <<'TWSRV'\n" + st.String() + "TWSRV\n")
	b.WriteString("chmod +x /etc/local.d/tw-server.start\nset +e\n/etc/local.d/tw-server.start\nsleep 1\necho tw-server-ok\n")
	return b.String()
}

// ServerOffScript stops a server's services and keeps them off (its files stay).
func ServerOffScript() string {
	return "rm -f /etc/local.d/tw-server.start\n" + serverStop + "echo tw-server-ok\n"
}

// serverStop stops the services (the patterns do not match the shell that runs them).
const serverStop = "for p in '[h]ttpd -p' '[f]tpd -w /srv' '[t]ftpd ' '[u]dhcpd /etc/udhcpd.conf'; do pkill -f \"$p\" 2>/dev/null; done\n"

// ServerStatusScript prints which services run and the DHCP leases (for ParseServerStatus).
const ServerStatusScript = `for x in 'http:[h]ttpd -p' 'ftp:[f]tpd -w /srv' 'tftp:[t]ftpd ' 'dhcp:[u]dhcpd /etc/udhcpd.conf' 'ssh:[d]ropbear'; do pgrep -f "${x#*:}" >/dev/null && echo "on ${x%%:*}"; done
{ busybox-extras dumpleases -f /var/lib/udhcpd/udhcpd.leases 2>/dev/null || busybox dumpleases -f /var/lib/udhcpd/udhcpd.leases 2>/dev/null; } | sed 1d | sed 's/^/lease /'
df -k /srv 2>/dev/null | awk 'NR==2{print "df " $2 " " $4}'
`

// ServerLease is a DHCP lease the server gave.
type ServerLease struct {
	MAC     string `json:"mac"`
	IP      string `json:"ip"`
	Name    string `json:"name"`
	Expires string `json:"expires"` // as dumpleases says it (remaining, or a time)
}

// ServerStatus is what ServerStatusScript printed.
type ServerStatus struct {
	On     []string      `json:"on"` // http ftp tftp dhcp ssh
	Leases []ServerLease `json:"leases"`
	DiskKB int64         `json:"diskKb"`
	FreeKB int64         `json:"freeKb"`
}

// ParseServerStatus reads ServerStatusScript's output.
func ParseServerStatus(out string) ServerStatus {
	var st ServerStatus
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 2 && f[0] == "on":
			st.On = append(st.On, f[1])
		case len(f) >= 3 && f[0] == "lease" && strings.Count(f[1], ":") == 5:
			l := ServerLease{MAC: strings.ToLower(f[1]), IP: f[2]}
			if len(f) > 3 {
				// "lease MAC IP [name] expires…": a name is a word without digits-only/time shape before the expiry
				rest := f[3:]
				if len(rest) > 1 || !strings.ContainsAny(rest[0], ":0123456789") {
					l.Name = rest[0]
					rest = rest[1:]
				}
				l.Expires = strings.Join(rest, " ")
			}
			st.Leases = append(st.Leases, l)
		case len(f) == 3 && f[0] == "df":
			st.DiskKB, _ = strconv.ParseInt(f[1], 10, 64)
			st.FreeKB, _ = strconv.ParseInt(f[2], 10, 64)
		}
	}
	sort.SliceStable(st.Leases, func(i, j int) bool {
		return ipToInt(net.ParseIP(st.Leases[i].IP)) < ipToInt(net.ParseIP(st.Leases[j].IP))
	})
	return st
}
