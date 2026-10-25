package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBulkImports(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	res, err := st.Import(write("lab.csv", "Name;IP;Device Type;Folder;User;Protocol;Port\ncore1;10.0.0.1;VOSS;Lab/Core;admin;;\nedge;10.0.0.2;exos;Lab;;telnet;2001\n;10.0.0.3;;;;;\n"), ImportOptions{})
	if err != nil || len(res.Imported) != 3 {
		t.Fatalf("csv %+v %v", res, err)
	}
	if s, _ := st.Get("Lab/Core/core1"); s.DeviceType != "VOSS" || s.Username != "admin" || s.Hostname != "10.0.0.1" {
		t.Fatalf("%+v", s)
	}
	if s, _ := st.Get("Lab/edge"); s.Protocol != ProtoTelnet || s.Port != 2001 {
		t.Fatalf("%+v", s)
	}
	if _, err := st.Get("10.0.0.3"); err != nil {
		t.Fatal("unnamed row:", err)
	}

	res, _ = st.Import(write("hosts", "[all:vars]\nansible_user=ops\n\n[core]\nsw1 ansible_host=10.1.1.1 ansible_network_os=extreme.voss.voss\nsw2 ansible_host=10.1.1.2 ansible_port=2222\n\n[edge]\nsw3 ansible_host=10.1.1.3 ansible_connection=telnet\n"), ImportOptions{TargetFolder: "Ansible"})
	if len(res.Imported) != 3 {
		t.Fatalf("ini %+v", res)
	}
	if s, _ := st.Get("Ansible/core/sw1"); s.DeviceType != "VOSS" || s.Username != "ops" || s.Hostname != "10.1.1.1" {
		t.Fatalf("%+v", s)
	}
	if s, _ := st.Get("Ansible/edge/sw3"); s.Protocol != ProtoTelnet || s.Port != 23 {
		t.Fatalf("%+v", s)
	}

	res, _ = st.Import(write("inv.yml", "all:\n  vars:\n    ansible_user: netops\n  children:\n    dc1:\n      children:\n        spines:\n          hosts:\n            spine1:\n              ansible_host: 10.2.0.1\n              ansible_network_os: exos\n"), ImportOptions{TargetFolder: "Y"})
	if len(res.Imported) != 1 {
		t.Fatalf("yaml %+v", res)
	}
	if s, _ := st.Get("Y/dc1/spines/spine1"); s.DeviceType != "EXOS" || s.Username != "netops" {
		t.Fatalf("%+v", s)
	}

	nb := NetBoxSessions([]NetBoxDevice{{Name: "leaf-1", PrimaryIP: &struct {
		Address string `json:"address"`
	}{"10.9.0.1/24"}, Site: &struct {
		Name string `json:"name"`
	}{"DC1"}}})
	if len(nb) != 1 || nb[0].Hostname != "10.9.0.1" || nb[0].Folder != "DC1" {
		t.Fatalf("%+v", nb)
	}
	aws, err := AWSSessions([]byte(`{"Reservations":[{"Instances":[{"InstanceId":"i-1","PrivateIpAddress":"172.31.0.5","KeyName":"lab","VpcId":"vpc-1","State":{"Name":"running"},"Tags":[{"Key":"Name","Value":"web"}]},{"InstanceId":"i-2","State":{"Name":"stopped"}}]}]}`), "us-east-1", false)
	if err != nil || len(aws) != 1 || aws[0].IdentityFile != "~/.ssh/lab.pem" || aws[0].Folder != "AWS/us-east-1/vpc-1" {
		t.Fatalf("%+v %v", aws, err)
	}
}
