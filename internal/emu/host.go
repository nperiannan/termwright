package emu

import (
	"fmt"
	"strconv"
	"strings"
)

// HostInfo is what a KVM host offers.
type HostInfo struct {
	User     string   `json:"user"`
	OS       string   `json:"os"`
	Kernel   string   `json:"kernel"`
	Arch     string   `json:"arch"`
	KVM      string   `json:"kvm"` // yes, noaccess, none
	VT       bool     `json:"vt"`  // the CPU has virtualization on
	QEMU     string   `json:"qemu"`
	QemuImg  bool     `json:"qemuImg"`
	CPUs     int      `json:"cpus"`
	Load     float64  `json:"load"`
	MemTotal int64    `json:"memTotal"` // MB
	MemFree  int64    `json:"memFree"`  // MB
	DiskFree int64    `json:"diskFree"` // MB, in the lab folder
	Sudo     bool     `json:"sudo"`     // password-less sudo (bridge mode)
	Dir      string   `json:"dir"`
	OSID     string   `json:"osId"`     // ID and ID_LIKE of /etc/os-release
	Pkg      string   `json:"pkg"`      // the package manager Set Up uses (apt, dnf, dnf-fedora, zypper, pacman)
	Plan     []string `json:"plan"`     // what Set Up would do
	CanSetup bool     `json:"canSetup"` // Set Up can fix the problems
	Uplink   string   `json:"uplink"`   // the port of the default route
	Bridges  []string `json:"bridges"`  // the host's bridges with a network port in them
	Wifi     bool     `json:"wifi"`     // the uplink is Wi-Fi
	LanIf    string   `json:"lanIf"`    // where switches join the host's network: a bridge, or the uplink (macvtap)
	LanKind  string   `json:"lanKind"`  // "bridge" or "macvtap" ("" = not possible)
	Guestfs  bool     `json:"guestfs"`  // libguestfs is there (persona files)
	Problems []string `json:"problems"` // what stops VMs from running
	Notes    []string `json:"notes"`    // what works, but differently
}

// ProbeScript checks a host (dir: the lab folder, relative to the home folder).
func ProbeScript(dir string) string {
	return `echo "user=$(id -un)"
. /etc/os-release 2>/dev/null; echo "os=$PRETTY_NAME"; echo "osid=$ID $ID_LIKE"; echo "kernel=$(uname -r)"; echo "arch=$(uname -m)"
if [ -e /dev/kvm ]; then if [ -r /dev/kvm ] && [ -w /dev/kvm ]; then echo kvm=yes; else echo kvm=noaccess; fi; else echo kvm=none; fi
grep -qE 'vmx|svm' /proc/cpuinfo 2>/dev/null && echo vt=yes
echo "qemu=$(qemu-system-x86_64 --version 2>/dev/null | head -1)"
command -v qemu-img >/dev/null 2>&1 && echo qemuimg=yes
echo "cpus=$(nproc 2>/dev/null)"; echo "load=$(cut -d' ' -f1 /proc/loadavg 2>/dev/null)"
awk '/^MemTotal:/{print "memtotal="int($2/1024)} /^MemAvailable:/{print "memfree="int($2/1024)}' /proc/meminfo 2>/dev/null
mkdir -p ` + Q(dir) + ` 2>/dev/null && echo "diskfree=$(df -Pk ` + Q(dir) + ` | awk 'NR==2{print int($4/1024)}')"
sudo -n true 2>/dev/null && echo sudo=yes
command -v base64 >/dev/null 2>&1 || echo nobase64=yes
command -v guestfish >/dev/null 2>&1 && echo guestfs=yes
dev=$(ip -o route get 1.1.1.1 2>/dev/null | sed -n 's/.* dev \([^ ]*\).*/\1/p'); echo "uplink=$dev"
[ -n "$dev" ] && [ -d "/sys/class/net/$dev/wireless" ] && echo wifi=yes
for b in /sys/class/net/*/bridge; do [ -d "$b" ] || continue; n=$(basename "$(dirname "$b")"); echo "bridge=$n:$(ls "/sys/class/net/$n/brif" 2>/dev/null | tr '\n' ',')"; done
`
}

