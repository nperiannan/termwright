// Package emu runs real network operating systems: the vendor's own VM images
// (qcow2 and the like), booted with QEMU/KVM on a Linux host that Termwright
// reaches over SSH. Nothing is installed on the host: Termwright copies the
// image there once, gives every switch a small copy-on-write disk on top of it
// and runs qemu-system-x86_64 for it, with
//
//   - the first network port as the management port: user-mode networking
//     with an SSH port forward on the host's 127.0.0.1 (no root needed), or a
//     tap on a bridge of the host (needs sudo);
//   - the other ports wired to each other point to point over UDP sockets on
//     the host's 127.0.0.1 - the lab's cables;
//   - the serial console and the QEMU monitor as telnet servers on the host's
//     127.0.0.1, reached through the SSH connection; the console is also
//     written to a log file, which shows how far the boot is;
//   - first-boot (day-0) commands typed on the console once the image is ready.
//
// This package builds the commands and reads their output; the app runs them.
package emu

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Image is a VM image and how to boot it (its profile).
type Image struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	File       string      `json:"file"`       // the image on this computer
	FileName   string      `json:"fileName"`   // its name on the hosts
	Size       int64       `json:"size"`       // bytes
	SHA256     string      `json:"sha256"`     // of the file, to know it is on a host already
	DeviceType string      `json:"deviceType"` // the sessions' device type (Linux, Cisco IOS…)
	CPUs       int         `json:"cpus"`
	RAM        int         `json:"ram"`      // MB
	NICs       int         `json:"nics"`     // network ports, the first one is management
	NIC        string      `json:"nic"`      // e1000, virtio-net-pci, vmxnet3…
	Disk       string      `json:"disk"`     // virtio, ide, sata, scsi
	PortName   string      `json:"portName"` // data port n as the OS calls it: "{n}" or "1/{n}"
	Ready      string      `json:"ready"`    // console text when it can be logged in to ("login:")
	Login      string      `json:"login"`
	Password   string      `json:"password,omitempty"` // in the keychain, never in the lab file
	Prompt     string      `json:"prompt"`             // regexp of the CLI prompt (default [>#$]\s*$)
	Day0       []string    `json:"day0"`               // first-boot commands, with {hostname} {mgmtIp} …
	Answers    []Answer    `json:"answers"`            // questions on the console and what to answer
	Extra      string      `json:"extra"`              // more qemu options
	Source     string      `json:"source"`             // where the profile came from (a GNS3 appliance…)
	AddrCmd    string      `json:"addrCmd"`            // a CLI command that prints the management address (host's network mode)
	Persona    *PersonaDef `json:"persona,omitempty"`  // a file that makes the image act as a given device
	Version    string      `json:"version"`            // the software version of the image (for the lists)
	Match      string      `json:"match,omitempty"`    // profiles: a regexp on the file name that picks this profile; a "version" group gives the version
	Serial     string      `json:"serial"`             // serial number format for new switches ({N:5} {A:2} {YY} {WW} …; "" = none)
}

// Answer: when the console shows Match (plain text), Send is typed (and Enter).
type Answer struct {
	Match string `json:"match"`
	Send  string `json:"send"`
}

// Template is a device to make switches from: an image with its resources,
// the persona model, and where serial numbers and MAC addresses start.
type Template struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	CPUs        int               `json:"cpus"`
	RAM         int               `json:"ram"`    // MB
	DiskGB      int               `json:"diskGb"` // 0 = the image's size
	Model       string            `json:"model"`  // persona model ("" = none)
	Module      string            `json:"module"`
	Values      map[string]string `json:"values"`
	SerialStart string            `json:"serialStart"` // the first serial number; the next switches count up from it
	MACStart    string            `json:"macStart"`    // the first switch's base MAC; the next ones follow
}

