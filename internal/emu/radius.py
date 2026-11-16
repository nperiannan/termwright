# Termwright lab RADIUS server: the lab's switches ask it about 802.1X and MAC logins. Reads radius.json beside it
# ({"bind", "port", "secret", "users": [{"user", "password", "vlan"}], "mab": [MAC or pattern], "mabVlan"}), answers
# Access-Requests on the port (PAP, EAP-MD5 and PEAP-MSCHAPv2 with State and Message-Authenticator, MAC authentication) and
# Accounting-Requests on the next one, and writes radius-stats.json (counts and the last 200 logins). A user name
# may be a pattern (user*). Accepted users get their VLAN (Tunnel-Type 13, Tunnel-Medium-Type 6,
# Tunnel-Private-Group-Id). PEAP uses the certificate and key in radius.json ("cert", "key": PEM, made by Termwright).
# Ends when radius.json is gone.
import fnmatch, hashlib, hmac, json, os, select, socket, ssl, struct, sys, time

d = sys.argv[1]
path, st_path = os.path.join(d, "radius.json"), os.path.join(d, "radius-stats.json")
cfg, m0 = {}, None
socks, bound = [], None
pending = {}  # State -> (user, EAP id, challenge, when)
peap = {}  # State -> a PEAP session: {name, tls, phase, user, ch, id, at}
st = {"accept": 0, "reject": 0, "challenge": 0, "acct": 0, "dropped": 0, "log": [], "err": ""}
changed, last = True, 0


# MS-CHAPv2 (RFC 2759) for PEAP: MD4 and DES are written out here, as OpenSSL 3 leaves MD4 out and Python has no DES
def md4(m):
    try:
        return hashlib.new("md4", m).digest()
    except ValueError:
        pass
    M = 0xFFFFFFFF
    rl = lambda x, n: ((x << n) | (x >> (32 - n))) & M
    n = len(m)
    m += b"\x80" + b"\0" * ((55 - n) % 64) + struct.pack("<Q", n * 8)
    h = [0x67452301, 0xefcdab89, 0x98badcfe, 0x10325476]
    rounds = ((lambda x, y, z: (x & y) | (~x & z), 0, range(16), (3, 7, 11, 19)),
              (lambda x, y, z: (x & y) | (x & z) | (y & z), 0x5A827999, (0, 4, 8, 12, 1, 5, 9, 13, 2, 6, 10, 14, 3, 7, 11, 15), (3, 5, 9, 13)),
              (lambda x, y, z: x ^ y ^ z, 0x6ED9EBA1, (0, 8, 4, 12, 2, 10, 6, 14, 1, 9, 5, 13, 3, 11, 7, 15), (3, 9, 11, 15)))
    for i in range(0, len(m), 64):
        X = struct.unpack("<16I", m[i:i + 64])
        a, b, c, d = h
        for fn, k, order, sh in rounds:
            for j, x in enumerate(order):
                a, b, c, d = d, rl((a + fn(b, c, d) + X[x] + k) & M, sh[j % 4]), b, c
        h = [(p + q) & M for p, q in zip(h, (a, b, c, d))]
    return struct.pack("<4I", *h)


_PC1 = (57, 49, 41, 33, 25, 17, 9, 1, 58, 50, 42, 34, 26, 18, 10, 2, 59, 51, 43, 35, 27, 19, 11, 3, 60, 52, 44, 36,
        63, 55, 47, 39, 31, 23, 15, 7, 62, 54, 46, 38, 30, 22, 14, 6, 61, 53, 45, 37, 29, 21, 13, 5, 28, 20, 12, 4)
_PC2 = (14, 17, 11, 24, 1, 5, 3, 28, 15, 6, 21, 10, 23, 19, 12, 4, 26, 8, 16, 7, 27, 20, 13, 2,
        41, 52, 31, 37, 47, 55, 30, 40, 51, 45, 33, 48, 44, 49, 39, 56, 34, 53, 46, 42, 50, 36, 29, 32)