// ParseProbe reads ProbeScript's output.
func ParseProbe(out, dir string) HostInfo {
	h := HostInfo{Dir: dir}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "user":
			h.User = v
		case "os":
			h.OS = v
		case "kernel":
			h.Kernel = v
		case "arch":
			h.Arch = v
		case "kvm":
			h.KVM = v
		case "vt":
			h.VT = true
		case "qemu":
			h.QEMU = strings.TrimSpace(strings.TrimPrefix(v, "QEMU emulator version"))
		case "qemuimg":
			h.QemuImg = true
		case "cpus":
			h.CPUs, _ = strconv.Atoi(v)
		case "load":
			h.Load, _ = strconv.ParseFloat(v, 64)
		case "memtotal":
			h.MemTotal, _ = strconv.ParseInt(v, 10, 64)
		case "memfree":
			h.MemFree, _ = strconv.ParseInt(v, 10, 64)
		case "diskfree":
			h.DiskFree, _ = strconv.ParseInt(v, 10, 64)
		case "sudo":
			h.Sudo = true
		case "guestfs":
			h.Guestfs = true
		case "uplink":
			h.Uplink = v
		case "wifi":
			h.Wifi = true
		case "bridge":
			name, members, _ := strings.Cut(v, ":")
			for _, m := range strings.Split(members, ",") {
				// a bridge with a real port in it (not Termwright's, libvirt's or Docker's own)
				if m != "" && !strings.HasPrefix(m, "tw") && !strings.HasPrefix(m, "vnet") && !strings.HasPrefix(m, "veth") && !strings.HasPrefix(m, "tap") {
					h.Bridges = append(h.Bridges, name)
					break
				}
			}
		case "osid":
			h.OSID = strings.TrimSpace(v)
		case "nobase64":
			h.Problems = append(h.Problems, "base64 is missing on the host (coreutils).")
		}
	}
	if h.Arch != "" && h.Arch != "x86_64" {
		h.Problems = append(h.Problems, "The host is "+h.Arch+"; network OS images need an x86_64 host.")
	}
	if h.QEMU == "" {
		h.Problems = append(h.Problems, "QEMU is not installed: install qemu-system-x86 and qemu-utils (apt) or qemu-kvm (dnf).")
	} else if !h.QemuImg {
		h.Problems = append(h.Problems, "qemu-img is missing: install qemu-utils (apt) or qemu-img (dnf).")
	}
	switch h.KVM {
	case "noaccess":
		h.Problems = append(h.Problems, fmt.Sprintf("%s may not use /dev/kvm: add the user to the kvm group (sudo usermod -aG kvm %s), then log in again.", orStr(h.User, "The user"), orStr(h.User, "$USER")))
	case "none":
		if h.VT {
			h.Problems = append(h.Problems, "/dev/kvm is missing: load the kvm module (sudo modprobe kvm_intel or kvm_amd).")
		} else {
			h.Notes = append(h.Notes, "No KVM (CPU virtualization is off or this is a VM without nested virtualization): VMs run in software emulation, many times slower.")
		}
	}
	ids := " " + h.OSID + " "
	switch {
	case strings.Contains(ids, " debian ") || strings.Contains(ids, " ubuntu "):
		h.Pkg = "apt"
	case strings.Contains(ids, " fedora ") && !strings.Contains(ids, " rhel "):
		h.Pkg = "dnf-fedora"
	case strings.Contains(ids, " rhel ") || strings.Contains(ids, " centos ") || strings.Contains(ids, " rocky ") || strings.Contains(ids, " almalinux "):
		h.Pkg = "dnf"
	case strings.Contains(ids, " suse ") || strings.Contains(ids, " opensuse ") || strings.Contains(ids, " sles "):
		h.Pkg = "zypper"
	case strings.Contains(ids, " arch "):
		h.Pkg = "pacman"
	}
	for _, b := range h.Bridges {
		if b == h.Uplink {
			h.LanIf, h.LanKind = b, "bridge" // the host's own address is on a bridge: switches join it
		}
	}
	if h.LanKind == "" && h.Uplink != "" && !h.Wifi {
		h.LanIf, h.LanKind = h.Uplink, "macvtap" // straight onto the network port, the host's set-up untouched
	}
	if !h.Guestfs && h.QEMU != "" {
		h.Notes = append(h.Notes, "libguestfs-tools is not installed: persona files (a device model and serial number for the image) cannot be written. Set Up installs it.")
	}
	needs := !h.Guestfs || h.QEMU == "" || !h.QemuImg || h.KVM == "noaccess" || h.KVM == "none" && h.VT
	if needs && h.Arch == "x86_64" || needs && h.Arch == "" {
		h.CanSetup = h.Pkg != "" || h.QEMU != ""
		h.Plan = SetupPlan(h)
	}
	if !h.Sudo {
		h.Notes = append(h.Notes, "No password-less sudo: management by port forwarding on the host. With sudo, switches can get their own addresses on a bridge.")
	}
	return h
}

// OK says the host can run VMs.
func (h HostInfo) OK() bool { return len(h.Problems) == 0 && h.User != "" }

// Fits is how many VMs of an image the host has room for.
func (h HostInfo) Fits(im *Image) int {
	ram := orInt(im.RAM, 1024)
	n := int(h.MemFree) / ram
	if c := orInt(im.CPUs, 1); h.CPUs > 0 && h.CPUs*2/c < n {
		n = h.CPUs * 2 / c // two vCPUs per core at most
	}
	if n < 0 {
		n = 0
	}
	return n
}
