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
		p = append(p, "apt-get install qemu-system-x86 qemu-utils")
	case "dnf-fedora":
		p = append(p, "dnf install qemu-system-x86-core qemu-img")
	case "dnf":
		p = append(p, "dnf install qemu-kvm qemu-img, and qemu-system-x86_64 as a link to /usr/libexec/qemu-kvm")
	case "zypper":
		p = append(p, "zypper install qemu-x86 qemu-tools")
	case "pacman":
		p = append(p, "pacman -S qemu-base")
	default:
		p = append(p, "install QEMU (the distribution "+orStr(h.OSID, "is unknown")+" is not supported - install qemu-system-x86_64 and qemu-img by hand)")
	}
	if h.KVM == "none" && h.VT {
		p = append(p, "load the KVM module (modprobe kvm_intel or kvm_amd)")
	}
	if h.KVM != "yes" {
		p = append(p, "add "+orStr(h.User, "the user")+" to the kvm group (usermod -aG kvm) and let the group use /dev/kvm")
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
  apt-get install -y -q qemu-system-x86 qemu-utils || { echo "@@fail apt-get install failed"; exit 1; } ;;
 *" fedora "*)
  dnf install -y qemu-system-x86-core qemu-img || { echo "@@fail dnf install failed"; exit 1; } ;;
 *" rhel "*|*" centos "*|*" rocky "*|*" almalinux "*|*" ol "*)
  dnf install -y qemu-kvm qemu-img || yum install -y qemu-kvm qemu-img || { echo "@@fail dnf install failed"; exit 1; }
  if ! command -v qemu-system-x86_64 >/dev/null 2>&1 && [ -x /usr/libexec/qemu-kvm ]; then ln -s /usr/libexec/qemu-kvm /usr/local/bin/qemu-system-x86_64; echo "@@note qemu-system-x86_64 is a link to /usr/libexec/qemu-kvm"; fi ;;
 *" suse "*|*" opensuse "*|*" sles "*)
  zypper -n install qemu-x86 qemu-tools || { echo "@@fail zypper install failed"; exit 1; } ;;
 *" arch "*)
  pacman -S --noconfirm --needed qemu-base || { echo "@@fail pacman failed"; exit 1; } ;;
 *) echo "@@fail this Linux ($ID) is not supported: install qemu-system-x86_64 and qemu-img by hand"; exit 1 ;;
esac
command -v qemu-system-x86_64 >/dev/null 2>&1 || { echo "@@fail qemu-system-x86_64 is still missing"; exit 1; }
echo "@@step kvm"
if [ ! -e /dev/kvm ]; then
 if grep -q vmx /proc/cpuinfo; then modprobe kvm_intel; elif grep -q svm /proc/cpuinfo; then modprobe kvm_amd; fi
fi
if [ ! -e /dev/kvm ]; then echo "@@note No /dev/kvm: CPU virtualization is off (BIOS/UEFI, or nested virtualization for a VM). The switches will run in software emulation, many times slower."; fi
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