_IP = (58, 50, 42, 34, 26, 18, 10, 2, 60, 52, 44, 36, 28, 20, 12, 4, 62, 54, 46, 38, 30, 22, 14, 6, 64, 56, 48, 40, 32, 24, 16, 8,
       57, 49, 41, 33, 25, 17, 9, 1, 59, 51, 43, 35, 27, 19, 11, 3, 61, 53, 45, 37, 29, 21, 13, 5, 63, 55, 47, 39, 31, 23, 15, 7)
_FP = tuple(_IP.index(i + 1) + 1 for i in range(64))
_E = (32, 1, 2, 3, 4, 5, 4, 5, 6, 7, 8, 9, 8, 9, 10, 11, 12, 13, 12, 13, 14, 15, 16, 17,
      16, 17, 18, 19, 20, 21, 20, 21, 22, 23, 24, 25, 24, 25, 26, 27, 28, 29, 28, 29, 30, 31, 32, 1)
_P = (16, 7, 20, 21, 29, 12, 28, 17, 1, 15, 23, 26, 5, 18, 31, 10, 2, 8, 24, 14, 32, 27, 3, 9, 19, 13, 30, 6, 22, 11, 4, 25)
_S = (bytes.fromhex("0e040d01020f0b08030a060c05090007000f07040e020d010a060c0b0905030804010e080d06020b0f0c0907030a05000f0c080204090107050b030e0a00060d"),
      bytes.fromhex("0f01080e060b03040907020d0c00050a030d04070f02080e0c00010a06090b05000e070b0a040d0105080c060903020f0d080a01030f04020b06070c00050e09"),
      bytes.fromhex("0a00090e06030f05010d0c070b0402080d0700090304060a0208050e0c0b0f010d060409080f03000b01020c050a0e07010a0d0006090807040f0e030b05020c"),
      bytes.fromhex("070d0e030006090a010208050b0c040f0d080b05060f00030407020c010a0e090a0609000c0b070d0f01030e05020804030f00060a010d080904050b0c07020e"),
      bytes.fromhex("020c0401070a0b060805030f0d000e090e0b020c04070d0105000f0a030908060402010b0a0d07080f090c050603000e0b080c07010e020d060f00090a040503"),
      bytes.fromhex("0c010a0f09020608000d03040e07050b0a0f0402070c090506010d0e000b0308090e0f0502080c030700040a010d0b060403020c09050f0a0b0e01070600080d"),
      bytes.fromhex("040b020e0f00080d030c0907050a06010d000b070409010a0e03050c020f080601040b0d0c03070e0a0f060800050902060b0d0801040a070905000f0e02030c"),
      bytes.fromhex("0d020804060f0b010a09030e05000c07010f0d080a0307040c05060b000e0902070b0401090c0e0200060a0d0f03050802010e07040a080d0f0c09000305060b"))


def _perm(v, table, n):  # the bits of an n-bit value v picked by table (1 = the leftmost)
    r = 0
    for t in table:
        r = (r << 1) | ((v >> (n - t)) & 1)
    return r


def des(key7, block):  # DES-encrypts 8 bytes with a 56-bit key given as 7 bytes (MS-CHAP's way)
    k = int.from_bytes(key7, "big")
    key = int.from_bytes(bytes(((k >> (49 - 7 * i)) & 0x7F) << 1 for i in range(8)), "big")
    cd = _perm(key, _PC1, 64)
    c, d = cd >> 28, cd & 0xFFFFFFF
    ks = []
    for sh in (1, 1, 2, 2, 2, 2, 2, 2, 1, 2, 2, 2, 2, 2, 2, 1):
        c = ((c << sh) | (c >> (28 - sh))) & 0xFFFFFFF
        d = ((d << sh) | (d >> (28 - sh))) & 0xFFFFFFF
        ks.append(_perm((c << 28) | d, _PC2, 56))
    v = _perm(int.from_bytes(block, "big"), _IP, 64)
    l, r = v >> 32, v & 0xFFFFFFFF
    for sk in ks:
        x = _perm(r, _E, 32) ^ sk
        o = 0
        for i in range(8):
            six = (x >> (42 - 6 * i)) & 0x3F
            o = (o << 4) | _S[i][((six & 0x20) | ((six & 1) << 4)) | ((six >> 1) & 0xF)]
        l, r = r, l ^ _perm(o, _P, 32)
    return _perm((r << 32) | l, _FP, 64).to_bytes(8, "big")


