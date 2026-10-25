package term

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// VBScript logon scripts (SecureCRT's original language) run with Windows
// Script Host: the script is wrapped in a .wsf job whose JScript part provides
// the `crt` object (see vbs/termwright_crt.js) and calls back to Termwright
// over HTTP on a local port.

//go:embed vbs/termwright_crt.js
var vbsCrt string

var (
	vbsHeader = regexp.MustCompile(`(?m)^[ \t]*#[ \t]*\$.*$`) // # $language = "VBScript", # $interface = "1.0"
	vbsMain   = regexp.MustCompile(`(?im)^[ \t]*(public[ \t]+|private[ \t]+)?sub[ \t]+main\b`)
)

// decodeScript reads a script file saved as UTF-16 (with BOM), UTF-8 or Windows ANSI.
func decodeScript(b []byte) string {
	switch {
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE, len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		be := b[0] == 0xFE
		u := make([]uint16, 0, len(b)/2)
		for i := 2; i+1 < len(b); i += 2 {
			if be {
				u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
			} else {
				u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
			}
		}
		return string(utf16.Decode(u))
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return string(b[3:])
	case utf8.Valid(b):
		return string(b)
	}
	r := make([]rune, len(b)) // ANSI: Latin-1 is close enough for scripts
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// buildWSF wraps a VBScript in a Windows Script Host job with the crt object.
func buildWSF(code string) string {
	code = vbsHeader.ReplaceAllString(code, "")
	cdata := func(s string) string {
		return "<![CDATA[\n" + strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>") + "\n]]>"
	}
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\" ?>\n<job id=\"termwright\">\n")
	b.WriteString("<script language=\"JScript\">" + cdata(vbsCrt) + "</script>\n")
	b.WriteString("<script language=\"VBScript\">" + cdata(code) + "</script>\n")
	if vbsMain.MatchString(code) {
		b.WriteString("<script language=\"VBScript\">" + cdata("Main") + "</script>\n")
	}
	b.WriteString("</job>\n")
	return b.String()
}

// runVBScript runs a .vbs file against a tab (Windows only).
func (m *Manager) runVBScript(tabID, file string, args []string) (string, error) {
	if runtime.GOOS != "windows" {
		return "", errors.New("VBScript needs Windows Script Host - on macOS and Linux use a Python (.py) or JavaScript (.js) script instead")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	wsf := buildWSF(decodeScript(data))
	return m.runBridge(tabID, file, true, func(ctx context.Context, dir string) (*exec.Cmd, error) {
		job := filepath.Join(dir, strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))+".wsf")
		if err := os.WriteFile(job, []byte(wsf), 0o600); err != nil {
			return nil, err
		}
		cscript := filepath.Join(os.Getenv("WINDIR"), "System32", "cscript.exe")
		if _, err := os.Stat(cscript); err != nil {
			cscript = "cscript.exe"
		}
		cmd := exec.CommandContext(ctx, cscript, append([]string{"//Nologo", job}, args...)...)
		cmd.Env = append(cmd.Environ(), "TERMWRIGHT_SCRIPT="+file)
		return cmd, nil
	})
}

// bridgeState is sent with each HTTP reply so the script's property-style
// members (CurrentRow, Connected, …) stay current.
func (h *pyHandler) bridgeState(op, tabID string) map[string]any {
	o, err := h.obj(tabID)
	if err != nil {
		return nil
	}
	t := o.tab
	t.mu.Lock()
	s, rows, cols := t.sess, t.rows, t.cols
	t.mu.Unlock()
	st := map[string]any{"tab": t.id, "connected": t.isConnected(), "rows": rows, "cols": cols,
		"path": s.Path, "host": s.Hostname, "port": s.Port, "caption": title(s)}
	switch op {
	case "send", "wait", "waitany", "sleep", "screen", "connect", "version":
		if sn := h.m.screen(t.id); sn != nil {
			st["row"], st["col"], st["rows"], st["cols"] = sn.Row, sn.Col, sn.Rows, sn.Cols
		}
	}
	return st
}

// serveHTTPBridge serves the VBScript crt object: POST /rpc with the token header.
func serveHTTPBridge(ctx context.Context, ln net.Listener, token string, h *pyHandler) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("X-Termwright-Token") != token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var req pyReq
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		res, err := h.handle(req)
		reply := map[string]any{"id": req.ID, "result": res}
		if err != nil {
			reply["error"] = err.Error()
		}
		var a struct {
			Tab string `json:"tab"`
		}
		_ = json.Unmarshal(req.Args, &a)
		if req.Op == "connect" {
			if id, ok := res.(string); ok {
				a.Tab = id
			}
		}
		reply["st"] = h.bridgeState(req.Op, a.Tab)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(reply)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	_ = srv.Serve(ln)
}
