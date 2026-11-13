"""termwright_lab - a small Python client for Termwright's local lab API.

Standard library only (Python 3.9+). Termwright must be running with
Settings -> AI Assistant -> Local API on. The contract is docs/api/openapi.yaml.

    from termwright_lab import Lab

    lab = Lab()                                   # finds the address and key itself
    lab.power("start", "leaf-1", "leaf-2")
    lab.wait(timeout=900)
    t = lab.traffic
    a = t.add_port("leaf-1", "5", ip="10.10.0.1/24")
    b = t.add_port("leaf-2", "5", ip="10.10.0.2/24")
    res = t.run_streams([dict(name="voice", src=a, dst=b, pps=1000, seconds=20, dscp=46, max_loss=0.1)])
    t.assert_pass(res)
"""
from __future__ import annotations

import json
import os
import pathlib
import platform
import re
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Iterable, Optional

__all__ = ["Lab", "Traffic", "ApiError", "pps_for", "find_endpoint"]


class ApiError(Exception):
    """The API refused a request (an RFC 9457 problem)."""

    def __init__(self, status: int, title: str = "", detail: str = "", kind: str = ""):
        super().__init__(f"{status} {title}: {detail}" if detail else f"{status} {title}")
        self.status, self.title, self.detail, self.kind = status, title, detail, kind


def _data_dir() -> pathlib.Path:
    if os.environ.get("TERMWRIGHT_HOME"):
        return pathlib.Path(os.environ["TERMWRIGHT_HOME"])
    home = pathlib.Path.home()
    system = platform.system()
    if system == "Darwin":
        return home / "Library" / "Application Support" / "Termwright"
    if system == "Windows":
        return pathlib.Path(os.environ.get("APPDATA", "")) / "Termwright"
    return home / ".config" / "Termwright"


def find_endpoint() -> tuple[str, str]:
    """The API address (up to /api/v1) and key: TERMWRIGHT_API and TERMWRIGHT_KEY, else mcp.json
    in Termwright's data folder (the key changes each time Termwright starts)."""
    url, key = os.environ.get("TERMWRIGHT_API", ""), os.environ.get("TERMWRIGHT_KEY", "")
    if url and key:
        return url.rstrip("/"), key
    p = _data_dir() / "mcp.json"
    try:
        ep = json.loads(p.read_text())
    except (OSError, ValueError) as e:
        raise ApiError(0, "not running", f"cannot read {p}: Termwright is not running with its Local API on ({e})")
    base = ep["url"]
    if base.endswith("/mcp"):
        base = base[: -len("/mcp")]
    return base.rstrip("/") + "/api/v1", ep["token"]


def _camel(name: str) -> str:
    head, *rest = name.split("_")
    return head + "".join(w.title() for w in rest)


def _body(fields: dict) -> dict:
    """snake_case keyword arguments as the API's camelCase fields (max_loss -> maxLoss); None is left out."""
    out = {}
    for k, v in fields.items():
        if v is not None:
            out[_camel(k.rstrip("_"))] = v
    return out


def pps_for(rate: float, size: int, unit: str = "mbps", line_mbps: float = 1000.0) -> int:
    """Frames per second for a rate the way hardware testers state it: unit "mbps" (layer-1 megabits per
    second, counting the 20 bytes of preamble and gap per frame) or "pct" (percent of line_mbps)."""
    mbps = rate if unit == "mbps" else line_mbps * rate / 100.0
    return max(1, int(mbps * 1e6 / 8 / (size + 20)))


