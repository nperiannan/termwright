# Termwright test ports: sends and counts frames on switch ports of the emulated lab.
# One per lab folder on a host. Reads traffic.json (written by Termwright), writes
# traffic-stats.json twice a second. A test port is a UDP socket pair with a switch
# port's QEMU socket network: frames the switch sends arrive on "listen", frames
# sent to "send" reach the switch.
#
# Streams: udp, icmp, tcp (SYN), arp or raw frames over IPv4 or IPv6, with a VLAN
# tag, PCP and DSCP, fixed / stepped / IMIX sizes, bursts, and fields that change
# per frame (MACs, addresses, VLAN, ports: incrementing or random). Multicast
# streams reach the test ports that joined the group (they send IGMP/MLD reports).
# Test ports can take their address by DHCP (and IPv6 by SLAAC from router
# advertisements), announce themselves with gratuitous ARP and LLDP, and answer
# ARP, neighbour solicitations and ping. They read the switch's LLDP (its name
# and port), and test ports with the same LAG name speak LACP (802.1AX, active):
# with the switch they form a link aggregation, and a stream to one of them is
# received on any of them. A test port can also stand for many clients (hosts
# behind it, each with its own MAC): they take addresses by DHCP, join multicast
# groups (IGMP/MLD) and log in by 802.1X (EAPOL, EAP-MD5), started a few at a time.
# It can speak STP or RSTP: BPDUs that claim a bridge (priority and MAC), and a
# topology change on request; the switch's BPDUs are read back.
# Each frame carries a signature: stream, run, sequence, send time and flow, so the
# receiver counts loss, out-of-order and duplicate frames, latency (a histogram for
# its percentiles) and jitter, per stream and per flow (the values of its changing
# fields), and the VLAN, priority and DSCP the frames arrived with. A round-trip
# stream's frames are sent back by the test port they reach: its latency is the
# round trip, measured on one clock (so also between test ports on two hosts).
# A pcap stream replays the frames of a capture (pcap/<id>.pcap, written by Termwright in
# classic little-endian pcap) at a rate or with the capture's own timing; the receiving
# test ports count its frames by their contents (from the EtherType on, any VLAN tags
# left out), so they are counted on switched paths, not routed ones.
import bisect, hashlib, json, math, os, random, socket, ssl, struct, sys, time, select

d = sys.argv[1]
cfg_path, st_path = os.path.join(d, "traffic.json"), os.path.join(d, "traffic-stats.json")
MAGIC = b"TWTG"
SIG = "!HIIdH"  # stream, run, sequence, send time, flow
SIGN = struct.calcsize(SIG)
MAXF = 1024  # flows counted one by one (a stream with more is counted as a whole)
BC = b"\xff" * 6
IMIX = [64] * 7 + [594] * 4 + [1518]


def mac_b(m):
    return bytes(int(x, 16) for x in m.split(":"))


def is6(a):
    return ":" in (a or "")


def ip_b(a):
    if not a:
        return b"\0\0\0\0"
    a = a.split("/")[0]
    return socket.inet_pton(socket.AF_INET6, a) if is6(a) else socket.inet_aton(a)


def ip_s(b):
    return socket.inet_ntop(socket.AF_INET6 if len(b) == 16 else socket.AF_INET, b)


