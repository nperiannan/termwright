package snmp

import (
	"context"
	"encoding/hex"
	"net"
	"strings"
	"testing"
	"time"
)

// RFC 3414 A.3.1 / A.3.2: password "maplesyrup", engine ID 00..02.
func TestKeyLocalization(t *testing.T) {
	eid, _ := hex.DecodeString("000000000000000000000002")
	for _, c := range []struct {
		a    Auth
		want string
	}{{AuthMD5, "526f5eed9fcce26f8964c2930787d82b"}, {AuthSHA, "6695febc9288e36282235fc7151f128497b38f3f"}} {
		ku, err := PasswordKey(c.a, "maplesyrup")
		if err != nil {
			t.Fatal(err)
		}
		k, _ := LocalKey(c.a, ku, eid)
		if hex.EncodeToString(k) != c.want {
			t.Fatalf("%s: %x", c.a, k)
		}
	}
}

func TestOIDCodec(t *testing.T) {
	for _, o := range []string{"1.3.6.1.2.1.1.1.0", "1.3.6.1.4.1.1916.1.1.1.1.4294967295", "2.999.3", "0.0"} {
		b, err := encOID(o)
		if err != nil {
			t.Fatal(o, err)
		}
		_, v, _, _ := decTLV(b)
		if got := decOID(v); got != o {
			t.Fatalf("%s -> %s", o, got)
		}
	}
	for _, n := range []int64{0, 1, 127, 128, -1, -128, -129, 2147483647, -2147483648} {
		_, v, _, _ := decTLV(encInt(tagInteger, n))
		if decInt(v) != n {
			t.Fatalf("int %d", n)
		}
	}
}

func agent(t *testing.T) (*Agent, string) {
	a := NewAgent("public", []byte{0x80, 0, 0x07, 0x7c, 3, 1, 2, 3, 4, 5, 6})
	a.Put(Varbind{OID: "1.3.6.1.2.1.1.1.0", Type: OctetString, Bytes: []byte("Extreme Networks Switch Engine")})
	a.Put(Varbind{OID: "1.3.6.1.2.1.1.3.0", Type: TimeTicks, Uint: 123456})
	a.Put(Varbind{OID: "1.3.6.1.2.1.1.5.0", Type: OctetString, Bytes: []byte("sw1")})
	for i := 1; i <= 60; i++ {
		a.Put(Varbind{OID: "1.3.6.1.2.1.2.2.1.8." + itoa(i), Type: Integer, Int: int64(1 + i%2)})
		a.Put(Varbind{OID: "1.3.6.1.2.1.2.2.1.10." + itoa(i), Type: Counter32, Uint: uint64(i * 1000)})
	}
	a.Put(Varbind{OID: "1.3.6.1.2.1.2.2.1.6.1", Type: OctetString, Bytes: []byte{0x40, 0xe3, 0x17, 0xcb, 0xb4, 0x01}})
	a.Put(Varbind{OID: "1.3.6.1.2.1.4.20.1.1.10.0.0.1", Type: IPAddress, Value: "10.0.0.1"})
	a.Put(Varbind{OID: "1.3.6.1.2.1.31.1.1.1.6.1", Type: Counter64, Uint: 1 << 40})
	addr, err := a.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a, addr
}

func itoa(i int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+i/10)) + string(rune('0'+i%10)))
}

