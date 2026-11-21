package skills

// Examples are starter skills (Add Examples): plain SKILL.md text the user changes to fit their network.
var Examples = []string{
	`---
name: troubleshoot-port
description: Find why a switch port is down, flaps or has errors - link state, counters, optics, logs and the other end. Use for "port down", "flapping", "CRC errors", "no link".
---
# Troubleshoot a port

1. Read the port's state and speed (EXOS: show ports <port> information detail; VOSS: show interfaces gigabitEthernet <port>; Cisco: show interfaces <port>).
2. Read its error counters twice, 10 seconds apart, and say which ones grow (EXOS: show ports <port> rxerrors; VOSS: show interfaces gigabitEthernet error <port>; Cisco: show interfaces <port> counters errors).
3. Read the optics when it has a transceiver (EXOS: show ports <port> transceiver information; VOSS: show pluggable-optical-modules detail <port>; Cisco: show interfaces <port> transceiver).
4. Read the log for the port (EXOS: show log match <port>; VOSS: show logging file | include <port>; Cisco: show logging | include <port>).
5. Find the other end with LLDP and say what is connected.
6. Report: what is wrong, the evidence for it, and what to check or change - do not change anything without asking.
`,
	`---
name: backup-before-change
description: Save the device's running configuration before any change, so it can be put back. Use before a configuration change, an upgrade or a reboot.
---
# Back up before a change

1. Read the running configuration (EXOS: show configuration; VOSS and Cisco: show running-config) - turn paging off first.
2. Tell the user the configuration was read and how many lines it has; Termwright's Configuration Backups (Session → Back Up Configuration) keep a copy with history.
3. Only then go on with the change, and say which commands put it back if the change goes wrong.
`,
	`---
name: upgrade-exos-firmware
description: Upgrade an EXOS switch to a new image - check the image and partition, download from Termwright's file server, install, reboot, verify. Use for "upgrade", "new firmware", "new image" on EXOS.
device-types: EXOS
---
# Upgrade EXOS firmware

1. show switch and show version: note the image and the partition in use (primary or secondary).
2. Ask which image (.xos file) unless the user said; it must be in Termwright's file server folder (File Servers).
3. download url {url of the image} vr VR-Mgmt - it installs to the partition that is not in use. Wait for it to finish.
4. use image partition <that partition>, then save configuration.
5. Ask before reboot: it interrupts traffic. On a production session only in the change window the instructions give.
6. After the reboot, connect again and show version must show the new image; say so, or what went wrong.
`,
}
