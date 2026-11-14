package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"net"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/secrets"
)

// The emulated lab for an AI agent (the Termwright connector): enough to take
// a test case from a topology to a result - see the images, build switches and
// cables, start them and wait until they are ready, type configuration and
// show commands on their consoles, check the cabling against LLDP, move a
// switch to another host, and read the Events. Lab changes are not asked
// (automated tests must not wait); each is kept in the lab's change history.

var mcpLabTools = []mcpTool{
	{"lab_dns", "The emulated lab's name servers: computers without one get them when they start, images' day 0 fills {dns} {dns2} {dnsList} with them, Alpine ISO installs use the first. Without servers it says what they are; with servers it sets them (at most three addresses; an empty list = the default 8.8.8.8 1.1.1.1). Running devices keep theirs until they restart.",
		mcpSchema(map[string]any{"servers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "the name servers to set (IPv4 or IPv6 addresses)"}})},
	{"lab_checkpoint", "Checkpoints of a running switch: its whole state (configuration, tables, sessions) kept in its disk, restored in seconds - like a snapshot, while it runs. Actions: list (a switch's checkpoints), take (now; note says why), restore (tag: back to that moment), remove (tag), auto (on: Termwright takes one before Claude or the API types on a switch, so lab_history revert undoes typed commands; off: none). Taking one pauses the switch for a moment.",
		mcpSchema(map[string]any{
			"action": map[string]any{"type": "string", "enum": []string{"list", "take", "restore", "remove", "auto"}},
			"switch": map[string]any{"type": "string"},
			"tag":    map[string]any{"type": "string", "description": "restore, remove: the checkpoint (from list)"},
			"note":   map[string]any{"type": "string", "description": "take: why"},
			"on":     map[string]any{"type": "boolean", "description": "auto: on or off"},
		})},
	{"lab_keys", "Type on the keyboard of a lab device's screen (VGA display: installers and systems without a serial console), or a line on an installer's serial console. switch: the device; install: an install's id (lab_install_status) instead. text is typed as keys (then Enter with enter); keys are QEMU key names (ret, esc, tab, up, down, f2, ctrl-alt-delete …). console: true types the line on the installer's serial console instead of its keyboard. The screen itself: GET /api/v1/lab/screen/{device} (or install:{id}) gives a PNG.",
		mcpSchema(map[string]any{
			"switch":  map[string]any{"type": "string"},
			"install": map[string]any{"type": "string"},
			"text":    map[string]any{"type": "string"},
			"enter":   map[string]any{"type": "boolean"},
			"keys":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"console": map[string]any{"type": "boolean", "description": "install: the serial console, not the keyboard"},
		})},
	{"lab_hub", "Hubs and L2 switches of the emulated lab: nodes without a VM that join switch ports (a hub repeats every frame to its other ports; an L2 switch learns MAC addresses). Ports go on at once, also on running switches; a hub's switches must run on one host. Actions: list, add (kind hub or switch, name), connect (hub, switch, port), disconnect (hub, switch, port), remove (hub).",
		mcpSchema(map[string]any{
			"action": map[string]any{"type": "string", "enum": []string{"list", "add", "connect", "disconnect", "remove"}},
			"kind":   map[string]any{"type": "string", "enum": []string{"hub", "switch"}},
			"name":   map[string]any{"type": "string", "description": "add: its name (default hub-1, l2sw-1 …)"},
			"hub":    map[string]any{"type": "string", "description": "the hub (name or id)"},
			"switch": map[string]any{"type": "string"},
			"port":   map[string]any{"type": "string", "description": "the switch's port (as the switch names it)"},
		})},
	{"lab_images", "List the images and templates the emulated lab can make switches from: id, name, software version, device type, port naming, and whether the image file is there. Filter by name and device type to keep the answer short.", mcpSchema(map[string]any{
		"name": map[string]any{"type": "string", "description": "only images and templates whose name, id or version contains this (any case)"},
		"type": map[string]any{"type": "string", "description": "only this device type (EXOS, VOSS, Linux, Windows …; any case)"},
	})},
	{"lab_install_iso", "Make an image from an ISO file on the computer Termwright runs on: the ISO is copied to a KVM host and installed on a blank disk there, then the disk becomes an image (lab_images lists it). mode alpine installs Alpine Linux by itself (give the root password); mode manual starts the installer and waits for the user to install on its console, then lab_install_finish saves it. Returns at once; follow it with lab_install_status. It is not asked; it is kept in the lab's change history (lab_history).",
		mcpSchema(map[string]any{
			"iso_path": map[string]any{"type": "string", "description": "path of the .iso file on the Termwright computer"},
			"name":     map[string]any{"type": "string", "description": "the new image's name (default: the ISO's name)"},
			"mode":     map[string]any{"type": "string", "enum": []string{"alpine", "manual"}, "description": "alpine = typed by Termwright; manual = the user installs"},
			"password": map[string]any{"type": "string", "description": "root password for the new system (alpine: required)"},
			"disk_gb":  map[string]any{"type": "integer", "description": "disk size in GB (default 8)"},
			"disk_mb":  map[string]any{"type": "integer", "description": "disk size in MB, for a size that is not whole GB (used instead of disk_gb)"},
			"ram_mb":   map[string]any{"type": "integer", "description": "memory for the installer (default 1024)"},
			"cpus":     map[string]any{"type": "integer", "description": "CPUs for the installer (default 2)"},
			"host":     map[string]any{"type": "string", "description": "KVM host name or session path (default: the lab's host)"},
		}, "iso_path", "mode")},
	{"lab_install_status", "Show the installs from ISO files: id, name, stage (copying, starting, installing, waiting, saving, fetching, done, failed, cancelled), what it does now, the end of the installer's console, the image it made.", mcpSchema(map[string]any{})},
	{"lab_install_finish", "End a manual install (stage waiting): the machine is shut down and its disk becomes an image. It is not asked; it is kept in the lab's change history (lab_history).",
		mcpSchema(map[string]any{"id": map[string]any{"type": "string", "description": "the install's id (lab_install_status)"}}, "id")},
	{"lab_add_switches", "Add switches to the emulated lab from an image or a template (ids from lab_images), optionally on a given KVM host and started. It is not asked; it is kept in the lab's change history (lab_history).",
		mcpSchema(map[string]any{
			"image":    map[string]any{"type": "string", "description": "image id (or give template)"},
			"template": map[string]any{"type": "string", "description": "template id (its image, resources and persona)"},
			"model":    map[string]any{"type": "string", "description": "persona model of the image, e.g. 5520-24T (optional)"},
			"count":    map[string]any{"type": "integer", "description": "how many (default 1, at most 32)"},
			"names":    map[string]any{"type": "string", "description": "their names, comma separated, or a pattern like leaf-{n} (optional)"},
			"host":     map[string]any{"type": "string", "description": "KVM host name or session path (default: the lab's host)"},
			"start":    map[string]any{"type": "boolean", "description": "start them at once"},
		})},
	{"lab_cable", "Connect (or remove) cables between switches of the emulated lab: switch names and port names as the switches call them (show_lab). A whole list at once; the user is asked first. New cables work at once between running switches on one host (between two hosts, once both have restarted); removed ones are pulled at once.",
		mcpSchema(map[string]any{
			"cables": map[string]any{"type": "array", "description": "cables: {a, a_port, b, b_port}; for remove only a and a_port", "items": map[string]any{"type": "object", "properties": map[string]any{
				"a": map[string]any{"type": "string"}, "a_port": map[string]any{"type": "string"}, "b": map[string]any{"type": "string"}, "b_port": map[string]any{"type": "string"}}}},
			"remove": map[string]any{"type": "boolean", "description": "remove these cables instead"},
		}, "cables")},
	{"lab_power", "Start, stop, restart or shut down switches of the emulated lab (names, or all). It is not asked; it is kept in the lab's change history (lab_history).",
		mcpSchema(map[string]any{
			"switches": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "switch names; empty = all"},
			"action":   map[string]any{"type": "string", "enum": []string{"start", "stop", "restart", "shutdown"}},
		}, "action")},
	{"lab_wait", "Wait until switches of the emulated lab are booted and configured (running, no step left), or the time is up; returns each one's state and problem.",
		mcpSchema(map[string]any{
			"switches": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "switch names; empty = all"},
			"timeout":  map[string]any{"type": "integer", "description": "seconds (default 600, at most 1800)"},
		})},
	{"lab_console", "Type commands on a switch's serial console (Termwright logs in with the image's login) and return each one's output: configuration or show commands, in the switch's own CLI. It is not asked; it is kept in the lab's change history (lab_history). Close the switch's console tab first.",
		mcpSchema(map[string]any{
			"switch":   map[string]any{"type": "string"},
			"commands": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "command lines, in order"},
			"timeout":  map[string]any{"type": "integer", "description": "seconds for all of them (default 180, at most 900)"},
		}, "switch", "commands")},
	{"lab_check_cabling", "Compare the switches' LLDP neighbours with the cables of the emulated lab (the switches must be running): each cable ok, wrong (what the port sees) or without a neighbour.", mcpSchema(map[string]any{})},
	{"lab_move", "Move a switch of the emulated lab to another KVM host: live (it keeps running), copy (saved, its disk copied, resumed) or image (boots there from day 0). It is not asked; it is kept in the lab's change history (lab_history).",
		mcpSchema(map[string]any{
			"switch": map[string]any{"type": "string"},
			"host":   map[string]any{"type": "string", "description": "KVM host name or session path"},
			"how":    map[string]any{"type": "string", "enum": []string{"live", "copy", "image"}},
		}, "switch", "host", "how")},
	{"lab_traffic", "The emulated lab's traffic generator: test ports on free switch ports (a sender and counter, no VM; it answers ARP and ping at its address) and streams of frames between them (udp, icmp, tcp SYN, arp, raw; VLAN tag, size, packets per second, seconds or count, pass when loss and latency are within limits). Actions: list, add_port, remove_port, add_stream, remove_stream, start, stop, report (pass/fail per stream with sent, received, loss, latency min/avg/p50/p95/p99/max, and per flow for streams with changing fields). Rates in pps, Mbit/s or % of line rate; round_trip streams measure latency across hosts. Kind pcap replays a capture file's frames (counted at the receiver by their contents). templates lists a library of ready tests (unicast, IMIX, QoS classes, MAC table fill, round trip, broadcast, RFC 2544/2889); apply_template puts one on the test ports given for its roles; save_template keeps the lab's streams and tests as a template of yours. Changes and starts are not asked; they are kept in the lab's change history (lab_history).",
		mcpSchema(map[string]any{
			"action":       map[string]any{"type": "string", "enum": []string{"list", "add_port", "set_port", "remove_port", "add_stream", "remove_stream", "start", "stop", "report", "templates", "apply_template", "save_template", "remove_template"}},
			"template":     map[string]any{"type": "string", "description": "apply_template, remove_template: the template (name or id); save_template: its name"},
			"ports":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "apply_template: the test ports (names) for its roles A, B, C… in order"},
			"description":  map[string]any{"type": "string", "description": "save_template: what it checks"},
			"pcap":         map[string]any{"type": "string", "description": "add_stream kind pcap: the capture file on this computer (classic pcap)"},
			"pcap_timing":  map[string]any{"type": "boolean", "description": "add_stream kind pcap: keep the capture's own gaps between frames (else the pps rate)"},
			"loops":        map[string]any{"type": "integer", "description": "add_stream kind pcap: times through the capture (default 1)"},
			"rewrite_macs": map[string]any{"type": "boolean", "description": "add_stream kind pcap: send from this test port's MAC to the destination test port's"},
			"switch":       map[string]any{"type": "string", "description": "add_port: the switch"},
			"port":         map[string]any{"type": "string", "description": "add_port: its free port; remove_port: the test port's name"},
			"ip":           map[string]any{"type": "string", "description": "add_port, set_port: the test port's address with prefix (10.10.0.1/24), or dhcp (from a DHCP server)"},
			"gateway":      map[string]any{"type": "string", "description": "add_port: for streams to another network"},
			"ip6":          map[string]any{"type": "string", "description": "add_port, set_port: an IPv6 address with prefix (fd00::1/64), or slaac (from router advertisements)"},
			"lldp":         map[string]any{"type": "boolean", "description": "add_port, set_port: announce it by LLDP (default on)"},
			"groups":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "add_port, set_port: multicast groups it joins (239.1.1.1, ff0e::1)"},
			"dscp":         map[string]any{"type": "integer", "description": "add_stream: IP DSCP 0-63"},
			"pcp":          map[string]any{"type": "integer", "description": "add_stream: 802.1p priority 0-7 (with vlan)"},
			"size_mode":    map[string]any{"type": "string", "enum": []string{"fixed", "inc", "random", "imix"}, "description": "add_stream: frame sizes (inc/random: size to size_max)"},
			"size_max":     map[string]any{"type": "integer"},
			"burst":        map[string]any{"type": "integer", "description": "add_stream: frames sent back to back (the rate stays the average)"},
			"mods":         map[string]any{"type": "array", "description": "add_stream: fields that change per frame: {field: srcMac|dstMac|srcIp|dstIp|vlan|sport|dport, mode: inc|random, count, step}", "items": map[string]any{"type": "object"}},
			"max_jitter":   map[string]any{"type": "number", "description": "ms for a pass (0 = not checked)"},
			"dst_mac":      map[string]any{"type": "string"},
			"src_ip":       map[string]any{"type": "string"},
			"dport":        map[string]any{"type": "integer"},
			"name":         map[string]any{"type": "string", "description": "add_stream: its name; remove_stream: the stream"},
			"from":         map[string]any{"type": "string", "description": "add_stream: test port name"},
			"to":           map[string]any{"type": "string", "description": "add_stream: test port name; empty = every other test port (broadcast) or dst_ip"},
			"kind":         map[string]any{"type": "string", "enum": []string{"udp", "icmp", "tcp", "arp", "raw", "pcap"}},
			"dst_ip":       map[string]any{"type": "string", "description": "add_stream: an address that is not a test port (icmp: its replies are counted), or a multicast group (received by the test ports that joined it)"},
			"vlan":         map[string]any{"type": "integer"},
			"size":         map[string]any{"type": "integer", "description": "frame bytes (60-9000, default 128)"},
			"pps":          map[string]any{"type": "integer", "description": "packets per second (default 100, at most 100000)"},
			"rate":         map[string]any{"type": "number", "description": "add_stream: the rate in rate_unit (instead of pps)"},
			"rate_unit":    map[string]any{"type": "string", "enum": []string{"pps", "mbps", "pct"}, "description": "add_stream: pps, mbps (Mbit/s on the wire, with preamble and gap) or pct (% of the sending test port's line rate)"},
			"round_trip":   map[string]any{"type": "boolean", "description": "add_stream: the test port it goes to sends each frame back; latency is the round trip (works between test ports on two hosts)"},
			"speed":        map[string]any{"type": "integer", "description": "add_port, set_port: the test port's line rate in Mbit/s, for rates in % of it (default 1000)"},
			"seconds":      map[string]any{"type": "integer"},
			"count":        map[string]any{"type": "integer"},
			"max_loss":     map[string]any{"type": "number", "description": "% for a pass (default 0)"},
			"max_latency":  map[string]any{"type": "number", "description": "ms for a pass (0 = not checked)"},
			"streams":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "start: the streams (none = all)"},
		}, "action")},
	{"lab_test", "Traffic tests on the emulated lab's test ports, run by Termwright as a series of trials: RFC 2544 throughput (the highest rate without loss per frame size), latency (at that rate), frame-loss (loss from 100 % down to 10 % of the top rate) and back-to-back (the longest burst without loss), RFC 2889 learning (how many source MACs a switch learns before it floods; needs a third test port as monitor), convergence (traffic while a cable is pulled or a switch stops or restarts: the frames lost and the outage in ms), and iperf (iperf3 between two host VMs of the lab, on their consoles: TCP or UDP rate, retransmits, jitter, loss). Actions: list (saved tests and the latest runs), add, remove, run (in the background), stop, results (of a run; the latest when no run is given). Adding, running and stopping are asked in Termwright first.",
		mcpSchema(map[string]any{
			"action":      map[string]any{"type": "string", "enum": []string{"list", "add", "remove", "run", "stop", "results"}},
			"name":        map[string]any{"type": "string", "description": "add: its name; remove, run: the test"},
			"kind":        map[string]any{"type": "string", "enum": []string{"throughput", "latency", "frame-loss", "back-to-back", "learning", "convergence", "iperf"}},
			"server":      map[string]any{"type": "string", "description": "add, iperf: the host VM that runs iperf3 -s"},
			"client":      map[string]any{"type": "string", "description": "add, iperf: the host VM that runs iperf3 -c"},
			"server_ip":   map[string]any{"type": "string", "description": "add, iperf: the server's address (on its data port; default its management address)"},
			"proto":       map[string]any{"type": "string", "enum": []string{"tcp", "udp"}, "description": "add, iperf"},
			"bandwidth":   map[string]any{"type": "string", "description": "add, iperf: target rate (100M, 1G; udp default 1M)"},
			"parallel":    map[string]any{"type": "integer", "description": "add, iperf: parallel streams"},
			"reverse":     map[string]any{"type": "boolean", "description": "add, iperf: the server sends"},
			"from":        map[string]any{"type": "string", "description": "add: the test port the frames go from (name)"},
			"to":          map[string]any{"type": "string", "description": "add: the test port they go to"},
			"both":        map[string]any{"type": "boolean", "description": "add: both ways at the same time"},
			"monitor":     map[string]any{"type": "string", "description": "add, learning: a third test port that sees flooded frames"},
			"vlan":        map[string]any{"type": "integer"},
			"sizes":       map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "add: frame sizes (default 64, 128, 256, 512, 1024, 1280, 1518)"},
			"trial":       map[string]any{"type": "integer", "description": "add: seconds per trial (default 5)"},
			"max_pps":     map[string]any{"type": "integer", "description": "add: the highest rate tried (default 10000)"},
			"rate":        map[string]any{"type": "integer", "description": "add: latency, frame-loss, convergence: the rate (pps)"},
			"loss":        map[string]any{"type": "number", "description": "add: loss % that still counts as none (default 0)"},
			"resolution":  map[string]any{"type": "number", "description": "add: search resolution, % (default 2)"},
			"max":         map[string]any{"type": "integer", "description": "add: back-to-back the longest burst, learning the most addresses tried"},
			"test_action": map[string]any{"type": "string", "enum": []string{"pull", "stop", "restart"}, "description": "add, convergence: what happens during the traffic"},
			"switch":      map[string]any{"type": "string", "description": "add, convergence: the switch"},
			"port":        map[string]any{"type": "string", "description": "add, convergence pull: the port whose cable is pulled"},
			"at":          map[string]any{"type": "integer", "description": "add, convergence: seconds after the start (default 5)"},
			"hold":        map[string]any{"type": "integer", "description": "add, convergence: seconds until the cable is plugged in / the switch started again (0 = not)"},
			"seconds":     map[string]any{"type": "integer", "description": "add, convergence: how long the traffic runs (default 30)"},
			"run":         map[string]any{"type": "string", "description": "results: the run id (default the latest)"},
		}, "action")},
	{"lab_capture", "Packet capture on a switch port of the emulated lab (its cable, both ways; the switch keeps running): start, stop, or read the packets (number, time, source, destination, protocol, length, info). Start and stop are not asked; they are kept in the lab's change history (lab_history).",
		mcpSchema(map[string]any{
			"action": map[string]any{"type": "string", "enum": []string{"start", "stop", "read", "list"}},
			"switch": map[string]any{"type": "string"},
			"port":   map[string]any{"type": "string", "description": "a data port, or mgmt for the switch's management port"},
			"filter": map[string]any{"type": "string", "description": "read: only packets whose source, destination, protocol or info contains this (e.g. ICMP, LLDP, 10.0.0.1)"},
			"last":   map[string]any{"type": "integer", "description": "read: how many of the newest packets (default 100, at most 2000)"},
		}, "action")},
	{"lab_link_check", "Watch a running switch's e1000/e1000e network cards in QEMU for some seconds (QEMU's own trace events) and say what QEMU did: links taken down for auto-negotiation the switch restarted, receivers the switch turned off, frames dropped. Tells a link flap of the emulation from one of the switch's software.",
		mcpSchema(map[string]any{
			"switch":  map[string]any{"type": "string"},
			"seconds": map[string]any{"type": "integer", "description": "5-120 (default 30)"},
		}, "switch")},
	{"lab_cpu", "CPU requests and limits of the emulated lab's switches and computers, in millicores as kubectl counts them (1000m = one host core): the request is its share when the host is busy (nothing is reserved; it may use more when the host is free), the limit the most it may use. They apply at once, no restart (a cgroup on its host). Without request_m and limit_m it reports each device's use over the last hour: average, p95, peak, how often it went above its request, how many times its limit held it back, and advice. Use it to keep a busy switch from slowing the others, or to give a switch under test more of the host.",
		mcpSchema(map[string]any{
			"switches":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "names (none = all)"},
			"request_m": map[string]any{"type": "integer", "description": "its CPU request in millicores (250 = 250m = a quarter of a core; 0 = none)"},
			"limit_m":   map[string]any{"type": "integer", "description": "its CPU limit in millicores (1500 = one and a half cores; 0 = no limit)"},
			"max_cores": map[string]any{"type": "number", "description": "older form of the limit: host cores (0.5 = half of one)"},
			"share":     map[string]any{"type": "integer", "description": "older form of the request: 100 = 1000m"},
		})},
	{"lab_computer", "A Linux computer of the emulated lab, like a PC on a desk: set one data port's IP configuration (IPv4 address/bits or dhcp, IPv6 address/bits or auto, default gateways, DNS server; typed on its console now and at each start), or run one of its apps and read what it printed: ping, ping6, traceroute, web (a page's text), dns (nslookup), iperf-server, iperf (5 s to a server).",
		mcpSchema(map[string]any{
			"action":   map[string]any{"type": "string", "enum": []string{"set", "run"}},
			"computer": map[string]any{"type": "string"},
			"port":     map[string]any{"type": "integer", "description": "set: the data port (1 = the first)"},
			"ipv4":     map[string]any{"type": "string", "description": "set: address/bits, dhcp, or none"},
			"ipv6":     map[string]any{"type": "string", "description": "set: address/bits, auto, or none"},
			"gateway":  map[string]any{"type": "string", "description": "set: its default gateway (none = the management network's)"},
			"gateway6": map[string]any{"type": "string"},
			"dns":      map[string]any{"type": "string"},
			"app":      map[string]any{"type": "string", "enum": []string{"ping", "ping6", "traceroute", "web", "dns", "iperf-server", "iperf"}},
			"target":   map[string]any{"type": "string", "description": "run: the address, host name or web address"},
		}, "action", "computer")},
	{"lab_address", "Addresses of the data ports of Linux computers in the emulated lab (Alpine, Debian, Ubuntu… added from the catalog or a GNS3 appliance): list them, or set one (typed on the computer's console now when it runs, and at each start). Switches' ports are set on their own CLI (lab_console).",
		mcpSchema(map[string]any{
			"action":  map[string]any{"type": "string", "enum": []string{"list", "set"}},
			"switch":  map[string]any{"type": "string", "description": "set: the computer"},
			"port":    map[string]any{"type": "integer", "description": "set: its data port (1 = eth1 on most images; eth0 is management)"},
			"address": map[string]any{"type": "string", "description": "set: address/bits (10.0.0.5/24, 2001:db8::5/64), dhcp, or none to remove it"},
		}, "action")},
	{"lab_qemu_command", "The QEMU command line a switch of the emulated lab runs with now (read on its KVM host), or the one it starts with when it is not running: network cards, CPU, machine, memory.",
		mcpSchema(map[string]any{"switch": map[string]any{"type": "string"}}, "switch")},
	{"lab_debug", "Everything to find why a switch or computer of the emulated lab is stuck (booting, day 0, address, login): its state and the step Termwright is on, its error, addresses, the image's login and ready settings (never its password), its Events and the end of its console since it started. Passwords and keys are hidden.",
		mcpSchema(map[string]any{
			"switch": map[string]any{"type": "string"},
			"lines":  map[string]any{"type": "integer", "description": "console lines from the end (default 200, at most 3000)"},
		}, "switch")},
	{"lab_events", "Read the emulated lab's Events log: what Termwright did to each switch (started, booted, day 0, persona, moves, errors), newest last.",
		mcpSchema(map[string]any{"last": map[string]any{"type": "integer", "description": "how many lines (default 60, at most 300)"}})},
	{"lab_history", "The emulated lab's change history: every change Claude or the API made (lab changes are not asked, so each is kept with the lab as it was before). Actions: list (newest first, with ids), plan (what revert would do for a change id), revert (bring the lab back to how it was before that change: devices added since are removed, cables, CPU, computers' addresses, test ports and streams set back, devices started or stopped as they were; commands typed on a switch are undone by restoring the checkpoint it got before them - Termwright takes one before typing, at most every 2 minutes per switch, none while traffic runs). Use revert when a test went wrong. lab_checkpoint takes, lists and restores checkpoints by hand.",
		mcpSchema(map[string]any{
			"action": map[string]any{"type": "string", "enum": []string{"list", "plan", "revert"}},
			"id":     map[string]any{"type": "string", "description": "plan, revert: the change's id (from list)"},
			"last":   map[string]any{"type": "integer", "description": "list: how many (default 20)"},
		}, "action")},
}

