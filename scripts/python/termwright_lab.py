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
        self.snmp = SNMP(self)
        self.http = HTTP(self)
        self.syslog = Syslog(self)
        self.devices = Devices(self)
        self.tests = Tests(self)

    # ---- plain requests ----
    def request(self, method: str, path: str, body: Any = None, timeout: Optional[float] = None, raw: bool = False, binary: bool = False,
                root: bool = False, **params):
        url = self.api + ("" if root else "/lab") + path
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
                if binary:
                    return r.read()
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

    def skills(self) -> list:
        """The AI skills (SKILL.md procedures the AI assistant follows), without their text."""
        return self.request("GET", "/ai/skills", root=True) or []

    def skill(self, name: str) -> dict:
        """One skill with the whole text of its SKILL.md."""
        return self.request("GET", "/ai/skills/" + urllib.parse.quote(name, safe=""), root=True)

    def save_skill(self, text: str, replace: str = "") -> dict:
        """Add or replace a skill from the text of a SKILL.md (replace: its name before, to rename it)."""
        return self.request("POST", "/ai/skills", {k: v for k, v in {"text": text, "replace": replace}.items() if v}, root=True)

    def tool(self, name: str, /, **args) -> str:
        """Run a lab tool (power, cable, console, wait, capture, address, cpu, computer, debug, ...). Its
        arguments are the ones in the OpenAPI document; "timeout" is also how long to wait for the answer."""
        body = {k: v for k, v in args.items() if v is not None}
        wait = max(self.timeout, float(body.get("timeout") or 0) + 60)
        r = self.request("POST", f"/tools/{name}", body, timeout=wait)
        return r["result"] if isinstance(r, dict) and "result" in r else r

    def power(self, action: str, *switches: str) -> str:
        return self.tool("power", action=action, switches=list(switches))

    def remove(self, *devices: str, sessions: bool = True) -> str:
        """Remove devices from the lab: stopped, their disks and cables gone, and their saved sessions unless
        sessions=False. The lab's history (history(), revert()) can bring them back."""
        return self.tool("remove", devices=list(devices), sessions=sessions)

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

    def screen(self, device: str, path: Optional[str] = None) -> bytes:
        """A PNG of what a device's display shows now (install:<id> for an installer); saved to path when given."""
        png = self.request("GET", "/screen/" + urllib.parse.quote(device, safe=":"), binary=True)
        if path:
            with open(path, "wb") as f:
                f.write(png)
        return png

    def keys(self, device: str, text: str = "", enter: bool = False, keys: Optional[list] = None) -> str:
        """Type on a device's keyboard: text, then Enter; keys are QEMU key names (esc, tab, f2, ctrl-alt-delete…)."""
        return self.tool("keys", switch=device, text=text, enter=enter, keys=keys)

    def install_type(self, install_id: str, line: str, console: bool = True) -> str:
        """Type a line (and Enter) into a running install: on its serial console, or (console=False) its keyboard."""
        return self.tool("keys", install=install_id, text=line, enter=True, console=console)

    def desktop(self, computer: str, on: bool = True, home: str = "") -> str:
        """Give a Linux computer a desktop with a web browser on its screen (restart it for the mouse)."""
        return self.tool("computer", action="desktop", computer=computer, on=on, home=home or None)

    def click(self, device: str, x: int, y: int, button: str = "left", double: bool = False) -> str:
        """Click on a device's screen at x, y (its pixels); a computer with a desktop."""
        return self.tool("keys", switch=device, click=dict(x=x, y=y, button=button, double=double))

    def routes(self, computer: str, action: str = "show", **args) -> str:
        """A Linux computer as a route generator (FRR): action set (proto="ospf"|"bgp", port, routes, prefix, area,
        as_, peer, peer_as), show, off, stream (from_=test port, pps: an ICMP stream to every prefix)."""
        if "as_" in args:
            args["as"] = args.pop("as_")
        if "from_" in args:
            args["from"] = args.pop("from_")
        return self.tool("routes", action=action, computer=computer, **args)

    def host_images(self, action: str = "matrix", **args) -> str:
        """Which KVM host has which image: action matrix, copy (image, hosts=[...] - none: every host without it),
        remove (image, host)."""
        return self.tool("host_images", action=action, **args)

    def radius(self, action: str = "show", **args) -> str:
        """The lab's RADIUS server (802.1X and MAC logins): action show, log, set (on, port, secret, mab, mab_vlan),
        add_user (user - a name or a pattern like user* - password, vlan), remove_user (user). reveal=True shows the
        secret and the passwords."""
        return self.tool("radius", action=action, **args)

    def hub(self, action: str = "list", **args) -> str:
        """Hubs, L2 switches and clouds: action list, add (kind="hub"|"switch", name), connect / disconnect (hub,
        switch, port), remove (hub), show (hub), ports (hub, count 8-24), port (hub, number, mode="access"|"trunk",
        vlan, vlans=[...]), management (hub, ip, vlan), cloud_ports (host, count 1-8)."""
        return self.tool("hub", action=action, **args)

    def checkpoint(self, switch: str, note: str = "") -> str:
        """Keep a running switch's whole state now (it pauses for a moment); lists its checkpoints."""
        return self.tool("checkpoint", action="take", switch=switch, note=note or None)

    def checkpoints(self, switch: str) -> str:
        return self.tool("checkpoint", action="list", switch=switch)

    def restore(self, switch: str, tag: str) -> str:
        """Bring a running switch back to a checkpoint (its configuration, tables and sessions)."""
        return self.tool("checkpoint", action="restore", switch=switch, tag=tag)

    def auto_checkpoints(self, on: bool = True) -> str:
        """A checkpoint before typed commands (so history revert undoes them), or none."""
        return self.tool("checkpoint", action="auto", on=on)

    def dns(self, *servers: str, default: bool = False) -> str:
        """The lab's name servers; with servers, sets them (at most three); default=True goes back to 8.8.8.8 1.1.1.1.
        Computers without one get them when they start; day 0 fills {dns} {dns2} {dnsList}."""
        if servers or default:
            return self.tool("dns", servers=list(servers))
        return self.tool("dns")

    def images(self, name: Optional[str] = None, type: Optional[str] = None) -> str:
        """The images and templates, filtered by part of a name and by device type (EXOS, VOSS, Linux…)."""
        return self.tool("images", name=name, type=type)

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
        """A test port on a free switch port; returns its name. Fields: ip6, gateway, vlan, groups, lldp, speed,
        lag, lacp_fast, clients (many hosts behind it: dict(n=200, dhcp=True, dhcp6=True, groups=[...], dot1x=dict(user="user{n}",
        password=..., method="peap"), rate=50)), stp (dict(mode="rstp", prio=4096)), vrrp (dict(ver=3, vrid=1, prio=50,
        vip="10.10.0.254", interval=1000, preempt=True)), bfd (dict(peer="10.10.0.1", tx=300, mult=3))."""
        before = {p["name"] for p in self.ports()}
        v = self.lab.request("POST", "/traffic/ports", _body(dict(switch=switch or None, port=port or None, ip=ip, **fields)))
        new = [p["name"] for p in (v.get("testers") or []) if p["name"] not in before]
        if len(new) != 1:
            raise ApiError(0, "unexpected", f"the new test port was not found among {new}")
        return new[0]

    def add_host_port(self, nic: str, host: Optional[str] = None, ip: Optional[str] = None, cabled: str = "", **fields) -> str:
        """A test port on a KVM host's spare network card (nic, e.g. "enp3s0") cabled to a real switch; returns its
        name. nics(host) lists the cards that can be one. The generator needs root on the host, or passwordless sudo."""
        return self.add_port("", "", ip=ip, nic=nic, host=host, cabled=cabled, **fields)

    def add_trex_port(self, trex: str, port: int, ip: Optional[str] = None, cabled: str = "", **fields) -> str:
        """A test port on a TRex server's port (add_trex first); TRex sends its streams at line rate. Returns its name."""
        return self.add_port("", "", ip=ip, trex=trex, trex_port=port, cabled=cabled, **fields)

    def nics(self, host: Optional[str] = None) -> list:
        """A KVM host's network cards: name, mac, up, speed, driver and used (why it cannot be a test port; "" = free)."""
        return self.lab.request("GET", "/traffic/nics", **({"host": host} if host else {}))

    def engines(self) -> dict:
        """What sends the streams: engine (python or fast), hosts and the TRex servers with their ports."""
        return self.lab.request("GET", "/traffic/engines")

    def set_engine(self, engine: str) -> dict:
        """The KVM hosts' engine: "python" (built in) or "fast" (a compiled helper copied to the hosts: millions of
        frames a second)."""
        return self.lab.request("PUT", "/traffic/engines", {"engine": engine})

    def add_trex(self, addr: str, name: str = "", port: int = 4501) -> dict:
        """Adds a TRex server (stateless mode, its API on TCP 4501) and checks it; returns the engines."""
        return self.lab.request("POST", "/traffic/trex", {"addr": addr, "name": name or addr, "port": port})

    def remove_trex(self, name: str) -> None:
        """Forgets a TRex server, its test ports and their streams."""
        self.lab.request("DELETE", "/traffic/trex/" + urllib.parse.quote(name, safe=""))

    def set_port(self, name: str, **fields) -> dict:
        """Change a test port: the fields of add_port, topologyChange=True (its next BPDUs say so) and silentBfd=True
        (no BFD for 5 s: bfdState["measured"] is how fast the switch saw it). vrrp=dict(..., silent=True) stops its
        VRRP advertisements: vrrpState["takeover"] is how fast the switch took over (ms)."""
        return self.lab.request("PATCH", "/traffic/ports/" + urllib.parse.quote(name, safe=""), _body(fields))

    def clients(self, name: str) -> dict:
        """How a test port's clients are doing: n, bound (DHCP), bound6 (DHCPv6), auth (802.1X), failed, list (the
        first 256: MAC, address, 802.1X state, why it failed, IPv6 address)."""
        for p in self.ports():
            if p["name"] == name:
                return p.get("clientState") or {}
        raise ApiError(404, "not-found", "no test port " + name)

    def remove_port(self, name: str) -> None:
        self.lab.request("DELETE", "/traffic/ports/" + urllib.parse.quote(name, safe=""))

    # ---- streams ----
    def add_stream(self, name: str, src: str, dst: Optional[str] = None, rate: Optional[float] = None,
                   rate_unit: str = "mbps", line_mbps: Optional[float] = None, **fields) -> dict:
        """A stream from test port src to dst (empty: every other port, or dst_ip). Fields as in the API,
        in snake_case: kind, size, pps, seconds, count, vlan, pcp (prio), dscp, size_mode, size_max, burst,
        max_loss, max_latency, max_jitter, mods, dst_ip, dst_mac, dport, rtt (round trip: the far test port
        sends each frame back; latency is the round trip). kind="pcap" replays a capture: pcap (its file on
        this computer), pcap_timing (its own gaps), loops, rewrite_macs. rate= gives the speed in Mbit/s on the wire, or with
        rate_unit="pct" in % of the sending test port's line rate (set_port(speed=...), default 1000 Mbit/s;
        line_mbps= works it out here instead) - in place of pps.
        kind="custom" sends a frame you write: hex (from the destination MAC on, no FCS; lengths and checksums
        are worked out unless no_fix=True, the counting signature goes at its end unless no_sig=True).
        udfs=[{"offset": 26, "width": 4, "mode": "inc", "start": 0x0a0a0001, "count": 100}] changes bytes per
        frame; bad="ipcsum" (or l4csum, ttl0, runt, frag-tiny, tcp-xmas … - see the guide) puts a fault in
        every frame; fuzz={"from": 14, "to": 42, "seed": 7} randomizes bytes; encap={"outer": {"vlan": 100},
        "mpls": [{"label": 1000}]} or {"vxlan": {"vni": 10010, "dst": "10.255.0.1"}} or {"gre": {"dst": ...}} or
        {"v6ext": [{"type": "hbh"}]} adds headers (the size stays the size on the wire); expect="drop" passes when the
        switch drops them, expect="any" only counts them.
        kind="session" opens real TCP sessions to dst, which answers them: session={"dport": 80, "count": 1000,
        "concurrent": 100, "rate": 100, "req": 1000, "resp": 10000, "hold": 0, "http": False, "expect": ""} (expect
        "refused": every one should get a reset, "dropped": no answer); its result has sessions (tried, ok, refused,
        timeout, setupAvg ms, cps); max_loss is the % of sessions that may fail, max_latency the setup time in ms."""
        if rate is not None:
            if "pps" in fields:
                raise ValueError("give pps or rate, not both")
            if rate_unit == "pps":
                fields["pps"] = int(rate)
            elif line_mbps is not None:
                size = int(fields.get("size") or 128)
                if fields.get("size_mode") == "imix":
                    size = 362
                fields["pps"] = pps_for(rate, size, rate_unit, line_mbps)
            else:
                fields["rate_unit"], fields["rate"] = rate_unit, rate
        if "pcp" in fields:
            fields["prio"] = fields.pop("pcp")
        if fields.get("expect") == "forward":
            fields.pop("expect")
        body = _body(dict(name=name, from_=src, to=dst, **fields))
        body.setdefault("size", 128)
        return self.lab.request("POST", "/traffic/streams", body)

    # ---- the template library ----
    def templates(self) -> str:
        """The library of ready tests (Termwright's and yours): name, test ports it needs, what it checks."""
        return self.lab.tool("traffic", action="templates")

    def apply_template(self, template: str, *ports: str) -> str:
        """Put a template's streams and tests on the test ports given for its roles A, B, C… in order."""
        return self.lab.tool("traffic", action="apply_template", template=template, ports=list(ports))

    def save_template(self, name: str, description: str = "") -> str:
        """Keep the lab's streams and saved tests as a template of yours (their test ports become roles A, B, …)."""
        return self.lab.tool("traffic", action="save_template", template=name, description=description)

    def remove_template(self, name: str) -> str:
        return self.lab.tool("traffic", action="remove_template", template=name)

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

    def port_stats(self) -> dict:
        """Every test port's statistics by its name (the Traffic page's Statistics): tx, rx (all its frames),
        txBytes, rxBytes, txRate, rxRate (frames/s now), txMbps, rxMbps, expected and got (its streams' frames that
        were for it), loss (%), latAvg, latMax (ms, -1 not measured), errors, streams (frames received per stream id)."""
        v = self.view()
        names = {t["id"]: t["name"] for t in v["testers"]}
        return {names.get(p["id"], p["id"]): p for p in v.get("ports") or []}

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

    def report(self, fmt: str = "json", path: Optional[str] = None) -> Any:
        """The run's report: json (the traffic view), csv, html, text, or pdf and xlsx (bytes). With path, saved there
        (lab.traffic.report("xlsx", "run.xlsx")): the report to share, with graphs, topology, frames sent and received."""
        if fmt not in ("json", "csv", "html", "text", "pdf", "xlsx"):
            raise ValueError("format: json, csv, html, text, pdf or xlsx")
        out = self.lab.request("GET", "/traffic/report", timeout=120 if fmt == "pdf" else None, raw=fmt != "json", binary=fmt in ("pdf", "xlsx"), format=fmt)
        if path:
            with open(path, "wb" if isinstance(out, bytes) else "w") as fh:
                fh.write(out if isinstance(out, (bytes, str)) else json.dumps(out, indent=2))
        return out

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


