package webrun

import "testing"

func TestLeftovers(t *testing.T) {
	ps := parsePS(`  100     1 Tue Nov 24 09:00:00 2026 /Applications/Termwright.app/Contents/MacOS/Termwright
  200   100 Tue Nov 24 10:00:00 2026 /usr/local/bin/node /Users/u/Library/Termwright/web/runner.js
  201   200 Tue Nov 24 10:00:01 2026 /Users/u/Library/Caches/ms-playwright/chromium-1/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing --user-data-dir=/var/folders/x/playwright_chromiumdev_profile-AAA --remote-debugging-pipe
  202   201 Tue Nov 24 10:00:02 2026 /Users/u/.../Google Chrome for Testing Helper (Renderer).app/x --type=renderer --user-data-dir=/var/folders/x/playwright_chromiumdev_profile-AAA
  300     1 Tue Nov 24 10:42:01 2026 /Users/u/Library/Caches/ms-playwright/chromium-1/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing --user-data-dir=/var/folders/x/playwright_chromiumdev_profile-BBB
  301   300 Tue Nov 24 10:42:02 2026 /x/Helper --type=gpu-process --user-data-dir=/var/folders/x/playwright_chromiumdev_profile-BBB
  400     1 Tue Nov 24 11:05:00 2026 /usr/local/bin/node /Users/u/Library/Termwright/web/runner.js
  401   400 Tue Nov 24 11:05:01 2026 /Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge --user-data-dir=/tmp/playwright_chromiumdev_profile-CCC
  500     1 Tue Nov 24 08:00:00 2026 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome
`)
	got := leftovers(ps, 100, "/Users/u/Library/Termwright/web")
	if len(got) != 3 || got[0].PID != 300 || got[0].Name != "Chrome for Testing" || got[0].Started != "2026-11-24 10:42" || got[1].PID != 400 || got[1].Name != "web runner" || got[2].Name != "Microsoft Edge" {
		t.Fatalf("%+v", got)
	}
	w := parseWin("12\t4\t2026-11-24 10:42\tC:\\x\\chrome.exe --user-data-dir=C:\\Temp\\playwright_chromiumdev_profile-D\r\n")
	if l := leftovers(w, 99, ""); len(l) != 1 || l[0].Name != "Chrome" {
		t.Fatalf("%+v", l)
	}
}
