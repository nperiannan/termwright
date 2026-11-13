# Python client for the emulated lab

`scripts/python/termwright_lab.py` is one file, standard library only (Python 3.9 or later), over the
[local API](API.md). Copy it next to your script or put its folder on `PYTHONPATH`. Termwright must be
running with **Settings → AI Assistant → Local API** on. The client finds the address and key by itself:
from `TERMWRIGHT_API` (up to `/api/v1`) and `TERMWRIGHT_KEY` when both are set, otherwise from `mcp.json` in
Termwright's data folder (`TERMWRIGHT_HOME` when set). The key changes each time Termwright starts, and the
client reads it again each time a `Lab()` is made.

```python
from termwright_lab import Lab

lab = Lab()
lab.power("start", "leaf-1", "leaf-2")
lab.wait(timeout=900)

t = lab.traffic
a = t.add_port("leaf-1", "5", ip="10.10.0.1/24")
b = t.add_port("leaf-2", "5", ip="10.10.0.2/24")
res = t.run_streams([dict(name="voice", src=a, dst=b, size=200, pps=1000, seconds=20, dscp=46, max_loss=0.1)])
t.assert_pass(res)
```

A refused request raises `ApiError` with `status`, `title` and `detail` (the RFC 9457 problem); `status` is 0
when Termwright cannot be reached.

## The lab

`Lab` has one method for each lab tool: `show()`, `power(action, *switches)`, `wait(*switches, timeout=600)`,
`cable(a, a_port, b, b_port)`, `uncable(a, a_port)`, `console(switch, *commands)`, `debug(switch)`,
`events()`, `computer_set(computer, port, ipv4=…, gateway=…)`, `computer(computer, app, target)`,
`address()`, `cpu(…, request_m=, limit_m=)`, `capture()`, and for images made from an ISO file `install_iso(iso_path, mode, password=…, disk_gb= or disk_mb=)`,
`install_status()`, `install_finish(id)` and `wait_install(id)`; `history()` lists what changed in the lab (with ids), `revert_plan(id)` and `revert(id)` bring the lab back to before a change. `tool(name, **arguments)` runs any tool in `GET /lab/tools`.

## Traffic

Test ports and streams are as in the API; the keyword arguments are the API's fields in snake_case
(`max_loss` is `maxLoss`).

| Call | What it does |
|---|---|
| `add_port(switch, port, ip=…)` | A test port on a free switch port; returns its name. `ip6`, `gateway`, `vlan`, `groups`, `lldp` as in the API |
| `add_stream(name, src, dst, …)` | A stream. `rate=` gives the speed in Mbit/s on the wire, or with `rate_unit="pct"` in % of the sending test port's line rate (`set_port(name, speed=10000)`; `line_mbps=` works it out in Python instead), in place of `pps=`; `rtt=True` measures round-trip latency (also between hosts); `pcp=` is the VLAN priority; `size_mode="imix"`, `mods=[…]` for flows. Results carry `p50`, `p95`, `p99`, `hist`, `flows`, `marks` |
| `start(*names)`, `stop()`, `wait_done()` | Send, stop, wait; `wait_done` returns the results by stream name |
| `run_streams([…])` | Add, send, wait, return the results, and remove the streams again (`keep=True` leaves them) |
| `assert_pass(results)` | Raises `AssertionError` naming each stream that failed and why |
| `report("json"\|"csv"\|"html"\|"text")` | The report of the last run |
| `save_test(name, kind, src, dst, …)`, `run_test(name)` | A saved test and its run: `throughput`, `latency`, `frame-loss`, `back-to-back` (RFC 2544), `learning` (RFC 2889), `convergence`, `iperf`. `run_test` returns the run (`rows`, `log`) and raises when it failed or was stopped |
| `rfc2544(name, src, dst, sizes=…)` | The four RFC 2544 tests in one call; returns `{kind: run}` |
| `convergence(name, src, dst, switch, port, action="pull", at=5, hold=10)` | Traffic while a cable is pulled or a switch stops or restarts; the rows hold the frames lost and the outage in ms |
| `iperf(name, server, client, server_ip, proto="tcp", parallel=4)` | iperf3 between two computers of the lab |
| `rows(run)` | The result rows of a run |
| `pps_for(rate, size)` | Frames per second for a rate in Mbps, as hardware testers count it (20 bytes of preamble and gap per frame) |

`scripts/python/example_traffic.py` is a complete run. With pytest or unittest, `assert_pass` is the check. `scripts/python/example_two_computers.py pc-1 pc-2 10.20.0.11/24 10.20.0.12/24` is the two-computer test: an address on each computer's first data port, ping and iperf3 both ways, PASS or FAIL per step (exit 1 when one fails).

## Tests of the client

`python3 -m unittest test_termwright_lab` in `scripts/python` runs a fake API server in the same process and
checks every call the client makes against `docs/api/openapi.yaml` (it needs `pyyaml` for that part and skips
it without).
