"""Tests for termwright_lab: a fake API server in this process, and a check of every call against
docs/api/openapi.yaml. Run: python3 -m unittest scripts/python/test_termwright_lab.py -v"""
import json
import pathlib
import re
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

import termwright_lab as tw

ROOT = pathlib.Path(__file__).resolve().parents[2]


class Fake(BaseHTTPRequestHandler):
    state = {}

    def log_message(self, *a):
        pass

    def reply(self, code, body, ctype="application/json"):
        data = body if isinstance(body, str) else json.dumps(body)
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.end_headers()
        self.wfile.write(data.encode())

    def handle_any(self):
        st = Fake.state
        n = int(self.headers.get("Content-Length") or 0)
        body = json.loads(self.rfile.read(n)) if n else None
        st.setdefault("calls", []).append((self.command, self.path, body))
        if self.headers.get("Authorization") != "Bearer KEY":
            return self.reply(401, {"title": "Unauthorized", "status": 401, "detail": "bad key"}, "application/problem+json")
        path = self.path.split("?")[0].removeprefix("/api/v1/lab")
        v = st.setdefault("view", {"testers": [], "streams": [], "results": [], "running": False})
        m = self.command
        if self.path.startswith("/api/v1/snmp"):
            if self.path == "/api/v1/snmp" and m == "POST":
                return self.reply(200, {"rows": [{"oid": "1.3.6.1.2.1.1.5.0", "name": "SNMPv2-MIB::sysName.0", "type": "OctetString",
                                                  "value": "sw1", "display": "sw1"}], "address": "10.0.0.1:161", "version": "2c"})
            if self.path.startswith("/api/v1/snmp/mib?"):
                return self.reply(200, [{"oid": "1.3.6.1.2.1.2.2.1.8", "name": "ifOperStatus", "module": "IF-MIB"}])
            if self.path == "/api/v1/snmp/mib/IF-MIB%3A%3AifOperStatus":
                return self.reply(200, {"oid": "1.3.6.1.2.1.2.2.1.8", "name": "ifOperStatus"})
            if self.path == "/api/v1/snmp/traps":
                return self.reply(200, {"state": {"on": False, "port": 0, "count": 0}, "traps": []})
        if self.path.startswith("/api/v1/syslog"):
            if self.path.startswith("/api/v1/syslog/messages") and m == "GET":
                return self.reply(200, [{"id": 7, "text": "1/1 link down", "severity": 3}])
            if self.path == "/api/v1/syslog/send":
                return self.reply(200, {"sent": "<187>1 ..."})
            if self.path == "/api/v1/syslog":
                return self.reply(200, {"on": False, "settings": {"udp": 514, "saveFiles": True}})
            if self.path == "/api/v1/syslog/start":
                return self.reply(200, {"on": True})
        if self.path == "/api/v1/ai/skills":
            return self.reply(200, [{"name": "troubleshoot-port", "description": "Port down or flapping", "on": True}] if self.command == "GET" else {"name": "add-vlan"})
        if self.path == "/api/v1/tools/device":
            return self.reply(200, {"result": "Lab/edge-sw3 looks like Huawei VRP"})
        if self.path.startswith("/api/v1/http"):
            if self.path == "/api/v1/http/send":
                return self.reply(200, {"status": 200, "body": '{"ok": true}', "ms": 3})
            if self.path == "/api/v1/http/collections/Lab%20checks/run":
                return self.reply(200, {"passed": 2, "failed": 0, "steps": []})
            if self.path == "/api/v1/http/import":
                return self.reply(201, {"id": "x", "requests": [{}]})
            if self.path == "/api/v1/http/environments":
                return self.reply(200, {"id": "dl2r1", "name": "dl2r1", "vars": []})
            if self.path == "/api/v1/http/variables":
                return self.reply(200, [{"name": "ep1", "value": "10.1.1.1", "source": "environment"}])
        if path == "/traffic/ports" and m == "POST":
            v["testers"].append({"id": "p%d" % (len(v["testers"]) + 1), "name": "Test port %d" % (len(v["testers"]) + 1)})
            return self.reply(201, v)
        if path == "/traffic/streams" and m == "POST":
            if body.get("pps") == 13:
                return self.reply(422, {"title": "Unprocessable", "status": 422, "detail": "no such port"}, "application/problem+json")
            v["streams"].append({"id": "s%d" % (len(v["streams"]) + 1), "name": body["name"]})
            return self.reply(201, v)
        if path.startswith("/traffic/streams/") and m == "DELETE":
            v["streams"] = [s for s in v["streams"] if s["name"] != path.rsplit("/", 1)[1]]
            return self.reply(200, v)
        if path == "/traffic/start":
            v["running"] = True
            st["polls"] = 0
            return self.reply(200, v)
        if path == "/traffic" and m == "GET":
            if v["running"]:
                st["polls"] += 1
                if st["polls"] >= 2:
                    v["running"] = False
                    v["results"] = [{"id": s["id"], "tx": 100, "rx": 100 if st.get("pass", True) else 90, "loss": 0.0 if st.get("pass", True) else 10.0,
                                     "result": "pass" if st.get("pass", True) else "fail", "why": "" if st.get("pass", True) else "loss 10% > 0.1%"} for s in v["streams"]]
            return self.reply(200, v)
        if path == "/traffic/report":
            fmt = self.path.split("format=")[-1]
            return self.reply(200, "a,b\n1,2\n" if fmt == "csv" else {"ok": True}, "text/csv" if fmt == "csv" else "application/json")
        if path == "/traffic/tests" and m == "GET":
            return self.reply(200, st.setdefault("tests", []))
        if path == "/traffic/tests" and m == "POST":
            st["tests"].append({"id": "t1", "name": body["name"], "kind": body["kind"]})
            return self.reply(201, st["tests"])
        if re.fullmatch(r"/traffic/tests/[^/]+/run", path):
            return self.reply(202, {"id": "r1", "status": "running"})
        if path == "/traffic/runs/r1":
            st["rp"] = st.get("rp", 0) + 1
            if st["rp"] < 2:
                return self.reply(200, {"id": "r1", "status": "running", "rows": []})
            return self.reply(200, {"id": "r1", "status": st.get("run_status", "done"), "rows": [{"size": 64, "rate": 99.0}], "error": ""})
        if path in ("/traffic/engines", "/traffic/trex", "/traffic/nics") or path.startswith("/traffic/trex/"):
            if m == "DELETE":
                self.send_response(204)
                self.end_headers()
                return
            if path == "/traffic/nics":
                return self.reply(200, [{"name": "enp3s0", "mac": "0c:42:a1:00:00:01", "up": True, "speed": 10000, "used": ""}])
            return self.reply(200, {"engine": (body or {}).get("engine", "python"), "hosts": [], "trex": []})
        if path == "/tools/power":
            return self.reply(200, {"result": "started %s" % ",".join(body["switches"])})
        if path == "/tools/console":
            return self.reply(200, {"result": "ok"})
        if path == "/tools/install_iso":
            st["inst"] = 0
            return self.reply(200, {"result": "Install started (id ab12): follow it with lab_install_status."})
        if path == "/tools/install_status":
            st["inst"] = st.get("inst", 0) + 1
            stage = "installing" if st["inst"] < 2 else st.get("inst_end", "done")
            return self.reply(200, {"result": "Installs (id | name | mode | stage | now | image):\n- ab12 | alp | alpine | %s | boot | %s\n" % (stage, "img1" if stage == "done" else "-")})
        if path == "/tools/install_finish":
            return self.reply(200, {"result": "Saving"})
        if path.startswith("/screen/"):
            self.send_response(200)
            self.send_header("Content-Type", "image/png")
            self.end_headers()
            self.wfile.write(b"\x89PNG fake")
            return
        if path in ("/tools/dns", "/tools/images", "/tools/checkpoint", "/tools/keys", "/tools/radius"):
            return self.reply(200, {"result": "ok"})
        if path == "/tools/history":
            return self.reply(200, {"result": "Changes, newest first"})
        if path == "/tools/wait":
            return self.reply(200, {"result": "all ready"})
        self.reply(404, {"title": "Not found", "status": 404, "detail": path}, "application/problem+json")

    do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = handle_any


class ClientTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.srv = HTTPServer(("127.0.0.1", 0), Fake)
        threading.Thread(target=cls.srv.serve_forever, daemon=True).start()
        cls.base = "http://127.0.0.1:%d/api/v1" % cls.srv.server_port

    @classmethod
    def tearDownClass(cls):
        cls.srv.shutdown()

    def setUp(self):
        Fake.state = {}
        self.lab = tw.Lab(self.base, "KEY")

    def test_bad_key_is_a_problem(self):
        with self.assertRaises(tw.ApiError) as c:
            tw.Lab(self.base, "NOPE").show()
        self.assertEqual(c.exception.status, 401)
        self.assertIn("bad key", str(c.exception))

    def test_unreachable(self):
        with self.assertRaises(tw.ApiError) as c:
            tw.Lab("http://127.0.0.1:9/api/v1", "K", timeout=2).show()
        self.assertEqual(c.exception.status, 0)

    def test_tools_and_timeouts_are_sent(self):
        self.assertEqual(self.lab.power("start", "a", "b"), "started a,b")
        self.lab.console("a", "show version", timeout=30)
        self.lab.wait("a", timeout=5)
        calls = {p: b for _, p, b in Fake.state["calls"]}
        self.assertEqual(calls["/api/v1/lab/tools/power"], {"action": "start", "switches": ["a", "b"]})
        self.assertEqual(calls["/api/v1/lab/tools/console"]["timeout"], 30)
        self.assertEqual(calls["/api/v1/lab/tools/wait"], {"switches": ["a"], "timeout": 5})

    def test_install_calls_and_wait(self):
        self.assertIn("ab12", self.lab.install_iso("/x/alpine.iso", "alpine", password="pw", disk_gb=4))
        self.assertIn("done", self.lab.wait_install("ab12", timeout=10, poll=0.01))
        calls = {p: b for _, p, b in Fake.state["calls"]}
        self.assertEqual(calls["/api/v1/lab/tools/install_iso"], {"iso_path": "/x/alpine.iso", "mode": "alpine", "password": "pw", "disk_gb": 4})
        self.lab.install_iso("/x/a.iso", disk_mb=512)
        self.assertEqual(Fake.state["calls"][-1][2], {"iso_path": "/x/a.iso", "mode": "manual", "disk_mb": 512})
        self.lab.install_finish("ab12")
        self.assertEqual(Fake.state["calls"][-1][2], {"id": "ab12"})

    def test_history_and_revert(self):
        self.lab.history(5)
        self.assertEqual(Fake.state["calls"][-1][2], {"action": "list", "last": 5})
        self.lab.revert("ab12")
        self.assertEqual(Fake.state["calls"][-1][1], "/api/v1/lab/tools/history")
        self.assertEqual(Fake.state["calls"][-1][2], {"action": "revert", "id": "ab12"})

    def test_dns_and_images(self):
        self.lab.dns("10.0.0.53", "9.9.9.9")
        self.assertEqual(Fake.state["calls"][-1][1:], ("/api/v1/lab/tools/dns", {"servers": ["10.0.0.53", "9.9.9.9"]}))
        self.lab.dns(default=True)
        self.assertEqual(Fake.state["calls"][-1][2], {"servers": []})
        self.lab.dns()
        self.assertEqual(Fake.state["calls"][-1][2], {})
        self.lab.restore("sw1", "tw-ck-a1")
        self.assertEqual(Fake.state["calls"][-1][1:], ("/api/v1/lab/tools/checkpoint", {"action": "restore", "switch": "sw1", "tag": "tw-ck-a1"}))
        self.lab.auto_checkpoints(False)
        self.assertEqual(Fake.state["calls"][-1][2], {"action": "auto", "on": False})
        self.assertEqual(self.lab.screen("install:ab12"), b"\x89PNG fake")
        self.assertEqual(Fake.state["calls"][-1][1], "/api/v1/lab/screen/install:ab12")
        self.lab.install_type("ab12", "root")
        self.assertEqual(Fake.state["calls"][-1][2], {"install": "ab12", "text": "root", "enter": True, "console": True})
        self.lab.images(type="EXOS")
        self.assertEqual(Fake.state["calls"][-1][1:], ("/api/v1/lab/tools/images", {"type": "EXOS"}))

    def test_radius_and_clients(self):
        self.lab.radius("add_user", user="user*", password="pw", vlan="100")
        self.assertEqual(Fake.state["calls"][-1][1:], ("/api/v1/lab/tools/radius", {"action": "add_user", "user": "user*", "password": "pw", "vlan": "100"}))
        t = self.lab.traffic
        a = t.add_port("leaf-1", "5", clients=dict(n=20, dhcp=True, dot1x=dict(user="user{n}", password="pw")), stp=dict(mode="rstp"))
        post = [c for c in Fake.state["calls"] if c[1] == "/api/v1/lab/traffic/ports"][-1]
        self.assertEqual(post[2]["clients"]["dot1x"]["user"], "user{n}")
        self.assertEqual(a, "Test port 1")
        self.assertEqual(tw._body(dict(topology_change=True)), {"topologyChange": True})

    def test_engines_host_and_trex_ports(self):
        t = self.lab.traffic
        self.assertEqual(t.set_engine("fast")["engine"], "fast")
        self.assertEqual(Fake.state["calls"][-1][:3], ("PUT", "/api/v1/lab/traffic/engines", {"engine": "fast"}))
        self.assertEqual(t.nics("kvm-2")[0]["name"], "enp3s0")
        self.assertEqual(Fake.state["calls"][-1][1], "/api/v1/lab/traffic/nics?host=kvm-2")
        t.add_host_port("enp3s0", host="kvm-2", ip="10.9.0.1/24", cabled="core-1 1/1")
        self.assertEqual(Fake.state["calls"][-1][2], {"ip": "10.9.0.1/24", "nic": "enp3s0", "host": "kvm-2", "cabled": "core-1 1/1"})
        t.add_trex("10.0.0.50", "trex-1")
        self.assertEqual(Fake.state["calls"][-1][2], {"addr": "10.0.0.50", "name": "trex-1", "port": 4501})
        t.add_trex_port("trex-1", 1)
        self.assertEqual(Fake.state["calls"][-1][2], {"trex": "trex-1", "trexPort": 1, "cabled": ""})
        t.remove_trex("trex 1")
        self.assertEqual(Fake.state["calls"][-1][:2], ("DELETE", "/api/v1/lab/traffic/trex/trex%201"))

    def test_install_wait_raises_on_failure(self):
        self.lab.install_iso("/x/a.iso")
        Fake.state["inst_end"] = "failed"
        with self.assertRaises(RuntimeError):
            self.lab.wait_install("ab12", timeout=10, poll=0.01)

    def test_snmp(self):
        r = self.lab.snmp.get("sysName.0", session="Core/sw1")
        self.assertEqual(tw.SNMP.table(r), {"SNMPv2-MIB::sysName.0": "sw1"})
        self.lab.snmp.walk("ifTable", address="10.0.0.1", community="public", max=50)
        self.lab.snmp.set("ifAdminStatus.3", "down", session="Lab/sw2")
        calls = [(p, b) for _, p, b in Fake.state["calls"] if p == "/api/v1/snmp"]
        self.assertEqual(calls[0][1], {"op": "get", "session": "Core/sw1", "oids": ["sysName.0"]})
        self.assertEqual(calls[1][1], {"op": "walk", "target": {"address": "10.0.0.1", "community": "public"}, "oids": ["ifTable"], "max": 50})
        self.assertEqual(calls[2][1], {"op": "set", "session": "Lab/sw2", "values": [{"oid": "ifAdminStatus.3", "value": "down"}]})
        self.assertEqual(self.lab.snmp.mib_search("ifOper")[0]["module"], "IF-MIB")
        self.assertEqual(self.lab.snmp.mib("IF-MIB::ifOperStatus")["name"], "ifOperStatus")
        self.assertFalse(self.lab.snmp.traps()["state"]["on"])

    def test_http(self):
        r = self.lab.http.post("/vlans", {"id": 10}, session="APIs/rest", params={"dry": 1})
        self.assertTrue(tw.HTTP.json(r)["ok"])
        self.assertEqual(self.lab.http.run("Lab checks", iterations=2)["passed"], 2)
        self.lab.http.import_("curl https://x/y")
        calls = {p: b for _, p, b in Fake.state["calls"]}
        self.assertEqual(calls["/api/v1/http/send"], {"request": {"method": "POST", "url": "/vlans", "params": [{"key": "dry", "value": "1"}],
                                                                  "body": {"mode": "json", "raw": '{"id": 10}'}}, "session": "APIs/rest"})
        self.assertEqual(calls["/api/v1/http/collections/Lab%20checks/run"], {"iterations": 2})
        self.assertEqual(calls["/api/v1/http/import"], {"text": "curl https://x/y"})
        self.assertEqual(self.lab.http.save_environment("dl2r1", {"ep1": "10.1.1.1", "Bearer": "t"}, secret=["Bearer"])["id"], "dl2r1")
        self.assertEqual(self.lab.http.variables(environment="dl2r1")[0]["source"], "environment")
        self.lab.http.run("Lab checks", environment="dl2r1")
        calls = {p: b for _, p, b in Fake.state["calls"]}
        self.assertEqual(calls["/api/v1/http/environments"]["vars"][1], {"key": "Bearer", "value": "t", "secret": True})
        self.assertEqual(calls["/api/v1/http/collections/Lab%20checks/run"], {"environment": "dl2r1"})

    def test_skills(self):
        self.assertEqual(self.lab.skills()[0]["name"], "troubleshoot-port")
        self.assertEqual(self.lab.save_skill("---\nname: add-vlan\ndescription: x\n---\n")["name"], "add-vlan")

    def test_devices(self):
        self.assertIn("Huawei VRP", self.lab.devices.identify("Lab/edge-sw3"))
        self.lab.devices.set_type("Lab/edge-sw3", "Huawei VRP")
        calls = {p: b for _, p, b in Fake.state["calls"]}
        self.assertEqual(calls["/api/v1/tools/device"], {"action": "set_type", "session": "Lab/edge-sw3", "type": "Huawei VRP"})

    def test_syslog(self):
        self.assertEqual(self.lab.syslog.messages("link down", severity="err")[0]["id"], 7)
        self.assertTrue(self.lab.syslog.send("10.0.0.5", "hi", severity="err").startswith("<187>"))
        self.assertTrue(self.lab.syslog.start(udp=1514)["on"])
        calls = [(m, p, b) for m, p, b in Fake.state["calls"]]
        self.assertIn(("GET", "/api/v1/syslog/messages?q=link+down&severity=err", None), calls)
        self.assertIn(("POST", "/api/v1/syslog/start", {"udp": 1514, "saveFiles": True}), calls)

    def test_pps_for(self):
        self.assertEqual(tw.pps_for(1000, 64), 1488095)
        self.assertEqual(tw.pps_for(10, 1518, "pct", 1000), 8127)
        self.assertEqual(tw.pps_for(0.0001, 1518), 1)

    def test_port_name_stream_body_and_rate(self):
        t = self.lab.traffic
        a = t.add_port("leaf-1", "5", ip="10.10.0.1/24")
        b = t.add_port("leaf-2", "5", ip="10.10.0.2/24")
        self.assertEqual((a, b), ("Test port 1", "Test port 2"))
        t.add_stream("v", a, b, rate=8.0, size=200, max_loss=0.1, dscp=46, pcp=5)
        body = Fake.state["calls"][-1][2]
        self.assertEqual(body, {"name": "v", "from": a, "to": b, "size": 200, "rateUnit": "mbps", "rate": 8.0, "maxLoss": 0.1, "dscp": 46, "prio": 5})
        t.add_stream("w", a, b, rate=10, rate_unit="pct", line_mbps=100, size=1518, rtt=True)
        body = Fake.state["calls"][-1][2]
        self.assertEqual((body["pps"], body["rtt"]), (812, True))
        with self.assertRaises(ValueError):
            t.add_stream("x", a, b, rate=1, pps=5)
        with self.assertRaises(tw.ApiError) as c:
            t.add_stream("x", a, b, pps=13)
        self.assertEqual(c.exception.status, 422)

    def test_run_streams_passes_and_cleans_up(self):
        t = self.lab.traffic
        a = t.add_port("l1", "5")
        b = t.add_port("l2", "5")
        res = t.run_streams([dict(name="v", src=a, dst=b, pps=100, seconds=1)], timeout=30)
        self.assertEqual(res["v"]["result"], "pass")
        t.assert_pass(res)
        self.assertEqual(Fake.state["view"]["streams"], [])

    def test_port_stats_by_name(self):
        t = self.lab.traffic
        a = t.add_port("l1", "5")
        Fake.state["view"]["ports"] = [{"id": "p1", "tx": 100, "rx": 90, "expected": 100, "got": 90, "loss": 10.0, "latAvg": 0.2, "errors": 0}]
        ps = t.port_stats()
        self.assertEqual(list(ps), [a])
        self.assertEqual(ps[a]["loss"], 10.0)

    def test_assert_pass_names_the_failure(self):
        Fake.state["pass"] = False
        t = self.lab.traffic
        a = t.add_port("l1", "5")
        b = t.add_port("l2", "5")
        res = t.run_streams([dict(name="v", src=a, dst=b, pps=100, seconds=1)], timeout=30)
        with self.assertRaises(AssertionError) as c:
            t.assert_pass(res)
        self.assertIn("v: fail", str(c.exception))
        self.assertIn("loss 10%", str(c.exception))
        with self.assertRaises(AssertionError):
            t.assert_pass({})

    def test_report_formats(self):
        t = self.lab.traffic
        self.assertEqual(t.report("json"), {"ok": True})
        self.assertEqual(t.report("csv"), "a,b\n1,2\n")
        with self.assertRaises(ValueError):
            t.report("docx")

    def test_saved_test_runs(self):
        t = self.lab.traffic
        t.save_test("up", "throughput", "Test port 1", "Test port 2", both=True, sizes=[64, 512], max_pps=2000)
        sent = Fake.state["calls"][-1][2]
        self.assertEqual(sent, {"name": "up", "kind": "throughput", "from": "Test port 1", "to": "Test port 2", "both": True, "sizes": [64, 512], "maxPps": 2000})
        run = t.run_test("up", poll=0.01)
        self.assertEqual(t.rows(run)[0]["size"], 64)

    def test_failed_run_raises(self):
        Fake.state["run_status"] = "failed"
        t = self.lab.traffic
        t.save_test("up", "throughput", "a", "b")
        with self.assertRaises(tw.ApiError):
            t.run_test("up", poll=0.01)

    def test_iperf_fields(self):
        self.lab.traffic.iperf("h", "pc-2", "pc-1", "10.10.0.12", parallel=4, bandwidth="100M")
        body = [b for m, p, b in Fake.state["calls"] if p.endswith("/traffic/tests") and m == "POST"][0]
        self.assertEqual(body["serverIp"], "10.10.0.12")
        self.assertEqual(body["parallel"], 4)
        self.assertNotIn("reverse", body)


