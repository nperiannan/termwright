package sim

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// The command line: a small line editor (echo, backspace, Ctrl+C, Ctrl+U,
// history with the arrow keys, Tab completion, ? help) and a matcher that
// takes abbreviations ("sh sw") the way the switches do.

type args map[string]string

type cmdDef struct {
	pat   []string // words; "<x>" = one value, "<x...>" = the rest of the line
	modes string   // the modes it is in ("" = every)
	help  string
	cmd   *CommandDef
	idx   int
}

// Shell is one login on a switch.
type Shell struct {
	d       *Device
	w       *bufio.Writer
	out     strings.Builder
	defs    []cmdDef
	mode    string // the pack's modes
	ifPorts []string
	ifVLAN  int
	cmdNo   int
	hist    []string
	ask     func(answer string) // a question waits for y/N
	askText string
	quit    bool
	paging  bool     // a long output stops after a screen (clipaging / terminal more)
	rows    int      // terminal height
	pend    []string // the rest of a long output, waiting for a key
	tty     bool     // an interactive login (paging applies)
	ping    *PingView
	err     ErrView
	args    args
	sel     []string
}

func newShell(d *Device, w io.Writer) *Shell {
	s := &Shell{d: d, w: bufio.NewWriter(w), cmdNo: 1, mode: d.osd.StartMode, paging: true, rows: 24}
	for i := range d.osd.Commands {
		c := &d.osd.Commands[i]
		s.defs = append(s.defs, cmdDef{pat: strings.Fields(c.Cmd), modes: c.Modes, help: c.Help, cmd: c, idx: i})
	}
	return s
}

// p adds output (\n becomes \r\n when flushed).
func (s *Shell) p(f string, v ...any) {
	fmt.Fprintf(&s.out, f, v...)
}

func (s *Shell) flush() {
	t := strings.ReplaceAll(s.out.String(), "\r\n", "\n")
	s.out.Reset()
	if s.tty && s.paging && s.rows > 3 {
		lines := strings.SplitAfter(t, "\n")
		if len(lines) > s.rows-1 {
			s.pend = lines[s.rows-1:]
			t = strings.Join(lines[:s.rows-1], "")
			_, _ = s.w.WriteString(strings.ReplaceAll(t, "\n", "\r\n") + s.morePrompt())
			_ = s.w.Flush()
			return
		}
	}
	_, _ = s.w.WriteString(strings.ReplaceAll(t, "\n", "\r\n"))
	_ = s.w.Flush()
}

func (s *Shell) morePrompt() string {
	if t := s.render("more", nil); t != "" {
		return t
	}
	return "--More--"
}

// more shows the next part of a long output: a screen (space), a line (Enter) or nothing (q).
func (s *Shell) more(c rune) {
	s.write("\r" + strings.Repeat(" ", len(s.morePrompt())) + "\r")
	n := 0
	switch c {
	case ' ':
		n = s.rows - 1
	case '\r', '\n':
		n = 1
	default:
		s.pend = nil
	}
	if n > len(s.pend) {
		n = len(s.pend)
	}
	t := strings.Join(s.pend[:n], "")
	s.pend = s.pend[n:]
	s.write(strings.ReplaceAll(t, "\n", "\r\n"))
	if len(s.pend) > 0 {
		s.write(s.morePrompt())
		return
	}
	if !strings.HasSuffix(t, "\n") && t != "" {
		s.write("\r\n")
	}
	s.write(s.prompt())
}

func (s *Shell) write(t string) { _, _ = s.w.WriteString(t); _ = s.w.Flush() }

func (s *Shell) prompt() string {
	if s.ask != nil {
		return s.askText
	}
	return s.render("prompt:"+s.mode, nil)
}