class Lab:
    """The emulated lab. Methods return what the API sent, as dicts and lists."""

    def __init__(self, api: Optional[str] = None, key: Optional[str] = None, timeout: float = 120.0):
        if api and key:
            self.api, self.key = api.rstrip("/"), key
        else:
            self.api, self.key = find_endpoint()
        self.timeout = timeout
        self.traffic = Traffic(self)

    # ---- plain requests ----
    def request(self, method: str, path: str, body: Any = None, timeout: Optional[float] = None, raw: bool = False, **params):
        url = self.api + "/lab" + path
        q = {k: v for k, v in params.items() if v is not None}
        if q:
            url += "?" + urllib.parse.urlencode(q)
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(url, data=data, method=method)
        req.add_header("Authorization", "Bearer " + self.key)
        if data is not None:
            req.add_header("Content-Type", "application/json")
        try:
            with urllib.request.urlopen(req, timeout=timeout or self.timeout) as r:
                text = r.read().decode()
        except urllib.error.HTTPError as e:
            try:
                p = json.loads(e.read().decode())
            except ValueError:
                p = {}
            raise ApiError(e.code, p.get("title", e.reason), p.get("detail", ""), str(p.get("type", ""))) from None
        except urllib.error.URLError as e:
            raise ApiError(0, "not reachable", str(e.reason)) from None
        if raw or not text.strip():
            return text
        try:
            return json.loads(text)
        except ValueError:
            return text

    # ---- the lab ----
    def show(self) -> dict:
        """The open topology: switches, state, addresses, consoles, cables, hosts, images, Events."""
        return self.request("GET", "")

    def tools(self) -> list:
        return self.request("GET", "/tools")

    def tool(self, name: str, /, **args) -> str:
        """Run a lab tool (power, cable, console, wait, capture, address, cpu, computer, debug, ...). Its
        arguments are the ones in the OpenAPI document; "timeout" is also how long to wait for the answer."""
        body = {k: v for k, v in args.items() if v is not None}
        wait = max(self.timeout, float(body.get("timeout") or 0) + 60)
        r = self.request("POST", f"/tools/{name}", body, timeout=wait)
        return r["result"] if isinstance(r, dict) and "result" in r else r

    def power(self, action: str, *switches: str) -> str:
        return self.tool("power", action=action, switches=list(switches))

    def wait(self, *switches: str, timeout: int = 600) -> str:
        """Block until the switches are booted and configured (none named: all)."""
        return self.tool("wait", switches=list(switches), timeout=timeout)

    def cable(self, a: str, a_port: str, b: str, b_port: str) -> str:
        return self.tool("cable", cables=[dict(a=a, a_port=a_port, b=b, b_port=b_port)])

    def uncable(self, a: str, a_port: str) -> str:
        return self.tool("cable", remove=True, cables=[dict(a=a, a_port=a_port)])

    def console(self, switch: str, *commands: str, timeout: int = 120) -> str:
        """Type commands on a switch's serial console and return each one's output."""
        return self.tool("console", switch=switch, commands=list(commands), timeout=timeout)

    def debug(self, switch: str, lines: int = 200) -> str:
        """Everything about why a switch or computer is stuck: state, step, error, Events, console end."""
        return self.tool("debug", switch=switch, lines=lines)

    def install_iso(self, iso_path: str, mode: str = "manual", name: Optional[str] = None, password: Optional[str] = None,
                    disk_gb: Optional[int] = None, ram_mb: Optional[int] = None, cpus: Optional[int] = None,
                    host: Optional[str] = None, disk_mb: Optional[int] = None) -> str:
        """Make an image from an ISO file of the Termwright computer: mode "alpine" installs Alpine Linux by itself
        (password = root's), "manual" waits for you to install on the installer's console (then install_finish)."""
        return self.tool("install_iso", iso_path=iso_path, mode=mode, name=name, password=password,
                         disk_gb=disk_gb, disk_mb=disk_mb, ram_mb=ram_mb, cpus=cpus, host=host)

    def history(self, last: int = 20) -> str:
        """What Claude and the API changed in the lab, newest first, with ids for revert()."""
        return self.tool("history", action="list", last=last)

    def revert_plan(self, change_id: str) -> str:
        """What revert() would do to bring the lab back to before a change."""
        return self.tool("history", action="plan", id=change_id)

    def revert(self, change_id: str) -> str:
        """Bring the lab back to how it was before a change (devices added since are removed, cables, CPU,
        addresses, test ports and streams set back, devices started or stopped as they were)."""
        return self.tool("history", action="revert", id=change_id)

    def install_status(self) -> str:
        return self.tool("install_status")

    def install_finish(self, install_id: str) -> str:
        return self.tool("install_finish", id=install_id)

    def wait_install(self, install_id: str, timeout: float = 1800, poll: float = 5.0) -> str:
        """Block until the install is done (returns its row); raises when it failed, was cancelled or took too long."""
        end = time.time() + timeout
        while True:
            for row in self.install_status().splitlines():
                f = [x.strip() for x in row.lstrip("- ").split("|")]
                if len(f) >= 6 and f[0] == install_id:
                    if f[3] == "done":
                        return row
                    if f[3] in ("failed", "cancelled"):
                        raise RuntimeError("install %s %s: %s" % (install_id, f[3], f[4]))
            if time.time() > end:
                raise TimeoutError("install %s was not done after %s s" % (install_id, timeout))
            time.sleep(poll)

    def events(self, last: int = 60) -> str:
        return self.tool("events", last=last)

    def address(self, computer: str, port: int, address: str) -> str:
        """A Linux computer's data port: address/bits, dhcp or none."""
        return self.tool("address", action="set", switch=computer, port=port, address=address)

    def computer(self, computer: str, app: str, target: str = "") -> str:
        """Run an app on a computer: ping, ping6, traceroute, web, dns, iperf-server, iperf."""
        return self.tool("computer", action="run", computer=computer, app=app, target=target)

    def computer_set(self, computer: str, port: int = 1, ipv4: Optional[str] = None, ipv6: Optional[str] = None,
                     gateway: Optional[str] = None, gateway6: Optional[str] = None, dns: Optional[str] = None) -> str:
        return self.tool("computer", action="set", computer=computer, port=port, ipv4=ipv4, ipv6=ipv6,
                         gateway=gateway, gateway6=gateway6, dns=dns)

    def cpu(self, *switches: str, request_m: Optional[int] = None, limit_m: Optional[int] = None,
            max_cores: Optional[float] = None, share: Optional[int] = None) -> str:
        """CPU request and limit in millicores (250 = 250m; 0 = none); without them, the report of the last hour."""
        return self.tool("cpu", switches=list(switches), request_m=request_m, limit_m=limit_m, max_cores=max_cores, share=share)

    def capture(self, **args) -> str:
        return self.tool("capture", **args)


