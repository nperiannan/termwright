package term

import (
	"bufio"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/session"
)

// Python scripts (SecureCRT style): .py files run with the user's Python 3.
// A small module provides the `crt` object; its calls come back to Termwright
// as JSON lines over a localhost socket protected by a random token.

//go:embed python/termwright_crt.py python/termwright_run.py
var pyFiles embed.FS

// Version is reported to scripts as crt.Version (set by the app).
var Version = "dev"

// findPython returns the interpreter command (and leading arguments).
func (m *Manager) findPython() ([]string, error) {
	if m.PythonPath != nil {
		if p := strings.TrimSpace(m.PythonPath()); p != "" {
			return []string{expandHome(p)}, nil
		}
	}
	cands := [][]string{{"python3"}, {"python"}}
	if runtime.GOOS == "windows" {
		cands = [][]string{{"py", "-3"}, {"python3"}, {"python"}}
	}
	for _, c := range cands {
		if p, err := exec.LookPath(c[0]); err == nil {
			return append([]string{p}, c[1:]...), nil
		}
	}
	return nil, errors.New("python 3 was not found - install it (python.org) or set its path in Global Options → General → Python interpreter")
}

type pyReq struct {
	ID   int             `json:"id"`
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args"`
}

// runPython runs a Python script against a tab; progress is reported with
// the same "script:status" / "script:log" events as JavaScript scripts.
func (m *Manager) runPython(tabID, file string, args []string) (string, error) {
	py, err := m.findPython()
	if err != nil {
		return "", err
	}
	return m.runBridge(tabID, file, false, func(ctx context.Context, dir string) (*exec.Cmd, error) {
		for _, n := range []string{"termwright_crt.py", "termwright_run.py"} {
			data, _ := pyFiles.ReadFile("python/" + n)
			if err := os.WriteFile(filepath.Join(dir, n), data, 0o600); err != nil {
				return nil, err
			}
		}
		cmdArgs := append(append(py[1:], "-u", filepath.Join(dir, "termwright_run.py"), file), args...)
		cmd := exec.CommandContext(ctx, py[0], cmdArgs...)
		cmd.Env = append(cmd.Environ(), "PYTHONIOENCODING=utf-8")
		return cmd, nil
	})
}

// runBridge runs a script process whose `crt` object calls back into
// Termwright: JSON lines over a localhost socket (Python), or HTTP POSTs on
// the same kind of socket (VBScript, which has no sockets of its own).
// Calls carry a random token.
func (m *Manager) runBridge(tabID, file string, httpMode bool, launch func(ctx context.Context, dir string) (*exec.Cmd, error)) (string, error) {
	t, err := m.get(tabID)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "termwright-script-")
	if err != nil {
		return "", err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	tok := make([]byte, 16)
	_, _ = rand.Read(tok)
	token := hex.EncodeToString(tok)

	ctx, cancel := context.WithCancel(context.Background())
	run := &scriptRun{id: newID(), tabID: tabID, name: filepath.Base(file), ctx: ctx, cancel: cancel}
	main := m.newTermObj(run, t)
	objs := map[string]*termObj{tabID: main}
	var objMu sync.Mutex

	cmd, err := launch(ctx, dir)
	if err != nil {
		cancel()
		ln.Close()
		os.RemoveAll(dir)
		return "", err
	}
	cmd.Dir = filepath.Dir(file)
	cmd.Env = append(cmd.Environ(), "TERMWRIGHT_RPC="+ln.Addr().String(), "TERMWRIGHT_TOKEN="+token, "TERMWRIGHT_TAB="+tabID)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		cancel()
		ln.Close()
		os.RemoveAll(dir)
		return "", fmt.Errorf("start %s: %w", filepath.Base(cmd.Path), err)
	}

	m.mu.Lock()
	m.scripts[run.id] = run
	m.mu.Unlock()
	status := func(state, msg string) {
		m.emit.Emit("script:status", map[string]string{"runId": run.id, "tabId": tabID, "name": run.name, "state": state, "message": msg})
	}
	status("running", "")

	var errTail []string
	var outWG sync.WaitGroup
	outWG.Add(2)
	go func() { // print() output -> notifications
		defer outWG.Done()
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			m.emit.Emit("script:log", map[string]string{"runId": run.id, "tabId": tabID, "message": sc.Text()})
		}
	}()
	go func() { // keep the end of stderr for the error message
		defer outWG.Done()
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			errTail = append(errTail, sc.Text())
			if len(errTail) > 30 {
				errTail = errTail[1:]
			}
		}
	}()
	h := &pyHandler{m: m, run: run, main: main, objs: objs, mu: &objMu}
	if httpMode {
		go serveHTTPBridge(ctx, ln, token, h)
	} else {
		go servePyBridge(ctx, ln, token, h)
	}
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	go func() {
		outWG.Wait()
		werr := cmd.Wait()
		defer func() {
			cancel()
			objMu.Lock()
			for _, o := range objs {
				o.unsub()
			}
			objMu.Unlock()
			m.mu.Lock()
			delete(m.scripts, run.id)
			m.mu.Unlock()
			os.RemoveAll(dir)
		}()
		switch {
		case ctx.Err() != nil:
			status("cancelled", "")
		case werr != nil:
			msg := werr.Error()
			for i := len(errTail) - 1; i >= 0; i-- { // last line of the traceback, e.g. "NameError: ..."
				if s := strings.TrimSpace(errTail[i]); s != "" {
					msg = s
					break
				}
			}
			status("error", msg)
		default:
			status("done", "")
		}
	}()
	return run.id, nil
}