class SNMP:
    """SNMP against a saved session (its community / v3 passwords stay in the OS credential store) or an address.
    Results are dicts: rows (oid, name, type, value, display), address, version, ms, requests, truncated."""

    def __init__(self, lab: Lab):
        self.lab = lab

    def run(self, op: str, *oids: str, session: Optional[str] = None, address: Optional[str] = None,
            version: Optional[str] = None, community: Optional[str] = None, values: Optional[list] = None,
            max: Optional[int] = None, max_rep: Optional[int] = None, timeout: Optional[int] = None) -> dict:
        body: dict = {"op": op}
        if session:
            body["session"] = session
        if address:
            body["target"] = {k: v for k, v in {"address": address, "version": version, "community": community}.items() if v}
        if oids:
            body["oids"] = list(oids)
        if values:
            body["values"] = values
        for k, v in (("max", max), ("maxRep", max_rep), ("timeout", timeout)):
            if v is not None:
                body[k] = v
        return self.lab.request("POST", "/snmp", body, root=True)

    def get(self, *oids: str, **target) -> dict:
        return self.run("get", *oids, **target)

    def walk(self, oid: str = "1.3.6.1.2.1", **target) -> dict:
        return self.run("walk", oid, **target)

    def bulk(self, *oids: str, **target) -> dict:
        return self.run("bulk", *oids, **target)

    def set(self, oid: str, value: Any, type: str = "", **target) -> dict:
        """Write one value (the session must allow writes). type is optional: the MIB gives it, enum names work."""
        v = {"oid": oid, "value": str(value)}
        if type:
            v["type"] = type
        return self.run("set", values=[v], **target)

    def mib_search(self, q: str) -> list:
        return self.lab.request("GET", "/snmp/mib", root=True, q=q) or []

    def mib(self, obj: str) -> dict:
        return self.lab.request("GET", "/snmp/mib/" + urllib.parse.quote(obj, safe=""), root=True)

    def send_trap(self, address: str, trap_oid: str, values: Optional[list] = None, version: str = "2c", community: str = "public", **v3) -> dict:
        """Send a test notification (values: [{"oid": "ifOperStatus.3", "value": "down"}]; v3: user, auth, authPass, priv, privPass)."""
        body = {"address": address, "trapOid": trap_oid, "version": version, "community": community, "values": values or [], **v3}
        return self.lab.request("POST", "/snmp/trap", body, root=True)

    def traps(self) -> dict:
        """The trap receiver's state and the traps it has received, newest first."""
        return self.lab.request("GET", "/snmp/traps", root=True)

    @staticmethod
    def table(result: dict) -> dict:
        """{oid name: display} of a result."""
        return {r["name"]: r["display"] for r in result.get("rows") or []}