def nt_hash(pw):
    return md4(pw.encode("utf-16-le"))


def chal_hash(peer, auth, user):
    return hashlib.sha1(peer + auth + user.split("\\")[-1].encode()).digest()[:8]


def nt_response(auth, peer, user, pw):
    ch, h = chal_hash(peer, auth, user), nt_hash(pw) + b"\0" * 5
    return des(h[0:7], ch) + des(h[7:14], ch) + des(h[14:21], ch)


def auth_response(pw, ntr, peer, auth, user):  # "S=…": the server shows it knows the password too
    d = hashlib.sha1(md4(nt_hash(pw)) + ntr + b"Magic server to client signing constant").digest()
    d = hashlib.sha1(d + chal_hash(peer, auth, user) + b"Pad to make it do more than one iteration").digest()
    return "S=" + d.hex().upper()


class Tls:
    """PEAP's TLS (1.2) carried in EAP: records in and out, cut into EAP packets of FRAG bytes (flags L, M, S)."""
    FRAG = 1000

    def __init__(s, server, cert=None, key=None):
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER if server else ssl.PROTOCOL_TLS_CLIENT)
        if server:
            ctx.load_cert_chain(cert, key)
        else:  # a lab: the server's certificate is not checked
            ctx.check_hostname, ctx.verify_mode = False, ssl.CERT_NONE
        ctx.maximum_version = ssl.TLSVersion.TLSv1_2
        s.i, s.o = ssl.MemoryBIO(), ssl.MemoryBIO()
        s.t = ctx.wrap_bio(s.i, s.o, server_side=server)
        s.rx, s.tx, s.done, s.sending = b"", b"", False, False

    def got(s, d):  # a PEAP payload (flags first): True when the whole message is in
        fl, d = d[0], d[1:]
        if fl & 0x80:
            d = d[4:]
        s.rx += d
        return not fl & 0x40

    def step(s):  # TLS on what came in: the application data it carried (an inner EAP message), or b""
        s.i.write(s.rx)
        s.rx, app = b"", b""
        if not s.done:
            try:
                s.t.do_handshake()
                s.done = True
            except ssl.SSLWantReadError:
                pass
        if s.done:
            try:
                app = s.t.read(65536)
            except (ssl.SSLWantReadError, ssl.SSLZeroReturnError):
                pass
        s.tx += s.o.read()
        return app

    def write(s, data):
        s.t.write(data)
        s.tx += s.o.read()

    def frag(s):  # the next PEAP payload to send: a message or its next fragment; b"\0" alone acknowledges
        if not s.tx:
            return b"\0"
        first, s.sending = not s.sending, True
        part, s.tx = s.tx[:s.FRAG], s.tx[s.FRAG:]
        more = 0x40 if s.tx else 0
        if not s.tx:
            s.sending = False
        if first and more:
            return bytes([0x80 | more]) + struct.pack("!I", len(part) + len(s.tx)) + part
        return bytes([more]) + part


def secret():
    return cfg.get("secret", "").encode()


def attrs(b):  # [(type, value)] of a packet's attributes
    out, i = [], 20
    while i + 2 <= len(b):
        t, n = b[i], b[i + 1]
        if n < 2:
            break
        out.append((t, b[i + 2:i + n]))
        i += n
    return out


def get(a, t):
    for k, v in a:
        if k == t:
            return v
    return None


def pack(code, ident, auth, al):
    body = b"".join(struct.pack("!BB", t, len(v) + 2) + v for t, v in al)
    return struct.pack("!BBH", code, ident, 20 + len(body)) + auth + body


