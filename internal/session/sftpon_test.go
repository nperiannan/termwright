package session

import "testing"

func TestSFTPOnRoundTrip(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := Session{Name: "dl2", Protocol: ProtoSSH2, Hostname: "gw", UseRemoteCommand: true, RemoteCommand: "ssh -t dl2"}
	for _, want := range []string{"", "jump", "target"} {
		s := base
		s.SFTPOn, s.Name = want, "dl2-"+want
		saved, err := st.Save(s, "")
		if err != nil {
			t.Fatal(err)
		}
		got, err := st.Get(saved.Path)
		if err != nil {
			t.Fatal(err)
		}
		if got.SFTPOn != want || got.SFTPToTarget != (want == "target") {
			t.Fatalf("want %q, got %q / %v", want, got.SFTPOn, got.SFTPToTarget)
		}
	}
	// a session saved by an older version: only the target flag
	s := base
	s.Name, s.SFTPToTarget = "old", true
	saved, _ := st.Save(s, "")
	if got, _ := st.Get(saved.Path); got.SFTPOn != "target" {
		t.Fatalf("old flag: got %q", got.SFTPOn)
	}
}

func TestWOLRoundTrip(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	s := Session{Name: "pc", Protocol: ProtoSSH2, Hostname: "10.0.0.5", MAC: "00:11:22:33:44:55", WOLBroadcast: "10.0.0.255", WOLPort: 7, WOLFrom: "local"}
	saved, err := st.Save(s, "")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(saved.Path)
	if got.WOLBroadcast != "10.0.0.255" || got.WOLPort != 7 || got.WOLFrom != "local" {
		t.Fatalf("%+v", got)
	}
}

func TestWOLSessionRoundTrip(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	s := Session{Name: "pc2", Protocol: ProtoWOL, MAC: "00:11:22:33:44:55", WOLNetwork: "192.168.1.0/24", WOLScans: 6, WOLScanSecs: 20, WOLPingSecs: -1}
	saved, err := st.Save(s, "")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(saved.Path)
	if got.Protocol != ProtoWOL || got.WOLNetwork != "192.168.1.0/24" || got.WOLScans != 6 || got.WOLScanSecs != 20 || got.WOLPingSecs != -1 {
		t.Fatalf("%+v", got)
	}
}

func TestAccountRoundTrip(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	saved, err := st.Save(Session{Name: "sw", Protocol: ProtoSSH2, Hostname: "10.0.0.1", Account: "lab-admin"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(saved.Path); got.Account != "lab-admin" {
		t.Fatalf("account %q", got.Account)
	}
}