// VM is one switch of the lab.
type VM struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"` // hostname, also the session name
	Image    string   `json:"image"`
	Index    int      `json:"index"` // its number in the lab: ports and MACs come from it
	CPUs     int      `json:"cpus"`
	RAM      int      `json:"ram"`
	MgmtIP   string   `json:"mgmtIp"` // address/bits it gets on day 0
	Gateway  string   `json:"gateway"`
	Day0Done bool     `json:"day0Done"`
	Serial   string   `json:"serial,omitempty"`  // given to the VM as its SMBIOS serial number ("" = the VM's own)
	BaseMAC  string   `json:"baseMac,omitempty"` // port 0's MAC, the others follow ("" = from the lab)
	DiskGB   int      `json:"diskGb,omitempty"`  // grow its disk to this size (0 = the image's)
	Template string   `json:"template,omitempty"`
	Persona  *Persona `json:"persona,omitempty"`
	X        int      `json:"x"`
	Y        int      `json:"y"`
}

// Cable joins data port APort of A to BPort of B (port numbers from 1).
type Cable struct {
	A     string `json:"a"`
	APort int    `json:"aPort"`
	B     string `json:"b"`
	BPort int    `json:"bPort"`
	Down  bool   `json:"down,omitempty"` // pulled: the link is off on both ends
}

// Lab is what a host runs.
type Lab struct {
	ID      string  `json:"id"`      // short, part of the folder and interface names
	Dir     string  `json:"dir"`     // on the host, relative to the home folder
	Mgmt    string  `json:"mgmt"`    // "lan" (the host's network, DHCP), "bridge" (a lab bridge on the host) or "forward"
	LanIf   string  `json:"lanIf"`   // lan: the host's bridge (tap on it) or network port (macvtap on it)
	LanKind string  `json:"lanKind"` // lan: "bridge" or "macvtap"
	MgmtNet string  `json:"mgmtNet"` // bridge mode: the bridge's network (10.99.0.0/24)
	Base    int     `json:"base"`    // first TCP/UDP port on the host's 127.0.0.1
	KVM     bool    `json:"kvm"`     // false: software emulation (slow, for tests)
	VMs     []VM    `json:"vms"`
	Cables  []Cable `json:"cables"`
}

// Ports on the host for VM i: SSH forward, console, monitor, and data port n's UDP socket.
func (l *Lab) SSHPort(v VM) int     { return l.Base + v.Index*4 }
func (l *Lab) ConsolePort(v VM) int { return l.Base + v.Index*4 + 1 }
func (l *Lab) MonitorPort(v VM) int { return l.Base + v.Index*4 + 2 }
func (l *Lab) udpPort(v VM, n int) int {
	return l.Base + 4000 + v.Index*64 + n
}

// VMDir is the lab's folder on the host; ImageDir the shared images.
func (l *Lab) VMDir() string    { return l.Dir + "/lab-" + l.ID }
func (l *Lab) ImageDir() string { return l.Dir + "/images" }

// VMByID finds a VM.
func (l *Lab) VMByID(id string) *VM {
	for i := range l.VMs {
		if l.VMs[i].ID == id {
			return &l.VMs[i]
		}
	}
	return nil
}

// MAC of port n (0 = management) of a VM: QEMU's own prefix, unique in the lab.
func (l *Lab) MAC(v VM, n int) string {
	if hw, err := net.ParseMAC(v.BaseMAC); err == nil && len(hw) == 6 {
		x := uint64(hw[0])<<40 | uint64(hw[1])<<32 | uint64(hw[2])<<24 | uint64(hw[3])<<16 | uint64(hw[4])<<8 | uint64(hw[5])
		x += uint64(n)
		return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", byte(x>>40), byte(x>>32), byte(x>>24), byte(x>>16), byte(x>>8), byte(x))
	}
	id, _ := strconv.ParseUint(l.ID, 16, 64)
	return fmt.Sprintf("52:54:%02x:%02x:%02x:%02x", byte(id), byte(v.Index>>8), byte(v.Index), byte(n))
}

