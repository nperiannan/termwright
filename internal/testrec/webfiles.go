package testrec

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

var webFileKinds = []string{"picture: ", "screenshot: ", "video: ", "trace: "}

// moveWebFiles moves the pictures, screenshots, videos and traces of the web steps into the run's folder
// (<run>/web/) and makes their lines in the output relative to it, so the report shows them and the run
// folder can be shared on its own.
func moveWebFiles(rep *RunReport, abs string) {
	dir := filepath.Join(abs, "web")
	made := false
	for ri := range rep.Results {
		for si := range rep.Results[ri].Steps {
			st := &rep.Results[ri].Steps[si]
			if !strings.Contains(st.Output, ": /") && !strings.Contains(st.Output, `:\`) {
				continue
			}
			lines := strings.Split(st.Output, "\n")
			for i, ln := range lines {
				for _, k := range webFileKinds {
					p, ok := strings.CutPrefix(ln, k)
					if !ok {
						continue
					}
					p, rest, _ := strings.Cut(p, "  ")
					if !filepath.IsAbs(p) {
						continue
					}
					if !made {
						if os.MkdirAll(dir, 0o755) != nil {
							return
						}
						made = true
					}
					dst := filepath.Join(dir, filepath.Base(p))
					if os.Rename(p, dst) != nil {
						continue
					}
					lines[i] = k + "web/" + filepath.Base(p)
					if rest != "" {
						lines[i] += "  " + rest
					}
				}
			}
			st.Output = strings.Join(lines, "\n")
		}
	}
}

// webMedia is the HTML of a step's pictures (a strip of thumbnails), failure screenshot and video.
func webMedia(out string) string {
	var pics, more strings.Builder
	n := 0
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "healed ") {
			fmt.Fprintf(&more, `<div class="warn">⚠ %s - the target was not found, a fallback was (update the test)</div>`, html.EscapeString(ln))
		}
		for _, k := range webFileKinds {
			p, ok := strings.CutPrefix(ln, k)
			if !ok || !strings.HasPrefix(p, "web/") {
				continue
			}
			p, _, _ = strings.Cut(p, "  ")
			e := html.EscapeString(p)
			switch k {
			case "picture: ":
				n++
				fmt.Fprintf(&pics, `<a href="%s" target="_blank"><img src="%s" loading="lazy" title="after action %d"></a>`, e, e, n)
			case "screenshot: ":
				fmt.Fprintf(&more, `<a href="%s" target="_blank"><img class="fail" src="%s" title="when it failed"></a>`, e, e)
			case "video: ":
				fmt.Fprintf(&more, `<video src="%s" controls preload="none"></video>`, e)
			case "trace: ":
				fmt.Fprintf(&more, `<a class="mut" href="%s">trace</a> `, e)
			}
		}
	}
	if pics.Len() == 0 && more.Len() == 0 {
		return ""
	}
	return `<div class="shots">` + pics.String() + more.String() + `</div>`
}
