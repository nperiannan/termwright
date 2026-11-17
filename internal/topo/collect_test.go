package topo

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAllowedIsReadOnly(t *testing.T) {
	for _, c := range []string{"show lldp neighbors detail", "SHOW sharing", "show lldp neighbors detail | json", "disable cli paging", "terminal length 0", "terminal more disable", "set cli screen-length 0"} {
		if !Allowed(c) {
			t.Errorf("%q should be allowed", c)
		}
	}
	for _, c := range []string{"", "configure terminal", "reload", "disable ports 1", "set cli screen-length 10", "show x; reload", "show x | include a", "show x | json | tee", "show x && reload", "show `id`", "terminal length 24", "write memory", "clear counters", "show x\nreload", "enable"} {
		if Allowed(c) {
			t.Errorf("%q must not be allowed", c)
		}
	}
	// every command in every plan passes
	for os, steps := range plans {
		for _, s := range steps {
			if !Allowed(s.Cmd) {
				t.Errorf("%s: plan command %q is not allowed", os, s.Cmd)
			}
		}
	}
}

func TestOSFor(t *testing.T) {
	for in, want := range map[string]string{"Extreme EXOS": "exos", "Switch Engine": "exos", "VOSS": "voss", "Fabric Engine 9.4": "voss", "Cisco IOS-XE": "ios", "Cisco NX-OS": "nxos", "Arista EOS": "eos", "Juniper Junos": "junos", "Linux": "", "": ""} {
		if got := OSFor(in); got != want {
			t.Errorf("OSFor(%q) = %q want %q", in, got, want)
		}
	}
}

// fixtureRunner answers each command of the plan from the saved lab output.
func fixtureRunner(t *testing.T, files map[string]string) (Runner, *[]string) {
	var ran []string
	return func(_ context.Context, cmds []string) ([]string, error) {
		outs := make([]string, len(cmds))
		for i, c := range cmds {
			ran = append(ran, c)
			if f, ok := files[c]; ok {
				outs[i] = fx(t, f)
			}
		}
		return outs, nil
	}, &ran
}

func TestCollectEXOS(t *testing.T) {
	run, ran := fixtureRunner(t, map[string]string{
		"show switch":                "exos/switch.txt",
		"show lldp neighbors detail": "exos/lldp_neighbors_detail.txt", "show edp ports all": "exos/edp_ports.txt",
		"show ports information": "exos/ports_info.txt", "show sharing": "exos/sharing.txt", "show stpd s0 ports": "exos/stpd_ports.txt",
	})
	d := Collect(context.Background(), Target{Session: "lab/t-3", Name: "5520-24t-3", OS: "exos"}, run, t0)
	if d.Err != "" || d.Chassis != "ac:ed:32:f5:af:00" || d.SysName != "5520-24t-3" {
		t.Fatalf("err=%q chassis=%q", d.Err, d.Chassis)
	}
	if len(d.Neighbors) != 7 { // 4 LLDP + 3 EDP
		t.Fatalf("neighbours: %d", len(d.Neighbors))
	}
	if len(d.Ports) != 3 || len(d.LAGs) != 1 {
		t.Fatalf("ports %d lags %d", len(d.Ports), len(d.LAGs))
	}
	if p := portOf(&d, "1"); p == nil || p.STP != "blocked" {
		t.Fatalf("port 1 STP: %+v", p)
	}
	for _, c := range *ran {
		if !Allowed(c) {
			t.Fatalf("ran %q", c)
		}
	}
	// the LLDP and the EDP entry of one cable make one link that lists both protocols
	m := Build([]Device{d}, nil, t0)
	var both bool
	for _, l := range m.Links {
		both = both || len(l.Seen) == 2
	}
	if !both {
		t.Fatalf("LLDP and EDP of one cable should be one link: %+v", m.Links)
	}
}

func TestCollectEXOSIsFreshest(t *testing.T) {
	// port 3: LLDP age 3 s, EDP age 26 s: both say 5520-24t-2, one link, the LLDP one is the newer
	run, _ := fixtureRunner(t, map[string]string{"show lldp neighbors detail": "exos/lldp_neighbors_detail.txt", "show edp ports all": "exos/edp_ports.txt", "show ports information": "exos/ports_info.txt"})
	d := Collect(context.Background(), Target{Session: "s", Name: "x", OS: "exos"}, run, t0)
	m := Build([]Device{d}, nil, t0)
	for _, l := range m.Links {
		if l.APort == "3" && l.BPort != "6" {
			t.Fatalf("port 3 should say remote port 6: %+v", l)
		}
	}
}

