package webrun

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The recording toolbar: check modes, undo, a value that is a variable, and its own clicks never recorded.
func TestRecordingToolbar(t *testing.T) {
	pw := os.Getenv("TW_PLAYWRIGHT")
	if pw == "" {
		t.Skip("TW_PLAYWRIGHT not set")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	os.Symlink(pw, filepath.Join(dir, "node_modules", "playwright"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><title>Devices</title><body><h1>Add device</h1>
<label for="sn">Serial number</label><input id="sn">
<label><input type="checkbox" id="mg"> Managed</label>
<button id="go" onclick="document.getElementById('out').textContent='Onboarded '+document.getElementById('sn').value">Onboard</button>
<button id="other">Other</button><p id="out"></p></body></html>`)
	}))
	defer srv.Close()
	r := Runner{Dir: dir}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var seen []string
	acts, err := r.Record(ctx, Step{URL: srv.URL}, func(a Action) { seen = append(seen, a.Do) }, Extra{Root: dir, RecVars: map[string]string{"serial": "SN123"}},
		Drive{Do: "fill", Target: "#sn", Value: "SN123"},
		Drive{Do: "click", Target: "#mg"},
		Drive{Do: "click", Target: "#go"},
		Drive{Do: "click", Target: "#other"},
		Drive{Do: "click", Target: `#__tw_rec button[data-a="undo"]`}, // drops "click Other"
		Drive{Do: "click", Target: `#__tw_rec button[data-m="text"]`}, // the next click is a check
		Drive{Do: "click", Target: "#out"},                            // ✓ shows "Onboarded SN123" -> {{serial}}
		Drive{Do: "click", Target: `#__tw_rec button[data-m="visible"]`},
		Drive{Do: "click", Target: "#go"}, // ✓ Onboard is visible
		Drive{Do: "click", Target: `#__tw_rec button[data-m="value"]`},
		Drive{Do: "click", Target: "#sn"}, // ✓ value {{serial}}
		Drive{Do: "click", Target: `#__tw_rec button[data-m="value"]`},
		Drive{Do: "click", Target: "#mg"}, // ✓ ticked
		Drive{Do: "click", Target: `#__tw_rec button[data-a="pause"]`},
		Drive{Do: "click", Target: "#other"}, // paused: not recorded
		Drive{Do: "click", Target: `#__tw_rec button[data-a="pause"]`},
	)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range acts {
		got = append(got, strings.TrimSpace(a.Do+" "+a.Target+" "+a.Value))
	}
	want := []string{"fill label=Serial number {{serial}}", "check label=Managed", "click #go", "expect #out Onboarded {{serial}}", "expect #go",
		"expect_value label=Serial number {{serial}}", "expect_checked label=Managed true"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("recorded:\n%s\nwant:\n%s\nseen: %v", strings.Join(got, "\n"), strings.Join(want, "\n"), seen)
	}
	if !strings.Contains(strings.Join(seen, ","), "__undo") {
		t.Fatalf("undo not passed on: %v", seen)
	}
}
