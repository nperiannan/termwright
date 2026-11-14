package sim

import (
	"fmt"
	"strings"
)

// Transcripts test a pack (tests/*.txt in it):
//
//	# a comment
//	@switch a model=D100-24T-4X os=demo name=sw1 mgmt=10.99.0.11/24 serial=DM2205-00001
//	@cable a 1 b 1/1
//	@on a                      the next commands go to switch a (each keeps its login)
//	> show version             a command
//	Switch          : …        the output, exactly; "..." skips lines; a line ending in "~" matches its start
//	@prompt * sw1.5 #          the prompt now (exactly, trailing spaces ignored)
//
// RunTranscript returns what did not match.
func RunTranscript(p *Pack, text string) []string {
	var fails []string
	var specs []Spec
	var links []Link
	type step struct {
		on, cmd string
		want    []string
		prompt  string
		line    int
	}
	var steps []step
	on := ""
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		switch {
		case strings.HasPrefix(l, "#") || strings.TrimSpace(l) == "" && (len(steps) == 0 || steps[len(steps)-1].cmd == ""):
		case strings.HasPrefix(l, "@switch "):
			f := strings.Fields(l)
			kv := map[string]string{}
			for _, x := range f[2:] {
				k, v, _ := strings.Cut(x, "=")
				kv[k] = v
			}
			g := Group{Model: kv["model"], OS: kv["os"], Count: 1, Names: kv["name"]}
			mgmt := kv["mgmt"]
			if mgmt == "" {
				mgmt = fmt.Sprintf("10.99.0.%d/24", 11+len(specs))
			}
			ss, _, err := Plan(p, []Group{g}, mgmt, CableNone, "", 0, nil, nil)
			if err != nil || len(ss) != 1 {
				fails = append(fails, fmt.Sprintf("line %d: %v", i+1, err))
				continue
			}
			s := ss[0]
			s.ID = f[1]
			s.Saved.MgmtIP = mgmt
			if kv["name"] != "" {
				s.Saved.Hostname = kv["name"]
			}
			if v := kv["serial"]; v != "" {
				s.Serial = v
			}
			if v := kv["mac"]; v != "" {
				s.MAC = v
			}
			specs = append(specs, s)
			if on == "" {
				on = s.ID
			}
		case strings.HasPrefix(l, "@cable "):
			f := strings.Fields(l)
			if len(f) == 5 {
				links = append(links, Link{A: f[1], APort: f[2], B: f[3], BPort: f[4]})
			}
		case strings.HasPrefix(l, "@on "):
			on = strings.TrimSpace(l[4:])
		case strings.HasPrefix(l, "@prompt "):
			steps = append(steps, step{on: on, prompt: strings.TrimRight(l[8:], " "), line: i + 1})
		case strings.HasPrefix(l, "> "):
			steps = append(steps, step{on: on, cmd: l[2:], line: i + 1})
		default:
			if len(steps) > 0 && steps[len(steps)-1].cmd != "" {
				steps[len(steps)-1].want = append(steps[len(steps)-1].want, l)
			}
		}
	}
	lab := NewLab(p, specs, links)
	shells := map[string]*Shell{}
	bufs := map[string]*stringsWriter{}
	for _, st := range steps {
		d := lab.Device(st.on)
		if d == nil {
			fails = append(fails, fmt.Sprintf("line %d: no switch %q", st.line, st.on))
			continue
		}
		sh := shells[st.on]
		if sh == nil {
			bufs[st.on] = &stringsWriter{}
			sh = newShell(d, bufs[st.on])
			shells[st.on] = sh
		}
		if st.cmd == "" {
			lab.mu.Lock()
			got := strings.TrimRight(sh.prompt(), " ")
			lab.mu.Unlock()
			if got != st.prompt {
				fails = append(fails, fmt.Sprintf("line %d: prompt %q, want %q", st.line, got, st.prompt))
			}
			continue
		}
		bufs[st.on].b = nil
		sh.exec(st.cmd)
		sh.flush()
		got := strings.Split(strings.ReplaceAll(bufs[st.on].String(), "\r\n", "\n"), "\n")
		if msg := matchLines(got, st.want); msg != "" {
			fails = append(fails, fmt.Sprintf("line %d: > %s\n%s\n--- got:\n%s", st.line, st.cmd, msg, strings.Join(got, "\n")))
		}
	}
	return fails
}

// matchLines compares output with the expected lines ("..." skips, "~" at the end matches the start).
func matchLines(got, want []string) string {
	trim := func(a []string) []string {
		for len(a) > 0 && strings.TrimSpace(a[len(a)-1]) == "" {
			a = a[:len(a)-1]
		}
		return a
	}
	got, want = trim(got), trim(want)
	g := 0
	for w := 0; w < len(want); w++ {
		if want[w] == "..." {
			if w+1 == len(want) {
				return ""
			}
			for g < len(got) && !lineMatch(got[g], want[w+1]) {
				g++
			}
			continue
		}
		if g >= len(got) {
			return fmt.Sprintf("missing: %q", want[w])
		}
		if !lineMatch(got[g], want[w]) {
			return fmt.Sprintf("line %d: %q\n   want: %q", g+1, got[g], want[w])
		}
		g++
	}
	if g < len(got) && (len(want) == 0 || want[len(want)-1] != "...") {
		return fmt.Sprintf("more output: %q", got[g])
	}
	return ""
}

func lineMatch(got, want string) bool {
	if strings.HasSuffix(want, "~") {
		return strings.HasPrefix(got, strings.TrimSuffix(want, "~"))
	}
	return got == want
}
