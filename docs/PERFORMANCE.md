# Performance

Measured in a Linux sandbox (no GPU) with simulated sessions and devices, 2026-11-08. The tests that produce these numbers are in the repository and run only when asked.

## Termwright

| What | 20,000 sessions | Notes |
|---|---|---|
| Session Manager, first load | 0.9 s | every session file is read once |
| Session Manager, reload after a change | 0.08 s | files that did not change are not read again (0.8 s before 0.33) |
| Session Manager in the window | 0.5 s to show, 35 rows drawn | only the visible rows are drawn, also with every folder open |
| Search as you type | 60-80 ms per key | |
| Save one session | 1 ms | |
| Export all to a zip | 2.2 s | |
| Sync, idle check every 30 s | 0.16 s | unchanged files are not read again |
| Terminal output to the window | 370-500 MB/s | 1 tab or 20 tabs at once, in 256 KB batches |
| Terminal drawing (xterm.js) | about 12 MB/s | 20 MB of `show interfaces` in 1.7 s; a keypress right after answered in 21 ms |
| Memory of the window | 40 MB | with 20,000 sessions |

## Team server

A team of 200 members, 5,000 inventory devices and a year of usage (584,000 recorded sessions, 51 MB):

| What | Time | Notes |
|---|---|---|
| Inventory download (each member) | 30 ms | |
| 200 members' heartbeat and status at the same moment | 0.45 s for all | *last used* is sent only when it changed (it was sent to everyone each time, 750 KB per member) |
| 200 members connecting at the same moment | 2.8 s for all | the first status has the full *last used* list |
| Usage report, 30 days / 1 year | 0.05 s / 0.7 s | compressed on the way (51 MB of JSON for a year) |
| Writing the usage history | 0.3-0.8 s, every 15 s in the background | was written on every change, holding everyone up |
| A reservation | 10 ms | |
| Push 5,000 devices at once | 1.8 s | |

Requests may be up to 16 MB (a restored backup up to 256 MB); before 0.33 every request was limited to 1 MB, so restoring the backup of a busy team failed.

## Traffic generator

Measured 2026-11-16 on a cloud VM with two 2.3 GHz server vCPUs, Linux 6.18 and Python 3.13. Two test ports wired to each other, no switch between them, one UDP stream for 6 seconds; the sending and the receiving test port run in the same engine on the same two vCPUs. The rates are the highest each engine kept with at most 0.01 % loss (the fast engine, 64 bytes over sockets: 0.12 %); Mbit/s count the frame with its FCS, without preamble and gap.

| Frame size | Built-in engine, sockets | Built-in engine, network cards | Fast engine, sockets | Fast engine, network cards |
|---|---|---|---|---|
| 64 bytes | 109,502 frames/s, 74 Mbit/s | 103,203 frames/s, 69 Mbit/s | 472,016 frames/s, 317 Mbit/s | 419,965 frames/s, 282 Mbit/s |
| 512 bytes | 82,003 frames/s, 349 Mbit/s | 80,161 frames/s, 341 Mbit/s | 315,119 frames/s, 1,341 Mbit/s | 315,084 frames/s, 1,341 Mbit/s |
| 1518 bytes | 58,106 frames/s, 715 Mbit/s | 61,087 frames/s, 752 Mbit/s | 210,035 frames/s, 2,584 Mbit/s | 210,032 frames/s, 2,584 Mbit/s |

- *Sockets*: the QEMU socket networking test ports on emulated switches use (a UDP socket pair). *Network cards*: a veth pair, as a host's spare network card cabled to a real switch is used (raw AF_PACKET sockets).
- Asked for more over network cards, the fast engine reached 628,661 frames/s of 64 bytes (0.22 % lost), 444,606 of 512 bytes (5.8 % lost) and 264,227 of 1518 bytes, 3.25 Gbit/s (21 % lost).
- The built-in engine sends every frame it is asked for, only later than asked when it is too slow; the fast engine keeps the rate and counts what did not arrive.
- TCP sessions between two test ports of the built-in engine: a setup time (SYN to SYN-ACK) of about 1.2 ms.
- An emulated switch is slower than either engine: a switch VM with one vCPU and rtl8139 cards forwarded about 3,500 frames/s of 64 bytes and 2,700 of 1518 bytes without loss (RFC 2544 throughput).
- TRex servers send at the line rate of their DPDK cards; Termwright only drives them, so their speed is TRex's own.

## Running the tests

```
TW_PERF=20000 go test -run TestPerfSessions -v ./app/
TW_PERF_MB=64 TW_PERF_TABS=1 go test -run TestPerfTerminalOutput -v ./internal/term/
TW_PERF_MB=8 TW_PERF_TABS=20 go test -run TestPerfTerminalOutput -v ./internal/term/
TW_PERF_TEAM=1 go test -run TestPerfTeam -v ./internal/team/
TW_BENCH=1 go test -run TestTrafficBench -v ./internal/emu/
TW_BENCH=1 TW_BENCH_NIC=1 TW_BENCH_RATES=64:420000,512:315000,1518:210000 go test -run TestTrafficBench -v ./internal/emu/   # as root: a veth pair
```

`TW_BENCH_RATES` sets the fast engine's rate per frame size (frames/s); without it, it is asked for 2,000,000 and shows how much arrives.

The window's numbers come from the Angular build with a simulated backend in a headless browser.
