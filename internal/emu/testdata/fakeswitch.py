# A switch for TestTrafficPyClients: 802.1X authenticator (relays EAP to the RADIUS server), DHCP server, IGMP
# listener and RSTP bridge on one port. argv: its port, the test port's, the RADIUS port, the secret, the result file.
import hashlib, hmac, json, os, socket, struct, sys, time

me, tp, rport, sec, out = int(sys.argv[1]), int(sys.argv[2]), int(sys.argv[3]), sys.argv[4].encode(), sys.argv[5]
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.bind(("127.0.0.1", me))
s.settimeout(0.2)
r = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
r.settimeout(1)
SW = bytes.fromhex("000496aabbcc")
ident, state, res = {}, {}, {"auth": {}, "fail": [], "dhcp": [], "igmp": [], "bpdu": 0, "pap": "", "mab": "", "papBad": ""}
rid = [0]


def send(dst, et, p):
    s.sendto(dst + SW + struct.pack("!H", et) + p, ("127.0.0.1", tp))


def radius(al):
    rid[0] = (rid[0] + 1) & 255
    auth = os.urandom(16)
    body = b"".join(struct.pack("!BB", t, len(v) + 2) + v for t, v in al)
    has_eap = any(t == 79 for t, v in al)
    if has_eap:
        body += b"\x50\x12" + b"\0" * 16
    p = struct.pack("!BBH", 1, rid[0], 20 + len(body)) + auth + body
    if has_eap:
        p = p[:-16] + hmac.new(sec, p, hashlib.md5).digest()
    for _ in range(3):  # (the server may still be starting)
        r.sendto(p, ("127.0.0.1", rport))
        try:
            a, _ = r.recvfrom(4096)
            break
        except socket.timeout:
            pass
    want = hashlib.md5(a[:4] + auth + a[20:] + sec).digest()
    assert a[4:20] == want, "bad response authenticator"
    at, i = [], 20
    while i + 2 <= len(a):
        at.append((a[i], a[i + 2:i + a[i + 1]]))
        i += a[i + 1]
    return a[0], at


def pap(user, pw):
    auth = os.urandom(16)
    h = hashlib.md5(sec + auth).digest()
    c = bytes(x ^ y for x, y in zip(pw.encode().ljust(16, b"\0"), h))
    rid[0] = (rid[0] + 1) & 255
    body = struct.pack("!BB", 1, len(user) + 2) + user.encode() + struct.pack("!BB", 2, 18) + c + b"\x06\x06\0\0\0\x0a" * (user.count(":") == 5)
    p = struct.pack("!BBH", 1, rid[0], 20 + len(body)) + auth + body
    r.sendto(p, ("127.0.0.1", rport))
    a, _ = r.recvfrom(4096)
    return {2: "accept", 3: "reject"}.get(a[0], str(a[0]))


end, bpdu_at = time.time() + float(sys.argv[6]), 0
while time.time() < end:
    if time.time() - bpdu_at > 0.5:  # RSTP: the test port is the root, this is its root port
        bpdu_at = time.time()
        root = struct.pack("!H", 4096) + bytes.fromhex("027477000001")
        body = struct.pack("!HBBB8sI8sHHHHH", 0, 2, 2, 0x08 | 0x30, root, 20000, struct.pack("!H", 32768) + SW, 0x8001, 256, 20 * 256, 2 * 256, 15 * 256) + b"\0"
        llc = b"\x42\x42\x03" + body
        s.sendto(b"\x01\x80\xc2\x00\x00\x00" + SW + struct.pack("!H", len(llc)) + llc, ("127.0.0.1", tp))
    try:
        f = s.recv(4000)
    except socket.timeout:
        continue
    src, et, b = f[6:12], struct.unpack("!H", f[12:14])[0], f[14:]
    if et < 0x600:
        res["bpdu"] += 1
    elif et == 0x888E:
        if b[1] == 1:  # Start
            send(src, 0x888E, struct.pack("!BBH", 2, 0, 9) + struct.pack("!BBHB", 1, 1, 5, 1))
        elif b[1] == 0:
            e = b[4:4 + struct.unpack("!H", b[2:4])[0]]
            if e[4] == 1:
                ident[src] = e[5:]
            al = [(1, ident.get(src, b"")), (79, e)] + ([(24, state[src])] if src in state else [])
            code, at = radius(al)
            eap = b"".join(v for t, v in at if t == 79)
            for t, v in at:
                if t == 24:
                    state[src] = v
            if code == 2:
                res["auth"][src.hex(":")] = dict(at).get(81, b"").decode()
            elif code == 3:
                res["fail"].append(src.hex(":"))
            send(src, 0x888E, struct.pack("!BBH", 2, 0, len(eap)) + eap)
    elif et == 0x0800 and b[9] == 17 and struct.unpack("!H", b[22:24])[0] == 67:
        bp = b[28:]
        kind = bp[242]
        n = len(res["dhcp"]) + 10
        yi = bytes([10, 5, 0, n])
        opts = bytes([53, 1, 2 if kind == 1 else 5, 54, 4, 10, 5, 0, 1, 1, 4, 255, 255, 255, 0, 3, 4, 10, 5, 0, 1, 51, 4, 0, 0, 14, 16, 255])
        if kind == 3:
            res["dhcp"].append(bp[28:34].hex(":"))
        rep = struct.pack("!BBBBI", 2, 1, 6, 0, struct.unpack("!I", bp[4:8])[0]) + b"\0" * 8 + yi + b"\0" * 8 + bp[28:44] + b"\0" * 192 + b"\x63\x82\x53\x63" + opts
        u = struct.pack("!HHHH", 67, 68, 8 + len(rep), 0) + rep
        ip = struct.pack("!BBHHHBBH4s4s", 0x45, 0, 20 + len(u), 0, 0, 64, 17, 0, bytes([10, 5, 0, 1]), b"\xff" * 4) + u
        send(bp[28:34], 0x0800, ip)
    elif et == 0x0800 and b[9] == 2:
        res["igmp"].append(src.hex(":"))
res["pap"], res["papBad"], res["mab"] = pap("admin", "letmein"), pap("admin", "nope"), pap("02:63:aa:00:00:07", "02:63:aa:00:00:07")
json.dump(res, open(out, "w"))