// tap is the management interface name in bridge mode (at most 15 characters).
func (l *Lab) tap(v VM) string { return fmt.Sprintf("tw%s%dm", l.ID, v.Index) }

// Bridge is the host's management bridge in bridge mode.
func (l *Lab) Bridge() string { return "tw" + l.ID + "br" }

// PortName is data port n as the switch calls it.
func (im *Image) PortNameOf(n int) string {
	f := im.PortName
	if f == "" {
		f = "{n}"
	}
	return strings.ReplaceAll(f, "{n}", strconv.Itoa(n))
}

// DataPorts is how many ports can be cabled.
func (im *Image) DataPorts() int {
	if im.NICs <= 1 {
		return 0
	}
	return im.NICs - 1
}

// Args are the qemu-system-x86_64 options for a VM.
func (l *Lab) Args(v VM, im *Image) []string {
	dir := l.VMDir()
	a := []string{"-name", v.Name, "-m", strconv.Itoa(orInt(v.RAM, im.RAM, 1024)), "-smp", strconv.Itoa(orInt(v.CPUs, im.CPUs, 1))}
	if l.KVM {
		a = append(a, "-enable-kvm", "-cpu", "host")
	} else {
		a = append(a, "-accel", "tcg")
	}
	disk := dir + "/" + v.ID + ".qcow2"
	switch im.Disk {
	case "ide", "scsi":
		a = append(a, "-drive", "file="+disk+",if="+im.Disk+",format=qcow2")
	case "sata":
		a = append(a, "-device", "ahci,id=ahci", "-drive", "file="+disk+",if=none,id=d0,format=qcow2", "-device", "ide-hd,drive=d0,bus=ahci.0")
	default:
		a = append(a, "-drive", "file="+disk+",if=virtio,format=qcow2")
	}
	nic := im.NIC
	if nic == "" {
		nic = "e1000"
	}
	// management: the first port
	switch {
	case l.Mgmt == "lan" && l.LanKind == "macvtap":
		a = append(a, "-netdev", "tap,id=n0,fd=3") // the macvtap device, opened by the start script
	case l.Mgmt == "bridge" || l.Mgmt == "lan":
		a = append(a, "-netdev", "tap,id=n0,ifname="+l.tap(v)+",script=no,downscript=no")
	default:
		a = append(a, "-netdev", fmt.Sprintf("user,id=n0,hostfwd=tcp:127.0.0.1:%d-:22", l.SSHPort(v)))
	}
	a = append(a, "-device", nic+",netdev=n0,mac="+l.MAC(v, 0))
	// data ports: cabled ones get a UDP socket to the other end
	peer := map[int]int{}
	for _, c := range l.Cables {
		if c.A == v.ID {
			if o := l.VMByID(c.B); o != nil {
				peer[c.APort] = l.udpPort(*o, c.BPort)
			}
		}
		if c.B == v.ID {
			if o := l.VMByID(c.A); o != nil {
				peer[c.BPort] = l.udpPort(*o, c.APort)
			}
		}
	}
	for n := 1; n < im.NICs; n++ {
		if p, ok := peer[n]; ok {
			a = append(a, "-netdev", fmt.Sprintf("socket,id=n%d,udp=127.0.0.1:%d,localaddr=127.0.0.1:%d", n, p, l.udpPort(v, n)),
				"-device", fmt.Sprintf("%s,netdev=n%d,mac=%s", nic, n, l.MAC(v, n)))
		} else {
			a = append(a, "-device", fmt.Sprintf("%s,mac=%s", nic, l.MAC(v, n)))
		}
	}
	if v.Serial != "" {
		sn := strings.ReplaceAll(v.Serial, ",", ",,")
		a = append(a, "-smbios", "type=1,serial="+sn, "-smbios", "type=2,serial="+sn, "-smbios", "type=3,serial="+sn)
	}
	a = append(a,
		"-chardev", fmt.Sprintf("socket,id=con0,host=127.0.0.1,port=%d,server=on,wait=off,telnet=on,logfile=%s/%s.log", l.ConsolePort(v), dir, v.ID),
		"-serial", "chardev:con0",
		"-monitor", fmt.Sprintf("telnet:127.0.0.1:%d,server,nowait", l.MonitorPort(v)),
		"-display", "none", "-daemonize", "-pidfile", dir+"/"+v.ID+".pid")
	if im.Extra != "" {
		a = append(a, splitArgs(im.Extra)...)
	}
	return a
}