// emuMCPID finds a switch by its name (or id).
func emuMCPID(v EmuView, name string) (EmuDevice, error) {
	for _, d := range v.Devices {
		if strings.EqualFold(d.Hostname, name) || d.ID == name {
			return d, nil
		}
	}
	return EmuDevice{}, fmt.Errorf("no switch %q in the lab (show_lab)", name)
}

// emuMCPHost finds a KVM host by its name or session path.
func emuMCPHost(name string) (string, error) {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	if name == "" {
		return emuLab.f.Host, nil
	}
	for _, h := range emuLab.f.Hosts {
		if h.Session == name || strings.EqualFold(h.Name, name) || strings.EqualFold(h.Session[strings.LastIndex(h.Session, "/")+1:], name) {
			return h.Session, nil
		}
	}
	return "", fmt.Errorf("no KVM host %q (Hosts in the emulated lab)", name)
}

func argStrs(m map[string]any, k string) []string {
	var out []string
	if l, ok := m[k].([]any); ok {
		for _, x := range l {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

// emuMCP runs a lab_ tool for the connector. Lab changes are not asked - automated tests must not wait
// for a person - but each one is kept in the lab's change history with the lab as it was before, so it
// can be undone (Changes tab, lab_history). Only "Never" in Settings → AI Assistant stops them.
func (a *App) emuMCP(name string, args map[string]any) (string, error) {
	return a.emuMCPWith(name, args, func(title, msg string, danger, light bool) error {
		if a.cfg.Get().AI.MCPCommands == "never" {
			return errors.New("the user does not let Claude change Termwright (Settings → AI Assistant)")
		}
		return nil
	})
}

// emuAsker asks the user before a change (light: a small one the connector does without asking).
type emuAsker func(title, msg string, danger, light bool) error

// emuMCPWith runs a lab_ tool; asker is called before each change and stops it with an error.
func (a *App) emuMCPWith(name string, args map[string]any, asker emuAsker) (string, error) {
	return a.emuMCPRun("ai", name, args, asker)
}

// emuMCPRun runs a lab_ tool for who (ai, api: what the Events show as the trigger). A change it makes
// is kept in the lab's change history (with the lab as it was before) when it was let through.
func (a *App) emuMCPRun(who, name string, args map[string]any, asker emuAsker) (out string, err error) {
	defer emuAs(who)()
	var rec *emuChangeRec
	refused := false
	if name != "lab_history" {
		inner := asker
		asker = func(title, msg string, danger, light bool) error {
			if rec == nil {
				rec = a.emuChangeBegin(who, name, title, msg)
				if args != nil {
					args["_change"] = rec.ID // a checkpoint taken for it says so
				}
			}
			if e := inner(title, msg, danger, light); e != nil {
				refused = true
				return e
			}
			return nil
		}
		defer func() {
			if rec != nil && !refused {
				if ck, ok := args["_ck"].(map[string]string); ok && len(ck) > 0 { // typed commands, undone by a checkpoint
					rec.Checkpoints, rec.Note, rec.Revertible = ck, "", true
				} else if w, ok := args["_ckwhy"].(string); ok && w != "" {
					rec.Note = "the commands typed are not undone: " + w
				}
				a.emuChangeEnd(rec, out, err)
			}
		}()
	}
	v, err := a.EmuState()
	if err != nil {
		return "", err
	}
	ask := func(title, msg string, danger bool) error { return asker(title, msg, danger, false) }
	light := func(title, msg string) error { return asker(title, msg, false, true) }
	ids := func(names []string) ([]string, []string, error) {
		var out, shown []string
		if len(names) == 0 {
			for _, d := range v.Devices {
				out, shown = append(out, d.ID), append(shown, d.Hostname)
			}
			return out, shown, nil
		}
		for _, n := range names {
			d, err := emuMCPID(v, n)
			if err != nil {
				return nil, nil, err
			}
			out, shown = append(out, d.ID), append(shown, d.Hostname)
		}
		return out, shown, nil
	}
	switch name {
	case "lab_checkpoint":
		if argStr(args, "action") == "auto" {
			on, _ := args["on"].(bool)
			if _, err := a.EmuSetCheckpoints(on); err != nil {
				return "", err
			}
			return map[bool]string{true: "Checkpoints before typed commands are on.", false: "Checkpoints before typed commands are off: Revert does not undo typed commands."}[on], nil
		}
		d, err := emuMCPID(v, argStr(args, "switch"))
		if err != nil {
			return "", err
		}
		switch argStr(args, "action") {
		case "take":
			if _, err := a.emuCheckpoint(d.ID, orStr(argStr(args, "note"), "taken by Claude"), argStr(args, "_change")); err != nil {
				return "", err
			}
			fallthrough
		case "list", "":
			emuLab.mu.Lock()
			var cks []emu.Checkpoint
			if vm := emuLab.f.Lab.VMByID(d.ID); vm != nil {
				cks = slices.Clone(vm.Checkpoints)
			}
			emuLab.mu.Unlock()
			if len(cks) == 0 {
				return d.Hostname + " has no checkpoint.", nil
			}
			var b strings.Builder
			b.WriteString(d.Hostname + "'s checkpoints (tag | when | why), newest last:\n")
			for _, c := range cks {
				fmt.Fprintf(&b, "- %s | %s | %s\n", c.Tag, c.At, c.Note)
			}
			return b.String(), nil
		case "restore":
			if err := light("Claude restores a checkpoint", "Claude (through the Termwright connector) restores "+d.Hostname+" to its checkpoint "+argStr(args, "tag")+"."); err != nil {
				return "", err
			}
			if err := a.emuRestore(d.ID, argStr(args, "tag")); err != nil {
				return "", err
			}
			return "Restored " + d.Hostname + " to " + argStr(args, "tag") + ".", nil
		case "remove":
			_, err := a.EmuCheckpointRemove(d.ID, argStr(args, "tag"))
			return "Removed.", err
		}
		return "", errors.New("action is list, take, restore, remove or auto")
	case "lab_keys":
		enter, _ := args["enter"].(bool)
		if inst := argStr(args, "install"); inst != "" {
			if c, _ := args["console"].(bool); c {
				if err := a.EmuInstallType(inst, argStr(args, "text"), enter); err != nil {
					return "", err
				}
				return "Typed on the installer's console.", nil
			}
			if err := a.EmuScreenKeys("install", inst, argStr(args, "text"), enter, argStrs(args, "keys")); err != nil {
				return "", err
			}
			return "Typed on the installer's keyboard.", nil
		}
		d, err := emuMCPID(v, argStr(args, "switch"))
		if err != nil {
			return "", err
		}
		if err := light("Claude types on a screen", "Claude (through the Termwright connector) types on the keyboard of "+d.Hostname+"."); err != nil {
			return "", err
		}
		if err := a.EmuScreenKeys("vm", d.ID, argStr(args, "text"), enter, argStrs(args, "keys")); err != nil {
			return "", err
		}
		return "Typed on " + d.Hostname + "'s keyboard.", nil
	case "lab_hub":
		list := func(nv EmuView) string {
			var b strings.Builder
			for _, h := range nv.Hubs {
				var legs []string
				for _, g := range h.Legs {
					legs = append(legs, g.Name+" "+g.PortName)
				}
				fmt.Fprintf(&b, "- %s (%s): %s\n", h.Name, map[bool]string{true: "L2 switch", false: "hub"}[h.Kind == "switch"], orStr(strings.Join(legs, ", "), "no ports yet"))
			}
			if b.Len() == 0 {
				return "No hub or L2 switch yet."
			}
			return b.String()
		}
		var nv EmuView
		var err error
		switch argStr(args, "action") {
		case "list", "":
			return list(v), nil
		case "add":
			if err := light("Claude adds a hub", "Claude (through the Termwright connector) adds a "+orStr(argStr(args, "kind"), "hub")+"."); err != nil {
				return "", err
			}
			nv, err = a.EmuHubAdd(argStr(args, "kind"), argStr(args, "name"), 0, 0)
		case "connect", "disconnect":
			if err := light("Claude changes a hub", "Claude (through the Termwright connector) "+argStr(args, "action")+"s "+argStr(args, "switch")+" "+argStr(args, "port")+" on "+argStr(args, "hub")+"."); err != nil {
				return "", err
			}
			if argStr(args, "action") == "connect" {
				nv, err = a.EmuHubConnect(argStr(args, "hub"), argStr(args, "switch"), argStr(args, "port"))
			} else {
				d, e := emuMCPID(v, argStr(args, "switch"))
				if e != nil {
					return "", e
				}
				emuLab.mu.Lock()
				vm := emuLab.f.Lab.VMByID(d.ID)
				n := 0
				if vm != nil {
					n = emuPort(emuImage(vm.Image), argStr(args, "port"))
				}
				emuLab.mu.Unlock()
				nv, err = a.EmuHubDisconnect(argStr(args, "hub"), d.ID, n)
			}
		case "remove":
			if err := light("Claude removes a hub", "Claude (through the Termwright connector) removes "+argStr(args, "hub")+"."); err != nil {
				return "", err
			}
			nv, err = a.EmuHubRemove(argStr(args, "hub"))
		default:
			return "", errors.New("action is list, add, connect, disconnect or remove")
		}
		if err != nil {
			return "", err
		}
		return list(nv), nil
	case "lab_dns":
		if _, ok := args["servers"]; ok {
			nv, err := a.EmuSetDNS(argStrs(args, "servers"))
			if err != nil {
				return "", err
			}
			return "The lab's name servers are now " + strings.Join(nv.DNS, ", ") + map[bool]string{true: "", false: " (the default)"}[nv.DNSSet] + ". Devices get them when they start.", nil
		}
		return "The lab's name servers: " + strings.Join(v.DNS, ", ") + map[bool]string{true: "", false: " (the default)"}[v.DNSSet] + ".", nil
	case "lab_images":
		var b strings.Builder
		q, typ := strings.ToLower(argStr(args, "name")), argStr(args, "type")
		hit := func(s ...string) bool {
			return q == "" || slices.ContainsFunc(s, func(x string) bool { return strings.Contains(strings.ToLower(x), q) })
		}
		shown, typeOK := 0, map[string]bool{}
		for _, im := range v.Images {
			typeOK[im.ID] = typ == "" || strings.EqualFold(im.DeviceType, typ)
		}
		b.WriteString("Images (id | name | version | device type | ports | file):\n")
		for _, im := range v.Images {
			if !typeOK[im.ID] || !hit(im.ID, im.Name, im.Version) {
				continue
			}
			shown++
			file := "missing: download or choose it in Termwright"
			if im.File != "" {
				file = "there"
			}
			fmt.Fprintf(&b, "- %s | %s | %s | %s | %d ports named %s | %s\n", im.ID, im.Name, im.Version, orDash(im.DeviceType), max(im.NICs-1, 0), orDash(im.PortName), file)
		}
		var ts []string
		for _, t := range v.Templates {
			if (typ == "" || typeOK[t.Image]) && hit(t.ID, t.Name) {
				ts = append(ts, fmt.Sprintf("- %s | %s | %s\n", t.ID, t.Name, t.Image))
			}
		}
		if len(ts) > 0 {
			b.WriteString("Templates (id | name | image):\n" + strings.Join(ts, ""))
		}
		if shown == 0 && len(ts) == 0 && (q != "" || typ != "") {
			return fmt.Sprintf("No image or template matches (of %d images, %d templates).", len(v.Images), len(v.Templates)), nil
		}
		return b.String(), nil
	case "lab_install_status":
		if len(v.Installs) == 0 {
			return "No installs from ISO files.", nil
		}
		var b strings.Builder
		b.WriteString("Installs (id | name | mode | stage | now | image):\n")
		for _, j := range v.Installs {
			fmt.Fprintf(&b, "- %s | %s | %s | %s | %s | %s\n", j.ID, j.Name, j.Mode, j.Stage, orStr(j.Err, j.Msg, "-"), orDash(j.Image))
			if emuInstallRunning(j.Stage) || j.Stage == instWaiting {
				fmt.Fprintf(&b, "  step %s%s\n", j.Step, emuInstallProgress(j))
			}
			if j.Tail != "" && emuInstallRunning(j.Stage) {
				fmt.Fprintf(&b, "  console: ...%s\n", strings.ReplaceAll(strings.TrimSpace(j.Tail[max(0, len(j.Tail)-200):]), "\n", " / "))
			}
		}
		return b.String(), nil
	case "lab_install_iso":
		r := EmuInstallRequest{ISO: argStr(args, "iso_path"), Name: argStr(args, "name"), Mode: argStr(args, "mode"), Password: argStr(args, "password"), GB: argInt(args, "disk_gb", 8, 512), MB: argInt(args, "disk_mb", 0, 512*1024), RAM: argInt(args, "ram_mb", 1024, 65536), CPUs: argInt(args, "cpus", 2, 64)}
		if r.ISO == "" {
			return "", errors.New("give iso_path")
		}
		var err error
		if r.Host, err = emuMCPHost(argStr(args, "host")); err != nil {
			return "", err
		}
		if err := ask("Claude wants to install from an ISO", fmt.Sprintf("Claude (through the Termwright connector) asks to copy %s to a KVM host and install it on a new %s disk (%s), then keep the disk as the image %q.", r.ISO, map[bool]string{true: fmt.Sprintf("%d MB", r.MB), false: fmt.Sprintf("%d GB", r.GB)}[r.MB > 0], map[string]string{"alpine": "Alpine Linux, typed by Termwright", "manual": "you install on its console"}[r.Mode], orStr(r.Name, filepath.Base(r.ISO))), false); err != nil {
			return "", err
		}
		nv, err := a.EmuInstallStart(r)
		if err != nil {
			return "", err
		}
		return "Install started (id " + nv.Installs[len(nv.Installs)-1].ID + "): follow it with lab_install_status.", nil
	case "lab_install_finish":
		id := argStr(args, "id")
		if err := ask("Claude wants to finish an install", "Claude (through the Termwright connector) asks to shut the installer machine down and keep its disk as an image (install "+id+").", false); err != nil {
			return "", err
		}
		if _, err := a.EmuInstallFinish(id); err != nil {
			return "", err
		}
		return "Saving the disk as an image: follow it with lab_install_status.", nil
	case "lab_add_switches":
		host, err := emuMCPHost(argStr(args, "host"))
		if err != nil {
			return "", err
		}
		g := EmuGroup{Image: argStr(args, "image"), Template: argStr(args, "template"), Model: argStr(args, "model"), Count: argInt(args, "count", 1, 32), Names: argStr(args, "names")}
		if g.Image == "" && g.Template == "" {
			return "", errors.New("give an image or a template id (lab_images)")
		}
		start, _ := args["start"].(bool)
		what := orStr(g.Template, g.Image)
		if err := ask("Claude wants to add switches", fmt.Sprintf("Claude (through the Termwright connector) asks to add %d switch(es) from %s%s on %s%s.", g.Count, what, map[bool]string{true: " (" + g.Model + ")", false: ""}[g.Model != ""], host[strings.LastIndex(host, "/")+1:], map[bool]string{true: " and start them", false: ""}[start]), false); err != nil {
			return "", err
		}
		before := map[string]bool{}
		for _, d := range v.Devices {
			before[d.ID] = true
		}
		nv, err := a.EmuAdd(EmuRequest{Host: host, Groups: []EmuGroup{g}, Start: start})
		if err != nil {
			return "", err
		}
		var made []string
		for _, d := range nv.Devices {
			if !before[d.ID] {
				made = append(made, d.Hostname)
			}
		}
		return "Added: " + strings.Join(made, ", ") + map[bool]string{true: " - starting (lab_wait until they are ready)", false: " - not started (lab_power start)"}[start], nil
	case "lab_cable":
		remove, _ := args["remove"].(bool)
		list, _ := args["cables"].([]any)
		if len(list) == 0 {
			return "", errors.New("give the cables")
		}
		type cab struct{ a, ap, b, bp string }
		var cs []cab
		var lines []string
		for _, x := range list {
			m, _ := x.(map[string]any)
			c := cab{argStr(m, "a"), argStr(m, "a_port"), argStr(m, "b"), argStr(m, "b_port")}
			if c.a == "" || c.ap == "" || !remove && (c.b == "" || c.bp == "") {
				return "", errors.New("each cable needs a, a_port, b and b_port")
			}
			if _, err := emuMCPID(v, c.a); err != nil {
				return "", err
			}
			if !remove {
				if _, err := emuMCPID(v, c.b); err != nil {
					return "", err
				}
				lines = append(lines, fmt.Sprintf("%s %s - %s %s", c.a, c.ap, c.b, c.bp))
			} else {
				lines = append(lines, fmt.Sprintf("%s %s", c.a, c.ap))
			}
			cs = append(cs, c)
		}
		title := map[bool]string{true: "Claude wants to remove cables", false: "Claude wants to connect cables"}[remove]
		if err := ask(title, "Claude (through the Termwright connector) asks to "+map[bool]string{true: "remove the cables at", false: "connect"}[remove]+":\n\n    "+strings.Join(lines, "\n    "), remove); err != nil {
			return "", err
		}
		var done, failed []string
		for i, c := range cs {
			da, _ := emuMCPID(v, c.a)
			var err error
			if remove {
				_, err = a.EmuDisconnect(da.ID, c.ap)
			} else {
				db, _ := emuMCPID(v, c.b)
				_, err = a.EmuConnect(EmuLinkView{A: da.ID, APort: c.ap, B: db.ID, BPort: c.bp})
			}
			if err != nil {
				failed = append(failed, lines[i]+": "+err.Error())
			} else {
				done = append(done, lines[i])
			}
		}
		out := fmt.Sprintf("Done: %d of %d.", len(done), len(cs))
		if len(failed) > 0 {
			out += "\nNot done:\n- " + strings.Join(failed, "\n- ")
		}
		if !remove && len(done) > 0 {
			out += "\nNew cables work once both switches have restarted (lab_power restart)."
		}
		return out, nil
	case "lab_power":
		act := argStr(args, "action")
		list, shown, err := ids(argStrs(args, "switches"))
		if err != nil {
			return "", err
		}
		if err := ask("Claude wants to "+act+" switches", "Claude (through the Termwright connector) asks to "+act+": "+strings.Join(shown, ", "), act != "start"); err != nil {
			return "", err
		}
		switch act {
		case "start":
			_, err = a.EmuStart(list)
		case "stop":
			_, err = a.EmuStop(list)
		case "restart":
			_, err = a.EmuRestart(list)
		case "shutdown":
			_, err = a.EmuShutDown(list)
		default:
			return "", errors.New("action is start, stop, restart or shutdown")
		}
		if err != nil {
			return "", err
		}
		return strings.Title(act) + " given for " + strings.Join(shown, ", ") + ". lab_wait follows them until they are ready.", nil //nolint:staticcheck
	case "lab_wait":
		names := argStrs(args, "switches")
		if _, _, err := ids(names); err != nil {
			return "", err
		}
		end := time.Now().Add(time.Duration(argInt(args, "timeout", 600, 1800)) * time.Second)
		for {
			v, _ = a.EmuState()
			var b strings.Builder
			all := true
			for _, d := range v.Devices {
				if len(names) > 0 && !slicesContainsFold(names, d.Hostname) {
					continue
				}
				ready := emuReadyForTests(d)
				all = all && (ready || d.Error != "")
				st := map[bool]string{true: "running", false: "stopped"}[d.Running]
				if d.StateText != "" {
					st = d.StateText
				}
				fmt.Fprintf(&b, "- %s: %s", d.Hostname, st)
				if d.Error != "" {
					b.WriteString(" - problem: " + d.Error)
				}
				if d.SSH != "" {
					b.WriteString(" - management " + d.SSH)
				}
				b.WriteString("\n")
			}
			if all {
				return "Ready (or stopped with a problem):\n" + b.String(), nil
			}
			if time.Now().After(end) {
				return "Not all ready when the time was up:\n" + b.String(), nil
			}
			time.Sleep(5 * time.Second)
		}
	case "lab_console":
		d, err := emuMCPID(v, argStr(args, "switch"))
		if err != nil {
			return "", err
		}
		cmds := argStrs(args, "commands")
		if len(cmds) == 0 {
			return "", errors.New("give the commands")
		}
		danger := false
		for _, c := range cmds {
			danger = danger || mcpRisky.MatchString(c)
		}
		if err := ask("Claude wants to type on a console", "Claude (through the Termwright connector) asks to type on the console of "+d.Hostname+":\n\n    "+strings.Join(cmds, "\n    "), danger); err != nil {
			return "", err
		}
		if tag, why := a.emuAutoCheckpoint(d.ID, "typing "+strings.Join(cmds, "; "), argStr(args, "_change")); tag != "" {
			args["_ck"] = map[string]string{d.ID: tag}
		} else {
			args["_ckwhy"] = why
		}
		out, err := a.emuConsoleRun(d.ID, cmds, time.Duration(argInt(args, "timeout", 180, 900))*time.Second)
		red, _ := aiRedact(out, false)
		if err != nil {
			return red, err
		}
		return red, nil
	case "lab_check_cabling":
		nv, err := a.EmuCheckCabling()
		if err != nil {
			return "", err
		}
		name := map[string]string{}
		for _, d := range nv.Devices {
			name[d.ID] = d.Hostname
		}
		var b strings.Builder
		for _, k := range nv.Links {
			fmt.Fprintf(&b, "- %s %s - %s %s: %s\n", name[k.A], k.APort, name[k.B], k.BPort, orStr(k.Lldp, "not checked (is it running?)"))
		}
		if b.Len() == 0 {
			return "There are no cables.", nil
		}
		return b.String(), nil
	case "lab_move":
		d, err := emuMCPID(v, argStr(args, "switch"))
		if err != nil {
			return "", err
		}
		host, err := emuMCPHost(argStr(args, "host"))
		if err != nil {
			return "", err
		}
		how := argStr(args, "how")
		if err := ask("Claude wants to move a switch", fmt.Sprintf("Claude (through the Termwright connector) asks to move %s to %s (%s).", d.Hostname, host[strings.LastIndex(host, "/")+1:], how), how != "live"); err != nil {
			return "", err
		}
		switch how {
		case "image":
			_, err = a.EmuMoveVM(d.ID, host)
		case "live", "copy":
			_, err = a.EmuMigrate(d.ID, host, how == "live")
		default:
			return "", errors.New("how is live, copy or image")
		}
		if err != nil {
			return "", err
		}
		return "Moving " + d.Hostname + " - lab_events shows each step; lab_wait until it is ready.", nil
	case "lab_traffic":
		tv, err := a.EmuTraffic()
		if err != nil {
			return "", err
		}
		tester := func(name string) string {
			for _, t := range tv.Testers {
				if strings.EqualFold(t.Name, name) || t.ID == name {
					return t.ID
				}
			}
			return ""
		}
		switch argStr(args, "action") {
		case "list":
			var b strings.Builder
			for _, t := range emuSortedTesters(tv.Testers) {
				sw := t.VM
				if d, err := emuMCPID(v, t.VM); err == nil {
					sw = d.Hostname
				}
				ip := orStr(t.IP, "none")
				if t.DHCP {
					ip = "DHCP " + orStr(t.Lease, "(no lease yet)")
				}
				if t.IP6 != "" || t.SLAAC {
					ip += ", IPv6 " + orStr(t.IP6, orStr(t.Lease6, "SLAAC (none yet)"))
				}
				fmt.Fprintf(&b, "- test port %s on %s port %d, MAC %s, IP %s\n", t.Name, sw, t.Port, t.MAC, ip)
			}
			if rep, _ := a.EmuTrafficReportText(); len(tv.Streams) > 0 {
				b.WriteString("Streams:\n" + rep)
			}
			if b.Len() == 0 {
				return "No test port yet: add_port puts one on a free switch port.", nil
			}
			return b.String(), nil
		case "add_port":
			d, err := emuMCPID(v, argStr(args, "switch"))
			if err != nil {
				return "", err
			}
			if err := ask("Claude wants to add a test port", fmt.Sprintf("Claude (through the Termwright connector) asks to put a test port on %s %s (%s).", d.Hostname, argStr(args, "port"), orStr(argStr(args, "ip"), "no address")), false); err != nil {
				return "", err
			}
			r, err := a.EmuTesterAdd(d.ID, argStr(args, "port"), argStr(args, "ip"), argStr(args, "gateway"), argInt(args, "vlan", 0, 4094))
			if err != nil {
				return "", err
			}
			t := r.Testers[len(r.Testers)-1]
			if ip6, gs := argStr(args, "ip6"), argStrs(args, "groups"); ip6 != "" || len(gs) > 0 || args["lldp"] != nil || args["speed"] != nil {
				t.IP6, t.Groups, t.Speed = ip6, gs, argInt(args, "speed", 0, 400000)
				if x, ok := args["lldp"].(bool); ok {
					t.LLDP = x
				}
				if r, err = a.EmuTesterSet(t); err != nil {
					return "", err
				}
			}
			return fmt.Sprintf("%s on %s %s, MAC %s, IP %s. %s", t.Name, d.Hostname, argStr(args, "port"), t.MAC, orStr(t.IP, "none"), r.Note), nil
		case "set_port":
			id := tester(argStr(args, "port"))
			var t *emu.Tester
			for i := range tv.Testers {
				if tv.Testers[i].ID == id {
					t = &tv.Testers[i]
				}
			}
			if t == nil {
				return "", errors.New("no test port " + argStr(args, "port"))
			}
			if err := light("Claude wants to change a test port", "Claude (through the Termwright connector) asks to change "+t.Name+"."); err != nil {
				return "", err
			}
			if _, ok := args["ip"]; ok {
				t.IP = argStr(args, "ip")
			}
			if _, ok := args["ip6"]; ok {
				t.IP6 = argStr(args, "ip6")
			}
			if _, ok := args["gateway"]; ok {
				t.GW = argStr(args, "gateway")
			}
			if _, ok := args["vlan"]; ok {
				t.VLAN = argInt(args, "vlan", 0, 4094)
			}
			if _, ok := args["groups"]; ok {
				t.Groups = argStrs(args, "groups")
			}
			if x, ok := args["lldp"].(bool); ok {
				t.LLDP = x
			}
			if _, ok := args["speed"]; ok {
				t.Speed = argInt(args, "speed", 0, 400000)
			}
			_, err := a.EmuTesterSet(*t)
			return "Changed " + t.Name + ".", err
		case "remove_port":
			id := tester(argStr(args, "port"))
			if id == "" {
				return "", errors.New("no such test port")
			}
			if err := ask("Claude wants to remove a test port", "Claude (through the Termwright connector) asks to remove "+argStr(args, "port")+" and its streams.", true); err != nil {
				return "", err
			}
			_, err := a.EmuTesterRemove(id)
			return "Removed.", err
		case "add_stream":
			st := emu.Stream{Name: argStr(args, "name"), From: tester(argStr(args, "from")), Kind: argStr(args, "kind"), DstIP: argStr(args, "dst_ip"), VLAN: argInt(args, "vlan", 0, 4094), Size: argInt(args, "size", 128, 9000), PPS: argInt(args, "pps", 100, 100000), Seconds: argInt(args, "seconds", 0, 86400), Count: argInt(args, "count", 0, 100000000)}
			if to := argStr(args, "to"); to != "" {
				if st.To = tester(to); st.To == "" {
					return "", errors.New("no test port " + to)
				}
			}
			if f, ok := args["max_loss"].(float64); ok {
				st.MaxLoss = f
			}
			if f, ok := args["max_latency"].(float64); ok {
				st.MaxLatency = f
			}
			if f, ok := args["max_jitter"].(float64); ok {
				st.MaxJitter = f
			}
			if f, ok := args["rate"].(float64); ok && argStr(args, "rate_unit") != "" && argStr(args, "rate_unit") != "pps" {
				st.RateUnit, st.Rate = argStr(args, "rate_unit"), f
			} else if ok {
				st.PPS = int(f)
			}
			st.RTT, _ = args["round_trip"].(bool)
			st.Pcap, st.Loops = argStr(args, "pcap"), argInt(args, "loops", 0, 1000000)
			st.PcapTiming, _ = args["pcap_timing"].(bool)
			st.RewriteMAC, _ = args["rewrite_macs"].(bool)
			st.DSCP, st.SizeMode, st.SizeMax, st.Burst = argInt(args, "dscp", 0, 63), strings.TrimPrefix(argStr(args, "size_mode"), "fixed"), argInt(args, "size_max", 0, 9000), argInt(args, "burst", 0, 10000)
			st.DstMAC, st.SrcIP, st.DPort, st.Prio = argStr(args, "dst_mac"), argStr(args, "src_ip"), argInt(args, "dport", 0, 65535), argInt(args, "pcp", 0, 7)
			if ms, ok := args["mods"].([]any); ok {
				for _, x := range ms {
					if m, ok := x.(map[string]any); ok {
						st.Mods = append(st.Mods, emu.Mod{Field: argStr(m, "field"), Mode: orStr(argStr(m, "mode"), "inc"), Count: argInt(m, "count", 1, 1<<24), Step: argInt(m, "step", 1, 1<<20)})
					}
				}
			}
			if err := light("Claude wants to add a stream", fmt.Sprintf("Claude (through the Termwright connector) asks to add the stream %s: %s from %s to %s.", orStr(st.Name, "(no name)"), orStr(st.Kind, "udp"), argStr(args, "from"), orStr(argStr(args, "to"), orStr(st.DstIP, "every other test port")))); err != nil {
				return "", err
			}
			if _, err := a.EmuStreamSave(st); err != nil {
				return "", err
			}
			return "Stream added; lab_traffic start runs it.", nil
		case "templates":
			var b strings.Builder
			b.WriteString("Templates (name | test ports | what it checks):\n")
			for _, t := range a.EmuTrafficTemplates() {
				own := ""
				if !t.Builtin && t.Saved != "" {
					own = " (yours)"
				}
				fmt.Fprintf(&b, "- %s%s | %d (%s) | %s\n", t.Name, own, t.Ports, strings.Join(t.Roles, ", "), t.Desc)
			}
			return b.String() + "apply_template with template and ports (one per role, in order) adds its streams and tests.", nil
		case "apply_template":
			nv, err := a.EmuTrafficTemplateApply(argStr(args, "template"), argStrs(args, "ports"))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Applied %s: the lab has %d streams and %d saved tests now; lab_traffic start runs the streams, lab_test runs the tests.", argStr(args, "template"), len(nv.Streams), len(a.EmuTests())), nil
		case "save_template":
			t, err := a.EmuTrafficTemplateSave(argStr(args, "template"), argStr(args, "description"), nil)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Saved the template %s: %d streams, %d tests, %d test ports.", t.Name, len(t.Streams), len(t.Tests), t.Ports), nil
		case "remove_template":
			return "Removed.", a.EmuTrafficTemplateRemove(argStr(args, "template"))
		case "remove_stream":
			for _, s := range tv.Streams {
				if s.Name == argStr(args, "name") || s.ID == argStr(args, "name") {
					if err := light("Claude wants to remove a stream", "Claude (through the Termwright connector) asks to remove the stream "+s.Name+"."); err != nil {
						return "", err
					}
					_, err := a.EmuStreamRemove(s.ID)
					return "Removed.", err
				}
			}
			return "", errors.New("no such stream")
		case "start":
			names := argStrs(args, "streams")
			if err := ask("Claude wants to start traffic", "Claude (through the Termwright connector) asks to start "+orStr(strings.Join(names, ", "), "every stream")+" on the lab's test ports.", false); err != nil {
				return "", err
			}
			if _, err := a.EmuTrafficStart(names); err != nil {
				return "", err
			}
			return "Started; lab_traffic report shows the counters (final when no stream is running).", nil
		case "stop":
			if err := light("Claude wants to stop traffic", "Claude (through the Termwright connector) asks to stop the traffic on the lab's test ports."); err != nil {
				return "", err
			}
			_, err := a.EmuTrafficStop()
			return "Stopped; the counters stay for the report.", err
		case "report":
			return a.EmuTrafficReportText()
		}
		return "", errors.New("action is list, add_port, set_port, remove_port, add_stream, remove_stream, start, stop or report")
	case "lab_test":
		tv, _ := a.EmuTraffic()
		port := func(n string) string {
			for _, t := range tv.Testers {
				if strings.EqualFold(t.Name, n) || t.ID == n {
					return t.ID
				}
			}
			return n
		}
		test := func(n string) string {
			for _, t := range a.EmuTests() {
				if strings.EqualFold(t.Name, n) || t.ID == n {
					return t.ID
				}
			}
			return n
		}
		switch argStr(args, "action") {
		case "list":
			var b strings.Builder
			for _, t := range a.EmuTests() {
				fmt.Fprintf(&b, "- %s: %s, %s → %s\n", t.Name, t.Kind, t.From, t.To)
			}
			for i, r := range a.EmuTestRuns() {
				if i == 0 {
					b.WriteString("Runs (newest first):\n")
				}
				fmt.Fprintf(&b, "- %s %s: %s %s%s\n", r.ID, r.Name, r.Status, r.Started, map[bool]string{true: " - " + r.Step, false: ""}[r.Step != ""])
			}
			if b.Len() == 0 {
				return "No test yet: add one.", nil
			}
			return b.String(), nil
		case "add":
			t := emu.TrafficTest{Name: argStr(args, "name"), Kind: argStr(args, "kind"), From: port(argStr(args, "from")), To: port(argStr(args, "to")), Monitor: port(argStr(args, "monitor")),
				VLAN: argInt(args, "vlan", 0, 4094), Trial: argInt(args, "trial", 0, 600), MaxPPS: argInt(args, "max_pps", 0, 100000), Rate: argInt(args, "rate", 0, 100000), Max: argInt(args, "max", 0, 1000000),
				Action: argStr(args, "test_action"), Switch: argStr(args, "switch"), Port: argStr(args, "port"), At: argInt(args, "at", 0, 3600), Hold: argInt(args, "hold", 0, 3600), Seconds: argInt(args, "seconds", 0, 86400),
				Server: argStr(args, "server"), Client: argStr(args, "client"), ServerIP: argStr(args, "server_ip"), Proto: argStr(args, "proto"), Bandwidth: argStr(args, "bandwidth"), Parallel: argInt(args, "parallel", 0, 128)}
			t.Reverse, _ = args["reverse"].(bool)
			t.Both, _ = args["both"].(bool)
			t.Loss, _ = args["loss"].(float64)
			t.Resolution, _ = args["resolution"].(float64)
			if xs, ok := args["sizes"].([]any); ok {
				for _, x := range xs {
					if f, ok := x.(float64); ok {
						t.Sizes = append(t.Sizes, int(f))
					}
				}
			}
			if err := light("Claude wants to add a test", "Claude (through the Termwright connector) asks to add the "+t.Kind+" test "+orStr(t.Name, "(no name)")+"."); err != nil {
				return "", err
			}
			if _, err := a.EmuTestSave(t); err != nil {
				return "", err
			}
			return "Added; lab_test run starts it.", nil
		case "remove":
			if err := light("Claude wants to remove a test", "Claude (through the Termwright connector) asks to remove the test "+argStr(args, "name")+"."); err != nil {
				return "", err
			}
			_, err := a.EmuTestRemove(test(argStr(args, "name")))
			return "Removed.", err
		case "run":
			if err := ask("Claude wants to run a test", "Claude (through the Termwright connector) asks to run the test "+argStr(args, "name")+" (traffic on the lab's test ports; a convergence test pulls a cable or stops a switch).", false); err != nil {
				return "", err
			}
			r, err := a.EmuTestRun(test(argStr(args, "name")))
			if err != nil {
				return "", err
			}
			return "Running as " + r.ID + " - lab_test results shows its progress and results.", nil
		case "stop":
			if err := light("Claude wants to stop the test", "Claude (through the Termwright connector) asks to stop the running test."); err != nil {
				return "", err
			}
			return "Stopped.", a.EmuTestStop()
		case "results":
			id := argStr(args, "run")
			if id == "" {
				if rs := a.EmuTestRuns(); len(rs) > 0 {
					id = rs[0].ID
				}
			}
			b, _, err := a.EmuTestRunReport(id, "text")
			return string(b), err
		}
		return "", errors.New("action is list, add, remove, run, stop or results")
	case "lab_capture":
		act := argStr(args, "action")
		if act == "list" {
			var b strings.Builder
			for _, c := range a.EmuCaptures() {
				fmt.Fprintf(&b, "- %s %s%s on %s: %s, %d bytes, started %s\n", c.Switch, c.Port, map[bool]string{true: " (cable to " + c.Peer + ")", false: ""}[c.Peer != ""], c.Host[strings.LastIndex(c.Host, "/")+1:], map[bool]string{true: "running", false: "stopped"}[c.Running], c.Size, c.Started)
			}
			if b.Len() == 0 {
				return "No capture yet.", nil
			}
			return b.String(), nil
		}
		d, err := emuMCPID(v, argStr(args, "switch"))
		if err != nil {
			return "", err
		}
		port := argStr(args, "port")
		if port == "" {
			return "", errors.New("give the port")
		}
		key := d.ID + "|" + port
		switch act {
		case "start":
			if err := ask("Claude wants to capture packets", fmt.Sprintf("Claude (through the Termwright connector) asks to capture %s %s (both ways; it keeps running).", d.Hostname, port), false); err != nil {
				return "", err
			}
			if _, err := a.EmuCaptureStart(d.ID, port); err != nil {
				return "", err
			}
			return "Capturing " + d.Hostname + " " + port + " - generate the traffic, then lab_capture read.", nil
		case "stop":
			if err := light("Claude wants to stop a capture", "Claude (through the Termwright connector) asks to stop capturing "+d.Hostname+" "+port+"."); err != nil {
				return "", err
			}
			if _, err := a.EmuCaptureStop(key); err != nil {
				return "", err
			}
			return "Stopped; the pcap file stays on the host (lab_capture read still works).", nil
		case "read":
			ps, err := a.EmuCapturePackets(key, 0)
			if err != nil {
				return "", err
			}
			f := strings.ToLower(argStr(args, "filter"))
			var keep []EmuPacket
			for _, p := range ps {
				if f == "" || strings.Contains(strings.ToLower(p.Src+" "+p.Dst+" "+p.Proto+" "+p.Info), f) {
					keep = append(keep, p)
				}
			}
			n := argInt(args, "last", 100, 2000)
			total := len(keep)
			if len(keep) > n {
				keep = keep[len(keep)-n:]
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%d packets match (%d in the capture); the last %d:\n", total, len(ps), len(keep))
			for _, p := range keep {
				fmt.Fprintf(&b, "%d %.3f %s -> %s %s %d %s\n", p.No, p.Time, p.Src, p.Dst, p.Proto, p.Len, p.Info)
			}
			return b.String(), nil
		}
		return "", errors.New("action is start, stop, read or list")
	case "lab_cpu":
		list, shown, err := ids(argStrs(args, "switches"))
		if err != nil {
			return "", err
		}
		mx, hasMax := args["max_cores"].(float64)
		sh, hasShare := args["share"].(float64)
		rq, hasReq := args["request_m"].(float64)
		lm, hasLim := args["limit_m"].(float64)
		if !hasMax && !hasShare && !hasReq && !hasLim {
			var b strings.Builder
			b.WriteString("Device | vCPU | request | limit | now | last hour: avg / p95 / peak | above request | held back by the limit | advice\n")
			mc := func(m float64, none string) string {
				if m <= 0 {
					return none
				}
				return strconv.Itoa(int(math.Round(m))) + "m"
			}
			for _, d := range v.Devices {
				if !slices.Contains(list, d.ID) {
					continue
				}
				hour, over, thr, adv := "-", "-", "-", "fine"
				if s := d.CPUStats; s != nil {
					hour = fmt.Sprintf("%s / %s / %s (%d min)", mc(s.Avg, "0m"), mc(s.P95, "0m"), mc(s.Peak, "0m"), s.Mins)
					if d.CPUReq > 0 {
						over = fmt.Sprintf("%d %% of the time, %d times", s.Over, s.OverN)
					}
					if d.CPULim > 0 {
						thr = fmt.Sprintf("%d times, %.1f s", s.Thr, s.ThrS)
					}
					if d.CPULim > 0 && (s.Thr >= 10 || s.ThrS >= 1) {
						adv = fmt.Sprintf("raise the limit (to about %s)", mc(math.Ceil(max(float64(d.CPULim)*1.5, s.P95*1.25)/100)*100, ""))
					} else if d.CPUReq > 0 && s.Avg > float64(d.CPUReq)*1.5 {
						adv = fmt.Sprintf("raise the request (to about %s)", mc(math.Ceil(s.P95/100)*100, ""))
					}
				}
				now := "-"
				if d.Running {
					now = mc(d.Cores*1000, "0m")
				}
				fmt.Fprintf(&b, "- %s | %d | %s | %s | %s | %s | %s | %s | %s\n", d.Hostname, d.CPUs, mc(float64(d.CPUReq), "none"), mc(float64(d.CPULim), "none"), now, hour, over, thr, adv)
			}
			return b.String(), nil
		}
		if err := light("Claude wants to change CPU limits", fmt.Sprintf("Claude (through the Termwright connector) asks to set the CPU of %s.", strings.Join(shown, ", "))); err != nil {
			return "", err
		}
		for _, id := range list {
			d, _ := emuMCPID(v, id)
			req, lim := d.CPUReq, d.CPULim
			if hasShare {
				req = int(sh) * 10
			}
			if hasMax {
				lim = int(math.Round(mx * 1000))
			}
			if hasReq {
				req = int(rq)
			}
			if hasLim {
				lim = int(lm)
			}
			if _, err := a.EmuSetCPUReq(id, req, lim); err != nil {
				return "", err
			}
		}
		return "Set for " + strings.Join(shown, ", ") + " (lab_events says how each took it).", nil
	case "lab_computer":
		d, err := emuMCPID(v, argStr(args, "computer"))
		if err != nil {
			return "", err
		}
		if argStr(args, "action") == "run" {
			if err := light("Claude wants to run an app on a computer", fmt.Sprintf("Claude (through the Termwright connector) asks to run %s %s on %s.", argStr(args, "app"), argStr(args, "target"), d.Hostname)); err != nil {
				return "", err
			}
			return a.EmuComputerRun(d.ID, argStr(args, "app"), argStr(args, "target"))
		}
		none := func(k string) string {
			if x := argStr(args, k); !strings.EqualFold(x, "none") {
				return x
			}
			return ""
		}
		n := EmuCompNet{Port: argInt(args, "port", 0, 4096), IPv4: none("ipv4"), IPv6: none("ipv6"), Gw: none("gateway"), Gw6: none("gateway6"), DNS: none("dns")}
		if _, ok := args["gateway"]; !ok {
			n.Gw = d.DataGw
		}
		if _, ok := args["gateway6"]; !ok {
			n.Gw6 = d.DataGw6
		}
		if _, ok := args["dns"]; !ok {
			n.DNS = d.DNS
		}
		if err := light("Claude wants to set a computer's addresses", fmt.Sprintf("Claude (through the Termwright connector) asks to set %s port %d: IPv4 %s, IPv6 %s, gateway %s, DNS %s.", d.Hostname, n.Port, orStr(n.IPv4, "none"), orStr(n.IPv6, "none"), orStr(n.Gw, "-"), orStr(n.DNS, "-"))); err != nil {
			return "", err
		}
		nv, err := a.EmuSetComputerNet(d.ID, n)
		if err != nil {
			return "", err
		}
		if !d.Running {
			return fmt.Sprintf("Saved: %s is not running; it is typed on its console when it starts.", d.Hostname), nil
		}
		// it is typed on the console, then read back: wait for the reading (a few seconds), so the answer is
		// what the computer has now, not what it had before
		want := strings.Split(n.IPv4, "/")[0]
		shows := func(x EmuDevice) string {
			return fmt.Sprintf("port %d %s; gateway %s; DNS %s", n.Port, orStr(x.Net.Addrs[n.Port], "no address"), orStr(x.Net.GW, "-"), orStr(x.Net.DNS, "-"))
		}
		for end := time.Now().Add(25 * time.Second); ; {
			for _, x := range nv.Devices {
				if x.ID != d.ID || x.Net == nil {
					continue
				}
				got := x.Net.Addrs[n.Port]
				if strings.EqualFold(n.IPv4, "dhcp") && got != "" || want == "" && got == "" || want != "" && !strings.EqualFold(n.IPv4, "dhcp") && strings.HasPrefix(got, want+"/") {
					return "Set. Its console shows: " + shows(x), nil
				}
			}
			if time.Now().After(end) {
				break
			}
			time.Sleep(time.Second)
			if nv, err = a.EmuState(); err != nil {
				return "", err
			}
		}
		for _, x := range nv.Devices {
			if x.ID == d.ID && x.Net != nil {
				return "Typed, but its console has not shown the new address yet (it may still be applying it; lab_debug shows the console). It shows: " + shows(x), nil
			}
		}
		return "Set (typed when it starts).", nil
	case "lab_address":
		if argStr(args, "action") == "set" {
			d, err := emuMCPID(v, argStr(args, "switch"))
			if err != nil {
				return "", err
			}
			addr, port := argStr(args, "address"), argInt(args, "port", 0, 4096)
			if strings.EqualFold(addr, "none") {
				addr = ""
			}
			if err := light("Claude wants to set an address", fmt.Sprintf("Claude (through the Termwright connector) asks to give %s port %d the address %s.", d.Hostname, port, orStr(addr, "none"))); err != nil {
				return "", err
			}
			if _, err := a.EmuSetPortAddr(d.ID, port, addr); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s port %d: %s (typed on its console when it runs; lab_events shows it)", d.Hostname, port, orStr(addr, "no address")), nil
		}
		var b strings.Builder
		for _, d := range v.Devices {
			if !d.Computer {
				continue
			}
			fmt.Fprintf(&b, "- %s (management %s):", d.Hostname, orStr(d.MgmtIP, "none yet"))
			if len(d.Addrs) == 0 {
				b.WriteString(" no data-port addresses")
			}
			for _, p := range slices.Sorted(maps.Keys(d.Addrs)) {
				fmt.Fprintf(&b, " port %d %s;", p, d.Addrs[p])
			}
			b.WriteString("\n")
		}
		if b.Len() == 0 {
			return "There are no Linux computers in the lab (add one from Add → Linux and test tools).", nil
		}
		return b.String(), nil
	case "lab_link_check", "lab_qemu_command":
		d, err := emuMCPID(v, argStr(args, "switch"))
		if err != nil {
			return "", err
		}
		if name == "lab_qemu_command" {
			return a.EmuQemuCommand(d.ID)
		}
		return a.emuLinkCheck(d.ID, argInt(args, "seconds", 30, 120))
	case "lab_debug":
		d, err := emuMCPID(v, argStr(args, "switch"))
		if err != nil {
			return "", err
		}
		out, err := a.emuDebug(d.ID, argInt(args, "lines", 200, 3000))
		if err != nil {
			return "", err
		}
		out, _ = aiRedact(out, false)
		return out, nil
	case "lab_history":
		switch argStr(args, "action") {
		case "plan", "revert":
			id := argStr(args, "id")
			if id == "" {
				return "", errors.New("give the id of the change (lab_history action=list)")
			}
			if argStr(args, "action") == "plan" {
				steps, err := a.EmuRevertPlan(id)
				if err != nil {
					return "", err
				}
				return "Revert would:\n- " + strings.Join(steps, "\n- "), nil
			}
			steps, err := a.emuRevertAs(id, emuWhoName(who))
			if err != nil {
				return "", err
			}
			return "Reverted:\n- " + strings.Join(steps, "\n- "), nil
		}
		cs := a.EmuChanges()
		if len(cs) == 0 {
			return "No changes yet.", nil
		}
		n := argInt(args, "last", 20, 200)
		var b strings.Builder
		b.WriteString("Changes, newest first (id | time | who | tool | what | result):\n")
		for i, c := range cs {
			if i >= n {
				break
			}
			t, _ := time.Parse(time.RFC3339, c.At)
			fmt.Fprintf(&b, "- %s | %s | %s | %s | %s | %s", c.ID, t.Format("15:04:05"), c.Who, c.Tool, c.What, c.Result)
			if c.Reverted != "" {
				b.WriteString(" | reverted")
			}
			if !c.Revertible {
				b.WriteString(" | cannot be undone")
			} else if c.Note != "" {
				b.WriteString(" | " + c.Note)
			}
			b.WriteString("\n")
		}
		return b.String(), nil
	case "lab_events":
		n := argInt(args, "last", 60, 300)
		ev := v.Events
		if len(ev) > n {
			ev = ev[len(ev)-n:]
		}
		var b strings.Builder
		for _, e := range ev {
			h := e.Host
			if h != "" {
				h = h[strings.LastIndex(h, "/")+1:] + " "
			}
			fmt.Fprintf(&b, "%s %s%s %s %s\n", e.At.Format("15:04:05"), h, e.Name, strings.ToUpper(e.Level), e.Text)
		}
		out, _ := aiRedact(b.String(), false)
		return out, nil
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

func slicesContainsFold(l []string, s string) bool {
	for _, x := range l {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// emuConsoleRun logs in on a switch's console and types commands, returning each one's output.
func (a *App) emuConsoleRun(id string, cmds []string, timeout time.Duration) (string, error) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var v *emu.VM
	if l != nil {
		v = l.VMByID(id)
	}
	if v == nil {
		emuLab.mu.Unlock()
		return "", errors.New("no such switch")
	}
	host := emuHostOf(*v)
	lh := a.emuFor(host)
	port := lh.ConsolePort(*v)
	ip := emuImage(v.Image)
	if ip == nil {
		emuLab.mu.Unlock()
		return "", errors.New("its image is gone")
	}
	im := *ip
	emuLab.mu.Unlock()
	im.Password, _ = secrets.Lookup(emuImageKey(im.ID))
	im.Password = orStr(im.Password, im.DefaultPassword)
	cl, err := a.emuClient(context.Background(), host)
	if err != nil {
		return "", err
	}
	conn, err := cl.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return "", fmt.Errorf("its console: %w (is it running, and its console tab closed?)", err)
	}
	c := emu.NewConsole(conn)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	prompt, err := emu.Login(ctx, c, &im)
	if err != nil {
		return "", fmt.Errorf("logging in on its console: %w", err)
	}
	var b strings.Builder
	for _, cmd := range cmds {
		emuLogf(emuLvInfo, host, v.Name, "console (AI connector) types: %s", cmd)
		out, err := emu.Run(ctx, c, &im, prompt, cmd)
		fmt.Fprintf(&b, "%s\n%s\n", cmd, strings.TrimRight(out, "\r\n "))
		if err != nil {
			return b.String(), fmt.Errorf("%s: %w", cmd, err)
		}
	}
	return b.String(), nil
}

// emuReadyForTests says whether a device is up for a test: running with nothing left to do (not booting,
// not typing its day 0, not moving) and no problem. A running device's state text says "running" (or
// "running · no persona"), so the state decides, not whether the text is empty.
func emuReadyForTests(d EmuDevice) bool {
	return d.Running && d.State == "running" && d.Error == ""
}
