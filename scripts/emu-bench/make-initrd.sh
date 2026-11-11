#!/bin/sh
# Builds netnos-initrd.gz: a tiny "switch" for TestEmuModes (busybox, e1000,
# a login on the serial console, DHCP, telnetd, httpd and an SSH banner).
set -e
here=$(cd "$(dirname "$0")" && pwd); out=${1:-$PWD/netnos-initrd.gz}
k=${KVER:-$(uname -r)}; t=$(mktemp -d); r=$t/root # KVER: the kernel the test boots
mkdir -p $r/bin $r/proc $r/dev $r/sys $r/www $r/usr/share/udhcpc $r/lib $r/tmp $r/etc
cp "$(command -v busybox)" $r/bin/
for c in sh ip udhcpc telnetd httpd nc insmod sleep cat printf echo mount ifconfig route ping hostname awk killall grep; do ln -sf busybox $r/bin/$c; done
unzstd -q -o $r/lib/e1000.ko /lib/modules/$k/kernel/drivers/net/ethernet/intel/e1000/e1000.ko.zst
echo '<h1>netnos</h1>' > $r/www/index.html
cp "$here/netnos-init" $r/init; cp "$here/udhcpc.script" $r/usr/share/udhcpc/default.script; chmod +x $r/init $r/usr/share/udhcpc/default.script
(cd $r && find . | cpio -o -H newc 2>/dev/null | gzip > "$out")
rm -rf $t; echo "$out"
