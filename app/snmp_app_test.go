package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/snmp"
)

// The SNMP tool against the package's test agent, given as an address (no saved session, no keychain).
func TestSNMPTool(t *testing.T) {
	ag := snmp.NewAgent("public", []byte{0x80, 0, 0x07, 0x7c, 3, 1, 2, 3, 4, 5, 6})
	ag.Put(snmp.Varbind{OID: "1.3.6.1.2.1.1.1.0", Type: snmp.OctetString, Bytes: []byte("Extreme Networks Fabric Engine")})
	ag.Put(snmp.Varbind{OID: "1.3.6.1.2.1.2.2.1.8.1", Type: snmp.Integer, Int: 1})
	ag.Put(snmp.Varbind{OID: "1.3.6.1.2.1.2.2.1.8.2", Type: snmp.Integer, Int: 2})
	addr, err := ag.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ag.Close()
	a := &App{}
	ctx := context.Background()
	out, err := a.netToolRun(ctx, "snmp", map[string]any{"action": "walk", "address": addr, "oids": []any{"IF-MIB::ifOperStatus"}}, nil)
	if err != nil || !strings.Contains(out, "IF-MIB::ifOperStatus.2 = down (2)") || !strings.Contains(out, "IF-MIB::ifOperStatus.1 = up (1)") {
		t.Fatalf("%v\n%s", err, out)
	}
	out, err = a.netToolRun(ctx, "snmp", map[string]any{"action": "get", "address": addr, "oids": []any{"sysDescr.0"}}, nil)
	if err != nil || !strings.Contains(out, "SNMPv2-MIB::sysDescr.0 = Extreme Networks Fabric Engine (OctetString)") {
		t.Fatalf("%v\n%s", err, out)
	}
	// a set is asked (the asker refuses here) before it is sent
	asked := false
	_, err = a.netToolRun(ctx, "snmp", map[string]any{"action": "set", "address": addr, "values": []any{map[string]any{"oid": "ifAdminStatus.1", "value": "down"}}},
		func(title, msg string, danger bool) error { asked = true; return errEmuAsk })
	if !asked || err != errEmuAsk {
		t.Fatalf("set not asked: %v", err)
	}
	// the in-app AI: Read only stops a set and asks; auto runs it on a non-production target
	st, err := a.NetAgentTool("snmp", map[string]any{"action": "set", "address": addr, "values": []any{map[string]any{"oid": "ifAdminStatus.1", "value": "down"}}}, "readonly", false)
	if err != nil || !st.Ask {
		t.Fatalf("readonly: %+v %v", st, err)
	}
	st, err = a.NetAgentTool("snmp", map[string]any{"action": "set", "address": addr, "values": []any{map[string]any{"oid": "ifAdminStatus.1", "value": "down"}}}, "auto", false)
	if st.Ask || !strings.Contains(st.Out+fmt.Sprint(err), "notWritable") {
		t.Fatalf("auto: %+v %v", st, err)
	}
	out, _ = a.netToolRun(ctx, "snmp", map[string]any{"action": "mib_search", "query": "ifOperStatus"}, nil)
	if !strings.Contains(out, "IF-MIB::ifOperStatus  1.3.6.1.2.1.2.2.1.8") {
		t.Fatal(out)
	}
	v, err := snmpValue(snmp.Standard(), SNMPValue{OID: "ifAdminStatus.3", Value: "testing"})
	if err != nil || v.Type != snmp.Integer || v.Int != 3 || v.OID != "1.3.6.1.2.1.2.2.1.7.3" {
		t.Fatalf("%+v %v", v, err)
	}
}