class Traffic:
    """The traffic generator: test ports, streams, saved tests (RFC 2544, RFC 2889, convergence, iperf3), reports."""

    def __init__(self, lab: Lab):
        self.lab = lab

    # ---- test ports ----
    def view(self) -> dict:
        v = self.lab.request("GET", "/traffic")
        for k in ("testers", "streams", "results"):
            v[k] = v.get(k) or []
        return v

    def ports(self) -> list:
        return self.view()["testers"]

    def add_port(self, switch: str, port: str, ip: Optional[str] = None, **fields) -> str:
        """A test port on a free switch port; returns its name. Fields: ip6, gateway, vlan, groups, lldp."""
        before = {p["name"] for p in self.ports()}
        v = self.lab.request("POST", "/traffic/ports", _body(dict(switch=switch, port=port, ip=ip, **fields)))
        new = [p["name"] for p in (v.get("testers") or []) if p["name"] not in before]
        if len(new) != 1:
            raise ApiError(0, "unexpected", f"the new test port was not found among {new}")
        return new[0]

    def set_port(self, name: str, **fields) -> dict:
        return self.lab.request("PATCH", "/traffic/ports/" + urllib.parse.quote(name, safe=""), _body(fields))

    def remove_port(self, name: str) -> None:
        self.lab.request("DELETE", "/traffic/ports/" + urllib.parse.quote(name, safe=""))

    # ---- streams ----
    def add_stream(self, name: str, src: str, dst: Optional[str] = None, rate: Optional[float] = None,
                   rate_unit: str = "mbps", line_mbps: float = 1000.0, **fields) -> dict:
        """A stream from test port src to dst (empty: every other port, or dst_ip). Fields as in the API,
        in snake_case: kind, size, pps, seconds, count, vlan, pcp (prio), dscp, size_mode, size_max, burst,
        max_loss, max_latency, max_jitter, mods, dst_ip, dst_mac, dport. rate= gives the speed in Mbps (or
        rate_unit="pct" of line_mbps) instead of pps."""
        if rate is not None:
            if "pps" in fields:
                raise ValueError("give pps or rate, not both")
            size = int(fields.get("size") or 128)
            if fields.get("size_mode") == "imix":
                size = 362
            fields["pps"] = pps_for(rate, size, rate_unit, line_mbps)
        if "pcp" in fields:
            fields["prio"] = fields.pop("pcp")
        body = _body(dict(name=name, from_=src, to=dst, **fields))
        body.setdefault("size", 128)
        return self.lab.request("POST", "/traffic/streams", body)

    def replace_stream(self, name: str, **fields) -> dict:
        return self.lab.request("PUT", "/traffic/streams/" + urllib.parse.quote(name, safe=""), _body(fields))

    def remove_stream(self, name: str) -> None:
        self.lab.request("DELETE", "/traffic/streams/" + urllib.parse.quote(name, safe=""))

    def start(self, *streams: str) -> dict:
        return self.lab.request("POST", "/traffic/start", {"streams": list(streams)})

    def stop(self) -> dict:
        return self.lab.request("POST", "/traffic/stop", {})

    def results(self) -> dict:
        """The latest result of each stream, by stream name."""
        v = self.view()
        names = {s["id"]: s["name"] for s in v["streams"]}
        return {names.get(r["id"], r["id"]): r for r in v["results"]}

    def wait_done(self, timeout: float = 600, poll: float = 1.0) -> dict:
        """Block until nothing is sending; returns results by stream name."""
        end = time.time() + timeout
        time.sleep(min(poll, 1.0))
        while True:
            v = self.view()
            if not v.get("running"):
                break
            if time.time() > end:
                self.stop()
                raise TimeoutError("the streams were still sending after %s s (stopped)" % timeout)
            time.sleep(poll)
        time.sleep(1.0)  # counters are final a moment after the stop
        return self.results()

    def run_streams(self, streams: Iterable[dict], timeout: float = 600, keep: bool = False) -> dict:
        """Add the streams (dicts for add_stream: name, src, dst, ...), send them, wait, return the results
        by name. The streams are removed afterwards unless keep=True."""
        names = []
        try:
            for s in streams:
                s = dict(s)
                name = s.pop("name")
                self.add_stream(name, s.pop("src"), s.pop("dst", None), **s)
                names.append(name)
            self.start(*names)
            return self.wait_done(timeout)
        finally:
            if not keep:
                for n in names:
                    try:
                        self.remove_stream(n)
                    except ApiError:
                        pass

    @staticmethod
    def assert_pass(results: dict) -> None:
        """Raise AssertionError naming every stream that did not pass, with the reason."""
        bad = [f"{n}: {r.get('result')} - {r.get('why', '')} (tx {r.get('tx')}, rx {r.get('rx')}, loss {r.get('loss')}%)"
               for n, r in results.items() if r.get("result") != "pass"]
        if bad or not results:
            raise AssertionError("; ".join(bad) or "no results")

    def report(self, fmt: str = "json") -> Any:
        if fmt not in ("json", "csv", "html", "text"):
            raise ValueError("format: json, csv, html or text")
        return self.lab.request("GET", "/traffic/report", raw=fmt != "json", format=fmt)

    # ---- saved tests ----
    def tests(self) -> list:
        return self.lab.request("GET", "/traffic/tests") or []

    def save_test(self, name: str, kind: str, src: Optional[str] = None, dst: Optional[str] = None, **fields) -> list:
        """Kinds: throughput, latency, frame-loss, back-to-back (RFC 2544), learning (RFC 2889),
        convergence, iperf. Replaces a test of the same name."""
        body = _body(dict(name=name, kind=kind, from_=src, to=dst, **fields))
        if any(t["name"] == name for t in self.tests()):
            return self.lab.request("PUT", "/traffic/tests/" + urllib.parse.quote(name, safe=""), body)
        return self.lab.request("POST", "/traffic/tests", body)

    def remove_test(self, name: str) -> None:
        self.lab.request("DELETE", "/traffic/tests/" + urllib.parse.quote(name, safe=""))

    def run_test(self, name: str, timeout: float = 1800, poll: float = 2.0) -> dict:
        """Run a saved test and wait for it; returns the run (status, rows, log). Raises when it failed."""
        r = self.lab.request("POST", "/traffic/tests/" + urllib.parse.quote(name, safe="") + "/run")
        run_id = r["id"]
        end = time.time() + timeout
        while True:
            run = self.lab.request("GET", "/traffic/runs/" + run_id)
            if run.get("status") != "running":
                break
            if time.time() > end:
                self.lab.request("POST", "/traffic/runs/stop", {})
                raise TimeoutError(f"test {name} was still running after {timeout} s (stopped)")
            time.sleep(poll)
        if run.get("status") != "done":
            raise ApiError(0, "test " + str(run.get("status")), run.get("error") or "")
        return run

    # RFC 2544 and friends, in one call each
    def rfc2544(self, name: str, src: str, dst: str, sizes: Iterable[int] = (64, 128, 256, 512, 1024, 1280, 1518),
                tests: Iterable[str] = ("throughput", "latency", "frame-loss", "back-to-back"), both: bool = True,
                max_pps: Optional[int] = None, trial: int = 5, **fields) -> dict:
        """The RFC 2544 suite: runs each test (saved as "<name> <kind>") and returns {kind: run}."""
        out = {}
        for kind in tests:
            n = f"{name} {kind}"
            self.save_test(n, kind, src, dst, both=both, sizes=list(sizes), max_pps=max_pps, trial=trial, **fields)
            out[kind] = self.run_test(n)
        return out

    def convergence(self, name: str, src: str, dst: str, switch: str, port: str, action: str = "pull",
                    seconds: int = 30, at: int = 5, hold: int = 10, rate: int = 1000, **fields) -> dict:
        """Traffic while a cable is pulled (action pull) or a switch stops/restarts; the run's rows hold the
        frames lost and the outage in ms."""
        self.save_test(name, "convergence", src, dst, both=True, rate=rate, seconds=seconds, action=action,
                       switch=switch, port=port, at=at, hold=hold, **fields)
        return self.run_test(name)

    def iperf(self, name: str, server: str, client: str, server_ip: str, proto: str = "tcp", seconds: int = 10,
              parallel: int = 1, bandwidth: Optional[str] = None, reverse: bool = False) -> dict:
        """iperf3 between two computers of the lab (their consoles)."""
        self.save_test(name, "iperf", server=server, client=client, server_ip=server_ip, proto=proto,
                       seconds=seconds, parallel=parallel, bandwidth=bandwidth, reverse=reverse or None)
        return self.run_test(name)

    @staticmethod
    def rows(run: dict) -> list:
        """The result rows of a run (a list of dicts)."""
        return run.get("rows") or []

    def runs(self) -> list:
        return self.lab.request("GET", "/traffic/runs") or []

    def stop_test(self) -> dict:
        return self.lab.request("POST", "/traffic/runs/stop", {})

    def run_report(self, run: str = "latest", fmt: str = "text") -> Any:
        return self.lab.request("GET", "/traffic/runs/" + run, raw=fmt != "json", format=fmt)