class HTTP:
    """The REST API client with saved API sessions (base URL, login and secrets stay in Termwright) and collections."""

    def __init__(self, lab: Lab):
        self.lab = lab

    def send(self, url: str, method: str = "GET", session: Optional[str] = None, env: str = "", headers: Optional[dict] = None,
             params: Optional[dict] = None, json_body: Any = None, text: Optional[str] = None, tests: str = "",
             vars: Optional[dict] = None, collection: Optional[str] = None, timeout: Optional[int] = None, environment: str = "") -> dict:
        """Send a request; a path goes after the session's base URL. Returns status, headers, body, ms, tests...
        environment: an environment's id or name (its variables win over the session's)."""
        req: dict = {"method": method.upper(), "url": url}
        if headers:
            req["headers"] = [{"key": k, "value": str(v)} for k, v in headers.items()]
        if params:
            req["params"] = [{"key": k, "value": str(v)} for k, v in params.items()]
        if json_body is not None:
            req["body"] = {"mode": "json", "raw": json.dumps(json_body)}
        elif text is not None:
            req["body"] = {"mode": "text", "raw": text}
        if tests:
            req["tests"] = tests
        body = {"request": req}
        for k, v in (("session", session), ("env", env), ("vars", vars), ("collection", collection), ("timeout", timeout), ("environment", environment)):
            if v:
                body[k] = v
        return self.lab.request("POST", "/http/send", body, root=True, timeout=max(self.lab.timeout, (timeout or 30) + 30))

    def get(self, url: str, **kw) -> dict:
        return self.send(url, "GET", **kw)

    def post(self, url: str, json_body: Any = None, **kw) -> dict:
        return self.send(url, "POST", json_body=json_body, **kw)

    @staticmethod
    def json(res: dict) -> Any:
        """The answer's body as JSON."""
        return json.loads(res.get("body") or "null")

    def collections(self) -> list:
        return self.lab.request("GET", "/http/collections", root=True) or []

    def collection(self, name: str) -> dict:
        return self.lab.request("GET", "/http/collections/" + urllib.parse.quote(name, safe=""), root=True)

    def save_collection(self, collection: dict) -> dict:
        if collection.get("id"):
            return self.lab.request("PUT", "/http/collections/" + urllib.parse.quote(collection["id"], safe=""), collection, root=True)
        return self.lab.request("POST", "/http/collections", collection, root=True)

    def run(self, collection: str, session: Optional[str] = None, env: str = "", requests: Optional[list] = None, folder: str = "",
            iterations: int = 0, data: str = "", stop_on_fail: bool = False, environment: str = "") -> dict:
        """Run a collection (postman.setNextRequest works); returns steps, passed, failed, tests."""
        body = {k: v for k, v in {"session": session, "env": env, "requests": requests, "folder": folder, "iterations": iterations,
                                  "data": data, "stopOnFail": stop_on_fail, "environment": environment}.items() if v}
        return self.lab.request("POST", "/http/collections/" + urllib.parse.quote(collection, safe="") + "/run", body, root=True, timeout=3600)

    def import_(self, text: str, name: str = "", session: str = "") -> dict:
        """Import a cURL command, a Postman collection or an OpenAPI document as a collection."""
        return self.lab.request("POST", "/http/import", {k: v for k, v in {"text": text, "name": name, "session": session}.items() if v}, root=True)

    def history(self) -> list:
        return self.lab.request("GET", "/http/history", root=True) or []

    def environments(self) -> list:
        """The environments (Postman's), the globals (id "globals") first; secret values are never given."""
        return self.lab.request("GET", "/http/environments", root=True) or []

    def save_environment(self, name: str, variables: dict, secret: Optional[list] = None, id: str = "") -> dict:
        """Add or replace an environment from {name: value}; names in secret go to the credential store."""
        sec = set(secret or [])
        vs = [{"key": k, "value": str(v), "secret": k in sec} for k, v in variables.items()]
        return self.lab.request("POST", "/http/environments", {"id": id, "name": name, "vars": vs}, root=True)

    def variables(self, session: Optional[str] = None, collection: Optional[str] = None, environment: str = "", env: str = "") -> list:
        """Every variable a request can use, with its value and source (secret values left out)."""
        body = {k: v for k, v in {"session": session, "collection": collection, "environment": environment, "env": env,
                                  "request": {"method": "GET", "url": ""}}.items() if v}
        return self.lab.request("POST", "/http/variables", body, root=True) or []