// servePyBridge serves the Python crt module: one connection, JSON lines.
func servePyBridge(ctx context.Context, ln net.Listener, token string, h *pyHandler) {
	stop := context.AfterFunc(ctx, func() { ln.Close() })
	defer stop()
	conn, err := ln.Accept()
	ln.Close()
	if err != nil {
		return
	}
	defer conn.Close()
	context.AfterFunc(ctx, func() { conn.Close() })
	r := bufio.NewReader(conn)
	first, err := r.ReadString('\n')
	var hello struct{ Token string }
	if err != nil || json.Unmarshal([]byte(first), &hello) != nil || hello.Token != token {
		return
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		var req pyReq
		if json.Unmarshal([]byte(line), &req) != nil {
			return
		}
		res, err := h.handle(req)
		reply := map[string]any{"id": req.ID, "result": res}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			reply["error"] = err.Error()
		}
		b, _ := json.Marshal(reply)
		if _, err := conn.Write(append(b, '\n')); err != nil {
			return
		}
	}
}

type pyHandler struct {
	m    *Manager
	run  *scriptRun
	main *termObj
	objs map[string]*termObj
	mu   *sync.Mutex
}

func (h *pyHandler) obj(id string) (*termObj, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if id == "" {
		return h.main, nil
	}
	if o, ok := h.objs[id]; ok {
		return o, nil
	}
	t, err := h.m.get(id)
	if err != nil {
		return nil, errors.New("the tab is closed")
	}
	o := h.m.newTermObj(h.run, t)
	h.objs[id] = o
	return o, nil
}

func secs(f float64) time.Duration { return time.Duration(f * float64(time.Second)) }

