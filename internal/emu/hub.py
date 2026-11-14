# Termwright hubs and L2 switches: lab nodes without a VM. Each leg is a switch port of the lab that has no
# cable (its QEMU sends to the UDP port a test port would listen on, and receives on another). A hub sends
# what comes in on one leg out of every other; an L2 switch learns the source MAC of each frame and sends a
# frame to the leg its destination was seen on (to every other leg when it is unknown, broadcast or
# multicast; entries age out after 300 s). Reads hubs.json ({"hubs": [{"id", "kind", "legs": [[listen, send],
# ...]}]}), writes hubs-stats.json (frames per hub and leg, learned MACs); ends when there are none.
import json, os, select, socket, sys, time

d = sys.argv[1]
path, st_path = os.path.join(d, "hubs.json"), os.path.join(d, "hubs-stats.json")
AGE = 300
socks, where, m0, last = {}, {}, None, 0  # listen port -> socket; listen port -> (hub, leg index)
hubs, macs, count = {}, {}, {}  # hub id -> {"kind", "legs"}; hub id -> {mac: (leg, seen)}; hub id -> [frames in per leg]
while True:
    try:
        m = os.stat(path).st_mtime
    except OSError:
        break
    if m != m0:
        m0 = m
        try:
            cfg = json.load(open(path)).get("hubs", [])
        except Exception:
            cfg = []
        hubs = {h["id"]: {"kind": h.get("kind", "hub"), "legs": [tuple(x) for x in h.get("legs", [])]} for h in cfg}
        want = {}
        for hid, h in hubs.items():
            for i, (li, se) in enumerate(h["legs"]):
                want[li] = (hid, i)
            count.setdefault(hid, [0] * len(h["legs"]))
            count[hid] = (count[hid] + [0] * len(h["legs"]))[:len(h["legs"])]
            macs.setdefault(hid, {})
        for lp in list(socks):
            if lp not in want:
                socks.pop(lp).close()
        for lp in want:
            if lp not in socks:
                s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
                s.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 4 << 20)
                try:
                    s.bind(("127.0.0.1", lp))
                except OSError:
                    s.close()
                    continue
                s.setblocking(False)
                socks[lp] = s
        where = want
        for hid in list(macs):  # a changed hub forgets what it learned
            if hid not in hubs:
                macs.pop(hid, None)
                count.pop(hid, None)
        if not socks:
            break
    r, _, _ = select.select(list(socks.values()), [], [], 0.5)
    now = time.time()
    for s in r:
        lp = s.getsockname()[1]
        hid, i = where.get(lp, (None, 0))
        h = hubs.get(hid)
        for _ in range(2000):
            try:
                f = s.recv(65536)
            except OSError:
                break
            if not h or len(f) < 14:
                continue
            count[hid][i] += 1
            outs = range(len(h["legs"]))
            if h["kind"] == "switch":
                tbl = macs[hid]
                if not f[6] & 1:
                    tbl[f[6:12]] = (i, now)
                to = tbl.get(f[0:6])
                if not f[0] & 1 and to and now - to[1] < AGE:
                    outs = [to[0]] if to[0] != i else []
            for o in outs:
                if o == i:
                    continue
                li, se = h["legs"][o]
                out = socks.get(li)
                if out is not None:
                    try:
                        out.sendto(f, ("127.0.0.1", se))
                    except OSError:
                        pass
    if now - last > 2:
        last = now
        st = {"at": now, "hubs": {hid: {"in": count.get(hid, []), "macs": [[m.hex(":"), v[0]] for m, v in macs.get(hid, {}).items() if now - v[1] < AGE][:500]} for hid in hubs}}
        try:
            with open(st_path + ".tmp", "w") as fh:
                json.dump(st, fh)
            os.replace(st_path + ".tmp", st_path)
        except OSError:
            pass