def reply(req, code, al, eap=None):  # the answer, signed: Message-Authenticator first, then the Response Authenticator
    al = list(al)
    if eap is not None:
        for i in range(0, len(eap), 253):
            al.append((79, eap[i:i + 253]))
    if eap is not None or get(attrs(req), 80) is not None:
        al.append((80, b"\0" * 16))
        p = pack(code, req[1], req[4:20], al)
        mac = hmac.new(secret(), p, hashlib.md5).digest()
        al[-1] = (80, mac)
    p = pack(code, req[1], req[4:20], al)
    return p[:4] + hashlib.md5(p + secret()).digest() + p[20:]


def ma_ok(req, a):  # the request's Message-Authenticator, when it has one
    m = get(a, 80)
    if m is None:
        return True
    z, i = bytearray(req), 20
    while i + 2 <= len(z):
        if z[i] == 80:
            z[i + 2:i + 18] = b"\0" * 16
            break
        i += max(2, z[i + 1])
    return hmac.compare_digest(hmac.new(secret(), bytes(z), hashlib.md5).digest(), m)


def pap(req, a):  # the User-Password, decrypted
    c = get(a, 2) or b""
    out, prev = b"", req[4:20]
    for i in range(0, len(c), 16):
        h = hashlib.md5(secret() + prev).digest()
        blk = c[i:i + 16]
        out += bytes(x ^ y for x, y in zip(blk, h))
        prev = blk
    return out.rstrip(b"\0").decode("utf-8", "replace")


def macnorm(s):
    h = "".join(ch for ch in s.lower() if ch in "0123456789abcdef")
    return h if len(h) == 12 else ""


def user(name):
    for u in cfg.get("users", []):
        if fnmatch.fnmatchcase(name, u.get("user", "")):
            return u
    return None


def vlan_attrs(v):
    if not v:
        return []
    return [(64, b"\0\0\0\x0d"), (65, b"\0\0\0\x06"), (81, str(v).encode())]


def log(nas, name, how, result, vlan=""):
    global changed
    st["log"] = (st["log"] + [[round(time.time()), nas, name, how, result, str(vlan or "")]])[-200:]
    st[result] = st.get(result, 0) + 1
    changed = True


def peap_req(req, ident, payload, state):  # an Access-Challenge carrying an EAP-Request of type PEAP (25)
    e = struct.pack("!BBHB", 1, (ident + 1) & 255, 5 + len(payload), 25) + payload
    st["challenge"] += 1
    return reply(req, 11, [(24, state)], e)


def peap_start(req, ident, name, nas):
    cert, key = os.path.join(d, "radius-cert.pem"), os.path.join(d, "radius-key.pem")
    try:
        t = Tls(True, cert, key)
    except (OSError, ssl.SSLError) as e:
        log(nas, name, "PEAP (no certificate: %s)" % e, "reject")
        return reply(req, 3, [], struct.pack("!BBH", 4, ident, 4))
    state = os.urandom(16)
    peap[state] = {"name": name, "tls": t, "phase": "tls", "user": name, "at": time.time()}
    return peap_req(req, ident, b"\x20", state)


