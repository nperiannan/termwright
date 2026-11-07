package main

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

// TestTeamUpdateEndToEnd updates a team server running in Docker on this
// machine, logging in over SSH as Termwright does. It runs only when asked:
//
//	TW_TEST_SSH=2222 TW_TEST_KEY=/path/id_ed25519 TW_TEST_NEW=/path/termwright-team TW_TEST_CONTAINER=… TW_TEST_ADDR=127.0.0.1:7464 TW_TEST_FP=… TW_TEST_VERSION=0.30.1
func TestTeamUpdateEndToEnd(t *testing.T) {
	port := os.Getenv("TW_TEST_SSH")
	if port == "" {
		t.Skip("set TW_TEST_SSH and the others to run it")
	}
	home := t.TempDir()
	t.Setenv("TERMWRIGHT_HOME", home)
	kh, _ := exec.Command("sh", "-c", "ssh-keyscan -p "+port+" 127.0.0.1 2>/dev/null").Output()
	_ = os.WriteFile(home+"/known_hosts", kh, 0o600)
	a := NewApp()
	if err := a.init(); err != nil {
		t.Fatal(err)
	}
	p, _ := strconv.Atoi(port)
	if _, err := a.store.Save(session.Session{Name: "lab-host", Protocol: "SSH2", Hostname: "127.0.0.1", Port: p, Username: "root",
		IdentityFile: os.Getenv("TW_TEST_KEY"), AuthMethods: "publickey"}, ""); err != nil {
		t.Fatal(err)
	}
	newBin, err := os.ReadFile(os.Getenv("TW_TEST_NEW"))
	if err != nil {
		t.Fatal(err)
	}
	old := teamBinarySource
	teamBinarySource = func(*App, context.Context, string, string) ([]byte, error) { return newBin, nil }
	defer func() { teamBinarySource = old }()
	oldV := Version
	Version = os.Getenv("TW_TEST_VERSION")
	defer func() { Version = oldV }()

	tc := config.TeamConn{ID: "t1", Name: "Lab D", Address: os.Getenv("TW_TEST_ADDR"), Fingerprint: os.Getenv("TW_TEST_FP"), Role: "owner"}
	var steps []string
	emit := func(step, state, text string) { steps = append(steps, state+" "+step+": "+text) }
	err = a.teamUpdate(context.Background(), "e2e", tc, TeamUpdateRequest{TeamID: "t1", SessionPath: "lab-host", Image: os.Getenv("TW_TEST_IMAGE")}, "127.0.0.1", emit)
	t.Log("\n" + strings.Join(steps, "\n"))
	if want := os.Getenv("TW_TEST_FAIL"); want != "" {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want an error with %q, got %v", want, err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
}
