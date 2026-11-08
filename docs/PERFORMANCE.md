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

## Running the tests

```
TW_PERF=20000 go test -run TestPerfSessions -v .
TW_PERF_MB=64 TW_PERF_TABS=1 go test -run TestPerfTerminalOutput -v ./internal/term/
TW_PERF_MB=8 TW_PERF_TABS=20 go test -run TestPerfTerminalOutput -v ./internal/term/
TW_PERF_TEAM=1 go test -run TestPerfTeam -v ./internal/team/
```

The window's numbers come from the Angular build with a simulated backend in a headless browser.