func (h *pyHandler) handle(req pyReq) (any, error) {
	var a struct {
		Tab      string   `json:"tab"`
		Text     string   `json:"text"`
		Patterns []string `json:"patterns"`
		Timeout  float64  `json:"timeout"`
		CI       bool     `json:"ci"`
		Message  string   `json:"message"`
		Title    string   `json:"title"`
		Default  string   `json:"default"`
		Secret   bool     `json:"secret"`
		Buttons  int      `json:"buttons"`
		Cmd      string   `json:"cmd"`
		Start    bool     `json:"start"`
		Append   bool     `json:"append"`
		Raw      bool     `json:"raw"`
		File     string   `json:"file"`
		MS       int      `json:"ms"`
	}
	_ = json.Unmarshal(req.Args, &a)
	m, ctx := h.m, h.run.ctx
	switch req.Op {
	case "version":
		return Version, nil
	case "sleep":
		select {
		case <-time.After(time.Duration(a.MS) * time.Millisecond):
		case <-ctx.Done():
		}
		return nil, nil
	case "tabs":
		m.mu.Lock()
		list := make([]*Tab, 0, len(m.tabs))
		for _, t := range m.tabs {
			list = append(list, t)
		}
		m.mu.Unlock()
		sort.Slice(list, func(i, j int) bool { return list[i].seq < list[j].seq })
		ids := make([]string, len(list))
		for i, t := range list {
			ids[i] = t.id
		}
		return ids, nil
	case "prompt":
		title := a.Title
		if title == "" {
			title = "Script: " + h.run.name
		}
		r, err := m.prompts.Ask(ctx, Prompt{TabID: h.run.tabID, Kind: "credentials", Title: title, Message: a.Message,
			Fields: []PromptField{{Label: "Value", Secret: a.Secret, Value: a.Default}}})
		if err != nil {
			return nil, nil // Cancel -> None
		}
		return r.Values[0], nil
	case "msgbox":
		choices := map[int][]string{0: {"OK"}, 1: {"OK", "Cancel"}, 2: {"Retry", "Ignore", "Abort"}, 3: {"Yes", "No", "Cancel"},
			4: {"Yes", "No"}, 5: {"Retry", "Cancel"}}[a.Buttons&0xF]
		if choices == nil {
			choices = []string{"OK"}
		}
		ids := map[string]int{"OK": 1, "Cancel": 2, "Abort": 3, "Retry": 4, "Ignore": 5, "Yes": 6, "No": 7}
		title := a.Title
		if title == "" {
			title = "Script: " + h.run.name
		}
		buttons := choices
		if choices[len(choices)-1] == "Cancel" {
			buttons = choices[:len(choices)-1] // the dialog has its own Cancel button
		}
		r, err := m.prompts.Ask(ctx, Prompt{TabID: h.run.tabID, Kind: "confirm", Title: title, Message: a.Message,
			Choices: buttons, Danger: a.Buttons&0xF0 == 16})
		if err != nil {
			if len(choices) == 2 && choices[1] == "No" {
				return 7, nil
			}
			return 2, nil
		}
		if id, ok := ids[r.Choice]; ok {
			return id, nil
		}
		return 1, nil
	case "connect":
		return h.connect(a.Cmd)
	}

	o, err := h.obj(a.Tab)
	if err != nil {
		return nil, err
	}
	t := o.tab
	switch req.Op {
	case "send":
		return nil, t.rawWrite([]byte(a.Text))
	case "break":
		return nil, m.SendBreak(t.id)
	case "wait":
		res := make([]*regexp.Regexp, len(a.Patterns))
		for i, p := range a.Patterns {
			q := regexp.QuoteMeta(p)
			if a.CI {
				q = "(?i)" + q
			}
			res[i] = regexp.MustCompile(q)
		}
		i, before, err := o.wait(res, secs(a.Timeout))
		if err != nil {
			return nil, err
		}
		return map[string]any{"index": i, "text": before}, nil
	case "waitany":
		i, _, err := o.wait([]*regexp.Regexp{regexp.MustCompile(`(?s).`)}, secs(a.Timeout))
		return i == 0, err
	case "screen":
		o.mu.Lock()
		text := o.recent
		o.mu.Unlock()
		t.mu.Lock()
		rows, cols := t.rows, t.cols
		t.mu.Unlock()
		res := map[string]any{"text": text, "rows": rows, "cols": cols}
		if sn := m.screen(t.id); sn != nil { // the real screen, as shown in the terminal
			res["lines"], res["row"], res["col"], res["rows"], res["cols"] = sn.Lines, sn.Row, sn.Col, sn.Rows, sn.Cols
		}
		return res, nil
	case "connected":
		return t.isConnected(), nil
	case "session":
		t.mu.Lock()
		s, logging, logFile := t.sess, t.logger != nil, ""
		if t.logger != nil {
			logFile = t.logger.Path()
		}
		t.mu.Unlock()
		return map[string]any{"path": s.Path, "name": s.Name, "host": s.Hostname, "port": s.Port, "user": s.Username,
			"protocol": s.Protocol, "logging": logging, "logfile": logFile, "caption": title(s)}, nil
	case "disconnect", "close":
		return nil, m.Disconnect(t.id)
	case "activate":
		m.emit.Emit("tab:activate", t.id)
		return nil, nil
	case "log":
		if !a.Start {
			return "", m.StopLog(t.id)
		}
		return m.StartLog(t.id, a.File, a.Append, a.Raw)
	}
	return nil, fmt.Errorf("crt: %s is not supported", req.Op)
}

