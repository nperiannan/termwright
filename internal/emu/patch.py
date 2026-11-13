# Termwright patch panel: cables added between running switches on this host, until they restart with
# them. A switch port without a cable sends its frames to the UDP port a test port would listen on and
# receives on another; this joins two such ports: what one sends goes to the other, from the port its
# QEMU expects. Reads patch.json ([[a listen, a send, b listen, b send], ...]); ends when it is empty.
import json, os, select, socket, sys

d = sys.argv[1]
path = os.path.join(d, "patch.json")
socks, peer, m0 = {}, {}, None  # listen port -> socket; listen port -> (peer's listen port, peer's send port)
while True:
    try:
        m = os.stat(path).st_mtime
    except OSError:
        break
    if m != m0:
        m0 = m
        try:
            pairs = json.load(open(path))
        except Exception:
            pairs = []
        want = {}
        for a_li, a_se, b_li, b_se in pairs:
            want[a_li], want[b_li] = (b_li, b_se), (a_li, a_se)
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
        peer = want
        if not socks:
            break
    r, _, _ = select.select(list(socks.values()), [], [], 0.5)
    for s in r:
        lp = s.getsockname()[1]
        to = peer.get(lp)
        out = socks.get(to[0]) if to else None
        for _ in range(2000):
            try:
                f = s.recv(65536)
            except OSError:
                break
            if out is not None:
                try:
                    out.sendto(f, ("127.0.0.1", to[1]))
                except OSError:
                    pass