// StartScript starts VMs (each with its image) on the host: their disks are
// made on first start, their console logs are emptied.
func (l *Lab) StartScript(vms []VM, imgs map[string]*Image) string {
	var b strings.Builder
	dir := l.VMDir()
	fmt.Fprintf(&b, "mkdir -p %s || exit 1\n", Q(dir))
	if l.Mgmt == "bridge" {
		br := l.Bridge()
		gw := l.gatewayPrefix()
		fmt.Fprintf(&b, "ip link show %s >/dev/null 2>&1 || { sudo -n ip link add %s type bridge && sudo -n ip addr add %s dev %s; }\nsudo -n ip link set %s up\n", br, br, gw, br, br)
	}
	for _, v := range vms {
		im := imgs[v.Image]
		if im == nil {
			fmt.Fprintf(&b, "echo 'fail %s no image'\n", v.ID)
			continue
		}
		fmt.Fprintf(&b, "if [ -f %s/%s.pid ] && kill -0 \"$(cat %s/%s.pid)\" 2>/dev/null; then echo 'up %s'; else\n", Q(dir), v.ID, Q(dir), v.ID, v.ID)
		b.WriteString(l.diskCmd(v, im))
		t, redir := l.tap(v), ""
		switch {
		case l.Mgmt == "lan" && l.LanKind == "macvtap":
			fmt.Fprintf(&b, " ip link show %s >/dev/null 2>&1 || sudo -n ip link add link %s name %s address %s type macvtap mode bridge; sudo -n ip link set %s up\n", t, Q(l.LanIf), t, l.MAC(v, 0), t)
			fmt.Fprintf(&b, " idx=$(cat /sys/class/net/%s/ifindex); for i in 1 2 3 4 5 6 7 8 9 10; do [ -e /dev/tap$idx ] && break; sleep 0.2; done; sudo -n chown \"$(id -un)\" /dev/tap$idx\n", t)
			redir = " 3<>/dev/tap$idx"
		case l.Mgmt == "bridge" || l.Mgmt == "lan":
			br := l.Bridge()
			if l.Mgmt == "lan" {
				br = l.LanIf
			}
			fmt.Fprintf(&b, " ip link show %s >/dev/null 2>&1 || sudo -n ip tuntap add %s mode tap user \"$(id -un)\"; sudo -n ip link set %s master %s up\n", t, t, t, Q(br))
		}
		fmt.Fprintf(&b, " : > %s/%s.log\n", Q(dir), v.ID)
		fmt.Fprintf(&b, " if out=$(qemu-system-x86_64 %s%s 2>&1); then echo 'started %s'; else echo \"fail %s $(echo $out | tr '\\n' ' ')\"; fi\nfi\n", joinArgs(l.Args(v, im)), redir, v.ID, v.ID)
	}
	return b.String()
}

