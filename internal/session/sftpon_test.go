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