func check(t *testing.T, cfg Config) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Dial(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	vs, err := c.Get(ctx, "1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0", "1.3.6.1.2.1.1.9.0")
	if err != nil {
		t.Fatal(err)
	}
	if vs[0].Value != "Extreme Networks Switch Engine" || vs[1].Uint != 123456 || vs[2].Type != NoSuchInst {
		t.Fatalf("get: %+v", vs)
	}
	w, err := c.Walk(ctx, "1.3.6.1.2.1.2.2", 25, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(w) != 121 || w[0].OID != "1.3.6.1.2.1.2.2.1.6.1" || w[0].Value != "40:e3:17:cb:b4:01" {
		t.Fatalf("walk: %d %+v", len(w), w[0])
	}
	n, err := c.GetNext(ctx, "1.3.6.1.2.1.31")
	if err != nil || n[0].Type != Counter64 || n[0].Uint != 1<<40 {
		t.Fatalf("getnext: %+v %v", n, err)
	}
	if _, err := c.Set(ctx, Varbind{OID: "1.3.6.1.2.1.1.5.0", Type: OctetString, Bytes: []byte("x")}); err == nil || !strings.Contains(err.Error(), "notWritable") {
		t.Fatalf("set on a read-only agent: %v", err)
	}
}

func TestV2c(t *testing.T) {
	_, addr := agent(t)
	check(t, Config{Address: addr, Version: V2c, Community: "public"})
}

func TestV1(t *testing.T) {
	_, addr := agent(t)
	check(t, Config{Address: addr, Version: V1, Community: "public"})
}

func TestV3(t *testing.T) {
	a, addr := agent(t)
	cases := []struct {
		user string
		auth Auth
		priv Priv
	}{{"noauth", AuthNone, PrivNone}, {"md5des", AuthMD5, PrivDES}, {"shaaes", AuthSHA, PrivAES}, {"sha256aes256", AuthSHA256, PrivAES256}, {"sha512aes192", AuthSHA512, PrivAES192}, {"sha224", AuthSHA224, PrivNone}}
	for _, c := range cases {
		if err := a.AddUser(c.user, c.auth, "authpass123", c.priv, "privpass456"); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range cases {
		t.Run(c.user, func(t *testing.T) {
			check(t, Config{Address: addr, Version: V3, User: c.user, Auth: c.auth, AuthPass: "authpass123", Priv: c.priv, PrivPass: "privpass456"})
		})
	}
	ctx := context.Background()
	for _, bad := range []struct {
		cfg  Config
		want string
	}{
		{Config{Address: addr, Version: V3, User: "shaaes", Auth: AuthSHA, AuthPass: "wrongpass1", Priv: PrivAES, PrivPass: "privpass456"}, "wrong authentication"},
		{Config{Address: addr, Version: V3, User: "nobody", Auth: AuthSHA, AuthPass: "authpass123"}, "unknown user"},
		{Config{Address: addr, Version: V3, User: "shaaes", Auth: AuthSHA, AuthPass: "authpass123", Priv: PrivAES, PrivPass: "wrongpriv1"}, "decryption"},
	} {
		c, err := Dial(ctx, bad.cfg)
		if err == nil {
			_, err = c.Get(ctx, "1.3.6.1.2.1.1.1.0")
			c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Fatalf("%s: want %q, got %v", bad.cfg.User, bad.want, err)
		}
	}
}

func TestSetWritable(t *testing.T) {
	a, addr := agent(t)
	a.SetWritable(true)
	ctx := context.Background()
	c, err := Dial(ctx, Config{Address: addr, Version: V2c, Community: "public"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Set(ctx, Varbind{OID: "1.3.6.1.2.1.1.5.0", Type: OctetString, Bytes: []byte("core-1")}); err != nil {
		t.Fatal(err)
	}
	v, _ := c.Get(ctx, "1.3.6.1.2.1.1.5.0")
	if v[0].Value != "core-1" {
		t.Fatalf("%+v", v)
	}
}

func TestNoAgent(t *testing.T) {
	ctx := context.Background()
	c, err := Dial(ctx, Config{Address: "127.0.0.1:9", Version: V2c, Community: "public", Timeout: 300 * time.Millisecond, Retries: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Get(ctx, "1.3.6.1.2.1.1.1.0"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestTraps(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	addr := pc.LocalAddr().String()
	if err := SendTrap(Config{Address: addr, Version: V2c, Community: "traps"}, "1.3.6.1.6.3.1.1.5.3", 4242, []Varbind{{OID: "1.3.6.1.2.1.2.2.1.1.7", Type: Integer, Int: 7}}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 65535)
	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, from, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	tr, reply, err := DecodeTrap(buf[:n], from.String(), nil)
	if err != nil || reply != nil || tr.TrapOID != "1.3.6.1.6.3.1.1.5.3" || tr.Uptime != 4242 || tr.Community != "traps" || len(tr.Varbinds) != 1 || tr.Version != "2c" {
		t.Fatalf("%+v %v", tr, err)
	}
	if err := SendTrap(Config{Address: addr, Version: V1, Community: "traps"}, "1.3.6.1.4.1.1916.4.1.0.5", 99, nil); err != nil {
		t.Fatal(err)
	}
	n, from, _ = pc.ReadFrom(buf)
	tr, _, err = DecodeTrap(buf[:n], from.String(), nil)
	if err != nil || tr.TrapOID != "1.3.6.1.4.1.1916.4.1.0.5" || tr.Version != "1" {
		t.Fatalf("v1: %+v %v", tr, err)
	}
	// v3 authPriv, sent with Termwright's engine ID
	if err := SendTrap(Config{Address: addr, Version: V3, User: "trapper", Auth: AuthSHA256, AuthPass: "authpass1", Priv: PrivAES, PrivPass: "privpass1"}, "1.3.6.1.6.3.1.1.5.4", 500, nil); err != nil {
		t.Fatal(err)
	}
	n, from, _ = pc.ReadFrom(buf)
	tr, _, err = DecodeTrap(buf[:n], from.String(), []TrapUser{{User: "trapper", Auth: AuthSHA256, AuthPass: "authpass1", Priv: PrivAES, PrivPass: "privpass1"}})
	if err != nil || tr.TrapOID != "1.3.6.1.6.3.1.1.5.4" || tr.Version != "3" || tr.User != "trapper" {
		t.Fatalf("v3: %+v %v", tr, err)
	}
	// an inform gets its answer
	b, _ := encodeCommunity(V2c, "traps", PDU{Type: PDUInform, RequestID: 77, Varbinds: []Varbind{{OID: "1.3.6.1.6.3.1.1.4.1.0", Type: ObjectID, Value: "1.3.6.1.6.3.1.1.5.4"}}})
	tr, reply, err = DecodeTrap(b, "x", nil)
	if err != nil || tr.Kind != "inform" || reply == nil {
		t.Fatalf("inform: %+v %v", tr, err)
	}
	if m, _ := decode(reply, nil); m.PDU.Type != PDUResponse || m.PDU.RequestID != 77 {
		t.Fatalf("inform reply: %+v", m.PDU)
	}
}
