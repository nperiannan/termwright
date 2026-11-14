"""An example: a voice stream, the RFC 2544 suite and a failover test on a running lab.
Termwright must be running with Settings -> AI Assistant -> Local API on.
Run: python3 example_traffic.py leaf-1 leaf-2"""
import sys

from termwright_lab import Lab

leaf1, leaf2 = (sys.argv[1:3] + ["leaf-1", "leaf-2"])[:2]
lab = Lab()
lab.power("start", leaf1, leaf2)
lab.wait(leaf1, leaf2, timeout=900)

t = lab.traffic
a = t.add_port(leaf1, "5", ip="10.10.0.1/24")
b = t.add_port(leaf2, "5", ip="10.10.0.2/24")

res = t.run_streams([
    dict(name="voice", src=a, dst=b, size=200, pps=1000, seconds=20, dscp=46, max_loss=0.1, max_latency=5, max_jitter=1),
    dict(name="video", src=b, dst=a, size=1280, rate=50, seconds=20, max_loss=0.01),
])
for name, r in res.items():
    print(f"{name}: {r['result']}  tx {r['tx']}  rx {r['rx']}  loss {r['loss']}%  latency {r['latAvg']} ms")
t.assert_pass(res)

runs = t.rfc2544("uplink", a, b, sizes=(64, 512, 1518), tests=("throughput", "latency"))
for kind, run in runs.items():
    for row in t.rows(run):
        print(kind, row)

open("report.csv", "w").write(t.report("csv"))
