package term

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPythonScript(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	m, em, tab := connectedTab(t)
	defer m.CloseAll()
	status := make(chan map[string]string, 4)
	em.mu.Lock()
	em.onScript = func(s map[string]string) {
		if s["state"] != "running" {
			status <- s
		}
	}
	em.mu.Unlock()
	dir := t.TempDir()
	script := filepath.Join(dir, "login_auto.py")
	os.WriteFile(script, []byte(`# $language = "Python3"
# $interface = "1.0"
def main():
    crt.Screen.Synchronous = True
    if not crt.Session.Connected:
        crt.Quit()
    crt.Screen.Send("show version\r")
    ok = crt.Screen.WaitForString("echo:show", 5)
    idx = crt.Screen.WaitForStrings(["nomatch", "VERSION"], 5, True)
    crt.Screen.Send("hello " + crt.Arguments.GetArg(0) + "\r")
    before = crt.Screen.ReadString("DUT1", 5)
    v = crt.Dialog.Prompt("Password?", "Login", "", True)
    t = crt.Screen.WaitForString("never", 0.3)
    btn = crt.Dialog.MessageBox("continue?", "Q", BUTTON_YESNO | ICON_QUESTION)
    crt.Screen.Send("result %s %d %s %s %d %s %s\r" % (ok, idx, v, t, crt.Arguments.Count, before.strip().endswith("hello"), btn == IDYES))
    print("printed from python")

main()
`), 0o644)
	if _, err := m.RunScriptFile(tab, script, "DUT1 DUT2"); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-status:
		if s["state"] != "done" {
			t.Fatalf("%+v\noutput:\n%s", s, em.output())
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("timeout; output:\n%s", em.output())
	}
	if out := em.output(); !strings.Contains(out, "echo:result True 2 pw False 2 True False") {
		t.Fatalf("unexpected output:\n%s", out)
	}

	bad := filepath.Join(dir, "bad.py")
	os.WriteFile(bad, []byte("crt.Screen.Send('x')\nundefined_name()\n"), 0o644)
	m.RunScriptFile(tab, bad, "")
	if s := <-status; s["state"] != "error" || !strings.Contains(s["message"], "NameError") {
		t.Fatalf("%+v", s)
	}

	slow := filepath.Join(dir, "slow.py")
	os.WriteFile(slow, []byte("crt.Screen.WaitForString('never-ever')\n"), 0o644)
	id, _ := m.RunScriptFile(tab, slow, "")
	time.Sleep(500 * time.Millisecond)
	m.CancelScript(id)
	if s := <-status; s["state"] != "cancelled" {
		t.Fatalf("%+v", s)
	}
}

// crt.Screen.Get / CurrentRow read the rendered screen when a terminal view exists.
func TestPythonScreenGet(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	m, em, tab := connectedTab(t)
	defer m.CloseAll()
	m.ScreenSnapshot = func(id string) *ScreenSnap {
		return &ScreenSnap{Lines: []string{"Port  Status     ", "1/1   up         ", "core-sw1:1#      "}, Row: 3, Col: 12, Rows: 3, Cols: 17}
	}
	status := make(chan map[string]string, 2)
	em.mu.Lock()
	em.onScript = func(s map[string]string) {
		if s["state"] != "running" {
			status <- s
		}
	}
	em.mu.Unlock()
	script := filepath.Join(t.TempDir(), "screen.py")
	os.WriteFile(script, []byte(`r = crt.Screen.CurrentRow
c = crt.Screen.CurrentColumn
prompt = crt.Screen.Get(r, 1, r, c - 1)
st = crt.Screen.Get(2, 7, 2, 8)
crt.Screen.Send("got %d %d [%s] [%s] %d %d\r" % (r, c, prompt, st, crt.Screen.Rows, crt.Screen.Columns))
`), 0o644)
	if _, err := m.RunScriptFile(tab, script, ""); err != nil {
		t.Fatal(err)
	}
	if s := <-status; s["state"] != "done" {
		t.Fatalf("%+v", s)
	}
	waitFor(t, func() bool { return strings.Contains(em.output(), "echo:got 3 12 [core-sw1:1#] [up] 3 17") })
}
