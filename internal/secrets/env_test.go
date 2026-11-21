package secrets

import "testing"

func TestEnvName(t *testing.T) {
	if n := EnvName("snmp-community:Lab/core-sw1"); n != "TERMWRIGHT_SECRET_SNMP_COMMUNITY_LAB_CORE_SW1" {
		t.Fatal(n)
	}
}