// connect opens a tab from SecureCRT command-line arguments ("/S <session>",
// "/SSH2 /L user /P port host", "/TELNET host [port]", "/SERIAL COM1 /BAUD 9600")
// and waits until it is connected.
func (h *pyHandler) connect(cmdline string) (string, error) {
	m := h.m
	w := splitArgs(cmdline)
	s := session.Defaults()
	s.AutoReconnect, s.LogOnConnect = false, false
	var pos []string
	for i := 0; i < len(w); i++ {
		next := func() string {
			if i+1 < len(w) {
				i++
				return w[i]
			}
			return ""
		}
		switch strings.ToUpper(w[i]) {
		case "/S":
			if m.LoadSession == nil {
				return "", errors.New("sessions are not available")
			}
			ls, err := m.LoadSession(next())
			if err != nil {
				return "", err
			}
			s = ls
		case "/SSH2", "/SSH1":
			s.Protocol, s.Port = session.ProtoSSH2, 22
		case "/TELNET":
			s.Protocol, s.Port = session.ProtoTelnet, 23
		case "/SERIAL":
			s.Protocol, s.ComPort, s.Port = session.ProtoSerial, next(), 0
		case "/RAW":
			s.Protocol = session.ProtoRaw
		case "/L":
			s.Username = next()
		case "/P":
			s.Port, _ = strconv.Atoi(next())
		case "/BAUD":
			s.BaudRate, _ = strconv.Atoi(next())
		case "/I":
			s.IdentityFile = next()
		case "/PASSWORD", "/AUTH", "/C", "/M", "/ACCEPTHOSTKEYS", "/ENCRYPTEDPASSWORD":
			if u := strings.ToUpper(w[i]); u != "/ACCEPTHOSTKEYS" {
				next() // not used: Termwright asks for passwords itself
			}
		default:
			pos = append(pos, w[i])
		}
	}
	if len(pos) > 0 && s.Hostname == "" && s.Protocol != session.ProtoSerial {
		host := pos[0]
		if u, hh, ok := strings.Cut(host, "@"); ok {
			s.Username, host = u, hh
		}
		s.Hostname = host
		if len(pos) > 1 {
			s.Port, _ = strconv.Atoi(pos[1])
		}
	}
	if s.Name == "" {
		s.Name = s.Hostname
		if s.Name == "" {
			s.Name = s.ComPort
		}
	}
	ti := m.Create(s)
	m.emit.Emit("tab:opened", ti) // the UI attaches a terminal and starts it
	t, _ := m.get(ti.ID)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if t.isConnected() {
			return ti.ID, nil
		}
		t.mu.Lock()
		st, info := t.state, t.info
		t.mu.Unlock()
		if st == StateError {
			return "", fmt.Errorf("connect failed: %s", info)
		}
		select {
		case <-h.run.ctx.Done():
			return "", ErrCancelled
		case <-time.After(150 * time.Millisecond):
		}
	}
	return "", errors.New("connect timed out")
}

// screen returns the tab's rendered screen, or nil.
func (m *Manager) screen(tabID string) *ScreenSnap {
	if m.ScreenSnapshot == nil {
		return nil
	}
	return m.ScreenSnapshot(tabID)
}