class Devices:
    """Device facts and device types of saved sessions (logs in in the background with the saved login)."""

    def __init__(self, lab: "Lab"):
        self.lab = lab

    def _run(self, **args) -> str:
        r = self.lab.request("POST", "/tools/device", {k: v for k, v in args.items() if v is not None}, root=True, timeout=max(self.lab.timeout, 150))
        return r["result"] if isinstance(r, dict) and "result" in r else r

    def facts(self, session: str, refresh: bool = False) -> str:
        """The device facts (model, version, serial number, MAC address, uptime ...); refresh reads them now."""
        return self._run(action="refresh_facts" if refresh else "facts", session=session)

    def identify(self, session: str) -> str:
        """Which device type's Detect pattern matches what the device shows (show version, display version ...)."""
        return self._run(action="identify", session=session)

    def set_type(self, session: str, device_type: str) -> str:
        """Set a session's device type (asked in Termwright on a production session or when changes ask)."""
        return self._run(action="set_type", session=session, type=device_type)

    def types(self) -> str:
        """The device types with their Detect patterns and commands."""
        return self._run(action="types")


class Tests:
    """Termwright tests: YAML files in the tests folder (cases/), run against saved sessions and API sessions."""

    def __init__(self, lab: "Lab"):
        self.lab = lab

    def _run(self, timeout: Optional[float] = None, **args) -> str:
        r = self.lab.request("POST", "/tools/tests", {k: v for k, v in args.items() if v is not None}, root=True, timeout=timeout or max(self.lab.timeout, 150))
        return r["result"] if isinstance(r, dict) and "result" in r else r

    def list(self) -> str:
        """The tests: file, name, number of steps, tags."""
        return self._run(action="list")

    def show(self, file: str) -> str:
        """One test as YAML (file relative to the tests folder; cases/ and .yaml may be left out)."""
        return self._run(action="show", file=file)

    def save(self, file: str, yaml_text: str) -> str:
        """Write a test from YAML (never with passwords: name saved sessions)."""
        return self._run(action="save", file=file, yaml=yaml_text)

    def run(self, *files: str, vars: Optional[dict] = None, testbed: Optional[str] = None, on_cleanup_fail: str = "",
            cleanup: str = "", timeout: float = 1800) -> str:
        """Run tests (none: all), optionally on a testbed (a file in testbeds/). on_cleanup_fail: what happens
        when a test's cleanup fails - "stop" (default: the remaining tests are skipped), "restore" (put back the
        configuration saved before the run) or "factory" (factory default); both may reboot the devices.
        cleanup: "steps" (default), "off" (no cleanup - tests build on each other) or "auto" (the cleanup
        steps, else the undo of the configuration commands that ran).
        cleanup: "steps" (default), "off" (no cleanup - tests build on each other) or "auto" (the cleanup
        steps, else the undo of the configuration commands that ran).
        Returns pass/fail per step and the report's path (JUnit XML and HTML in reports/)."""
        return self._run(timeout=timeout, action="run", files=list(files) or None, vars=vars, testbed=testbed,
                         on_cleanup_fail=on_cleanup_fail or None, cleanup=cleanup or None)

    def new_testbed(self, name: str, folder: str) -> str:
        """Write testbeds/<name>.yaml with a role for each saved session of a session folder (core-sw1 -> {{core_sw1}})."""
        return self._run(action="new_testbed", name=name, folder=folder)

    def generate(self, *files: str) -> str:
        """Write the Go, Python and TCL tests again (none: all); see USER_GUIDE "Tests"."""
        return self._run(action="generate", files=list(files) or None)

    def schedules(self) -> str:
        """The scheduled runs with their last result and next run."""
        return self._run(action="schedules")

    def schedule(self, name: str, repeat: str = "daily", at: Optional[str] = None, day: Optional[int] = None, hours: Optional[int] = None,
                 files: Optional[list] = None, testbed: Optional[str] = None, production: bool = False, id: Optional[str] = None) -> str:
        """Add (or, with id, change) a scheduled run: repeat daily, weekdays, weekly (day 0-6) or hours; at "HH:MM"."""
        return self._run(action="schedule", name=name, repeat=repeat, at=at, day=day, hours=hours, files=files, testbed=testbed,
                         production="true" if production else None, id=id)

    def unschedule(self, id: str) -> str:
        """Remove a scheduled run."""
        return self._run(action="unschedule", id=id)

    def results(self) -> str:
        """The last run's report."""
        return self._run(action="results")


