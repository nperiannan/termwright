package session

import (
	"regexp"
	"testing"
)

// The device types added for Huawei VRP, Eltex MES, Ruckus ICX and Cisco NX-OS read their facts from
// sample output (as published in the vendors' documentation).
func TestMoreDeviceTypeFacts(t *testing.T) {
	out := map[string]map[string]string{
		"Huawei VRP": {
			"display version": "Huawei Versatile Routing Platform Software\nVRP (R) software, Version 5.170 (S5735 V200R019C10SPC500)\nCopyright (C) 2000-2020 HUAWEI TECH Co., Ltd.\nHUAWEI S5735-L24P4S-A Routing Switch uptime is 12 weeks, 3 days, 1 hour, 2 minutes\n",
			"display esn":     "ESN of slot 0: 2102351931P0K3000032\n",
			"display bridge mac-address": "System bridge MAC address: 00e0-fc12-3456\n",
			"display current-configuration | include sysname": " sysname AGG-SW-01\n",
		},
		"Eltex MES": {
			"show system":    "System Description:                       MES2324B 28-port 1G/10G Managed Switch\nSystem Up Time (days,hour:min:sec):       05,02:13:45\nSystem Name:                              mes-floor2\nSystem MAC Address:                       a8:f9:4b:8b:d5:40\n",
			"show version":   "Active-image: flash://system/images/mes2300-4019-R33.ros\n  Version: 4.0.19\n",
			"show system id": "Unit    Serial number\n---- -----------------\n1       ES5D000123\n",
		},
		"Ruckus ICX": {
			"show version": "  Copyright (c) Ruckus Networks, Inc. All rights reserved.\n    UNIT 1: compiled on Mar 17 2023\n        SW: Version 08.0.95hT213\n  HW: Stackable ICX7150-48P-4X1G\n  Serial  #:FEK3220P0AX\n  The system uptime is 41 days 3 hours 2 minutes 4 seconds\n",
			"show running-config | include hostname": "hostname icx-access-3\n",
		},
		"Cisco NX-OS": {
			"show version": "Software\n  NXOS: version 9.3(8)\nHardware\n  cisco Nexus9000 C93180YC-EX chassis\n  Processor Board ID FDO21231ABC\n  Device name: n9k-leaf1\nKernel uptime is 41 day(s), 3 hour(s), 2 minute(s), 4 second(s)\n",
		},
		"Cisco IOS": {
			"show version": "Cisco IOS XE Software, Version 17.03.04\nleaf-sw uptime is 1 week\nModel Number                       : C9300-48P\nSystem Serial Number               : FOC2345X0AB\n",
		},
	}
	want := map[string]map[string]string{
		"Huawei VRP":  {"Hostname": "AGG-SW-01", "Model": "S5735-L24P4S-A", "Version": "5.170 (S5735 V200R019C10SPC500)", "Serial": "2102351931P0K3000032", "MAC": "00e0-fc12-3456", "Uptime": "12 weeks, 3 days, 1 hour, 2 minutes"},
		"Eltex MES":   {"Hostname": "mes-floor2", "Model": "MES2324B", "Version": "4.0.19", "Serial": "ES5D000123", "MAC": "a8:f9:4b:8b:d5:40", "Uptime": "05,02:13:45"},
		"Ruckus ICX":  {"Hostname": "icx-access-3", "Model": "ICX7150-48P-4X1G", "Version": "08.0.95hT213", "Serial": "FEK3220P0AX", "Uptime": "41 days 3 hours 2 minutes 4 seconds"},
		"Cisco NX-OS": {"Hostname": "n9k-leaf1", "Model": "C93180YC-EX", "Version": "9.3(8)", "Serial": "FDO21231ABC", "Uptime": "41 day(s), 3 hour(s), 2 minute(s), 4 second(s)"},
		"Cisco IOS":   {"Hostname": "leaf-sw", "Model": "C9300-48P", "Version": "17.03.04", "Serial": "FOC2345X0AB"},
	}
	names := map[string]bool{}
	for _, d := range BuiltinDeviceTypes() {
		names[d.Name] = true
	}
	for typ, w := range want {
		if !names[typ] {
			t.Errorf("%s is not built in", typ)
		}
		got := map[string]string{}
		for _, r := range BuiltinFacts(typ) {
			m := regexp.MustCompile(`(?m)` + r.Regex).FindStringSubmatch(out[typ][r.Command])
			for _, g := range m[min(1, len(m)):] {
				if g != "" {
					got[r.Name] = g
					break
				}
			}
		}
		for k, v := range w {
			if got[k] != v {
				t.Errorf("%s %s = %q, want %q", typ, k, got[k], v)
			}
		}
	}
}
