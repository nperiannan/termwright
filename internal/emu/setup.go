package emu

import "strings"

// Setting up a KVM host: QEMU from the distribution's packages, the KVM
// module, and the user in the kvm group. Everything runs as root in one sudo
// (the password, when sudo asks for one, goes to sudo on its input).

// SetupPlan says what SetupScript would do on a host, for the user to read first.
func SetupPlan(h HostInfo) []string {
	var p []string
	switch pm := h.Pkg; pm {
	case "apt":
		p = append(p, "apt-get install qemu-system-x86 qemu-utils libguestfs-tools, and make /boot/vmlinuz-* readable (libguestfs needs it)")
	case "dnf-fedora":
		p = append(p, "dnf install qemu-system-x86-core qemu-img guestfs-tools")
	case "dnf":
		p = append(p, "dnf install qemu-kvm qemu-img guestfs-tools, and qemu-system-x86_64 as a link to /usr/libexec/qemu-kvm")
	case "zypper":
		p = append(p, "zypper install qemu-x86 qemu-tools guestfs-tools")
	case "pacman":
		p = append(p, "pacman -S qemu-base libguestfs")
	default:
		p = append(p, "install QEMU (the distribution "+orStr(h.OSID, "is unknown")+" is not supported - install qemu-system-x86_64 and qemu-img by hand)")
	}
	if h.Nat == "" {
		p = append(p, "install libvirt and start its default NAT network (virbr0, with DHCP) - switches get addresses there like on GNS3's NAT node")
	}
	if h.KVM == "none" && h.VT {
		p = append(p, "load the KVM module (modprobe kvm_intel or kvm_amd)")
	}
	if h.KVM != "yes" {
		p = append(p, "add "+orStr(h.User, "the user")+" to the kvm group (usermod -aG kvm) and let the group use /dev/kvm")
	}
	if !h.Sudo {
		p = append(p, "let "+orStr(h.User, "the user")+" run the network commands the switches need (ip, iptables, sysctl, virsh, arping, and chown of /dev/tap*) with sudo without a password - only these, in /etc/sudoers.d/termwright")
	}
	return p
}

