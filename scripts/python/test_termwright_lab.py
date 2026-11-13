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
        if path in ("/tools/dns", "/tools/images"):
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
        self.lab.images(type="EXOS")
        self.assertEqual(Fake.state["calls"][-1][1:], ("/api/v1/lab/tools/images", {"type": "EXOS"}))

    def test_install_wait_raises_on_failure(self):
        self.lab.install_iso("/x/a.iso")
        Fake.state["inst_end"] = "failed"
        with self.assertRaises(RuntimeError):
            self.lab.wait_install("ab12", timeout=10, poll=0.01)

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
            t.report("pdf")

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