class ContractTest(unittest.TestCase):
    def test_every_endpoint_is_in_openapi(self):
        try:
            import yaml
        except ImportError:
            self.skipTest("pyyaml not installed")
        doc = yaml.safe_load((ROOT / "docs/api/openapi.yaml").read_text())
        for method, path in tw.ENDPOINTS:
            self.assertIn(path, doc["paths"], path)
            self.assertIn(method.lower(), doc["paths"][path], f"{method} {path}")

    def test_field_names_are_in_the_schemas(self):
        try:
            import yaml
        except ImportError:
            self.skipTest("pyyaml not installed")
        doc = yaml.safe_load((ROOT / "docs/api/openapi.yaml").read_text())
        S = doc["components"]["schemas"]
        stream = set(S["StreamInput"]["properties"])
        for k in ("maxLoss", "maxLatency", "maxJitter", "dscp", "prio", "sizeMode", "sizeMax", "dstIp", "dstMac", "dport", "burst", "mods", "from", "to"):
            self.assertIn(k, stream, k)
        test = set(S["TestInput"]["properties"])
        for k in ("maxPps", "serverIp", "sizes", "trial", "both", "hold", "at", "action", "switch", "port", "proto", "parallel", "bandwidth", "reverse", "client", "server"):
            self.assertIn(k, test, k)
        tool = doc["paths"]["/lab/tools/cable"]["post"]["requestBody"]["content"]["application/json"]["schema"]
        self.assertIn("cables", tool["properties"])
        statuses = S["TestRun"]["properties"]["status"]["description"]
        self.assertIn("done", statuses)


if __name__ == "__main__":
    unittest.main()
