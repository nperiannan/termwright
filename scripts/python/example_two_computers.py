"""The two-computer test: two Linux computers cabled to one switch, an address each on their first data
port, a ping and iperf3 both ways; it fails (exit 1) when a step does not pass.
Termwright must be running with Settings -> AI Assistant -> Local API on.
Run: python3 example_two_computers.py pc-1 pc-2 10.20.0.11/24 10.20.0.12/24"""
import re
import sys

from termwright_lab import Lab  # noqa: E402

pc1, pc2, ip1, ip2 = (sys.argv[1:5] + ["pc-1", "pc-2", "10.20.0.11/24", "10.20.0.12/24"][len(sys.argv[1:5]):])[:4]
a1, a2 = ip1.split("/")[0], ip2.split("/")[0]
lab = Lab()
lab.wait(pc1, pc2, timeout=600)
print(lab.computer_set(pc1, port=1, ipv4=ip1))
print(lab.computer_set(pc2, port=1, ipv4=ip2))

ok = True
for src, dst in ((pc1, a2), (pc2, a1)):
    out = lab.computer(src, "ping", dst)
    loss = re.search(r"(\d+)% packet loss", out)
    passed = bool(loss) and loss.group(1) == "0"
    ok &= passed
    print(f"ping {src} -> {dst}: {'PASS' if passed else 'FAIL'} ({loss.group(0) if loss else 'no answer'})")

for server, client, addr in ((pc2, pc1, a2), (pc1, pc2, a1)):
    try:
        run = lab.traffic.iperf(f"iperf {client} to {server}", server=server, client=client, server_ip=addr, seconds=5)
    except Exception as e:  # the run failed or timed out
        ok = False
        print(f"iperf {client} -> {server}: FAIL ({e})")
        continue
    rows = lab.traffic.rows(run)
    mbps = max((r.get("mbps") or 0 for r in rows), default=0)
    passed = run.get("status") == "done" and mbps > 0
    ok &= passed
    print(f"iperf {client} -> {server}: {'PASS' if passed else 'FAIL'} ({mbps:.1f} Mbit/s, {run.get('status')})")

print("ALL PASS" if ok else "SOME FAILED")
sys.exit(0 if ok else 1)
