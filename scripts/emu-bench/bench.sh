#!/bin/sh
# Test bench for TestEmuModes (as root): sh bench.sh, then
#   TW_EMU_BENCH=netnos-initrd.gz TW_EMU_KERNEL=/boot/vmlinuz-$(uname -r) TW_EMU_SSH_KEY=<key> go test -run TestEmuModes .
# (the host sshd config and key: see emu_app_test.go TestEmuLab)
# A host (netns twh, reached on 10.77.0.2:2299) on a LAN (netns twl, 10.50.0.0/24, gateway .254, a server .142, DHCP .100-.120)
for n in twh twl; do ip netns del $n 2>/dev/null; done; ip link del twr0 2>/dev/null
ip netns add twh; ip netns add twl
ip link add twr0 type veth peer name h0 netns twh
ip addr add 10.77.0.1/30 dev twr0; ip link set twr0 up
ip -n twh addr add 10.77.0.2/30 dev h0; ip -n twh link set h0 up; ip -n twh link set lo up
ip link add up0 netns twh type veth peer name lan0 netns twl
ip -n twh addr add 10.50.0.20/24 dev up0; ip -n twh link set up0 up; ip -n twh route add default via 10.50.0.254
ip -n twl addr add 10.50.0.254/24 dev lan0; ip -n twl addr add 10.50.0.142/24 dev lan0; ip -n twl link set lan0 up; ip -n twl link set lo up
mkdir -p /tmp/twb; cat > /tmp/twb/udhcpd-lan.conf <<C
start 10.50.0.100
end 10.50.0.120
interface lan0
option subnet 255.255.255.0
option router 10.50.0.254
lease_file /tmp/twb/lan.leases
pidfile /tmp/twb/udhcpd-lan.pid
auto_time 2
C
touch /tmp/twb/lan.leases; ip netns exec twl busybox udhcpd /tmp/twb/udhcpd-lan.conf
ip netns exec twl sh -c 'busybox httpd -p 10.50.0.142:80 -h /tmp/twb; echo server > /tmp/twb/index.html'
# NAT network like libvirt's default: virbr0 192.168.122.1/24 with DHCP and masquerade
ip -n twh link add virbr0 type bridge; ip -n twh addr add 192.168.122.1/24 dev virbr0; ip -n twh link set virbr0 up
cat > /tmp/twb/udhcpd-nat.conf <<C
start 192.168.122.100
end 192.168.122.150
interface virbr0
option subnet 255.255.255.0
option router 192.168.122.1
lease_file /tmp/twb/nat.leases
pidfile /tmp/twb/udhcpd-nat.pid
auto_time 2
C
touch /tmp/twb/nat.leases; ip netns exec twh busybox udhcpd /tmp/twb/udhcpd-nat.conf
ip netns exec twh sysctl -qw net.ipv4.ip_forward=1
ip netns exec twh iptables -t nat -A POSTROUTING -s 192.168.122.0/24 ! -d 192.168.122.0/24 -j MASQUERADE
# libvirt's lease file, from udhcpd's
mkdir -p /var/lib/libvirt/dnsmasq
(while [ -e /tmp/twb/udhcpd-nat.pid ]; do busybox dumpleases -f /tmp/twb/nat.leases 2>/dev/null | awk 'BEGIN{print "["} NR>1{printf "%s  {\n    \"ip-address\": \"%s\",\n    \"mac-address\": \"%s\"\n  }", (n++?",\n":""), $2, $1} END{print "\n]"}' > /var/lib/libvirt/dnsmasq/virbr0.status; sleep 2; done) &
# the host's SSH
ip netns exec twh /usr/sbin/sshd -f /home/claude/emu-test/sshd/sshd_config -o ListenAddress=10.77.0.2 -o PidFile=/tmp/twb/sshd.pid
echo bench up