def csum(b):
    if len(b) % 2:
        b += b"\0"
    s = sum(struct.unpack("!%dH" % (len(b) // 2), b))
    while s >> 16:
        s = (s & 0xFFFF) + (s >> 16)
    return (~s) & 0xFFFF


def l2(dst, src, vlan, prio, etype):
    h = dst + src
    if vlan:
        h += struct.pack("!HH", 0x8100, (prio << 13) | vlan)
    return h + struct.pack("!H", etype)


def ipv4(src, dst, proto, payload, tos=0, ttl=64, opts=b""):
    hl = 20 + len(opts)
    h = struct.pack("!BBHHHBBH4s4s", 0x40 | (hl // 4), tos, hl + len(payload), 0, 0x4000, ttl, proto, 0, src, dst) + opts
    h = h[:10] + struct.pack("!H", csum(h)) + h[12:]
    return h + payload


def ipv6(src, dst, nh, payload, tc=0, hop=64):
    return struct.pack("!IHBB16s16s", (6 << 28) | (tc << 20), len(payload), nh, hop, src, dst) + payload


def l4sum(src, dst, proto, p):
    if len(src) == 16:
        ph = src + dst + struct.pack("!IxxxB", len(p), proto)
    else:
        ph = src + dst + struct.pack("!BBH", 0, proto, len(p))
    return csum(ph + p)


def l3_at(f):  # where the IP header starts (past any VLAN tags), and the EtherType
    o = 12
    while f[o:o + 2] in (b"\x81\x00", b"\x88\xa8", b"\x91\x00") and len(f) >= o + 6:
        o += 4
    return o + 2, bytes(f[o:o + 2])


def l4fix(f, l4, proto, src, dst):  # UDP length and the UDP, TCP or ICMP checksum, worked out (f: bytearray)
    if proto == 17 and len(f) >= l4 + 8:
        f[l4 + 4:l4 + 8] = struct.pack("!HH", len(f) - l4, 0)
        f[l4 + 6:l4 + 8] = struct.pack("!H", l4sum(src, dst, 17, bytes(f[l4:])) or 0xFFFF)
    elif proto == 6 and len(f) >= l4 + 20:
        f[l4 + 16:l4 + 18] = b"\0\0"
        f[l4 + 16:l4 + 18] = struct.pack("!H", l4sum(src, dst, 6, bytes(f[l4:])))
    elif proto in (1, 58) and len(f) >= l4 + 4:
        f[l4 + 2:l4 + 4] = b"\0\0"
        f[l4 + 2:l4 + 4] = struct.pack("!H", l4sum(src, dst, 58, bytes(f[l4:])) if proto == 58 else csum(bytes(f[l4:])))


def fixup(f):  # a frame's IP and UDP lengths and its checksums worked out (IPv4 or IPv6; UDP, TCP, ICMP)
    f = bytearray(f)
    o, et = l3_at(f)
    if et == b"\x08\x00" and len(f) >= o + 20 and f[o] >> 4 == 4:
        hl = (f[o] & 15) * 4
        if hl < 20 or len(f) < o + hl:
            return bytes(f)
        f[o + 2:o + 4] = struct.pack("!H", len(f) - o)
        f[o + 10:o + 12] = b"\0\0"
        f[o + 10:o + 12] = struct.pack("!H", csum(bytes(f[o:o + hl])))
        if not struct.unpack("!H", f[o + 6:o + 8])[0] & 0x3FFF:  # a whole datagram, not a fragment
            l4fix(f, o + hl, f[o + 9], bytes(f[o + 12:o + 16]), bytes(f[o + 16:o + 20]))
    elif et == b"\x86\xdd" and len(f) >= o + 40:
        f[o + 4:o + 6] = struct.pack("!H", len(f) - o - 40)
        l4fix(f, o + 40, f[o + 6], bytes(f[o + 8:o + 24]), bytes(f[o + 24:o + 40]))
    return bytes(f)


def malform(f, bad, sig):  # a fault put in a frame (BadFrames in traffic.go); sig: its counting signature
    f = bytearray(f)
    o, et = l3_at(f)
    ip4 = et == b"\x08\x00" and len(f) >= o + 20 and f[o] >> 4 == 4
    hl = (f[o] & 15) * 4 if ip4 else 0

    def hsum(n=None):  # the IPv4 header checksum again, after a change
        n = n or hl
        f[o + 10:o + 12] = b"\0\0"
        f[o + 10:o + 12] = struct.pack("!H", csum(bytes(f[o:o + n])))

    l4 = o + hl
    if bad == "ipcsum" and ip4:
        f[o + 10] ^= 0x5A
    elif bad == "l4csum" and ip4:
        at = {17: 6, 6: 16, 1: 2}.get(f[o + 9])
        if at is not None and len(f) > l4 + at:
            f[l4 + at] ^= 0x5A
    elif bad == "iplen-long" and ip4:
        f[o + 2:o + 4] = struct.pack("!H", min(65535, len(f) - o + 200))
        hsum()
    elif bad == "iplen-short" and ip4:
        f[o + 2:o + 4] = struct.pack("!H", 10)
        hsum()
    elif bad == "ihl" and ip4:
        f[o] = 0x43
        hsum(20)
    elif bad == "ipver" and ip4:
        f[o] = 0x70 | (f[o] & 15)
        hsum()
    elif bad in ("ttl0", "ttl1") and ip4:
        f[o + 8] = 0 if bad == "ttl0" else 1
        hsum()
    elif bad == "ipopts" and ip4:
        f[l4:l4] = b"\x07\x07\x04" + b"\0" * 4 + b"\x00"  # record route with one slot, end of options
        f[o] = 0x40 | ((hl + 8) // 4)
        return fixup(f)
    elif bad == "frag-first" and ip4:
        f[o + 6:o + 8] = struct.pack("!H", 0x2000)  # more fragments; none follows
        hsum()
    elif bad == "frag-tiny" and ip4:
        f[o + 6:o + 8] = struct.pack("!H", 0x2001)  # offset 8 bytes, 8 bytes long: over the UDP or TCP header
        del f[l4 + 8:]
        f[o + 2:o + 4] = struct.pack("!H", hl + 8)
        hsum()
    elif bad in ("src-martian", "src-bcast", "land") and ip4:
        f[o + 12:o + 16] = f[o + 16:o + 20] if bad == "land" else bytes([127, 0, 0, 1]) if bad == "src-martian" else b"\xff" * 4
        if bad == "land" and f[o + 9] in (6, 17):
            f[l4:l4 + 2] = f[l4 + 2:l4 + 4]
        return fixup(f)
    elif bad in ("tcp-synfin", "tcp-null", "tcp-xmas") and ip4 and f[o + 9] == 6:
        f[l4 + 13] = {"tcp-synfin": 0x03, "tcp-null": 0x00, "tcp-xmas": 0x29}[bad]
        return fixup(f)
    elif bad == "smac-mcast":
        f[6] |= 1
    elif bad == "smac-zero":
        f[6:12] = b"\0" * 6
    elif bad == "runt":  # 48 bytes with the FCS: its headers up to the EtherType, then the signature
        f = (f[:o] + sig)[:44]
        f += b"\0" * (44 - len(f))
    elif bad == "vlan4095":
        if f[12:14] == b"\x81\x00":
            f[14:16] = struct.pack("!H", (f[14] & 0xF0) << 8 | 4095)
        else:
            f[12:12] = struct.pack("!HH", 0x8100, 4095)
    elif bad == "qinq":
        f[12:12] = struct.pack("!HH", 0x8100, 100)
    elif bad == "etype":
        f[o - 2:o] = b"\x88\xb6"
    return bytes(f)


def udf_put(f, us, seq, rnd):  # user-defined fields: Width bytes at Offset, set for this frame
    f = bytearray(f)
    for u in us:
        w, at = max(1, min(4, u.get("width", 1))), u.get("offset", 0)
        if at < 0 or at + w > len(f):
            continue
        vs = u.get("values") or []
        if u.get("mode") == "list" and vs:
            v = vs[seq % len(vs)]
        else:
            n = max(1, u.get("count") or 256)
            k = rnd.randrange(n) if u.get("mode") == "random" else seq % n
            v = u.get("start", 0) + (-k if u.get("mode") == "dec" else k) * (u.get("step") or 1)
        f[at:at + w] = (v % (1 << (8 * w))).to_bytes(w, "big")
    return bytes(f)


def fuzzed(f, z, seq):  # random bytes from From up to To (0: up to the signature), the same again for the same seed
    a = max(0, z.get("from", 14))
    b = z.get("to") or (f.find(MAGIC) if f.find(MAGIC) > 0 else len(f))
    b = min(len(f), b)
    if b <= a:
        return f
    return f[:a] + random.Random((z.get("seed") or 1) * 1000003 + seq).randbytes(b - a) + f[b:]


def mcast_mac(ip):
    if len(ip) == 16:
        return b"\x33\x33" + ip[12:]
    return b"\x01\x00\x5e" + bytes([ip[1] & 0x7F]) + ip[2:]


def is_mcast(ip):
    return (len(ip) == 4 and 224 <= ip[0] <= 239) or (len(ip) == 16 and ip[0] == 0xFF)


def arp(op, smac, sip, tmac, tip):
    return struct.pack("!HHBBH6s4s6s4s", 1, 0x0800, 6, 4, op, smac, sip, tmac, tip)


def eui64(mac):
    return bytes([mac[0] ^ 2]) + mac[1:3] + b"\xff\xfe" + mac[3:6]


def prefix_len(mask):
    return bin(int.from_bytes(mask, "big")).count("1")


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


class Tester:
    def __init__(s, t):
        s.id, s.listen = t["id"], t["listen"]
        s.xid = random.randrange(1 << 32)
        s.dhcp_state, s.dhcp_at, s.lease, s.server, s.offer = "", 0, None, None, None
        s.rs_at, s.lldp_at, s.garp_at = 0, 0, 0
        s.to = ("127.0.0.1", t["send"])
        s.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.sock.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 4 << 20)
        s.sock.bind(("127.0.0.1", t["listen"]))
        s.sock.setblocking(False)
        s.arp = {}  # ip bytes -> mac bytes
        s.last_arp, s.last_join = 0, 0
        s.set(t)

    def set(s, t):
        s.name = t.get("name", t["id"])
        s.mac, s.vlan = mac_b(t["mac"]), t.get("vlan", 0)
        s.dhcp, s.slaac, s.lldp = t.get("dhcp", False), t.get("slaac", False), t.get("lldp", False)
        if not s.dhcp or not s.lease:
            s.ip = ip_b(t.get("ip", ""))
            s.gw = ip_b(t.get("gw", "")) if t.get("gw") else None
        if not s.slaac or not getattr(s, "ip6", None):
            s.ip6 = ip_b(t["ip6"]) if t.get("ip6") else None
        s.ll = b"\xfe\x80" + b"\0" * 6 + eui64(s.mac)  # its IPv6 link-local address
        s.groups = [ip_b(g) for g in t.get("groups") or []]
        s.last_join, s.garp_at = 0, 0
        s.lag, s.lacp_fast = t.get("lag", ""), bool(t.get("lacpFast"))
        cl = t.get("clients") or {}
        key = (cl.get("n", 0), cl.get("mac", ""))
        if getattr(s, "cl_key", None) != key:
            s.cl_key, s.clients = key, [Client(s, k, bump(mac_b(cl["mac"]), k)) for k in range(min(4000, cl.get("n", 0)))] if cl.get("n") and cl.get("mac") else []
            s.cl_start, s.cl_tick = 0, 0
            s.cmac, s.cip, s.c6 = {c.mac: c for c in s.clients}, {}, {}  # its clients by MAC and address, for what comes back to them
        s.cl = cl
        s.stp = t.get("stp") or {}
        if not hasattr(s, "stp_at"):
            s.stp_at, s.stp_tx, s.stp_rx, s.stp_tc, s.stp_peer, s.stp_tcn = 0, 0, 0, 0, None, s.stp.get("tc", 0)
        if s.stp.get("tc", 0) != s.stp_tcn:  # a topology change asked for: the next BPDUs say so
            s.stp_tcn, s.stp_tc, s.stp_at = s.stp.get("tc", 0), 4, 0
        s.lacp_port = t.get("lacpPort", 1)
        if not hasattr(s, "partner"):
            s.partner, s.partner_at, s.lacp_at, s.neighbour = None, 0, 0, None
        now = time.time()
        s.vr_set(t.get("vrrp"), now)
        s.bfd_set(t.get("bfd"), now)

    def send(s, frame, runt=False):
        if len(frame) < 60 and not runt:
            frame += b"\0" * (60 - len(frame))
        try:
            s.sock.sendto(frame, s.to)
        except OSError:
            port(s.id)[6] += 1
            return
        c = port(s.id)
        c[0] += 1
        c[2] += len(frame) + 4

    def ask(s, ip):  # ARP request
        s.send(l2(BC, s.mac, s.vlan, 0, 0x0806) + arp(1, s.mac, s.ip, b"\0" * 6, ip))

    def garp(s):  # gratuitous ARP: the switch learns where it is
        if s.ip != b"\0\0\0\0":
            s.send(l2(BC, s.mac, s.vlan, 0, 0x0806) + arp(1, s.mac, s.ip, b"\0" * 6, s.ip))

    def dhcp_send(s, kind):  # DISCOVER (1) or REQUEST (3), broadcast
        opts = bytes([53, 1, kind, 61, 7, 1]) + s.mac + bytes([55, 3, 1, 3, 6, 12, len(s.name[:30])]) + s.name[:30].encode()
        if kind == 3 and s.offer:
            opts += bytes([50, 4]) + s.offer[0] + bytes([54, 4]) + s.offer[1]
        bootp = struct.pack("!BBBBIHH4s4s4s4s16s64s128s4s", 1, 1, 6, 0, s.xid, 0, 0x8000, b"\0" * 4, b"\0" * 4, b"\0" * 4, b"\0" * 4, s.mac + b"\0" * 10, b"\0" * 64, b"\0" * 128, b"\x63\x82\x53\x63") + opts + b"\xff"
        p = struct.pack("!HHHH", 68, 67, 8 + len(bootp), 0) + bootp
        src, dst = b"\0" * 4, b"\xff" * 4
        p = p[:6] + struct.pack("!H", l4sum(src, dst, 17, p) or 0xFFFF) + p[8:]
        s.send(l2(BC, s.mac, s.vlan, 0, 0x0800) + ipv4(src, dst, 17, p, 0, 64))

    def rs(s):  # a router solicitation, for SLAAC
        p = struct.pack("!BBHI", 133, 0, 0, 0) + b"\x01\x01" + s.mac
        dst = socket.inet_pton(socket.AF_INET6, "ff02::2")
        p = p[:2] + struct.pack("!H", l4sum(s.ll, dst, 58, p)) + p[4:]
        s.send(l2(b"\x33\x33\x00\x00\x00\x02", s.mac, s.vlan, 0, 0x86DD) + ipv6(s.ll, dst, 58, p, 0, 255))

    def lldp_send(s):  # LLDP: the switch sees "Termwright <name>" on its port
        def tlv(t, v):
            return struct.pack("!H", (t << 9) | len(v)) + v
        name = ("Termwright " + s.name).encode()[:200]
        f = tlv(1, b"\x04" + s.mac) + tlv(2, b"\x07" + s.name.encode()[:200]) + tlv(3, struct.pack("!H", 120)) + tlv(4, b"test port") + tlv(5, name)
        if s.ip != b"\0\0\0\0":
            f += tlv(8, bytes([5, 1]) + s.ip + b"\x02\x00\x00\x00\x00\x00")
        s.send(b"\x01\x80\xc2\x00\x00\x0e" + s.mac + b"\x88\xcc" + f + tlv(0, b""))

    def lag_id(s):  # its LAG's system (a MAC from its name) and key
        h = sum(s.lag.encode()) * 7919 + len(s.lag)
        return b"\x02\x74\x77\x1a" + struct.pack("!H", h & 0xFFFF), 1000 + (h % 9000)

    def lacp_state(s, now):
        st = 0x05 | (0x02 if s.lacp_fast else 0)  # active, aggregation, the timeout it asks for
        p = s.partner if s.partner and now - s.partner_at < (3 if s.lacp_fast else 90) else None
        if p:
            st |= 0x08  # in sync: it takes the partner as it is
            if p["state"] & 0x08:
                st |= 0x30  # collecting and distributing once the partner is in sync too
        else:
            st |= 0x40  # defaulted
        return st, p

    def lacp_send(s, now):  # an LACPDU: actor (this test port in its LAG), partner (what the switch said)
        sysmac, key = s.lag_id()
        st, p = s.lacp_state(now)
        actor = struct.pack("!BBH6sHHHB3x", 1, 20, 32768, sysmac, key, 32768, s.lacp_port, st)
        if p:
            partner = struct.pack("!BBH6sHHHB3x", 2, 20, p["prio"], p["sys"], p["key"], p["pprio"], p["port"], p["state"])
        else:
            partner = struct.pack("!BBH6sHHHB3x", 2, 20, 0, b"\0" * 6, 0, 0, 0, 0)
        pdu = b"\x01\x01" + actor + partner + struct.pack("!BBH12x", 3, 16, 0) + b"\0\0" + b"\0" * 50
        s.send(b"\x01\x80\xc2\x00\x00\x02" + s.mac + b"\x88\x09" + pdu)

    def lacp_got(s, b, now):  # an LACPDU from the switch: its actor is our partner
        if len(b) < 2 + 20 or b[0] != 1 or b[2] != 1:
            return
        prio, sysm, key, pprio, port, st = struct.unpack("!H6sHHHB", b[4:19])
        was = s.lacp_state(now)[0]
        s.partner, s.partner_at = {"prio": prio, "sys": sysm, "key": key, "pprio": pprio, "port": port, "state": st}, now
        if s.lacp_state(now)[0] != was:
            s.lacp_send(now)  # tell the switch at once when its own state changed

    def lldp_got(s, b):  # the switch's LLDP: its name, chassis and port
        i, n = 0, {}
        while i + 2 <= len(b):
            h = struct.unpack("!H", b[i:i + 2])[0]
            t, ln = h >> 9, h & 0x1FF
            v = b[i + 2:i + 2 + ln]
            i += 2 + ln
            if t == 0:
                break
            if t == 1 and v:
                n["chassis"] = v[1:].hex(":") if v[0] == 4 and len(v) == 7 else v[1:].decode("utf-8", "replace")
            elif t == 2 and v:
                n["port"] = v[1:].hex(":") if v[0] == 3 and len(v) == 7 else v[1:].decode("utf-8", "replace")
            elif t == 4:
                n["portDesc"] = v.decode("utf-8", "replace")
            elif t == 5:
                n["name"] = v.decode("utf-8", "replace")
        if n:
            s.neighbour = n

    def lacp_view(s, now):
        if not s.lag:
            return None
        st, p = s.lacp_state(now)
        return {"lag": s.lag, "up": st & 0x30 == 0x30, "state": st, "partner": {"sys": p["sys"].hex(":"), "key": p["key"], "port": p["port"], "state": p["state"]} if p else None}

    def bpdu(s, now):  # a configuration BPDU (STP) or an RST BPDU: this test port as root
        rst = s.stp.get("mode") == "rstp"
        prio = int(s.stp.get("prio", 32768) if s.stp.get("prio") is not None else 32768) & 0xF000
        bid = struct.pack("!H", prio) + s.mac
        flags = 0
        if s.stp_tc > 0:
            flags |= 0x01  # topology change
            s.stp_tc -= 1
        if rst:
            flags |= 0x02 | 0x0C | 0x30  # proposal, port role designated, learning, forwarding
        body = struct.pack("!HBBB8sI8sHHHHH", 0, 2 if rst else 0, 2 if rst else 0, flags, bid, 0, bid, 0x8001, 0, 20 * 256, 2 * 256, 15 * 256) + (b"\0" if rst else b"")
        llc = b"\x42\x42\x03" + body
        s.send(b"\x01\x80\xc2\x00\x00\x00" + s.mac + struct.pack("!H", len(llc)) + llc)
        s.stp_tx += 1

    def bpdu_got(s, b):  # the switch's BPDU: who it says is root, its own bridge, its port's role
        if len(b) < 35 or b[0:3] != b"\x42\x42\x03":
            return
        b = b[3:]
        ver, typ, flags = b[2], b[3], b[4]
        s.stp_rx += 1
        if typ == 0x80:
            s.stp_peer = {"type": "TCN"}
            return
        root, cost, bridge, port = b[5:13], struct.unpack("!I", b[13:17])[0], b[17:25], struct.unpack("!H", b[25:27])[0]
        role = ["unknown", "alternate/backup", "root", "designated"][(flags >> 2) & 3] if ver >= 2 else ""
        s.stp_peer = {"version": ver, "root": "%d/%s" % (struct.unpack("!H", root[:2])[0], root[2:].hex(":")), "cost": cost,
                      "bridge": "%d/%s" % (struct.unpack("!H", bridge[:2])[0], bridge[2:].hex(":")), "port": "%x" % port, "role": role, "tc": bool(flags & 1),
                      "rootIsUs": root[2:] == s.mac}

    def tick(s, now):  # its own protocols: DHCP, SLAAC, gratuitous ARP, LLDP, LACP, STP, its clients
        if s.dhcp:
            if s.dhcp_state in ("", "discover") and now - s.dhcp_at > 4:
                s.dhcp_state, s.dhcp_at = "discover", now
                s.dhcp_send(1)
            elif s.dhcp_state == "request" and now - s.dhcp_at > 4:
                s.dhcp_state, s.dhcp_at = "discover", now
                s.dhcp_send(1)
            elif s.dhcp_state == "bound" and now > s.lease_end:
                s.dhcp_state, s.dhcp_at = "request", now
                s.dhcp_send(3)
        if s.slaac and not s.ip6 and now - s.rs_at > 8:
            s.rs_at = now
            s.rs()
        if s.lldp and now - s.lldp_at > 30:
            s.lldp_at = now
            s.lldp_send()
        if s.garp_at == 0 and s.ip != b"\0\0\0\0":
            s.garp_at = now
            s.garp()
        if s.lag and now - s.lacp_at > (1 if s.lacp_fast else 30) - (0.02 if s.lacp_at else 99):
            s.lacp_at = now
            s.lacp_send(now)
        if s.stp.get("mode") and now - s.stp_at > 2:
            s.stp_at = now
            s.bpdu(now)
        if s.vrrp:
            s.vr_tick(now)
        if s.bfd and s.ip != b"\0\0\0\0":
            s.bfd_tick(now)
        if s.clients and now - s.cl_tick > 0.1:
            s.cl_tick = now
            if not s.cl_start:
                s.cl_start = now
            rate = max(1, int(s.cl.get("rate", 50)))  # clients started a second
            started = min(len(s.clients), int((now - s.cl_start) * rate) + 1)
            for c in s.clients[:started]:
                c.tick(now)

    def join(s):  # IGMPv2 / MLDv1 reports for the groups it is in
        for g in s.groups:
            if len(g) == 4:
                p = struct.pack("!BBH4s", 0x16, 0, 0, g)
                p = p[:2] + struct.pack("!H", csum(p)) + p[4:]
                s.send(l2(mcast_mac(g), s.mac, s.vlan, 0, 0x0800) + ipv4(s.ip, g, 2, p, 0xC0, 1, b"\x94\x04\x00\x00"))
            elif s.ip6:
                p = struct.pack("!BBHHH16s", 131, 0, 0, 0, 0, g)
                p = p[:2] + struct.pack("!H", l4sum(s.ip6, g, 58, p)) + p[4:]
                hbh = struct.pack("!BB", 58, 0) + b"\x05\x02\x00\x00\x01\x00"  # router alert, PadN
                s.send(l2(mcast_mac(g), s.mac, s.vlan, 0, 0x86DD) + ipv6(s.ip6, g, 0, hbh + p, 0, 1))

    # ---- VRRP (RFC 3768 v2, RFC 5798 v3): the test port as a second router for the switch's virtual address ----
    def vr_set(s, v, now):
        if not v:
            s.vrrp = None
            return
        old = getattr(s, "vrrp", None) or {}
        s.vrrp = v
        s.vr_vip = ip_b(v.get("vip", ""))
        s.vr_v6 = len(s.vr_vip) == 16
        s.vr_vmac = bytes([0, 0, 0x5E, 0, 2 if s.vr_v6 else 1, v.get("vrid", 1) & 255])
        s.vr_iv = max(0.01, (v.get("interval") or 1000) / 1000.0)
        if v.get("ver") == 2:
            s.vr_iv = max(1, round(s.vr_iv))
        silent = bool(v.get("silent"))
        if not hasattr(s, "vr_state") or (old.get("vrid"), old.get("vip"), old.get("ver")) != (v.get("vrid"), v.get("vip"), v.get("ver")):
            s.vr_state, s.vr_at, s.vr_down, s.vr_master, s.vr_mprio = "backup", 0, now + s.vr_down_time(), "", 0
            s.vr_changes, s.vr_since, s.vr_t0, s.vr_take, s.vr_last_us = 0, now, 0, 0, None
        if silent and s.vr_state != "silent":
            s.vr_t0, s.vr_state = now, "silent"  # as if the router died: no more advertisements, no ARP
        elif not silent and s.vr_state == "silent":
            s.vr_state, s.vr_down, s.vr_t0 = "backup", now + s.vr_down_time(), 0
        if old.get("prio") != v.get("prio") and s.vr_state == "backup" and v.get("preempt") and v.get("prio", 100) > s.vr_mprio:
            s.vr_down = now + s.vr_skew()  # a higher priority with preempt: take over soon

    def vr_skew(s):
        return (256 - s.vrrp.get("prio", 100)) * s.vr_iv / 256

    def vr_down_time(s):
        return 3 * s.vr_iv + s.vr_skew()

    def vr_me(s):
        return s.ll if s.vr_v6 else s.ip

    def vr_send(s, prio=None):
        v, pr = s.vrrp, s.vrrp.get("prio", 100) if prio is None else prio
        src, ver = s.vr_me(), 3 if s.vr_v6 else v.get("ver", 3)
        if ver == 2:
            p = struct.pack("!BBBBBBH", 0x21, v.get("vrid", 1), pr, 1, 0, int(s.vr_iv), 0) + s.vr_vip + b"\0" * 8
            p = p[:6] + struct.pack("!H", csum(p)) + p[8:]
        else:
            p = struct.pack("!BBBBHH", 0x31, v.get("vrid", 1), pr, 1, int(round(s.vr_iv * 100)) & 0xFFF, 0) + s.vr_vip
        if s.vr_v6:
            dst = socket.inet_pton(socket.AF_INET6, "ff02::12")
            p = p[:6] + struct.pack("!H", l4sum(src, dst, 112, p)) + p[8:]
            s.send(l2(b"\x33\x33\x00\x00\x00\x12", s.vr_vmac, s.vlan, 0, 0x86DD) + ipv6(src, dst, 112, p, 0, 255))
        else:
            dst = b"\xe0\x00\x00\x12"
            if ver == 3:
                p = p[:6] + struct.pack("!H", l4sum(src, dst, 112, p)) + p[8:]
            s.send(l2(b"\x01\x00\x5e\x00\x00\x12", s.vr_vmac, s.vlan, 0, 0x0800) + ipv4(src, dst, 112, p, 0xC0, 255))

    def vr_become(s, state, now):
        if state == s.vr_state:
            return
        s.vr_state, s.vr_since = state, now
        us = state == "master"
        if s.vr_last_us is not None and s.vr_last_us != us:
            s.vr_changes += 1
        s.vr_last_us = us
        if us:  # the virtual address is here now: tell the switch's MAC table
            s.vr_send()
            s.vr_at = now
            if not s.vr_v6:
                s.send(l2(BC, s.vr_vmac, s.vlan, 0, 0x0806) + arp(1, s.vr_vmac, s.vr_vip, b"\0" * 6, s.vr_vip))

    def vr_tick(s, now):
        if s.vr_state == "master" and now - s.vr_at >= s.vr_iv:
            s.vr_at = now
            s.vr_send()
        elif s.vr_state == "backup" and now >= s.vr_down:
            s.vr_master, s.vr_mprio = "", 0
            s.vr_become("master", now)

    def vr_got(s, b, v6, now):
        o = 40 if v6 else (b[0] & 15) * 4
        p = b[o:]
        if len(p) < 8 or p[1] != s.vrrp.get("vrid", 1) or (p[0] & 15) != 1:
            return
        src = b[8:24] if v6 else b[12:16]
        if src == s.vr_me():
            return
        pr, name = p[2], ip_s(src)
        if s.vr_state == "silent":
            if s.vr_t0 and pr > 0 and not s.vr_take:  # the switch took over: how long it took
                s.vr_take = int((now - s.vr_t0) * 1000)
            s.vr_master, s.vr_mprio = name, pr
            if s.vr_last_us is not False:
                s.vr_changes += 1 if s.vr_last_us else 0
                s.vr_last_us = False
            return
        mine = s.vrrp.get("prio", 100)
        if s.vr_state == "master":
            if pr > mine or pr == mine and src > s.vr_me():
                s.vr_master, s.vr_mprio = name, pr
                s.vr_down = now + s.vr_down_time()
                s.vr_become("backup", now)
            return
        if pr == 0:  # the master stops: take over after the skew
            s.vr_down = now + s.vr_skew()
            return
        if pr >= mine or not s.vrrp.get("preempt"):
            s.vr_master, s.vr_mprio = name, pr
            s.vr_down = now + s.vr_down_time()
            if s.vr_last_us is None:
                s.vr_last_us = False

    def vr_view(s, now):
        us = s.vr_state == "master"
        return {"state": s.vr_state, "us": us, "master": "" if us else s.vr_master, "prio": s.vrrp.get("prio", 100) if us else s.vr_mprio,
                "since": round(s.vr_since), "changes": s.vr_changes, "takeover": s.vr_take}

    # ---- BFD (RFC 5880/5881): a single-hop session with the switch, to time how fast it sees a dead link ----
    def bfd_set(s, v, now):
        if not v:
            s.bfd = None
            return
        old = getattr(s, "bfd", None) or {}
        s.bfd = v
        s.bfd_peer = ip_b(v.get("peer", ""))
        s.bfd_tx, s.bfd_rx, s.bfd_mult = max(10, v.get("tx") or 300) / 1000.0, max(10, v.get("rx") or v.get("tx") or 300) / 1000.0, max(1, v.get("mult") or 3)
        if not hasattr(s, "bfd_state") or old.get("peer") != v.get("peer"):
            s.bfd_state, s.bfd_disc, s.bfd_rdisc, s.bfd_at, s.bfd_rx_at = 1, random.randrange(1, 1 << 32), 0, 0, 0
            s.bfd_rmult, s.bfd_rtx, s.bfd_rrx, s.bfd_diag, s.bfd_poll = 3, 1.0, 1.0, 0, False
            s.bfd_flaps, s.bfd_since, s.bfd_ups, s.bfd_t0, s.bfd_meas, s.bfd_quiet, s.bfd_n = 0, now, 0, 0, 0, 0, v.get("silent", 0)
        if v.get("silent", 0) != s.bfd_n:  # Go Silent: stop sending for 5 s; the switch must notice
            s.bfd_n, s.bfd_quiet, s.bfd_t0, s.bfd_meas = v.get("silent", 0), now + 5, now, 0

    def bfd_go(s, st, now, diag=0):
        if st == s.bfd_state:
            return
        if st == 3:
            s.bfd_ups += 1
            if s.bfd_ups > 1:
                s.bfd_flaps += 1
        s.bfd_state, s.bfd_since, s.bfd_diag = st, now, diag
        s.bfd_at = 0  # the new state goes out at once

    def bfd_send(s, now, final=False):
        mac = s.arp.get(s.bfd_peer)
        if not mac:
            if now - s.last_arp > 1:
                s.last_arp = now
                s.ask(s.bfd_peer)
            return
        up = s.bfd_state == 3
        p = struct.pack("!BBBBIIIII", 0x20 | s.bfd_diag, s.bfd_state << 6 | (0x10 if final else 0) | (0x20 if s.bfd_poll else 0), s.bfd_mult, 24,
                        s.bfd_disc, s.bfd_rdisc, int((s.bfd_tx if up else 1.0) * 1e6), int(s.bfd_rx * 1e6), 0)
        u = struct.pack("!HHHH", 49152 + (s.bfd_disc & 0x3FFF), 3784, 8 + len(p), 0) + p
        u = u[:6] + struct.pack("!H", l4sum(s.ip, s.bfd_peer, 17, u) or 0xFFFF) + u[8:]
        s.send(l2(mac, s.mac, s.vlan, 0, 0x0800) + ipv4(s.ip, s.bfd_peer, 17, u, 0xC0, 255))

    def bfd_tick(s, now):
        if s.bfd_state in (2, 3) and s.bfd_rx_at and now - s.bfd_rx_at > s.bfd_rmult * max(s.bfd_rx, s.bfd_rtx):
            s.bfd_go(1, now, 1)  # control detection time expired
            s.bfd_rdisc = 0
        if s.bfd_quiet and now < s.bfd_quiet:
            return
        s.bfd_quiet = 0
        iv = max(s.bfd_tx, s.bfd_rrx) if s.bfd_state == 3 else 1.0
        if now - s.bfd_at >= iv * random.uniform(0.75, 1.0):
            s.bfd_at = now
            s.bfd_send(now)

    def bfd_got(s, b, now):
        o = (b[0] & 15) * 4
        p = b[o + 8:]
        if len(p) < 24 or p[0] >> 5 != 1 or b[8] != 255:
            return
        st, flags, mult = p[1] >> 6, p[1] & 0x3F, p[2]
        my, your, rtx, rrx = struct.unpack("!IIII", p[4:20])
        if your == 0 and st not in (0, 1):
            return
        s.bfd_rdisc, s.bfd_rmult, s.bfd_rtx, s.bfd_rrx, s.bfd_rx_at = my, mult, rtx / 1e6, rrx / 1e6, now
        if s.bfd_t0 and st in (0, 1) and not s.bfd_meas:  # we went quiet and the switch saw it: how long it took
            s.bfd_meas = int((now - s.bfd_t0) * 1000)
        if st == 0:
            if s.bfd_state != 1:
                s.bfd_go(1, now, 3)
        elif s.bfd_state == 1:
            if st == 1:
                s.bfd_go(2, now)
            elif st == 2:
                s.bfd_go(3, now)
        elif s.bfd_state == 2:
            if st in (2, 3):
                s.bfd_go(3, now)
        elif s.bfd_state == 3 and st == 1:
            s.bfd_go(1, now, 3)
        if flags & 0x20 and not (s.bfd_quiet and now < s.bfd_quiet):  # a poll: answer with the final bit
            s.bfd_send(now, True)

    def bfd_view(s, now):
        return {"state": ("admin-down", "down", "init", "up")[s.bfd_state], "since": round(s.bfd_since), "flaps": s.bfd_flaps,
                "detect": int(s.bfd_rmult * max(s.bfd_rrx, s.bfd_tx) * 1000) if s.bfd_rx_at else 0, "peerTx": int(s.bfd_rtx * 1000) if s.bfd_rx_at else 0,
                "measured": s.bfd_meas, "quiet": bool(s.bfd_quiet and now < s.bfd_quiet)}


PAE = b"\x01\x80\xc2\x00\x00\x03"


class Client:
    """A host behind a test port: its own MAC, DHCP and DHCPv6, multicast groups and 802.1X (EAP-MD5 or PEAP-MSCHAPv2)."""

    def __init__(s, tp, idx, mac):
        s.tp, s.idx, s.mac = tp, idx, mac
        s.ip, s.gw, s.lease = b"\0\0\0\0", None, None
        s.xid = random.randrange(1 << 32)
        s.dhcp_state, s.dhcp_at, s.lease_end, s.offer = "", 0, 0, None
        s.x_state, s.x_at, s.x_tries, s.x_why = "", 0, 0, ""
        s.join_at = 0
        s.ip6, s.lease6, s.d6_state, s.d6_at, s.d6_end, s.d6_srv, s.d6_ia = None, "", "", 0, 0, None, None
        s.d6_xid, s.tls = random.randrange(1 << 24), None

    def send(s, dst, et, payload):
        s.tp.send(l2(dst, s.mac, s.tp.vlan, 0, et) + payload)

    def name(s, pat):
        return (pat or "").replace("{n}", str(s.idx + 1)).replace("{mac}", s.mac.hex(":"))

    def eapol(s, typ, body=b""):
        s.send(PAE, 0x888E, struct.pack("!BBH", 2, typ, len(body)) + body)

    def eap(s, code, ident, typ, data):
        p = struct.pack("!BBHB", code, ident, 5 + len(data), typ) + data
        s.eapol(0, p)

    def eap_got(s, b, now):  # an EAPOL frame for this client
        if len(b) < 4 or b[1] != 0:
            return
        e = b[4:4 + struct.unpack("!H", b[2:4])[0]]
        if len(e) < 4:
            return
        code, ident = e[0], e[1]
        cl = s.tp.cl.get("dot1x") or {}
        if code == 1 and len(e) >= 5:
            typ = e[4]
            if typ == 1:  # identity
                s.eap(2, ident, 1, s.name(cl.get("user", "user{n}")).encode())
                s.x_state, s.x_at = "identity", now
            elif typ == 4 and len(e) >= 6 and cl.get("method") == "peap":  # MD5 offered: we want PEAP
                s.eap(2, ident, 3, b"\x19")
            elif typ == 25 and cl.get("method") == "peap":
                s.peap_got(ident, e[5:], now)
            elif typ == 4 and len(e) >= 6:  # MD5 challenge
                n = e[5]
                ch = e[6:6 + n]
                h = hashlib.md5(bytes([ident]) + s.name(cl.get("password", "password")).encode() + ch).digest()
                s.eap(2, ident, 4, bytes([16]) + h)
                s.x_state, s.x_at = "challenge", now
            else:  # another method: ask for ours
                s.eap(2, ident, 3, b"\x19" if cl.get("method") == "peap" else b"\x04")
        elif code == 3:
            s.x_state, s.x_at, s.x_why, s.tls = "authenticated", now, "", None
            if s.dhcp_state == "bound":  # a new VLAN may come with it: ask again
                s.dhcp_state, s.dhcp_at = "", 0
        elif code == 4:
            s.x_state, s.x_at, s.tls = "failed", now, None
            s.x_why = s.x_why or "EAP failure"

    def dhcp_send(s, kind):
        name = ("client-%d" % (s.idx + 1)).encode()
        opts = bytes([53, 1, kind, 61, 7, 1]) + s.mac + bytes([55, 3, 1, 3, 6, 12, len(name)]) + name
        if kind == 3 and s.offer:
            opts += bytes([50, 4]) + s.offer[0] + bytes([54, 4]) + s.offer[1]
        bootp = struct.pack("!BBBBIHH4s4s4s4s16s64s128s4s", 1, 1, 6, 0, s.xid, 0, 0x8000, b"\0" * 4, b"\0" * 4, b"\0" * 4, b"\0" * 4, s.mac + b"\0" * 10, b"\0" * 64, b"\0" * 128, b"\x63\x82\x53\x63") + opts + b"\xff"
        p = struct.pack("!HHHH", 68, 67, 8 + len(bootp), 0) + bootp
        src, dst = b"\0" * 4, b"\xff" * 4
        p = p[:6] + struct.pack("!H", l4sum(src, dst, 17, p) or 0xFFFF) + p[8:]
        s.send(BC, 0x0800, ipv4(src, dst, 17, p, 0, 64))

    def dhcp_got(s, kind, yi, o, now):
        if kind == 2 and s.dhcp_state == "discover":
            s.offer = (yi, o.get(54, b"\0" * 4))
            s.dhcp_state, s.dhcp_at = "request", now
            s.dhcp_send(3)
        elif kind == 5:
            mask = o.get(1, b"\xff\xff\xff\0")
            s.tp.cip.pop(s.ip, None)
            s.ip, s.dhcp_state = yi, "bound"
            s.tp.cip[yi] = s
            s.gw = o[3][:4] if o.get(3) else None
            lt = struct.unpack("!I", o[51])[0] if len(o.get(51, b"")) == 4 else 3600
            s.lease_end = now + max(30, lt // 2)
            s.lease = socket.inet_ntoa(yi) + "/" + str(prefix_len(mask))
            s.join_at = 0
        elif kind == 6:
            s.dhcp_state, s.dhcp_at, s.offer = "", 0, None

    def join(s):
        for g in s.tp.cl.get("groups") or []:
            g = ip_b(g)
            if len(g) == 4:
                p = struct.pack("!BBH4s", 0x16, 0, 0, g)
                p = p[:2] + struct.pack("!H", csum(p)) + p[4:]
                s.send(mcast_mac(g), 0x0800, ipv4(s.ip, g, 2, p, 0xC0, 1, b"\x94\x04\x00\x00"))
            else:
                ll = b"\xfe\x80" + b"\0" * 6 + eui64(s.mac)
                p = struct.pack("!BBHHH16s", 131, 0, 0, 0, 0, g)
                p = p[:2] + struct.pack("!H", l4sum(ll, g, 58, p)) + p[4:]
                hbh = struct.pack("!BB", 58, 0) + b"\x05\x02\x00\x00\x01\x00"
                s.send(mcast_mac(g), 0x86DD, ipv6(ll, g, 0, hbh + p, 0, 1))

    def tick(s, now):
        cl = s.tp.cl
        x = cl.get("dot1x")
        if x and s.x_state in ("", "started", "identity", "challenge", "tls", "inner") and now - s.x_at > {"": 0, "started": 5}.get(s.x_state, 30):
            if s.x_tries >= 3 and s.x_state:
                s.x_state, s.x_why = "failed", "no answer from the switch"
            else:
                s.x_tries += 1
                s.x_state, s.x_at = "started", now
                s.eapol(1)  # EAPOL-Start
        if x and s.x_state != "authenticated":
            return  # the port lets nothing else through before it is authenticated
        if cl.get("dhcp"):
            if s.dhcp_state in ("", "discover") and now - s.dhcp_at > 4:
                s.dhcp_state, s.dhcp_at = "discover", now
                s.dhcp_send(1)
            elif s.dhcp_state == "request" and now - s.dhcp_at > 4:
                s.dhcp_state, s.dhcp_at = "discover", now
                s.dhcp_send(1)
            elif s.dhcp_state == "bound" and now > s.lease_end:
                s.dhcp_state, s.dhcp_at = "request", now
                s.dhcp_send(3)
        if cl.get("dhcp6"):
            if s.d6_state in ("", "solicit", "request") and now - s.d6_at > 4:
                s.d6_state, s.d6_at = "solicit", now
                s.d6_send(1)
            elif s.d6_state == "bound" and now > s.d6_end:
                s.d6_state, s.d6_at = "renew", now
                s.d6_send(5)
            elif s.d6_state == "renew" and now - s.d6_at > 10:
                s.d6_state, s.d6_at = "", 0
        if cl.get("groups") and (not cl.get("dhcp") or s.dhcp_state == "bound") and now - s.join_at > 10:
            s.join_at = now
            s.join()

    def view(s):
        return [s.mac.hex(":"), s.lease or "", s.x_state, s.x_why, s.lease6]
    # ---- PEAPv0 with EAP-MSCHAPv2 inside (the inner EAP without its header, as Microsoft's PEAP does) ----
    def peap_got(s, ident, d, now):
        if not d:
            return
        if d[0] & 0x20:  # start: our ClientHello
            s.tls = Tls(False)
            s.tls.step()
            s.x_state, s.x_at, s.x_why = "tls", now, ""
            s.eap(2, ident, 25, s.tls.frag())
            return
        t = getattr(s, "tls", None)
        if t is None:
            return
        if t.tx and len(d) == 1:  # the server took a fragment: the next one
            s.eap(2, ident, 25, t.frag())
            return
        if not t.got(d):  # more of the server's message to come
            s.eap(2, ident, 25, b"\0")
            return
        try:
            app = t.step()
            if app:
                out = s.peap_inner(app, now)
                if out:
                    t.write(out)
        except ssl.SSLError as e:
            s.x_state, s.x_at, s.x_why, s.tls = "failed", now, "TLS: " + (e.reason or str(e)), None
            return
        s.eap(2, ident, 25, t.frag())

    def peap_inner(s, a, now):
        cl = s.tp.cl.get("dot1x") or {}
        user, pw = s.name(cl.get("user", "user{n}")), s.name(cl.get("password", "password"))
        if len(a) >= 11 and a[0] in (1, 2) and struct.unpack("!H", a[2:4])[0] == len(a) and a[4] == 33:  # the result TLV (with its header)
            ok = a[5:11] == b"\x80\x03\x00\x02\x00\x01"
            if not ok:
                s.x_why = s.x_why or "the server said no (result TLV)"
            return struct.pack("!BBHB", 2, a[1], 11, 33) + b"\x80\x03\x00\x02\x00" + (b"\x01" if ok else b"\x02")
        if a[0] == 1:  # inner identity
            s.x_state, s.x_at = "inner", now
            return b"\x01" + user.encode()
        if a[0] == 26 and len(a) >= 2:
            if a[1] == 1 and len(a) >= 22:  # the server's challenge: our response
                s.chal, s.pchal = a[6:22], os.urandom(16)
                ntr = nt_response(s.chal, s.pchal, user, pw)
                s.x_state, s.x_at = "challenge", now
                return struct.pack("!BBBHB", 26, 2, a[2], 54 + len(user.encode()), 49) + s.pchal + b"\0" * 8 + ntr + b"\0" + user.encode()
            if a[1] == 3:
                return b"\x1a\x03"
            if a[1] == 4:
                s.x_why = "wrong user name or password (MS-CHAPv2)"
                return b"\x1a\x04"
        return b"\x03\x1a"  # anything else: we speak MS-CHAPv2

    # ---- DHCPv6 (RFC 8415): solicit, advertise, request, reply; renew at T1 ----
    def d6_send(s, kind):
        cid = struct.pack("!HH", 1, 10) + b"\0\x03\0\x01" + s.mac  # client ID: DUID-LL
        ia = s.d6_ia if kind in (3, 5) and s.d6_ia else b""
        o = cid + struct.pack("!HHIII", 3, 12 + len(ia), s.idx + 1, 0, 0) + ia + struct.pack("!HHH", 8, 2, 0) + struct.pack("!HHH", 6, 2, 23)
        if kind in (3, 5) and s.d6_srv:
            o += struct.pack("!HH", 2, len(s.d6_srv)) + s.d6_srv
        p = struct.pack("!I", kind << 24 | s.d6_xid) + o
        ll, dst = b"\xfe\x80" + b"\0" * 6 + eui64(s.mac), socket.inet_pton(socket.AF_INET6, "ff02::1:2")
        u = struct.pack("!HHHH", 546, 547, 8 + len(p), 0) + p
        u = u[:6] + struct.pack("!H", l4sum(ll, dst, 17, u)) + u[8:]
        s.tp.c6[ll] = s
        s.send(b"\x33\x33\x00\x01\x00\x02", 0x86DD, ipv6(ll, dst, 17, u, 0, 1))

    def d6_got(s, p, now):
        if len(p) < 4 or struct.unpack("!I", p[:4])[0] & 0xFFFFFF != s.d6_xid:
            return
        kind, o, i, srv, ia = p[0], {}, 4, None, None
        while i + 4 <= len(p):
            c, n = struct.unpack("!HH", p[i:i + 4])
            o.setdefault(c, p[i + 4:i + 4 + n])
            i += 4 + n
        srv, ia = o.get(2), o.get(3)
        addr, t1 = None, 0
        if ia and len(ia) >= 12:
            t1, j = struct.unpack("!I", ia[4:8])[0], 12
            while j + 4 <= len(ia):
                c, n = struct.unpack("!HH", ia[j:j + 4])
                if c == 5 and n >= 24:
                    addr, valid = ia[j + 4:j + 20], struct.unpack("!I", ia[j + 24:j + 28])[0]
                    t1 = t1 or valid // 2
                j += 4 + n
        if kind == 2 and s.d6_state == "solicit" and addr:  # advertise: ask for it
            s.d6_srv, s.d6_ia, s.d6_state, s.d6_at = srv, ia[12:], "request", now
            s.d6_send(3)
        elif kind == 7 and addr and s.d6_state in ("request", "renew"):
            s.tp.c6.pop(s.ip6, None)
            s.ip6, s.d6_state, s.d6_end = addr, "bound", now + max(30, t1 or 1800)
            s.tp.c6[addr] = s
            s.lease6 = socket.inet_ntop(socket.AF_INET6, addr)
        elif kind == 7 and s.d6_state in ("request", "renew"):  # no address (a status code): from the start again
            s.d6_state, s.d6_at = "", now

    def na(s, b, f, vlan):  # a neighbour solicitation for one of its IPv6 addresses: advertise it
        me, src = b[48:64], b[8:24]
        if src == b"\0" * 16:
            return
        p = struct.pack("!BBHI16s", 136, 0, 0, 0x60000000, me) + b"\x02\x01" + s.mac
        p = p[:2] + struct.pack("!H", l4sum(me, src, 58, p)) + p[4:]
        s.tp.send(l2(f[6:12], s.mac, vlan, 0, 0x86DD) + ipv6(me, src, 58, p, 0, 255))


def bump(b, n):  # an address or MAC plus n
    v = (int.from_bytes(b, "big") + n) % (1 << (8 * len(b)))
    return v.to_bytes(len(b), "big")


class Stream:
    def __init__(s, x, idx, testers, run):
        s.x, s.idx, s.run = x, idx, run
        s.tp = testers.get(x["from"])
        s.pps = max(1, x.get("pps", 100))
        s.size = max(60, min(9000, x.get("size", 128)))
        s.size_max = max(s.size, min(9000, x.get("sizeMax", 0) or s.size))
        s.count = x.get("count") or int(s.pps * max(1, x.get("seconds", 10)))
        s.burst = max(1, x.get("burst", 1))
        s.seq, s.start, s.gw_wait = 0, None, 0
        s.dst = mac_b(x["dstMac"]) if x.get("dstMac") else None
        s.mods = x.get("mods") or []
        s.udfs = x.get("udfs") or []
        s.rnd = random.Random(idx * 7919 + run)
        s.nflows = math.prod(max(1, m.get("count", 1)) for m in s.mods) if s.mods else 0
        s.flow, s.ftx, s.flab = 0, {}, {}
        s.pkts, s.offs, s.timing, s.dur = [], [], False, 0
        s.tmpl = b""
        s.enc, s.odst = x.get("encap") or {}, None  # headers added around its frames; the tunnel's next hop
        s.over = enc_over(s.enc)
        if x.get("kind") == "custom":
            try:
                s.tmpl = bytes.fromhex(x.get("hex", ""))
            except ValueError:
                s.tmpl = b""
            if len(s.tmpl) < 14:
                err[x["id"]] = "the frame needs at least its MAC addresses and EtherType (14 bytes)"
                s.count = 0
        if x.get("kind") == "pcap":
            s.pkts, s.offs = read_pcap(os.path.join(d, "pcap", x.get("pcapId", "") + ".pcap"))
            n = len(s.pkts)
            if n == 0:
                err[x["id"]] = "the capture is not on this host, or has no frames"
            s.timing = bool(x.get("pcapTiming")) and n > 1
            s.dur = s.offs[-1] + s.offs[-1] / (n - 1) if n > 1 else 0
            if not x.get("count") and not (x.get("seconds") and not s.timing):
                s.count = n * max(1, x.get("loops", 1))
            if n == 0:
                s.count = 0

    def k(s, m):  # which of its values this frame's field takes (0 .. count-1)
        n = max(1, m.get("count", 1))
        return s.rnd.randrange(n) if m.get("mode") == "random" else s.seq % n

    def frame_size(s):
        m = s.x.get("sizeMode", "")
        if m == "imix":
            return IMIX[s.seq % len(IMIX)]
        if m == "inc":
            return s.size + s.seq % (s.size_max - s.size + 1)
        if m == "random":
            return s.rnd.randint(s.size, s.size_max)
        return s.size

    def frame(s, now):
        x, tp = s.x, s.tp
        if s.pkts:  # a capture's frame, as it was (or from this test port to the one it goes to)
            f = s.pkts[s.seq % len(s.pkts)]
            if x.get("rewriteMacs"):
                f = (s.dst or f[:6]) + tp.mac + f[12:]
            return f
        k = x.get("kind", "udp")
        src_ip = ip_b(x["srcIp"]) if x.get("srcIp") else tp.ip
        dst_ip = ip_b(x.get("dstIp") or "")
        if dst_ip == b"\0\0\0\0" and x.get("to") in testers:  # an address it took by DHCP
            dst_ip = testers[x["to"]].ip
        if len(dst_ip) == 16 and len(src_ip) != 16:
            src_ip = tp.ip6 or b"\0" * 16
        src_mac, dst = tp.mac, s.dst or BC
        if s.dst is None and is_mcast(dst_ip):
            dst = mcast_mac(dst_ip)
        vlan, prio, sport, dport = x.get("vlan", 0), x.get("prio", 0), 49152 + s.idx, x.get("dport", 5001 if k != "tcp" else 80)
        flow, mult, lab = 0, 1, []
        for m in s.mods:
            kk, f = s.k(m), m.get("field")
            v = kk * m.get("step", 1)
            flow, mult = flow + kk * mult, mult * max(1, m.get("count", 1))
            if f == "srcMac":
                src_mac = bump(src_mac, v)
            elif f == "dstMac":
                dst = bump(dst, v)
            elif f == "srcIp":
                src_ip = bump(src_ip, v)
            elif f == "dstIp":
                dst_ip = bump(dst_ip, v)
                if is_mcast(dst_ip) and s.dst is None:
                    dst = mcast_mac(dst_ip)
            elif f == "vlan" and vlan:
                vlan = (vlan - 1 + v) % 4094 + 1
            elif f == "sport":
                sport = (sport + v) & 0xFFFF
            elif f == "dport":
                dport = (dport + v) & 0xFFFF
        if 0 < s.nflows <= MAXF:
            s.ftx[flow] = s.ftx.get(flow, 0) + 1
            if flow not in s.flab:
                show = {"srcMac": lambda: src_mac.hex(":"), "dstMac": lambda: dst.hex(":"), "srcIp": lambda: ip_s(src_ip), "dstIp": lambda: ip_s(dst_ip),
                        "vlan": lambda: str(vlan), "sport": lambda: str(sport), "dport": lambda: str(dport)}
                s.flab[flow] = " ".join(m.get("field", "") + " " + show.get(m.get("field"), lambda: "?")() for m in s.mods)
        else:
            flow = 0xFFFF
        sig = MAGIC + struct.pack(SIG, s.idx, s.run, s.seq, now, flow)
        # sizes count the FCS (64-1518 bytes, as testers and RFC 2544 do); QEMU's sockets carry frames without it
        size, tos = max(46 if s.over else 60, s.frame_size() - 4 - s.over), x.get("dscp", 0) << 2
        if k == "custom":  # the frame as written; the signature at its end, so the receiver counts it
            f = s.tmpl
            if x.get("rewriteMacs"):
                f = (s.dst or BC) + tp.mac + f[12:]
            if not x.get("noSig"):
                f = f + b"\0" * max(0, size - len(f) - len(sig)) + sig
            if s.udfs:
                f = udf_put(f, s.udfs, s.seq, s.rnd)
            if not x.get("noFix"):
                f = fixup(f)
            return s.finish(f, sig)
        if k == "arp":
            f = l2(BC if not s.dst else s.dst, src_mac, vlan, prio, 0x0806) + arp(1, src_mac, src_ip, b"\0" * 6, dst_ip) + sig
        elif k == "raw":
            f = l2(dst, src_mac, vlan, prio, 0x88B5) + sig
        else:
            v6 = len(dst_ip) == 16
            hl = (18 if vlan else 14) + (40 if v6 else 20) + (20 if k == "tcp" else 8)
            data = sig + b"\0" * max(0, size - hl - len(sig))
            if k == "icmp":
                p = struct.pack("!BBHHH", 128 if v6 else 8, 0, 0, 0x7477, s.seq & 0xFFFF) + data
                p = p[:2] + struct.pack("!H", l4sum(src_ip, dst_ip, 58, p) if v6 else csum(p)) + p[4:]
                proto = 58 if v6 else 1
            elif k == "tcp":
                p = struct.pack("!HHIIBBHHH", sport, dport, s.seq, 0, 5 << 4, 0x02, 65535, 0, 0) + data
                p = p[:16] + struct.pack("!H", l4sum(src_ip, dst_ip, 6, p)) + p[18:]
                proto = 6
            else:
                p = struct.pack("!HHHH", sport, dport, 8 + len(data), 0) + data
                p = p[:6] + struct.pack("!H", l4sum(src_ip, dst_ip, 17, p) or 0xFFFF) + p[8:]
                proto = 17
            if v6:
                f = l2(dst, src_mac, vlan, prio, 0x86DD) + ipv6(src_ip, dst_ip, proto, p, tos)
            else:
                f = l2(dst, src_mac, vlan, prio, 0x0800) + ipv4(src_ip, dst_ip, proto, p, tos)
        if len(f) < size and k in ("raw", "arp"):
            f += b"\0" * (size - len(f))
        if s.udfs:
            f = udf_put(f, s.udfs, s.seq, s.rnd)
            if not x.get("noFix"):
                f = fixup(f)
        return s.finish(f, sig)

    def finish(s, f, sig):  # its fault and its fuzzed bytes, when it has them; then the headers added around it
        if s.x.get("bad"):
            f = malform(f, s.x["bad"], sig)
        if s.x.get("fuzz"):
            f = fuzzed(f, s.x["fuzz"], s.seq)
        if s.enc:
            f = s.encap(f)
        return f

    def encap(s, f):  # IPv6 extension headers, MPLS labels, a GRE or VXLAN tunnel and an outer VLAN tag, in that order
        e, tp = s.enc, s.tp
        if e.get("v6ext"):
            o, et = l3_at(f)
            if et == b"\x86\xdd" and len(f) >= o + 40:
                f = v6ext(f, o, e["v6ext"])
        if e.get("mpls"):
            o, et = l3_at(f)
            n = len(e["mpls"])
            st = b"".join(struct.pack("!I", (m.get("label", 16) & 0xFFFFF) << 12 | (m.get("tc", 0) & 7) << 9 | (i == n - 1) << 8 | ((m.get("ttl") or 64) & 255))
                          for i, m in enumerate(e["mpls"]))
            f = f[:o - 2] + b"\x88\x47" + st + f[o:]
        if e.get("gre"):
            g = e["gre"]
            o, et = l3_at(f)
            key = g.get("key") or 0
            hdr = struct.pack("!H", 0x2000 if key else 0) + et + (struct.pack("!I", key & 0xFFFFFFFF) if key else b"")
            src = ip_b(g["src"]) if g.get("src") else tp.ip
            f = (s.odst or BC) + f[6:o - 2] + b"\x08\x00" + ipv4(src, ip_b(g.get("dst", "")), 47, hdr + f[o:])
        if e.get("vxlan"):
            v = e["vxlan"]
            src, dst = ip_b(v["src"]) if v.get("src") else tp.ip, ip_b(v.get("dst", ""))
            u = struct.pack("!HHHH", 49152 + s.idx, v.get("port") or 4789, 16 + len(f), 0) + struct.pack("!II", 0x08000000, (v.get("vni", 1) & 0xFFFFFF) << 8) + f
            u = u[:6] + struct.pack("!H", l4sum(src, dst, 17, u) or 0xFFFF) + u[8:]
            f = l2(s.odst or BC, tp.mac, 0, 0, 0x0800) + ipv4(src, dst, 17, u)
        if e.get("outer"):
            t = e["outer"]
            f = f[:12] + struct.pack("!HH", t.get("tpid") or 0x88A8, (t.get("prio", 0) & 7) << 13 | (t.get("vlan", 1) & 0xFFF)) + f[12:]
        return f


def enc_over(e):  # the bytes the added headers take
    n = (4 if e.get("outer") else 0) + 4 * len(e.get("mpls") or [])
    if e.get("gre"):
        n += 24 + (4 if e["gre"].get("key") else 0)
    if e.get("vxlan"):
        n += 50
    for h in e.get("v6ext") or []:
        n += 8 + (16 * len(v6segs(h)) if h.get("type") == "rt" else 0)
    return n


def v6segs(h):
    return [ip_b(a) for a in (h.get("segs") or "").replace(",", " ").split() if ":" in a]


def v6ext(f, o, hs):  # IPv6 extension headers after the IPv6 header at o (hop-by-hop first, as RFC 8200 orders them)
    order = {"hbh": 0, "dst": 1, "rt": 2, "frag": 3}
    hs = sorted(hs, key=lambda h: order.get(h.get("type"), 9))
    code = {"hbh": 0, "dst": 60, "rt": 43, "frag": 44}
    nh, body = f[o + 6], b""
    for i in range(len(hs) - 1, -1, -1):
        h, t = hs[i], hs[i].get("type")
        if t == "hbh":  # router alert, PadN
            x = struct.pack("!BB", nh, 0) + b"\x05\x02\x00\x00\x01\x00"
        elif t == "dst":  # PadN only
            x = struct.pack("!BB", nh, 0) + b"\x01\x04\x00\x00\x00\x00"
        elif t == "rt":  # a segment routing header (type 4), its segments already done (segments left 0)
            sg = v6segs(h) or [f[o + 24:o + 40]]
            x = struct.pack("!BBBBBBH", nh, 2 * len(sg), 4, 0, len(sg) - 1, 0, 0) + b"".join(reversed(sg))
        elif t == "frag":
            x = struct.pack("!BBHI", nh, 0, ((h.get("offset") or 0) & 0x1FFF) << 3 | (1 if h.get("more") else 0), 0x74770000 + o)
        else:
            continue
        body, nh = x + body, code[t]
    pl = len(body) + len(f) - o - 40
    return f[:o + 4] + struct.pack("!HB", pl, nh) + f[o + 7:o + 40] + body + f[o + 40:]


cfg_m, cfg = 0, {}
testers, streams, run = {}, [], 0
tx, rx, other, lat, err, ooo, dup, jit, seen = {}, {}, {}, {}, {}, {}, {}, {}, {}
hist, frx, qos = {}, {}, {}  # latency histogram, per-flow receive counters, what the frames arrived with
ftx, flab = {}, {}  # per flow: frames sent, its field values
fsent, fgot = {}, {}  # per stream: its first frame sent, and the first that arrived where it goes (hex, for the report)
ports = {}  # per test port: its counters (see port)
last_st = 0


SAMPLE = 160  # bytes of a stream's first frame kept for the report (with its length: "508:0004…")


def reset():
    global tx, rx, other, lat, err, ooo, dup, jit, seen, hist, frx, qos, ports
    tx, rx, other, lat, err, ooo, dup, jit, seen = {}, {}, {}, {}, {}, {}, {}, {}, {}
    hist, frx, qos = {}, {}, {}
    ports = {}
    ftx.clear()
    flab.clear()
    fsent.clear()
    fgot.clear()


def load():
    global cfg, testers, streams, run
    try:
        c = json.load(open(cfg_path))
    except Exception:
        return
    want = {t["id"]: t for t in c.get("testers", [])}
    for tid in list(testers):
        if tid not in want or want[tid]["listen"] != testers[tid].listen:
            testers[tid].sock.close()
            del testers[tid]
    for tid, t in want.items():
        if tid not in testers:
            try:
                testers[tid] = Tester(t)
            except OSError as e:
                err["port " + tid] = str(e)
        else:
            testers[tid].set(t)
    cfg = c
    lagof.clear()
    lagof.update({t["id"]: t.get("lag", "") for t in c.get("testers", []) if t.get("lag")})
    phash.clear()
    for x in c.get("streams", []):
        if x.get("kind") == "pcap" and x.get("on") and x.get("pcapId"):
            for f in read_pcap(os.path.join(d, "pcap", x["pcapId"] + ".pcap"))[0]:
                phash[l3key(f)] = x
    if not c.get("run"):  # stopped: no more sending, the counts stay
        streams = []
    elif c["run"] != run:
        run = c["run"]
        reset()
        streams = [Stream(x, i, testers, run) for i, x in enumerate(c.get("streams", [])) if x.get("on") and x["from"] in testers]


def latency(sid, flow, ts, st):
    """A frame's latency (ms, from its send time): average, min, max, its histogram bucket (a quarter of an
    octave of microseconds, for the percentiles), jitter, and per flow."""
    dl = (time.time() - ts) * 1000
    l = lat.setdefault(sid, [0.0, 0.0, 0, 1e9])
    l[0] += dl
    l[1] = max(l[1], dl)
    l[2] += 1
    l[3] = min(l[3], dl)
    b = str(min(120, max(0, int(4 * math.log2(max(dl * 1000, 1))))))
    h = hist.setdefault(sid, {})
    h[b] = h.get(b, 0) + 1
    if flow != 0xFFFF:
        fr = frx.setdefault(sid, {}).setdefault(flow, [0, 0.0, 0.0, 0])
        fr[1] += dl
        fr[2] = max(fr[2], dl)
        fr[3] += 1
    if st[2] is not None:
        j = jit.setdefault(sid, [0.0, 0])
        j[0] += abs(dl - st[2])
        j[1] += 1
    st[2] = dl


def port(tid):
    """A test port's counters: frames sent, received, bytes sent, received (with the FCS), its streams' frames
    received (by stream), latency (sum, count, max), errors (send failures, out of order, duplicates)."""
    c = ports.get(tid)
    if c is None:
        c = ports[tid] = [0, 0, 0, 0, {}, [0.0, 0, 0.0], 0]
    return c


def got(sid, tp, seq, ts, flow=0xFFFF, mark=""):
    """A frame of a stream arrived at a test port: counts, order, latency, jitter, per flow, how it was marked."""
    rx[sid] = rx.get(sid, 0) + 1
    k = (sid, tp.id)
    pc = port(tp.id)
    st = seen.get(k)
    if st is None:
        st = seen[k] = [-1, set(), None]  # highest sequence, recent ones, last latency
    hi, recent = st[0], st[1]
    if seq in recent:
        dup[sid] = dup.get(sid, 0) + 1
        rx[sid] -= 1
        pc[6] += 1
        return
    pc[4][sid] = pc[4].get(sid, 0) + 1
    recent.add(seq)
    if len(recent) > 8192:
        st[1] = recent = {q for q in recent if q > hi - 4096}
    if seq < hi:
        ooo[sid] = ooo.get(sid, 0) + 1
        pc[6] += 1
    else:
        st[0] = seq
    if mark:
        q = qos.setdefault(sid, {})
        q[mark] = q.get(mark, 0) + 1
    if flow != 0xFFFF:
        frx.setdefault(sid, {}).setdefault(flow, [0, 0.0, 0.0, 0])[0] += 1
    if ts > 0:
        latency(sid, flow, ts, st)
        pl = pc[5]
        pl[0] += st[2]
        pl[1] += 1
        pl[2] = max(pl[2], st[2])


PCAPS = {}  # path -> (frames, offsets in seconds from the first)


def read_pcap(path):
    """The frames of a classic pcap file and when each was captured (seconds after the first)."""
    if path in PCAPS:
        return PCAPS[path]
    frames, offs = [], []
    try:
        b = open(path, "rb").read()
    except OSError:
        b = b""
    if len(b) >= 24:
        m = b[:4]
        bo = "<" if m in (b"\xd4\xc3\xb2\xa1", b"\x4d\x3c\xb2\xa1") else ">"
        div = 1e9 if m in (b"\x4d\x3c\xb2\xa1", b"\xa1\xb2\x3c\x4d") else 1e6
        i, t0 = 24, None
        while i + 16 <= len(b):
            sec, sub, incl, _ = struct.unpack(bo + "IIII", b[i:i + 16])
            data = b[i + 16:i + 16 + incl]
            i += 16 + incl
            if len(data) < 14:
                continue
            t = sec + sub / div
            t0 = t if t0 is None else t0
            frames.append(data + b"\0" * max(0, 60 - len(data)))
            offs.append(max(0.0, t - t0))
    PCAPS[path] = (frames, offs)
    return frames, offs


def l3key(f):
    """A frame's contents from its EtherType on (VLAN tags left out, padding too): how a replayed frame is known."""
    o = 12
    while f[o:o + 2] in (b"\x81\x00", b"\x88\xa8", b"\x91\x00"):
        o += 4
    return hash(f[o:].rstrip(b"\0"))


phash = {}  # pcap streams' frames: l3key -> stream
lagof = {}  # test port -> its LAG's name (a stream to one member is received on any)


def reflect(tp, f, o, et):
    """A round-trip stream's frame goes back where it came from: MACs, addresses and ports swapped (the
    checksums stay right: their sums do not change)."""
    b = bytearray(f[6:12] + tp.mac + f[12:])
    i = o + 2
    if et == 0x0800 and len(b) >= i + 20:
        ihl = (b[i] & 15) * 4
        b[i + 12:i + 16], b[i + 16:i + 20] = b[i + 16:i + 20], b[i + 12:i + 16]
        l4, proto = i + ihl, b[i + 9]
    elif et == 0x86DD and len(b) >= i + 40:
        b[i + 8:i + 24], b[i + 24:i + 40] = b[i + 24:i + 40], b[i + 8:i + 24]
        l4, proto = i + 40, b[i + 6]
    else:
        l4, proto = 0, 0
    if proto in (6, 17) and len(b) >= l4 + 4:
        b[l4:l4 + 2], b[l4 + 2:l4 + 4] = b[l4 + 2:l4 + 4], b[l4:l4 + 2]
    tp.send(bytes(b))


def handle(tp, f):
    o = 12
    vlan = prio = 0
    if f[o:o + 2] == b"\x81\x00":
        tci = struct.unpack("!H", f[o + 2:o + 4])[0]
        vlan, prio = tci & 0xFFF, tci >> 13
        o += 4
    et = struct.unpack("!H", f[o:o + 2])[0]
    b = f[o + 2:]
    if et < 0x0600:  # 802.3 length: LLC - spanning tree
        if b[:3] == b"\x42\x42\x03":
            tp.bpdu_got(b)
        return
    if tp.clients:
        c = tp.cmac.get(f[0:6])
        if et == 0x888E:
            if c is not None:
                c.eap_got(b, time.time())
            elif f[0:6] == PAE:  # to the PAE group address: the clients still logging in
                for c in tp.clients:
                    if c.x_state in ("started", "identity", "challenge", "tls", "inner"):
                        c.eap_got(b, time.time())
            return
        if et == 0x0800 and len(b) >= 28 + 240 and b[9] == 17:
            ihl = (b[0] & 15) * 4
            u = b[ihl:]
            if struct.unpack("!H", u[2:4])[0] == 68 and len(u) >= 8 + 240:
                bp = u[8:]
                cc = tp.cmac.get(bp[28:34])
                if cc is not None and bp[0] == 2 and struct.unpack("!I", bp[4:8])[0] == cc.xid:
                    o2, i2 = {}, 240
                    while i2 < len(bp) and bp[i2] != 255:
                        if bp[i2] == 0:
                            i2 += 1
                            continue
                        if i2 + 1 >= len(bp):
                            break
                        o2[bp[i2]] = bp[i2 + 2:i2 + 2 + bp[i2 + 1]]
                        i2 += 2 + bp[i2 + 1]
                    cc.dhcp_got(o2.get(53, b"\0")[0], bp[16:20], o2, time.time())
                    return
        if et == 0x86DD and len(b) >= 48:
            if b[6] == 17 and b[42:44] == b"\x02\x22":  # DHCPv6 to a client
                cc = tp.cmac.get(f[0:6])
                if cc is not None:
                    cc.d6_got(b[48:], time.time())
                    return
            elif b[6] == 58 and len(b) >= 64 and b[40] == 135:  # a neighbour solicitation for a client's address
                cc = tp.c6.get(b[48:64])
                if cc is not None:
                    cc.na(b, f, vlan)
                    return
        if et == 0x0806 and len(b) >= 28:
            op, smac, sip, tip = struct.unpack("!6xH6s4s6x4s", b[:28])
            cc = tp.cip.get(tip)
            if op == 1 and cc is not None:
                cc.send(smac, 0x0806, arp(2, cc.mac, cc.ip, smac, sip))
    if et == 0x8809:  # slow protocols: LACP
        if tp.lag:
            tp.lacp_got(b, time.time())
        return
    if et == 0x88CC:
        tp.lldp_got(b)
        return
    if tp.vrrp and (et == 0x0800 and len(b) >= 28 and b[9] == 112 or et == 0x86DD and len(b) >= 48 and b[6] == 112):
        tp.vr_got(b, et == 0x86DD, time.time())
        return
    if tp.bfd and et == 0x0800 and len(b) >= 52 and b[9] == 17 and b[(b[0] & 15) * 4 + 2:(b[0] & 15) * 4 + 4] == b"\x0e\xc8":
        tp.bfd_got(b, time.time())
        return
    i = f.find(MAGIC)
    if i >= 0 and len(f) >= i + 4 + SIGN:
        idx, r, seq, ts, flow = struct.unpack(SIG, f[i + 4:i + 4 + SIGN])
        if r == run and idx < len(cfg.get("streams", [])):
            x = cfg["streams"][idx]
            sid = x["id"]
            echo = x["from"] == tp.id and not x.get("to") and x.get("kind") == "icmp"  # the address answered
            if x.get("rtt") and x["from"] == tp.id and x.get("to"):  # its own frame, sent back: the round trip
                k = (sid, "rtt")
                latency(sid, flow, ts, seen.setdefault(k, [-1, set(), None]))
            elif x.get("to") and x["to"] != tp.id and not (tp.lag and x["to"] in lagof and lagof[x["to"]] == tp.lag):
                other[sid] = other.get(sid, 0) + 1
            elif x["from"] != tp.id or echo:
                dst = ip_b(x.get("dstIp") or "")
                if is_mcast(dst) and not x.get("mods") and dst not in tp.groups:
                    other[sid] = other.get(sid, 0) + 1  # flooded to a port that did not join
                else:
                    dscp = b[1] >> 2 if et == 0x0800 and len(b) > 1 else ((b[0] & 15) << 4 | b[1] >> 4) >> 2 if et == 0x86DD and len(b) > 1 else -1
                    rtt = x.get("rtt") and (x.get("to") == tp.id or tp.lag and lagof.get(x.get("to")) == tp.lag)
                    if sid not in fgot:
                        fgot[sid] = "%d:%s" % (len(f), f[:SAMPLE].hex())
                    got(sid, tp, seq, ts if x["from"] in testers and not rtt else 0, flow, "%d/%d/%d" % (vlan, prio, dscp))
                    if rtt:
                        reflect(tp, f, o, et)
        return
    if phash:
        x = phash.get(l3key(f))
        if x is not None and x["from"] != tp.id:
            sid = x["id"]
            if x.get("to") and x["to"] != tp.id:
                other[sid] = other.get(sid, 0) + 1
            else:
                rx[sid] = rx.get(sid, 0) + 1
                pc = port(tp.id)[4]
                pc[sid] = pc.get(sid, 0) + 1
                dscp = b[1] >> 2 if et == 0x0800 and len(b) > 1 else ((b[0] & 15) << 4 | b[1] >> 4) >> 2 if et == 0x86DD and len(b) > 1 else -1
                q = qos.setdefault(sid, {})
                mk = "%d/%d/%d" % (vlan, prio, dscp)
                q[mk] = q.get(mk, 0) + 1
            return
    if et == 0x0806 and len(b) >= 28:
        op, smac, sip, tip = struct.unpack("!6xH6s4s6x4s", b[:28])
        if sip != b"\0\0\0\0":
            tp.arp[sip] = smac
        if op == 1 and tip == tp.ip and tp.ip != b"\0\0\0\0":
            tp.send(l2(smac, tp.mac, vlan, 0, 0x0806) + arp(2, tp.mac, tp.ip, smac, sip))
        elif op == 1 and tp.vrrp and tp.vr_state == "master" and tip == tp.vr_vip:  # the virtual address, while it is master
            tp.send(l2(smac, tp.vr_vmac, vlan, 0, 0x0806) + arp(2, tp.vr_vmac, tip, smac, sip))
    elif et == 0x0800 and len(b) >= 28 and b[9] == 1 and b[16:20] == tp.ip and b[20] == 8:
        ihl = (b[0] & 15) * 4
        icmp = bytearray(b[ihl:struct.unpack("!H", b[2:4])[0]])
        icmp[0] = 0
        icmp[2:4] = b"\0\0"
        icmp[2:4] = struct.pack("!H", csum(bytes(icmp)))
        tp.send(l2(f[6:12], tp.mac, vlan, 0, 0x0800) + ipv4(tp.ip, b[12:16], 1, bytes(icmp)))
    elif et == 0x0800 and len(b) >= 28 + 240 and b[9] == 17 and tp.dhcp:  # a DHCP reply for it?
        ihl = (b[0] & 15) * 4
        u = b[ihl:]
        if struct.unpack("!H", u[2:4])[0] == 68 and len(u) >= 8 + 240:
            bp = u[8:]
            op, xid, yi = bp[0], struct.unpack("!I", bp[4:8])[0], bp[16:20]
            if op == 2 and xid == tp.xid and bp[236:240] == b"\x63\x82\x53\x63":
                o, i2 = {}, 240
                while i2 < len(bp) and bp[i2] != 255:
                    if bp[i2] == 0:
                        i2 += 1
                        continue
                    if i2 + 1 >= len(bp):
                        break
                    o[bp[i2]] = bp[i2 + 2:i2 + 2 + bp[i2 + 1]]
                    i2 += 2 + bp[i2 + 1]
                kind = o.get(53, b"\0")[0]
                if kind == 2 and tp.dhcp_state == "discover":  # OFFER: ask for it
                    tp.offer = (yi, o.get(54, b"\0" * 4))
                    tp.dhcp_state, tp.dhcp_at = "request", time.time()
                    tp.dhcp_send(3)
                elif kind == 5:  # ACK: it is ours
                    mask = o.get(1, b"\xff\xff\xff\0")
                    tp.ip, tp.dhcp_state = yi, "bound"
                    if o.get(3):
                        tp.gw = o[3][:4]
                    lt = struct.unpack("!I", o[51])[0] if len(o.get(51, b"")) == 4 else 3600
                    tp.lease_end = time.time() + max(30, lt // 2)
                    tp.lease = {"ip": socket.inet_ntoa(yi) + "/" + str(prefix_len(mask)), "gw": socket.inet_ntoa(tp.gw) if tp.gw else "", "server": socket.inet_ntoa(o.get(54, b"\0" * 4))}
                    tp.garp_at = 0
                elif kind == 6:  # NAK: start again
                    tp.dhcp_state, tp.dhcp_at, tp.offer = "", 0, None
    elif et == 0x0800 and len(b) >= 24 and b[9] == 2:  # an IGMP query: report the groups again
        ihl = (b[0] & 15) * 4
        if len(b) > ihl and b[ihl] == 0x11:
            tp.last_join = 0
            for c in tp.clients:
                c.join_at = 0
    elif et == 0x86DD and len(b) >= 48 and b[6] == 58 and (tp.ip6 or tp.slaac or tp.vrrp):
        t, src, dst = b[40], b[8:24], b[24:40]
        if t == 134 and tp.slaac and not tp.ip6:  # a router advertisement: an address from its prefix
            i2 = 56
            while i2 + 8 <= len(b):
                ot, ol = b[i2], b[i2 + 1] * 8
                if ol == 0:
                    break
                if ot == 3 and ol == 32 and b[i2 + 3] & 0x40 and b[i2 + 2] == 64:  # prefix information, autonomous, /64
                    tp.ip6 = b[i2 + 16:i2 + 24] + eui64(tp.mac)
                    tp.lease6 = socket.inet_ntop(socket.AF_INET6, tp.ip6) + "/64"
                    tp.gw6 = socket.inet_ntop(socket.AF_INET6, src)
                i2 += ol
        elif t == 135 and len(b) >= 64 and tp.vrrp and tp.vr_v6 and tp.vr_state == "master" and b[48:64] == tp.vr_vip:  # the virtual address
            p = struct.pack("!BBHI16s", 136, 0, 0, 0x60000000, tp.vr_vip) + b"\x02\x01" + tp.vr_vmac
            p = p[:2] + struct.pack("!H", l4sum(tp.vr_vip, src, 58, p)) + p[4:]
            tp.send(l2(f[6:12], tp.vr_vmac, vlan, 0, 0x86DD) + ipv6(tp.vr_vip, src, 58, p, 0, 255))
        elif t == 135 and len(b) >= 64 and (b[48:64] == tp.ip6 or b[48:64] == tp.ll):  # neighbour solicitation for its address: advertise
            me = b[48:64]
            p = struct.pack("!BBHI16s", 136, 0, 0, 0x60000000, me) + b"\x02\x01" + tp.mac
            p = p[:2] + struct.pack("!H", l4sum(me, src, 58, p)) + p[4:]
            tp.send(l2(f[6:12], tp.mac, vlan, 0, 0x86DD) + ipv6(me, src, 58, p, 0, 255))
        elif t == 128 and dst == tp.ip6:  # echo request: reply
            p = bytearray(b[40:])
            p[0], p[2:4] = 129, b"\0\0"
            p[2:4] = struct.pack("!H", l4sum(tp.ip6, src, 58, bytes(p)))
            tp.send(l2(f[6:12], tp.mac, vlan, 0, 0x86DD) + ipv6(tp.ip6, src, 58, bytes(p)))
        elif t == 130:  # MLD query
            tp.last_join = 0


while True:
    try:
        m = os.stat(cfg_path).st_mtime
    except OSError:
        break
    if m != cfg_m:
        cfg_m = m
        load()
    if not cfg.get("testers"):
        break
    now = time.time()
    for t in testers.values():
        t.tick(now)
        if t.groups and now - t.last_join > 10:
            t.last_join = now
            t.join()
    socks = [t.sock for t in testers.values()]
    busy = any(s.tp is not None and s.seq < s.count for s in streams)
    r, _, _ = select.select(socks, [], [], 0.001 if busy else 0.05) if socks else ([], [], [])
    for t in testers.values():
        if t.sock in r:
            for _ in range(4000):
                try:
                    f = t.sock.recv(65536)
                except BlockingIOError:
                    break
                except OSError:
                    break
                if len(f) >= 14:
                    c = port(t.id)
                    c[1] += 1
                    c[3] += len(f) + 4
                    try:
                        handle(t, f)
                    except Exception:
                        pass
    # at most 2,000 frames a round over all streams, so the frames coming back are read before the
    # socket's buffer overflows (4,000 per stream lost a quarter of the frames of two streams at 50,000 pps)
    live = sum(1 for s in streams if s.tp is not None and s.seq < s.count)
    for s in streams:
        if s.tp is None or s.seq >= s.count:
            continue
        x = s.x
        if s.dst is None and x.get("routed") and x.get("kind") not in ("arp", "pcap"):
            gw = s.tp.gw
            mac = s.tp.arp.get(gw) if gw else None
            if not mac:
                if now - s.tp.last_arp > 1 and gw:
                    s.tp.last_arp = now
                    s.tp.ask(gw)
                s.gw_wait = s.gw_wait or now
                if now - s.gw_wait > 6:
                    err[x["id"]] = "no ARP reply from the gateway " + x.get("gwIp", "")
                    s.seq = s.count
                continue
            s.dst = mac
        elif s.dst is None and x.get("toMac"):
            s.dst = mac_b(x["toMac"])
        if s.odst is None and x.get("tunArp"):  # a tunnel: its frames go to the tunnel end (or the gateway to it)
            nh = ip_b(x["tunArp"])
            mac = s.tp.arp.get(nh)
            if not mac:
                if now - s.tp.last_arp > 1:
                    s.tp.last_arp = now
                    s.tp.ask(nh)
                s.gw_wait = s.gw_wait or now
                if now - s.gw_wait > 6:
                    err[x["id"]] = "no ARP reply from the tunnel's next hop " + x["tunArp"]
                    s.seq = s.count
                continue
            s.odst = mac
        if s.start is None:
            s.start = now
        if s.timing:  # the capture's own gaps, over and over
            el = now - s.start
            lp = int(el // s.dur)
            due = min(s.count, lp * len(s.pkts) + bisect.bisect_right(s.offs, el - lp * s.dur))
        else:
            due = min(s.count, int((now - s.start) * s.pps) + 1)
        if s.burst > 1:  # whole bursts, as often as the rate allows
            due = min(s.count, ((due + s.burst - 1) // s.burst) * s.burst)
        n = 0
        while s.seq < due and n < max(64, 2000 // max(1, live)):
            fr = s.frame(time.time())
            if x["id"] not in fsent:
                fsent[x["id"]] = "%d:%s" % (len(fr), fr[:SAMPLE].hex())
            s.tp.send(fr, x.get("bad") == "runt")
            s.seq += 1
            n += 1
        tx[x["id"]] = s.seq
        if s.ftx:
            ftx[x["id"]], flab[x["id"]] = s.ftx, s.flab
    if now - last_st > 0.5:
        last_st = now
        st = {"run": run, "at": now, "tx": tx, "rx": rx, "other": other, "lat": lat, "err": err, "ooo": ooo, "dup": dup, "jit": jit,
              "busy": {s.x["id"]: s.seq < s.count for s in streams}, "testers": sorted(testers),
              "hist": hist, "frx": frx, "qos": qos, "ftx": ftx, "flab": flab, "fsent": fsent, "fgot": fgot,
              "ports": {k: {"tx": c[0], "rx": c[1], "txb": c[2], "rxb": c[3], "srx": c[4], "lat": c[5], "err": c[6]} for k, c in ports.items()},
              "lacp": {t.id: t.lacp_view(now) for t in testers.values() if t.lag},
              "vrrp": {t.id: t.vr_view(now) for t in testers.values() if t.vrrp}, "bfd": {t.id: t.bfd_view(now) for t in testers.values() if t.bfd},
              "clients": {t.id: {"n": len(t.clients), "bound": sum(c.dhcp_state == "bound" for c in t.clients), "bound6": sum(c.d6_state == "bound" for c in t.clients), "auth": sum(c.x_state == "authenticated" for c in t.clients),
                                 "failed": sum(c.x_state == "failed" for c in t.clients), "list": [c.view() for c in t.clients[:256]]} for t in testers.values() if t.clients},
              "stp": {t.id: dict(t.stp_peer or {}, sent=t.stp_tx, got=t.stp_rx) for t in testers.values() if t.stp.get("mode")}, "lldp": {t.id: t.neighbour for t in testers.values() if t.neighbour},
              "lease": {t.id: t.lease for t in testers.values() if t.lease}, "lease6": {t.id: {"ip": t.lease6, "gw": t.gw6} for t in testers.values() if getattr(t, "lease6", None)}}
        try:
            with open(st_path + ".tmp", "w") as fh:
                json.dump(st, fh)
            os.replace(st_path + ".tmp", st_path)
        except OSError:
            pass