// diskCmd makes a VM's disk (a copy-on-write layer on the image) and grows it to its size.
func (l *Lab) diskCmd(v VM, im *Image) string {
	disk := l.VMDir() + "/" + v.ID + ".qcow2"
	s := fmt.Sprintf(" [ -f %s ] || qemu-img create -q -f qcow2 -F qcow2 -b \"$HOME/%s/%s\" %s || { echo 'fail %s disk'; }\n", Q(disk), l.ImageDir(), im.FileName, Q(disk), v.ID)
	if v.DiskGB > 0 {
		s += fmt.Sprintf(" cur=$(qemu-img info --output=json %s 2>/dev/null | sed -n 's/.*\"virtual-size\": *\\([0-9]*\\).*/\\1/p' | head -1); [ -n \"$cur\" ] && [ \"$cur\" -lt %d ] && qemu-img resize -q %s %dG\n", Q(disk), int64(v.DiskGB)<<30, Q(disk), v.DiskGB)
	}
	return s
}

// PrepareScript makes the disks of VMs without starting them.
func (l *Lab) PrepareScript(vms []VM, imgs map[string]*Image) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mkdir -p %s || exit 1\n", Q(l.VMDir()))
	for _, v := range vms {
		if im := imgs[v.Image]; im != nil {
			b.WriteString(l.diskCmd(v, im))
		}
	}
	return b.String()
}

// StopScript stops VMs (and removes their taps in bridge mode).
func (l *Lab) StopScript(ids []string) string {
	var b strings.Builder
	dir := l.VMDir()
	for _, id := range ids {
		fmt.Fprintf(&b, "if [ -f %s/%s.pid ]; then kill \"$(cat %s/%s.pid)\" 2>/dev/null; rm -f %s/%s.pid; fi\n", Q(dir), id, Q(dir), id, Q(dir), id)
		if l.Mgmt == "bridge" || l.Mgmt == "lan" {
			if v := l.VMByID(id); v != nil {
				fmt.Fprintf(&b, "sleep 0.3; sudo -n ip link del %s 2>/dev/null\n", l.tap(*v))
			}
		}
		fmt.Fprintf(&b, "echo 'stopped %s'\n", id)
	}
	return b.String()
}

// ResetScript stops VMs and throws their disks away: they boot as from the image again.
func (l *Lab) ResetScript(ids []string) string {
	var b strings.Builder
	b.WriteString(l.StopScript(ids))
	for _, id := range ids {
		fmt.Fprintf(&b, "sleep 0.5; rm -f %s/%s.qcow2 %s/%s.log\n", Q(l.VMDir()), id, Q(l.VMDir()), id)
	}
	return b.String()
}

// StatusScript prints "clk <ticks per second>", a line per VM: "up <id> <CPU
// ticks used so far>" or "down <id>",
// then "log <id> <base64 of the console log's end>" for the ones in tails.
func (l *Lab) StatusScript(tails []string) string {
	dir := Q(l.VMDir())
	s := "cd " + dir + " 2>/dev/null || exit 0\necho \"clk $(getconf CLK_TCK 2>/dev/null || echo 100)\"\nawk '/^MemTotal:/{t=$2} /^MemAvailable:/{a=$2} END{print \"mem \" int(t/1024) \" \" int(a/1024)}' /proc/meminfo\necho \"cores $(nproc) $(cut -d' ' -f1 /proc/loadavg)\"\nfor p in *.pid; do [ -e \"$p\" ] || continue; n=${p%.pid}; pid=$(cat \"$p\")\n" +
		" if kill -0 \"$pid\" 2>/dev/null; then echo \"up $n $(awk '{print $14+$15}' /proc/$pid/stat 2>/dev/null)\"; else echo \"down $n\"; fi; done\n"
	for _, id := range tails {
		s += fmt.Sprintf("[ -f %s.log ] && echo \"log %s $(tail -c 4000 %s.log | base64 | tr -d '\\n')\"\n", id, id, id)
	}
	return s
}

// Status is what StatusScript found.
type Status struct {
	Clk      float64            // CPU ticks per second
	MemTotal int64              // the host's memory, MB
	MemFree  int64              // available now, MB
	Cores    int                // the host's CPU cores
	Load     float64            // its load average
	Up       map[string]float64 // running VMs and the CPU ticks they used
	Log      map[string]string  // console log ends
}

