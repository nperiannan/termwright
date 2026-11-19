package discover

import (
	"context"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func oidLess(a, b string) bool {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(x) && i < len(y); i++ {
		p, _ := strconv.Atoi(x[i])
		q, _ := strconv.Atoi(y[i])
		if p != q {
			return p < q
		}
	}
	return len(x) < len(y)
}

// fakeBulkAgent answers GetBulk from a table and counts how many requests it got.
func fakeBulkAgent(t *testing.T, table map[string]any, community string) (addr string, requests *atomic.Int32) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no UDP:", err)
	}
	t.Cleanup(func() { pc.Close() })
	var oids []string
	for o := range table {
		oids = append(oids, o)
	}
	sort.Slice(oids, func(i, j int) bool { return oidLess(oids[i], oids[j]) })
	n := new(atomic.Int32)
	go func() {
		buf := make([]byte, 4096)
		for {
			l, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			n.Add(1)
			_, msg, _, _ := decTLV(buf[:l])
			_, _, msg, _ = decTLV(msg)
			_, com, msg, _ := decTLV(msg)
			if string(com) != community {
				continue // wrong community: no answer, like a real agent
			}
			tag, pdu, _, _ := decTLV(msg)
			if tag != 0xA5 {
				continue
			}
			_, id, pdu, _ := decTLV(pdu)
			_, _, pdu, _ = decTLV(pdu) // non-repeaters
			_, rep, pdu, _ := decTLV(pdu)
			_, list, _, _ := decTLV(pdu)
			_, vb, _, _ := decTLV(list)
			_, o, _, _ := decTLV(vb)
			from0 := decOID(o)
			var vbs []byte
			count := 0
			for _, k := range oids {
				if !oidLess(from0, k) || count >= int(decInt(rep)) {
					continue
				}
				e, _ := encOID(k)
				var val []byte
				switch v := table[k].(type) {
				case string:
					val = tlv(0x04, []byte(v))
				case []byte:
					val = tlv(0x04, v)
				case int:
					val = encInt(int64(v))
				}
				vbs = append(vbs, tlv(0x30, append(e, val...))...)
				count++
			}
			if count == 0 { // endOfMibView
				e, _ := encOID(from0)
				vbs = tlv(0x30, append(e, 0x82, 0x00))
			}
			rp := append(append(append(tlv(0x02, id), encInt(0)...), encInt(0)...), tlv(0x30, vbs)...)
			resp := tlv(0x30, append(append(encInt(1), tlv(0x04, com)...), tlv(0xA2, rp)...))
			_, _ = pc.WriteTo(resp, from)
		}
	}()
	return pc.LocalAddr().String(), n
}

func TestSNMPWalk(t *testing.T) {
	table := map[string]any{
		"1.3.6.1.2.1.31.1.1.1.1.1": "1:1", "1.3.6.1.2.1.31.1.1.1.1.2": "1:2",
		"1.3.6.1.2.1.2.2.1.8.1": 1, "1.3.6.1.2.1.2.2.1.8.2": 2,
		"1.0.8802.1.1.2.1.4.1.1.5.0.3.1": []byte{0xac, 0xed, 0x32, 0x00, 0x00, 0x01}, // binary chassis id
	}
	for i := 0; i < 45; i++ { // more than one GetBulk page
		table["1.3.6.1.2.1.31.1.1.1.18."+strconv.Itoa(i+1)] = "d" + strconv.Itoa(i)
	}
	addr, reqs := fakeBulkAgent(t, table, "public")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	vbs, err := SNMPWalk(ctx, addr, "public", "1.3.6.1.2.1.31.1.1.1.1", time.Second, 1000)
	if err != nil || len(vbs) != 2 || string(vbs[0].Bytes) != "1:1" || vbs[1].OID != "1.3.6.1.2.1.31.1.1.1.1.2" {
		t.Fatalf("ifName: %v %+v", err, vbs)
	}
	vbs, err = SNMPWalk(ctx, addr, "public", "1.3.6.1.2.1.2.2.1.8", time.Second, 1000)
	if err != nil || len(vbs) != 2 || !vbs[0].IsInt || vbs[0].Int != 1 || vbs[1].Int != 2 {
		t.Fatalf("ifOperStatus: %v %+v", err, vbs)
	}
	vbs, err = SNMPWalk(ctx, addr, "public", "1.0.8802.1.1.2.1.4.1.1.5", time.Second, 1000)
	if err != nil || len(vbs) != 1 || len(vbs[0].Bytes) != 6 || vbs[0].Bytes[0] != 0xac || vbs[0].Bytes[5] != 0x01 {
		t.Fatalf("binary value: %v %+v", err, vbs)
	}
	before := reqs.Load()
	vbs, err = SNMPWalk(ctx, addr, "public", "1.3.6.1.2.1.31.1.1.1.18", time.Second, 1000)
	if err != nil || len(vbs) != 45 || reqs.Load()-before < 3 {
		t.Fatalf("paging: %v n=%d requests=%d", err, len(vbs), reqs.Load()-before)
	}
	// the cap on values
	if vbs, _ = SNMPWalk(ctx, addr, "public", "1.3.6.1.2.1.31.1.1.1.18", time.Second, 25); len(vbs) < 25 || len(vbs) > 45 {
		t.Fatalf("cap: %d", len(vbs))
	}
	// an empty subtree
	if vbs, err = SNMPWalk(ctx, addr, "public", "1.3.6.1.4.1.999", time.Second, 100); err != nil || len(vbs) != 0 {
		t.Fatalf("empty: %v %+v", err, vbs)
	}
	// a wrong community gets no answer: a timeout error, not a hang
	if _, err = SNMPWalk(ctx, addr, "private", "1.3.6.1.2.1.31.1.1.1.1", 300*time.Millisecond, 10); err == nil {
		t.Fatal("wrong community must fail")
	}
}