class Syslog:
    """Termwright's syslog server (UDP, TCP, TLS) and client."""

    def __init__(self, lab: Lab):
        self.lab = lab

    def status(self) -> dict:
        return self.lab.request("GET", "/syslog", root=True)

    def start(self, udp: Optional[int] = None, tcp: Optional[int] = None, tls: Optional[int] = None, **settings) -> dict:
        """Start (or restart) it; no arguments use the saved settings. Ports below 1024 may need administrator rights."""
        body = {k: v for k, v in {"udp": udp, "tcp": tcp, "tls": tls, **settings}.items() if v is not None}
        if body:
            cur = self.status().get("settings") or {}
            body = {**cur, **body}
        return self.lab.request("POST", "/syslog/start", body or None, root=True)

    def stop(self) -> dict:
        return self.lab.request("POST", "/syslog/stop", {}, root=True)

    def messages(self, q: str = "", host: str = "", severity: str = "", minutes: int = 0, after: int = 0, limit: int = 0) -> list:
        """Messages received, oldest first: words or /regex/ in q, a sender, this severity or worse, the last minutes."""
        return self.lab.request("GET", "/syslog/messages", root=True, q=q or None, host=host or None, severity=severity or None,
                                minutes=minutes or None, after=after or None, limit=limit or None) or []

    def wait_for(self, q: str, timeout: float = 60, **kw) -> dict:
        """Wait until a message matching q arrives (after now); returns it."""
        start = max([m["id"] for m in self.messages(limit=1)] or [0])
        end = time.time() + timeout
        while time.time() < end:
            got = self.messages(q, after=start, **kw)
            if got:
                return got[0]
            time.sleep(1)
        raise TimeoutError("no syslog message matching %r" % q)

    def clear(self) -> None:
        self.lab.request("DELETE", "/syslog/messages", root=True)

    def send(self, target: str, text: str, severity: str = "notice", facility: str = "user", proto: str = "udp", **kw) -> str:
        """Send a test message to a syslog server; returns it as sent."""
        body = {"target": target, "text": text, "severity": severity, "facility": facility, "proto": proto, **kw}
        return self.lab.request("POST", "/syslog/send", body, root=True)["sent"]

    def rules(self) -> list:
        return self.lab.request("GET", "/syslog/rules", root=True) or []

    def set_rules(self, rules: list) -> list:
        return self.lab.request("PUT", "/syslog/rules", {"rules": rules}, root=True)


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
    ("POST", "/snmp"), ("GET", "/snmp/mib"), ("GET", "/snmp/mib/{object}"), ("GET", "/snmp/traps"), ("POST", "/snmp/trap"),
    ("POST", "/http/send"), ("GET", "/http/collections"), ("POST", "/http/collections"), ("GET", "/http/collections/{collection}"),
    ("PUT", "/http/collections/{collection}"), ("POST", "/http/collections/{collection}/run"), ("POST", "/http/import"), ("GET", "/http/history"),
    ("GET", "/syslog"), ("POST", "/syslog/start"), ("POST", "/syslog/stop"), ("GET", "/syslog/messages"), ("DELETE", "/syslog/messages"),
    ("POST", "/syslog/send"), ("GET", "/syslog/rules"), ("PUT", "/syslog/rules"),
]