// ParseStatus reads StatusScript's output.
func ParseStatus(out string, b64 func(string) ([]byte, error)) Status {
	st := Status{Up: map[string]float64{}, Log: map[string]string{}}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 3 && f[0] == "mem":
			st.MemTotal, _ = strconv.ParseInt(f[1], 10, 64)
			st.MemFree, _ = strconv.ParseInt(f[2], 10, 64)
		case len(f) == 3 && f[0] == "cores":
			st.Cores, _ = strconv.Atoi(f[1])
			st.Load, _ = strconv.ParseFloat(f[2], 64)
		case len(f) == 2 && f[0] == "clk":
			st.Clk, _ = strconv.ParseFloat(f[1], 64)
		case len(f) >= 2 && f[0] == "up":
			c := 0.0
			if len(f) > 2 {
				c, _ = strconv.ParseFloat(f[2], 64)
			}
			st.Up[f[1]] = c
		case len(f) >= 2 && f[0] == "log":
			if len(f) == 3 {
				if raw, err := b64(f[2]); err == nil {
					st.Log[f[1]] = CleanConsole(string(raw))
				}
			} else {
				st.Log[f[1]] = ""
			}
		}
	}
	return st
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b[()][A-Z0-9]|\r`)

// CleanConsole removes terminal escape sequences and telnet bytes from console text.
func CleanConsole(s string) string {
	s = ansi.ReplaceAllString(s, "")
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0xff && i+1 < len(s) { // telnet IAC
			if s[i+1] >= 251 && s[i+1] <= 254 {
				i += 2
			} else {
				i++
			}
			continue
		}
		if c == '\n' || c == '\t' || c >= 0x20 && c != 0x7f {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// Ready says whether the console log shows the image's ready text.
func (im *Image) ReadyIn(log string) bool {
	r := im.Ready
	if r == "" {
		return true
	}
	return strings.Contains(log, r)
}

// gatewayPrefix: the bridge's own address (the network's first) with its bits.
func (l *Lab) gatewayPrefix() string {
	p, err := netip.ParsePrefix(l.MgmtNet)
	if err != nil {
		return "10.99.0.1/24"
	}
	return p.Masked().Addr().Next().String() + "/" + strconv.Itoa(p.Bits())
}

// Address gives VM index i its management address and gateway.
func (l *Lab) Address(i int) (string, string) {
	if l.Mgmt == "lan" {
		return "", "" // from the network's DHCP; learned from the switch
	}
	if l.Mgmt != "bridge" {
		return "10.0.2.15/24", "10.0.2.2" // QEMU's user-mode network, the same in every VM
	}
	p, err := netip.ParsePrefix(l.MgmtNet)
	if err != nil {
		p = netip.MustParsePrefix("10.99.0.0/24")
	}
	gw := p.Masked().Addr().Next()
	a := gw
	for k := 0; k < 10+i; k++ {
		a = a.Next()
	}
	return a.String() + "/" + strconv.Itoa(p.Bits()), gw.String()
}

// FreeDataPort is the lowest data port of v without a cable.
func (l *Lab) FreeDataPort(v VM, im *Image) int {
	used := map[int]bool{}
	for _, c := range l.Cables {
		if c.A == v.ID {
			used[c.APort] = true
		}
		if c.B == v.ID {
			used[c.BPort] = true
		}
	}
	for n := 1; n <= im.DataPorts(); n++ {
		if !used[n] {
			return n
		}
	}
	return 0
}

// Wire cables new VMs: "leaf-spine" (the lab's first two VMs are the cores,
// linked to each other; every other VM to both), "chain", "ring" or "".
func (l *Lab) Wire(add []VM, kind string, imgs map[string]*Image) {
	link := func(a, b VM) {
		pa, pb := l.FreeDataPort(a, imgs[a.Image]), l.FreeDataPort(b, imgs[b.Image])
		if pa > 0 && pb > 0 {
			l.Cables = append(l.Cables, Cable{A: a.ID, APort: pa, B: b.ID, BPort: pb})
		}
	}
	switch kind {
	case "leaf-spine":
		if len(l.VMs) < 2 {
			return
		}
		cores := l.VMs[:2]
		isNew := map[string]bool{}
		for _, v := range add {
			isNew[v.ID] = true
		}
		if isNew[cores[1].ID] {
			link(cores[0], cores[1])
		}
		for _, v := range add {
			if v.ID == cores[0].ID || v.ID == cores[1].ID {
				continue
			}
			link(cores[0], v)
			link(cores[1], v)
		}
	case "chain", "ring":
		for i := 0; i+1 < len(add); i++ {
			link(add[i], add[i+1])
		}
		if kind == "ring" && len(add) > 2 {
			link(add[len(add)-1], add[0])
		}
	}
}

// Fill puts a VM's values into a day-0 command.
func Fill(cmd string, v VM, n int) string {
	addr, bits := v.MgmtIP, ""
	if p, err := netip.ParsePrefix(v.MgmtIP); err == nil {
		addr, bits = p.Addr().String(), strconv.Itoa(p.Bits())
	}
	mask := ""
	if b, err := strconv.Atoi(bits); err == nil {
		m := uint32(0xffffffff) << (32 - b)
		mask = fmt.Sprintf("%d.%d.%d.%d", m>>24, m>>16&255, m>>8&255, m&255)
	}
	r := strings.NewReplacer("{hostname}", v.Name, "{mgmtIp}", v.MgmtIP, "{mgmtAddr}", addr, "{mgmtBits}", bits, "{mgmtMask}", mask, "{gateway}", v.Gateway, "{n}", strconv.Itoa(n))
	return r.Replace(cmd)
}

// Q quotes for a POSIX shell (paths relative to the home folder stay unquoted when safe).
func Q(s string) string {
	if regexp.MustCompile(`^[A-Za-z0-9_./:=,@+-]+$`).MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func joinArgs(a []string) string {
	q := make([]string, len(a))
	for i, s := range a {
		q[i] = Q(s)
	}
	return strings.Join(q, " ")
}

// splitArgs splits options at spaces, keeping "quoted parts" together.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '"' || c == '\''):
			quote = c
		case quote == 0 && (c == ' ' || c == '\t' || c == '\n'):
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func orInt(v ...int) int {
	for _, x := range v {
		if x > 0 {
			return x
		}
	}
	return 0
}

// SortVMs orders VMs by index.
func SortVMs(v []VM) { sort.Slice(v, func(i, j int) bool { return v[i].Index < v[j].Index }) }

// NextSerial is the serial number i steps after start: its last number
// counts up, keeping its width ("AR022205Q-40036", 2 → "AR022205Q-40038").
func NextSerial(start string, i int) string {
	if start == "" || i == 0 {
		return start
	}
	j := len(start)
	for j > 0 && start[j-1] >= '0' && start[j-1] <= '9' {
		j--
	}
	digits := start[j:]
	if digits == "" {
		return fmt.Sprintf("%s-%d", start, i)
	}
	n, _ := strconv.ParseUint(digits, 10, 64)
	return start[:j] + fmt.Sprintf("%0*d", len(digits), n+uint64(i))
}

// NextMAC is the base MAC of the i-th switch from start: each switch keeps a
// block of 64 addresses (its ports take the first ones).
func NextMAC(start string, i int) string {
	hw, err := net.ParseMAC(start)
	if err != nil || len(hw) != 6 {
		return ""
	}
	x := uint64(hw[0])<<40 | uint64(hw[1])<<32 | uint64(hw[2])<<24 | uint64(hw[3])<<16 | uint64(hw[4])<<8 | uint64(hw[5])
	x += uint64(i) * 64
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", byte(x>>40), byte(x>>32), byte(x>>24), byte(x>>16), byte(x>>8), byte(x))
}