// SetupScript is the root part (run as: sudo sh -c <script>, with TWUSER set to the login user).
// It prints "@@step <name>", "@@note <text>", "@@fail <text>" and "@@done".
func SetupScript() string {
	return strings.TrimSpace(`
. /etc/os-release 2>/dev/null
ids=" $ID $ID_LIKE "
echo "@@step packages"
case "$ids" in
 *" debian "*|*" ubuntu "*)
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -q || { echo "@@fail apt-get update failed"; exit 1; }
  apt-get install -y -q qemu-system-x86 qemu-utils libguestfs-tools || { echo "@@fail apt-get install failed"; exit 1; } ;;
 *" fedora "*)
  dnf install -y qemu-system-x86-core qemu-img guestfs-tools || { echo "@@fail dnf install failed"; exit 1; } ;;
 *" rhel "*|*" centos "*|*" rocky "*|*" almalinux "*|*" ol "*)
  dnf install -y qemu-kvm qemu-img guestfs-tools || yum install -y qemu-kvm qemu-img libguestfs-tools-c || { echo "@@fail dnf install failed"; exit 1; }
  if ! command -v qemu-system-x86_64 >/dev/null 2>&1 && [ -x /usr/libexec/qemu-kvm ]; then ln -s /usr/libexec/qemu-kvm /usr/local/bin/qemu-system-x86_64; echo "@@note qemu-system-x86_64 is a link to /usr/libexec/qemu-kvm"; fi ;;
 *" suse "*|*" opensuse "*|*" sles "*)
  zypper -n install qemu-x86 qemu-tools guestfs-tools || { echo "@@fail zypper install failed"; exit 1; } ;;
 *" arch "*)
  pacman -S --noconfirm --needed qemu-base libguestfs || { echo "@@fail pacman failed"; exit 1; } ;;
 *) echo "@@fail this Linux ($ID) is not supported: install qemu-system-x86_64 and qemu-img by hand"; exit 1 ;;
esac
command -v qemu-system-x86_64 >/dev/null 2>&1 || { echo "@@fail qemu-system-x86_64 is still missing"; exit 1; }
# libguestfs (persona files) reads the host's kernel, which Ubuntu keeps readable by root only
chmod 0644 /boot/vmlinuz-* 2>/dev/null || true
echo "@@step nat"
# libvirt's default network: a NAT bridge (virbr0) with DHCP for the switches' management ports
if ! ip link show virbr0 >/dev/null 2>&1; then
 case "$ids" in
  *" debian "*|*" ubuntu "*) apt-get install -y -q libvirt-daemon-system dnsmasq-base ;;
  *" fedora "*|*" rhel "*|*" centos "*|*" rocky "*|*" almalinux "*|*" ol "*) dnf install -y libvirt-daemon-config-network libvirt-daemon-kvm || yum install -y libvirt-daemon-config-network libvirt-daemon-kvm ;;
  *" suse "*|*" opensuse "*|*" sles "*) zypper -n install libvirt-daemon-config-network libvirt-daemon-qemu ;;
  *" arch "*) pacman -S --noconfirm --needed libvirt dnsmasq ;;
 esac
 systemctl enable --now libvirtd >/dev/null 2>&1 || systemctl enable --now virtqemud.socket virtnetworkd.socket >/dev/null 2>&1 || true
 virsh -c qemu:///system net-info default >/dev/null 2>&1 || { [ -f /usr/share/libvirt/networks/default.xml ] && virsh -c qemu:///system net-define /usr/share/libvirt/networks/default.xml >/dev/null; }
 virsh -c qemu:///system net-start default >/dev/null 2>&1
 virsh -c qemu:///system net-autostart default >/dev/null 2>&1
 if ip link show virbr0 >/dev/null 2>&1; then echo "@@note NAT network virbr0: $(ip -4 -o addr show dev virbr0 | awk '{print $4}')"; else echo "@@note libvirt's NAT network could not be started: NAT management is not available (the other modes are)"; fi
fi
echo "@@step kvm"
if [ ! -e /dev/kvm ]; then
 if grep -q vmx /proc/cpuinfo; then modprobe kvm_intel; elif grep -q svm /proc/cpuinfo; then modprobe kvm_amd; fi
fi
if [ ! -e /dev/kvm ]; then echo "@@note No /dev/kvm: CPU virtualization is off (BIOS/UEFI, or nested virtualization for a VM). The switches will run in software emulation, many times slower."; fi
echo "@@step sudo"
# only the network commands the switches need, without a password (checked with visudo before it is used)
if [ -n "$TWUSER" ] && [ "$TWUSER" != root ] && [ ! -f /etc/sudoers.d/termwright ]; then
 cmds=""; for c in ip iptables sysctl virsh arping; do p=$(command -v $c 2>/dev/null || ls /usr/sbin/$c /sbin/$c 2>/dev/null | head -1); [ -n "$p" ] && cmds="$cmds${cmds:+, }$p"; done
 ch=$(command -v chown); cmds="$cmds, $ch $TWUSER /dev/tap*"
 [ -d /var/lib/libvirt/dnsmasq ] && cmds="$cmds, $(command -v cat) /var/lib/libvirt/dnsmasq/*"
 t=$(mktemp); printf '# Termwright: the network commands of its emulated switches\n%s ALL=(root) NOPASSWD: %s\n' "$TWUSER" "$cmds" > "$t"
 if visudo -cf "$t" >/dev/null 2>&1; then install -m 0440 "$t" /etc/sudoers.d/termwright && echo "@@note $TWUSER may run $cmds with sudo without a password (/etc/sudoers.d/termwright)"; else echo "@@note the sudo rule did not pass visudo: not written"; fi
 rm -f "$t"
fi
echo "@@step group"
getent group kvm >/dev/null 2>&1 || groupadd -r kvm
if [ -n "$TWUSER" ] && [ "$TWUSER" != root ]; then usermod -aG kvm "$TWUSER" || { echo "@@fail usermod failed"; exit 1; }; fi
if [ -e /dev/kvm ]; then chgrp kvm /dev/kvm && chmod 660 /dev/kvm; fi
echo "@@done"`)
}

// SetupCommand runs SetupScript as root: with sudo -n, or sudo -S reading the password from the input.
func SetupCommand(password bool) string {
	sudo := "sudo -n"
	if password {
		sudo = "sudo -S -p ''"
	}
	return `TWUSER=$(id -un); export TWUSER; if [ "$(id -u)" = 0 ]; then sh -c ` + Q(SetupScript()) + `; else ` + sudo + ` env TWUSER="$TWUSER" sh -c ` + Q(SetupScript()) + `; fi`
}