def peap_step(req, a, eap, nas):
    state = get(a, 24) or b""
    p = peap.get(state)
    ident = eap[1]
    if p is None:
        return reply(req, 3, [], struct.pack("!BBH", 4, ident, 4))
    p["at"], t, d2 = time.time(), p["tls"], eap[5:]
    fail = lambda why: (peap.pop(state, None), log(nas, p["user"], "PEAP-MSCHAPv2" + why, "reject"), reply(req, 3, [], struct.pack("!BBH", 4, ident, 4)))[2]
    if not d2:
        return fail(" (empty)")
    if t.tx and len(d2) == 1:  # the client took a fragment: the next one
        return peap_req(req, ident, t.frag(), state)
    if not t.got(d2):
        return peap_req(req, ident, b"\0", state)
    try:
        app = t.step()
    except ssl.SSLError as e:
        return fail(" (TLS: %s)" % (e.reason or e))
    ph = p["phase"]
    if ph == "tls":
        if t.done:
            p["phase"] = "acked"
        return peap_req(req, ident, t.frag(), state)
    if ph == "acked":  # the tunnel is up: the inner identity request
        t.write(b"\x01")
        p["phase"] = "identity"
        return peap_req(req, ident, t.frag(), state)
    if not app:
        return peap_req(req, ident, t.frag(), state)
    if ph == "identity" and app[0] == 1:
        p["user"], p["ch"], p["id"] = app[1:].decode("utf-8", "replace"), os.urandom(16), os.urandom(1)[0]
        name = b"termwright"
        t.write(struct.pack("!BBBHB", 26, 1, p["id"], 21 + len(name), 16) + p["ch"] + name)
        p["phase"] = "chap"
    elif ph == "chap" and app[0] == 26 and len(app) >= 54 and app[1] == 2:
        peer, ntr = app[6:22], app[30:54]
        u = user(p["user"])
        if u is not None and hmac.compare_digest(nt_response(p["ch"], peer, p["user"], u.get("password", "")), ntr):
            msg = (auth_response(u.get("password", ""), ntr, peer, p["ch"], p["user"]) + " M=OK").encode()
            t.write(struct.pack("!BBBH", 26, 3, p["id"], 4 + len(msg)) + msg)
            p["phase"], p["vlan"] = "success", u.get("vlan")
        else:
            msg = b"E=691 R=0 C=" + p["ch"].hex().upper().encode() + b" V=3 M=Authentication failed"
            t.write(struct.pack("!BBBH", 26, 4, p["id"], 4 + len(msg)) + msg)
            p["phase"] = "failure"
    elif ph in ("success", "failure") and app[0] == 26:
        ok = ph == "success"
        p["tlv"] = (ident + 1) & 255
        t.write(struct.pack("!BBHB", 1, p["tlv"], 11, 33) + b"\x80\x03\x00\x02\x00" + (b"\x01" if ok else b"\x02"))
        p["phase"] = "result-ok" if ok else "result-no"
    elif ph in ("result-ok", "result-no") and len(app) >= 11 and app[4] == 33:
        peap.pop(state, None)
        if ph == "result-ok" and app[5:11] == b"\x80\x03\x00\x02\x00\x01":
            log(nas, p["user"], "PEAP-MSCHAPv2", "accept", p.get("vlan"))
            return reply(req, 2, vlan_attrs(p.get("vlan")) + [(1, p["user"].encode())], struct.pack("!BBH", 3, ident, 4))
        log(nas, p["user"], "PEAP-MSCHAPv2", "reject")
        return reply(req, 3, [], struct.pack("!BBH", 4, ident, 4))
    else:
        return fail(" (unexpected %s in %s)" % (app[:2].hex(), ph))
    return peap_req(req, ident, t.frag(), state)


