# Termwright lab RADIUS server: the lab's switches ask it about 802.1X and MAC logins. Reads radius.json beside it
# ({"bind", "port", "secret", "users": [{"user", "password", "vlan"}], "mab": [MAC or pattern], "mabVlan"}), answers
# Access-Requests on the port (PAP, EAP-MD5 with State and Message-Authenticator, MAC authentication) and
# Accounting-Requests on the next one, and writes radius-stats.json (counts and the last 200 logins). A user name
# may be a pattern (user*). Accepted users get their VLAN (Tunnel-Type 13, Tunnel-Medium-Type 6,
# Tunnel-Private-Group-Id). Ends when radius.json is gone.
import fnmatch, hashlib, hmac, json, os, select, socket, struct, sys, time

d = sys.argv[1]
path, st_path = os.path.join(d, "radius.json"), os.path.join(d, "radius-stats.json")
cfg, m0 = {}, None
socks, bound = [], None
pending = {}  # State -> (user, EAP id, challenge, when)
st = {"accept": 0, "reject": 0, "challenge": 0, "acct": 0, "dropped": 0, "log": [], "err": ""}
changed, last = True, 0


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
        p = pending.pop(get(a, 24) or b"", None)
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
    if changed and now - last > 1:
        changed, last = False, now
        try:
            with open(st_path + ".new", "w") as f:
                json.dump(dict(st, at=round(now)), f)
            os.replace(st_path + ".new", st_path)
        except OSError:
            pass