func TestCollectFailures(t *testing.T) {
	d := Collect(context.Background(), Target{Name: "x", OS: "exos"}, func(context.Context, []string) ([]string, error) { return nil, errors.New("the login was refused") }, t0)
	if d.Err != "the login was refused" {
		t.Fatalf("got %q", d.Err)
	}
	d = Collect(context.Background(), Target{Name: "x", OS: "linux"}, nil, t0)
	if d.Err == "" {
		t.Fatal("unsupported OS should say so")
	}
	// a refused command is a note; the rest is still read
	run := func(_ context.Context, cmds []string) ([]string, error) {
		outs := make([]string, len(cmds))
		for i, c := range cmds {
			if c == "show edp ports all" {
				outs[i] = "                                     ^\n%% Invalid input detected at '^' marker.\n"
			}
			if c == "show lldp neighbors detail" {
				outs[i] = fx(t, "exos/lldp_neighbors_detail.txt")
			}
		}
		return outs, nil
	}
	d = Collect(context.Background(), Target{Name: "x", OS: "exos"}, run, t0)
	if d.Err != "" || len(d.Neighbors) != 4 || len(d.Notes) != 1 || !strings.Contains(d.Notes[0], "show edp ports all") {
		t.Fatalf("err=%q n=%d notes=%v", d.Err, len(d.Neighbors), d.Notes)
	}
	// the answer has fewer outputs than commands
	d = Collect(context.Background(), Target{Name: "x", OS: "exos"}, func(context.Context, []string) ([]string, error) { return []string{"a"}, nil }, t0)
	if d.Err == "" {
		t.Fatal("a short answer is an error")
	}
}

func TestCollectVOSS(t *testing.T) {
	run, _ := fixtureRunner(t, map[string]string{"show sys-info": "voss/sys_info.txt", "show lldp neighbor": "voss/lldp_neighbor.txt", "show interfaces gigabitEthernet interface": "voss/interfaces.txt", "show mlt": "voss/mlt_empty.txt", "show spanning-tree mstp port role": "voss/stp_port_role.txt"})
	d := Collect(context.Background(), Target{Session: "s", Name: "5520-24T-FabricEngine", OS: "voss"}, run, t0)
	if d.Err != "" || d.Chassis != "ac:ed:32:c4:64:00" || d.SysName != "5520-24T-FabricEngine" || len(d.Neighbors) != 2 || len(d.Ports) != 2 {
		t.Fatalf("%+v", d)
	}
	if p := portOf(&d, "1/1"); p == nil || p.STP != "disabled" {
		t.Fatalf("1/1: %+v", p)
	}
}

func TestSystemNameOfOthers(t *testing.T) {
	// documented layouts (no such device in the lab): IOS "show version", Junos "show version", EOS/NX-OS "show hostname"
	for _, c := range []struct{ os, cmd, out, want string }{
		{"ios", "show version", "Cisco IOS Software\nsw1 uptime is 1 week, 2 days\n", "sw1"},
		{"junos", "show version", "Hostname: vsrx1\nModel: vsrx\n", "vsrx1"},
		{"eos", "show hostname", "Hostname: leaf1\nFQDN:     leaf1.example.net\n", "leaf1"},
		{"nxos", "show hostname", "n9k-1\n", "n9k-1"},
	} {
		run := func(_ context.Context, cmds []string) ([]string, error) {
			outs := make([]string, len(cmds))
			for i, x := range cmds {
				if x == c.cmd {
					outs[i] = c.out
				}
				if x == "show interfaces status" || x == "show interface status" || x == "show interfaces terse" {
					outs[i] = "Gi1/0/1 connected\n"
				}
			}
			return outs, nil
		}
		d := Collect(context.Background(), Target{Name: "x", OS: c.os}, run, t0)
		if d.Err != "" || d.SysName != c.want {
			t.Errorf("%s: err=%q sysname=%q want %q", c.os, d.Err, d.SysName, c.want)
		}
	}
}