// Run reads keystrokes until the user logs out or the input ends.
func (s *Shell) Run(r io.Reader) {
	s.tty = true
	s.p("%s", s.render("banner", nil))
	s.flush()
	s.write(s.prompt())
	br := bufio.NewReader(r)
	var line []rune
	hi := -1
	lastCR := false
	for !s.quit {
		c, _, err := br.ReadRune()
		if err != nil {
			return
		}
		if c == '\n' && lastCR {
			lastCR = false
			continue
		}
		lastCR = c == '\r'
		if len(s.pend) > 0 {
			s.more(c)
			continue
		}
		switch {
		case c == '\r' || c == '\n':
			s.write("\r\n")
			text := string(line)
			line, hi = nil, -1
			s.exec(text)
			s.flush()
			if !s.quit && len(s.pend) == 0 {
				s.write(s.prompt())
			}
		case c == 3: // Ctrl+C
			s.write("^C\r\n")
			line, s.ask = nil, nil
			if to := s.d.osd.CtrlCTo[s.mode]; to != "" {
				s.mode, s.ifPorts, s.ifVLAN = to, nil, 0
			}
			s.write(s.prompt())
		case c == 21: // Ctrl+U
			s.write(strings.Repeat("\b \b", len(line)))
			line = nil
		case c == 127 || c == 8:
			if len(line) > 0 {
				line = line[:len(line)-1]
				s.write("\b \b")
			}
		case c == 27: // arrows: ESC [ A/B/C/D
			b1, _, _ := br.ReadRune()
			b2, _, _ := br.ReadRune()
			if b1 != '[' || (b2 != 'A' && b2 != 'B') || len(s.hist) == 0 {
				continue
			}
			if b2 == 'A' && hi < len(s.hist)-1 {
				hi++
			} else if b2 == 'B' && hi >= 0 {
				hi--
			}
			s.write(strings.Repeat("\b \b", len(line)))
			line = nil
			if hi >= 0 {
				line = []rune(s.hist[len(s.hist)-1-hi])
			}
			s.write(string(line))
		case c == '\t':
			if add := s.complete(string(line)); add != "" {
				line = append(line, []rune(add)...)
				s.write(add)
			}
		case c == '?' && s.ask == nil:
			s.write("?\r\n")
			s.help(string(line))
			s.flush()
			s.write(s.prompt() + string(line))
		case c >= 32 && c != utf8.RuneError:
			line = append(line, c)
			s.write(string(c))
		}
	}
}

// exec runs one line (a command, or the answer to a question).
func (s *Shell) exec(text string) {
	if s.ask != nil {
		f := s.ask
		s.ask = nil
		f(strings.TrimSpace(text))
		return
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return
	}
	if len(s.hist) == 0 || s.hist[len(s.hist)-1] != text {
		s.hist = append(s.hist, text)
	}
	s.d.lab.mu.Lock()
	defer s.d.lab.mu.Unlock()
	if s.template(words) {
		s.counted()
		return
	}
	def, a, failAt, why := s.match(words)
	if def == nil && why == "" && s.acceptConfig(text, words) {
		s.counted()
		return
	}
	if def == nil {
		s.fail(text, words, failAt, why)
		return
	}
	s.runCommand(def, a)
	s.counted()
}

func (s *Shell) counted() {
	if s.d.osd.Count {
		s.cmdNo++
	}
}

func (s *Shell) inMode(d *cmdDef) bool { return d.modes == "" || strings.Contains(d.modes, s.mode) }