def access(req, nas):
    a = attrs(req)
    if not ma_ok(req, a):
        st["dropped"] += 1
        return None
    name = (get(a, 1) or b"").decode("utf-8", "replace")
    eap = b"".join(v for t, v in a if t == 79)
    if eap:
        if len(eap) < 5 or eap[0] != 2:
            return reply(req, 3, [], bytes([4, eap[1] if len(eap) > 1 else 0, 0, 4]))
        ident, typ = eap[1], eap[4]
        if typ == 1:  # identity: ask for MD5
            name = eap[5:].decode("utf-8", "replace") or name
            ch = os.urandom(16)
            state = os.urandom(16)
            pending[state] = (name, (ident + 1) & 255, ch, time.time())
            st["challenge"] += 1
            return reply(req, 11, [(24, state)], struct.pack("!BBHBB", 1, (ident + 1) & 255, 22, 4, 16) + ch)
        if typ == 25:
            return peap_step(req, a, eap, nas)
        p = pending.pop(get(a, 24) or b"", None)
        if typ == 3 and 25 in eap[5:]:  # a NAK asking for PEAP
            return peap_start(req, ident, p[0] if p else name, nas)
        if typ == 4 and p and len(eap) >= 22:
            u = user(p[0])
            ok = u is not None and hmac.compare_digest(hashlib.md5(bytes([p[1]]) + u.get("password", "").encode() + p[2]).digest(), eap[6:22])
            if ok:
                log(nas, p[0], "EAP-MD5", "accept", u.get("vlan"))
                return reply(req, 2, vlan_attrs(u.get("vlan")) + [(1, p[0].encode())], struct.pack("!BBH", 3, ident, 4))
            log(nas, p[0], "EAP-MD5", "reject")
            return reply(req, 3, [], struct.pack("!BBH", 4, ident, 4))
        log(nas, p[0] if p else name, "EAP type %d" % typ, "reject")
        return reply(req, 3, [], struct.pack("!BBH", 4, ident, 4))
    pw = pap(req, a) if get(a, 2) is not None else ""
    svc = get(a, 6)
    mac = macnorm(name)
    if mac and (svc == b"\0\0\0\x0a" or macnorm(pw) == mac or get(a, 2) is None):  # MAC authentication
        if any(fnmatch.fnmatchcase(mac, "".join(ch for ch in m.lower() if ch not in ":-.")) for m in cfg.get("mab", [])):
            log(nas, name, "MAC", "accept", cfg.get("mabVlan"))
            return reply(req, 2, vlan_attrs(cfg.get("mabVlan")), None)
        if user(name) is None:
            log(nas, name, "MAC", "reject")
            return reply(req, 3, [], None)
    u = user(name)
    if u is not None and get(a, 2) is not None and hmac.compare_digest(pw.encode(), u.get("password", "").encode()):
        log(nas, name, "PAP", "accept", u.get("vlan"))
        return reply(req, 2, vlan_attrs(u.get("vlan")), None)
    log(nas, name, "PAP", "reject")
    return reply(req, 3, [], None)


while True:
    try:
        m = os.stat(path).st_mtime
    except OSError:
        break
    if m != m0:
        m0 = m
        try:
            cfg = json.load(open(path))
        except Exception:
            cfg = {}
        for k, fn in (("cert", "radius-cert.pem"), ("key", "radius-key.pem")):  # PEAP's certificate and key
            if cfg.get(k):
                try:
                    fd = os.open(os.path.join(d, fn), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
                    with os.fdopen(fd, "w") as fh:
                        fh.write(cfg[k])
                except OSError:
                    pass
    want = (cfg.get("bind", "0.0.0.0"), int(cfg.get("port", 1812)))
    if want != bound:  # (also again while it cannot listen: the lab's bridge may come later)
        for s in socks:
            s.close()
        socks, bound = [], want
        try:
            for p in (want[1], want[1] + 1):
                s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
                s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                s.bind((want[0], p))
                socks.append(s)
            st["err"], changed = "", True
        except OSError as e:
            for s in socks:
                s.close()
            socks, bound = [], None
            why = "cannot listen on %s:%d: %s" % (want[0], want[1], e.strerror)
            if st["err"] != why:
                st["err"], changed = why, True
    r, _, _ = select.select(socks, [], [], 0.5) if socks else ([], [], time.sleep(0.5))
    for s in r:
        try:
            req, src = s.recvfrom(4096)
        except OSError:
            continue
        if len(req) < 20 or len(req) < struct.unpack("!H", req[2:4])[0]:
            continue
        req = req[:struct.unpack("!H", req[2:4])[0]]
        out = None
        if req[0] == 1 and s is socks[0]:
            out = access(req, src[0])
        elif req[0] == 4 and s is socks[-1]:  # accounting: the request is signed with the secret
            z = req[:4] + b"\0" * 16 + req[20:]
            if hmac.compare_digest(hashlib.md5(z + secret()).digest(), req[4:20]):
                st["acct"] += 1
                changed = True
                out = reply(req, 5, [], None)
        if out:
            s.sendto(out, src)
    now = time.time()
    for k in [k for k, v in pending.items() if now - v[3] > 60]:
        del pending[k]
    for k in [k for k, v in peap.items() if now - v["at"] > 60]:
        del peap[k]
    if changed and now - last > 1:
        changed, last = False, now
        try:
            with open(st_path + ".new", "w") as f:
                json.dump(dict(st, at=round(now)), f)
            os.replace(st_path + ".new", st_path)
        except OSError:
            pass
