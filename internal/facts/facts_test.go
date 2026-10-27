package facts

import "testing"

func TestParse(t *testing.T) {
	voss := "General Info :\n\n\tSysDescr     : VSP-4900-48P (8.10.1.0)\n\tSysName      : core-sw1\n\tSysUpTime    : 2 day(s), 04:11:32\n\nChassis Info:\n\n\tChassis            : 4900-48P\n\tSerial#            : 2022G-01234\n\tModelName          : 4900-48P\n\tBaseMacAddr        : 00:51:00:a1:b2:00\n"
	if f := Parse("voss", voss); f.Version != "8.10.1.0" || f.Serial != "2022G-01234" || f.Model != "4900-48P" || f.MAC != "00:51:00:a1:b2:00" {
		t.Fatalf("voss: %+v", f)
	}
	exos := "SysName:          edge-sw1\nSystem MAC:       00:04:96:C3:D4:12\nSystem Type:      X440G2-48p-10G4 \n\nSwitch          : 800599-00-01 1702G-00451 Rev 1 BootROM: 2.0.1.7    IMG: 31.7.1.4  \n"
	if f := Parse("exos", exos); f.Version != "31.7.1.4" || f.Serial != "1702G-00451" || f.Model != "X440G2-48p-10G4" || f.MAC != "00:04:96:C3:D4:12" {
		t.Fatalf("exos: %+v", f)
	}
	ios := "Cisco IOS Software, C2960X Software (C2960X-UNIVERSALK9-M), Version 15.2(7)E8, RELEASE SOFTWARE (fc1)\nModel number                    : WS-C2960X-48FPD-L\nSystem serial number            : FOC2034X1AB\n"
	if f := Parse("", ios); f.Version != "15.2(7)E8" || f.Serial != "FOC2034X1AB" || f.Model != "WS-C2960X-48FPD-L" {
		t.Fatalf("ios: %+v", f)
	}
	lx := "NAME=\"Ubuntu\"\nPRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\n"
	if f := Parse("linux", lx); f.Version != "Ubuntu 24.04.1 LTS" {
		t.Fatalf("linux: %+v", f)
	}
	if Family("VOSS") != "voss" || Family("EXOS 31") != "exos" || Family("Cisco IOS") != "cisco" || Family("Linux") != "linux" || Family("Printer") != "" {
		t.Fatal("family")
	}
}