// match finds the command for the words. why: "incomplete", "ambiguous" or "".
func (s *Shell) match(words []string) (*cmdDef, args, int, string) {
	type cand struct {
		d     *cmdDef
		a     args
		exact int
	}
	var cands []cand
	failAt, incomplete := 0, false
	for i := range s.defs {
		d := &s.defs[i]
		if !s.inMode(d) {
			continue
		}
		a, k, exact, done := args{}, 0, 0, false
		ok := true
		for j, p := range d.pat {
			if strings.HasSuffix(p, "...>") {
				if j >= len(words) {
					ok = false
					incomplete = incomplete || k == len(words)
					break
				}
				a[strings.Trim(p, "<.>")] = strings.Join(words[j:], " ")
				k, done = len(words), true
				break
			}
			if j >= len(words) {
				ok = false
				incomplete = incomplete || k == len(words)
				break
			}
			w := words[j]
			if strings.HasPrefix(p, "<") {
				a[strings.Trim(p, "<>")] = w
				k++
				continue
			}
			if !strings.HasPrefix(strings.ToLower(p), strings.ToLower(w)) {
				ok = false
				break
			}
			if strings.EqualFold(p, w) {
				exact++
			}
			k++
		}
		if k > failAt {
			failAt = k
		}
		if ok && (done || k == len(words)) && (done || len(d.pat) == len(words)) {
			cands = append(cands, cand{d, a, exact})
		}
	}
	if len(cands) == 0 {
		if incomplete && failAt >= len(words) {
			return nil, nil, failAt, "incomplete"
		}
		return nil, nil, failAt, ""
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].exact > cands[j].exact })
	if len(cands) > 1 && cands[0].exact == cands[1].exact && litKey(cands[0].d) != litKey(cands[1].d) {
		return nil, nil, len(words), "ambiguous"
	}
	return cands[0].d, cands[0].a, 0, ""
}

func litKey(d *cmdDef) string {
	var b []string
	for _, p := range d.pat {
		if !strings.HasPrefix(p, "<") {
			b = append(b, p)
		}
	}
	return strings.Join(b, " ")
}

func (s *Shell) fail(text string, words []string, at int, why string) {
	a := args{"line": strings.TrimSpace(text)}
	switch why {
	case "incomplete", "ambiguous":
		s.p("%s", s.render("error:"+why, a))
		return
	}
	col := len(s.prompt())
	off := 0
	for i := 0; i <= at && i < len(words); i++ {
		j := strings.Index(text[off:], words[i])
		if i == at {
			col += off + j
			break
		}
		off += j + len(words[i])
	}
	if at >= len(words) {
		col += len(text)
	}
	a["caret"] = strings.Repeat(" ", col)
	s.p("%s", s.render("error:invalid", a))
}

// next: the words that can come after the typed ones (help and Tab).
func (s *Shell) next(line string) (cands [][2]string, partial string, complete bool) {
	words := strings.Fields(line)
	if !strings.HasSuffix(line, " ") && len(words) > 0 {
		partial = words[len(words)-1]
		words = words[:len(words)-1]
	}
	seen := map[string]bool{}
	for i := range s.defs {
		d := &s.defs[i]
		if !s.inMode(d) || len(d.pat) < len(words) {
			continue
		}
		ok := true
		for j, w := range words {
			p := d.pat[j]
			if strings.HasSuffix(p, "...>") {
				break
			}
			if !strings.HasPrefix(p, "<") && !strings.HasPrefix(strings.ToLower(p), strings.ToLower(w)) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if len(d.pat) == len(words) && partial == "" {
			complete = true
			continue
		}
		if len(d.pat) <= len(words) {
			continue
		}
		p := d.pat[len(words)]
		if strings.HasPrefix(p, "<") || strings.HasPrefix(strings.ToLower(p), strings.ToLower(partial)) {
			if !seen[p] {
				seen[p] = true
				cands = append(cands, [2]string{p, d.help})
			}
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i][0] < cands[j][0] })
	return cands, partial, complete
}

func (s *Shell) help(line string) {
	cands, _, complete := s.next(line)
	for _, c := range cands {
		s.p("  %-22s %s\n", c[0], c[1])
	}
	if complete {
		s.p("  <cr>\n")
	}
	if len(cands) == 0 && !complete {
		s.p("%s", s.render("error:unrecognized", nil))
	}
}

func (s *Shell) complete(line string) string {
	cands, partial, _ := s.next(line)
	var lits []string
	for _, c := range cands {
		if !strings.HasPrefix(c[0], "<") {
			lits = append(lits, c[0])
		}
	}
	if len(lits) != 1 || partial == "" {
		return ""
	}
	return lits[0][len(partial):] + " "
}
