package term

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildWSF(t *testing.T) {
	src := "# $language = \"VBScript\"\r\n# $interface = \"1.0\"\r\nSub Main\r\n  crt.Screen.Send \"x]]>y\" & vbCr\r\nEnd Sub\r\n"
	w := buildWSF(decodeScript([]byte(src)))
	if strings.Contains(w, "$language") || !strings.Contains(w, "Main\n]]>") || !strings.Contains(w, `language="JScript"`) ||
		!strings.Contains(w, "x]]]]><![CDATA[>y") {
		t.Fatalf("wsf:\n%s", w)
	}
	if strings.Count(buildWSF("crt.Screen.Send \"a\""), "<script") != 2 {
		t.Fatal("Main must only be called when the script defines it")
	}
	u16 := []byte{0xFF, 0xFE, 'S', 0, 'u', 0, 'b', 0}
	if decodeScript(u16) != "Sub" || decodeScript([]byte{0x63, 0x61, 0x66, 0xE9}) != "café" {
		t.Fatal("decode")
	}
}

// The JScript crt object (run under Node with a WSH stand-in) against the
// real HTTP bridge, a connected test tab and a rendered-screen snapshot.
func TestVBScriptBridge(t *testing.T) {
	for _, b := range []string{"node", "curl"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skip(b + " not installed")
		}
	}
	m, em, tab := connectedTab(t)
	defer m.CloseAll()
	m.ScreenSnapshot = func(id string) *ScreenSnap {
		return &ScreenSnap{Lines: []string{"Welcome             ", "router> show version", "router>             "}, Row: 3, Col: 9, Rows: 3, Cols: 20}
	}
	status := make(chan map[string]string, 4)
	em.mu.Lock()
	em.onScript = func(s map[string]string) {
		if s["state"] != "running" {
			status <- s
		}
	}
	em.mu.Unlock()
	autoAnswer(m, em, "pw")
	dir := t.TempDir()
	user := filepath.Join(dir, "user.js")
	// the calls a VBScript makes, including omitted optional arguments
	os.WriteFile(user, []byte(`
crt.Screen.Synchronous = true;
if (!crt.Session.Connected) WScript.Quit(3);
crt.Screen.Send("show version\r");
var ok = crt.Screen.WaitForString("echo:show");
var idx = crt.Screen.WaitForStrings(new VBArray(["nomatch", "VERSION"]).toArray(), 5, true);
var row = crt.Screen.CurrentRow, col = crt.Screen.CurrentColumn;
var prompt = crt.Screen.Get(row, 1, row, col - 1);
var pw = crt.Dialog.Prompt("Password?", "Login", "", true);
var to = crt.Screen.WaitForString("never", 1);
crt.Screen.Send("result " + ok + " " + idx + " " + row + " [" + prompt + "] " + pw + " " + to + " " + crt.Arguments.Count + " " + crt.Arguments(0) + "\r");
crt.Screen.WaitForString("echo:result");
`), 0o644)
	harness, _ := filepath.Abs("testdata/wsh_harness.js")
	crtJS, _ := filepath.Abs("vbs/termwright_crt.js")
	_, err := m.runBridge(tab, filepath.Join(dir, "login.vbs"), true, func(ctx context.Context, d string) (*exec.Cmd, error) {
		return exec.CommandContext(ctx, "node", harness, crtJS, user, "DUT1", "DUT2"), nil
	})
	if err != nil {
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
	if out := em.output(); !strings.Contains(out, "echo:result true 2 3 [router> ] pw false 2 DUT1") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}
