package config

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreBackup(t *testing.T) {
	data, other := t.TempDir(), t.TempDir()
	sess := filepath.Join(other, "Sessions")
	zp := filepath.Join(other, "b.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{
		"Termwright/settings.json":       `{"sessionsDir":"` + filepath.ToSlash(sess) + `","theme":"light"}`,
		"Termwright/ai-chats.json":       `{"chats":[]}`,
		"Sessions/Lab/sw1.json":          `{"name":"sw1"}`,
		"Termwright/../../escape.txt":    "no",
		"Other/x.txt":                    "no",
		"Termwright/restore-pending/a.x": "no",
	} {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
	os.WriteFile(filepath.Join(data, "keep.txt"), []byte("mine"), 0o600)
	os.WriteFile(filepath.Join(data, "settings.json"), []byte(`{"theme":"dark"}`), 0o600)

	info, err := StageRestore(data, zp)
	if err != nil || info.Files != 3 || !PendingRestore(data) {
		t.Fatalf("stage: %+v %v", info, err)
	}
	if b, _ := os.ReadFile(filepath.Join(data, "settings.json")); string(b) != `{"theme":"dark"}` {
		t.Fatal("staging must not touch the settings in use")
	}
	if err := ApplyPendingRestore(data); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(data, "settings.json")); string(b) == `{"theme":"dark"}` {
		t.Fatal("settings not restored")
	}
	if _, err := os.Stat(filepath.Join(sess, "Lab", "sw1.json")); err != nil {
		t.Fatal("sessions not restored to the sessions folder")
	}
	if b, _ := os.ReadFile(filepath.Join(data, "keep.txt")); string(b) != "mine" {
		t.Fatal("files not in the backup must stay")
	}
	for _, p := range []string{filepath.Join(filepath.Dir(data), "escape.txt"), filepath.Join(data, "escape.txt")} {
		if _, err := os.Stat(p); err == nil {
			t.Fatal("a path outside the backup's folders was written")
		}
	}
	if PendingRestore(data) {
		t.Fatal("the staged restore is still there")
	}
	if _, err := StageRestore(data, filepath.Join(data, "keep.txt")); err == nil {
		t.Fatal("not a zip")
	}
}