# Every (method, path) the client calls: a test checks them against docs/api/openapi.yaml.
ENDPOINTS = [
    ("GET", "/lab"), ("GET", "/lab/tools"),
    ("POST", "/lab/tools/power"), ("POST", "/lab/tools/wait"), ("POST", "/lab/tools/cable"),
    ("POST", "/lab/tools/console"), ("POST", "/lab/tools/debug"), ("POST", "/lab/tools/events"),
    ("POST", "/lab/tools/address"), ("POST", "/lab/tools/computer"), ("POST", "/lab/tools/cpu"),
    ("POST", "/lab/tools/capture"),
    ("GET", "/lab/traffic"), ("GET", "/lab/traffic/report"),
    ("POST", "/lab/traffic/ports"), ("PATCH", "/lab/traffic/ports/{port}"), ("DELETE", "/lab/traffic/ports/{port}"),
    ("POST", "/lab/traffic/streams"), ("PUT", "/lab/traffic/streams/{stream}"), ("DELETE", "/lab/traffic/streams/{stream}"),
    ("POST", "/lab/traffic/start"), ("POST", "/lab/traffic/stop"),
    ("GET", "/lab/traffic/tests"), ("POST", "/lab/traffic/tests"), ("PUT", "/lab/traffic/tests/{test}"),
    ("DELETE", "/lab/traffic/tests/{test}"), ("POST", "/lab/traffic/tests/{test}/run"),
    ("GET", "/lab/traffic/runs"), ("GET", "/lab/traffic/runs/{run}"), ("POST", "/lab/traffic/runs/stop"),
]
