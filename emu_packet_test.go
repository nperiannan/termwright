package main

import (
	"encoding/hex"
	"strings"
	"testing"
)

func fieldText(fs []EmuField, ind string) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(ind + f.Name + "=" + f.Value + "\n")
		b.WriteString(fieldText(f.Kids, ind+"  "))
	}
	return b.String()
}

func findField(fs []EmuField, name string) *EmuField {
	for i := range fs {
		if fs[i].Name == name {
			return &fs[i]
		}
		if f := findField(fs[i].Kids, name); f != nil {
			return f
		}
	}
	return nil
}

// Each packet decodes into its layers and fields, each with its own bytes.
func TestEmuFields(t *testing.T) {
	lldp, _ := hex.DecodeString("0180c200000eaced32e556008" + "8cc" + "020704aced32e55600" + "0402073" + "1" + "06020078" + "0a0a353532302d3234742d33" + "0e0400140014" + "0000")
	fs := emuFields(lldp)
	txt := fieldText(fs, "")
	for _, want := range []string{"Ethernet II=ac:ed:32:e5:56:00 → 01:80:c2:00:00:0e", "Chassis ID=ac:ed:32:e5:56:00", "Port ID=1", "Time to live=120 s", "System name=5520-24t-3", "Capabilities=bridge, router (on: bridge, router)", "LLDP=5520-24t-3"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("no %q in\n%s", want, txt)
		}
	}
	if f := findField(fs, "System name"); f == nil || string(lldp[f.Off+2:f.Off+f.N]) != "5520-24t-3" {
		t.Fatalf("system name bytes %+v", f)
	}
	ping, _ := hex.DecodeString("0274770000020274770000018100" + "000a" + "0800" + "45000054abcd40004001" + "0000" + "0a0a00010a0a0002" + "0800f7ff00010002" + "74776774")
	txt = fieldText(emuFields(ping), "")
	for _, want := range []string{"802.1Q VLAN=VLAN 10, priority 0", "IPv4=10.10.0.1 → 10.10.0.2", "Flags=DF", "ICMP=echo request", "Sequence=2"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("no %q in\n%s", want, txt)
		}
	}
	stp, _ := hex.DecodeString("0180c2000000aced320b27000027424203000002023c8000aced320b2700000000008000aced320b27008001000014000200" + "0f0000")
	txt = fieldText(emuFields(stp), "")
	for _, want := range []string{"STP=BPDU RST/MST", "Version=RSTP", "Root=32768 / ac:ed:32:0b:27:00", "Hello time=2 s", "Forward delay=15 s"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("no %q in\n%s", want, txt)
		}
	}
	if !strings.Contains(fieldText(emuFields([]byte{1, 2}), ""), "too short") {
		t.Fatal("a short frame")
	}
	// the chosen packets go to a pcap file that reads back
	rs := []emuRec{{1700000000.25, len(lldp), lldp}, {1700000001.5, len(ping), ping}}
	back, ok := emuPcapRecords(emuPcapWrite(rs))
	if !ok || len(back) != 2 || string(back[1].data) != string(ping) || back[1].ts != 1700000001.5 {
		t.Fatalf("pcap round trip %v %d", ok, len(back))
	}
}
