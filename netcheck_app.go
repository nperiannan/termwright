package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Network Check (Transfer → Network Check…): reachability from this computer
// (the device tabs run their own ping in the frontend) and iPerf3 throughput.
// Everything here only reads: ping, DNS, a TCP connect, traceroute.

// NCResult is one cell of the matrix for this computer.
type NCResult struct {
	Target  string  `json:"target"`  // as asked: "gateway", "dns", "internet", "10.20.0.112", "host:443"
	Address string  `json:"address"` // what was checked (the gateway's address, the resolved IP)
	State   string  `json:"state"`   // "ok", "bad", "warn", "na"
	Summary string  `json:"summary"` // "3.4 ms", "0 of 4", "ping blocked, TCP 443 open"
	Ms      float64 `json:"ms"`
	Detail  string  `json:"detail"` // the commands and their output
}

// ncRun runs a program (tests replace it).
var ncRun = func(ctx context.Context, name string, args ...string) (string, error) {
	return runOutput(ctx, name, args...)
}

// NetCheckLocal checks every target from this computer, at the same time.
func (a *App) NetCheckLocal(targets []string) []NCResult {
	out := make([]NCResult, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			out[i] = ncCheck(ctx, runtime.GOOS, strings.TrimSpace(t))
		}(i, t)
	}
	wg.Wait()
	return out
}

func ncCheck(ctx context.Context, goos, t string) NCResult {
	r := NCResult{Target: t, State: "na"}
	switch strings.ToLower(t) {
	case "gateway":
		gw := defaultGateway(ctx, goos)
		if gw == "" {
			r.State, r.Summary = "bad", "no default gateway"
			return r
		}
		r.Address = gw
		ok, ms, sum, det := ping(ctx, goos, gw)
		r.Detail = det
		if ok {
			r.State, r.Ms, r.Summary = "ok", ms, fmtMs(ms)
		} else {
			r.State, r.Summary = "bad", sum
		}
		return r
	case "dns":
		start := time.Now()
		ips, err := net.DefaultResolver.LookupHost(ctx, "example.com")
		ms := float64(time.Since(start).Microseconds()) / 1000
		if err != nil || len(ips) == 0 {
			r.State, r.Summary, r.Detail = "bad", "names do not resolve", fmt.Sprint(err)
			return r
		}
		r.State, r.Ms, r.Summary, r.Detail = "ok", ms, fmtMs(ms), "example.com → "+strings.Join(ips, ", ")
		return r
	case "internet":
		r.Address = "1.1.1.1"
		ms, err := tcpTime(ctx, "1.1.1.1:443")
		ok, pms, _, det := ping(ctx, goos, "1.1.1.1")
		r.Detail = det
		switch {
		case err == nil && ok:
			r.State, r.Ms, r.Summary = "ok", pms, fmtMs(pms)
		case err == nil:
			r.State, r.Ms, r.Summary = "ok", ms, fmtMs(ms)+" (ping blocked, TCP 443 open)"
		default:
			r.State, r.Summary = "bad", "no connection to the internet"
			r.Detail += "\nTCP 1.1.1.1:443: " + err.Error()
		}
		return r
	}
	host, port := t, ""
	if h, p, err := net.SplitHostPort(t); err == nil {
		host, port = h, p
	}
	r.Address = host
	if net.ParseIP(host) == nil {
		ips, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil || len(ips) == 0 {
			r.State, r.Summary = "bad", "name does not resolve"
			return r
		}
		r.Address = ips[0]
	}
	if port != "" {
		ms, err := tcpTime(ctx, net.JoinHostPort(r.Address, port))
		if err != nil {
			r.State, r.Summary, r.Detail = "bad", "TCP "+port+" closed", err.Error()
			return r
		}
		r.State, r.Ms, r.Summary, r.Detail = "ok", ms, fmtMs(ms)+" · TCP "+port, "TCP "+port+" open in "+fmtMs(ms)
		return r
	}
	ok, ms, sum, det := ping(ctx, goos, r.Address)
	r.Detail = det
	if ok {
		r.State, r.Ms, r.Summary = "ok", ms, fmtMs(ms)
		return r
	}
	// ping is often filtered: try the usual management ports
	for _, p := range []string{"22", "443", "23"} {
		if ms, err := tcpTime(ctx, net.JoinHostPort(r.Address, p)); err == nil {
			r.State, r.Ms, r.Summary = "warn", ms, "ping blocked, TCP "+p+" open"
			return r
		}
	}
	r.State, r.Summary = "bad", sum
	return r
}

func fmtMs(ms float64) string {
	if ms >= 10 {
		return fmt.Sprintf("%.0f ms", ms)
	}
	return fmt.Sprintf("%.1f ms", ms)
}

func tcpTime(ctx context.Context, addr string) (float64, error) {
	d := net.Dialer{Timeout: 3 * time.Second}
	start := time.Now()
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return 0, err
	}
	c.Close()
	return float64(time.Since(start).Microseconds()) / 1000, nil
}

var (
	reRecv    = regexp.MustCompile(`(?i)(\d+) (?:packets )?transmitted, (\d+) (?:packets )?received`)
	reWinRecv = regexp.MustCompile(`(?i)Sent = (\d+), Received = (\d+)`)
	reAvg     = regexp.MustCompile(`(?i)(?:min/avg/max|round-trip)[^=]*=\s*[\d.]+/([\d.]+)`)
	reWinAvg  = regexp.MustCompile(`(?i)Average = (\d+)ms`)
)

// ping sends 4 pings with the system's ping (no administrator rights needed).
func ping(ctx context.Context, goos, host string) (ok bool, ms float64, summary, out string) {
	var args []string
	switch goos {
	case "windows":
		args = []string{"-n", "4", "-w", "1000", host}
	case "darwin":
		args = []string{"-c", "4", "-i", "0.3", "-t", "5", host}
	default:
		args = []string{"-c", "4", "-i", "0.3", "-W", "1", host}
	}
	out, _ = ncRun(ctx, "ping", args...)
	return parsePing(out, host)
}

func parsePing(out, host string) (ok bool, ms float64, summary, text string) {
	text = "ping " + host + "\n" + strings.TrimSpace(out)
	sent, recv := -1, -1
	if m := reRecv.FindStringSubmatch(out); m != nil {
		sent, _ = strconv.Atoi(m[1])
		recv, _ = strconv.Atoi(m[2])
	} else if m := reWinRecv.FindStringSubmatch(out); m != nil {
		sent, _ = strconv.Atoi(m[1])
		recv, _ = strconv.Atoi(m[2])
	}
	if m := reAvg.FindStringSubmatch(out); m != nil {
		ms, _ = strconv.ParseFloat(m[1], 64)
	} else if m := reWinAvg.FindStringSubmatch(out); m != nil {
		ms, _ = strconv.ParseFloat(m[1], 64)
	}
	switch {
	case recv > 0:
		summary = fmtMs(ms)
		if recv < sent {
			summary += fmt.Sprintf(" · %d of %d", recv, sent)
		}
		return true, ms, summary, text
	case sent > 0:
		return false, 0, fmt.Sprintf("0 of %d answered", sent), text
	}
	return false, 0, "no answer", text
}

var reGwMac = regexp.MustCompile(`(?m)^\s*gateway:\s*(\S+)`)
var reGwLinux = regexp.MustCompile(`default via (\S+)`)
var reGwWin = regexp.MustCompile(`(?m)^\s*0\.0\.0\.0\s+0\.0\.0\.0\s+(\d+\.\d+\.\d+\.\d+)`)

func defaultGateway(ctx context.Context, goos string) string {
	switch goos {
	case "darwin":
		out, _ := ncRun(ctx, "route", "-n", "get", "default")
		if m := reGwMac.FindStringSubmatch(out); m != nil {
			return m[1]
		}
	case "windows":
		out, _ := ncRun(ctx, "route", "print", "-4", "0.0.0.0")
		if m := reGwWin.FindStringSubmatch(out); m != nil {
			return m[1]
		}
	default:
		out, _ := ncRun(ctx, "ip", "route", "show", "default")
		if m := reGwLinux.FindStringSubmatch(out); m != nil {
			return m[1]
		}
	}
	return ""
}

// NetCheckTrace runs traceroute (tracert on Windows) to a target from this computer.
func (a *App) NetCheckTrace(target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	host := target
	if h, _, err := net.SplitHostPort(target); err == nil {
		host = h
	}
	switch strings.ToLower(host) {
	case "gateway":
		host = defaultGateway(ctx, runtime.GOOS)
	case "internet", "":
		host = "1.1.1.1"
	case "dns":
		host = "example.com"
	}
	var out string
	var err error
	switch runtime.GOOS {
	case "windows":
		out, err = ncRun(ctx, "tracert", "-d", "-h", "15", "-w", "1000", host)
	default:
		out, err = ncRun(ctx, "traceroute", "-n", "-m", "15", "-w", "1", "-q", "1", host)
	}
	if out == "" && err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("traceroute is not installed on this computer")
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ---- iPerf3 (the iperf3 program on this computer) ----

// IperfInfo: whether iperf3 is there, and the server's state.
type IperfInfo struct {
	Path    string     `json:"path"` // "" = not installed
	Running bool       `json:"running"`
	Port    int        `json:"port"`
	Last    *IperfTest `json:"last,omitempty"` // the last test a device ran against the server
	Error   string     `json:"error,omitempty"`
}

// IperfTest is one iPerf3 result.
type IperfTest struct {
	Peer        string  `json:"peer"`
	Seconds     float64 `json:"seconds"`
	SentBps     float64 `json:"sentBps"`
	ReceivedBps float64 `json:"receivedBps"`
	Retransmits int     `json:"retransmits"`
	Time        string  `json:"time"`
}

var iperf struct {
	sync.Mutex
	cmd    *exec.Cmd
	cancel context.CancelFunc
	port   int
	last   *IperfTest
	err    string
	idle   *time.Timer
}

var iperfLook = exec.LookPath

func iperfPath() string {
	if p, err := iperfLook("iperf3"); err == nil {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/iperf3", "/usr/local/bin/iperf3", `C:\Program Files\iperf3\iperf3.exe`} {
		if _, err := iperfLook(p); err == nil {
			return p
		}
	}
	return ""
}

// IperfState reports iperf3 and the server.
func (a *App) IperfState() IperfInfo {
	iperf.Lock()
	defer iperf.Unlock()
	return IperfInfo{Path: iperfPath(), Running: iperf.cmd != nil, Port: iperf.port, Last: iperf.last, Error: iperf.err}
}

// IperfServerStart runs "iperf3 -s" on a port until stopped, 30 minutes
// without tests, or Termwright closes; each test's result is sent as "iperf:test".
func (a *App) IperfServerStart(port int) (IperfInfo, error) {
	bin := iperfPath()
	if bin == "" {
		return a.IperfState(), errors.New("iperf3 is not installed - macOS: brew install iperf3 · Windows: winget install iperf3 · Linux: the iperf3 package")
	}
	if port <= 0 || port > 65535 {
		port = 5201
	}
	iperf.Lock()
	if iperf.cmd != nil {
		iperf.Unlock()
		return a.IperfState(), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "-s", "-p", strconv.Itoa(port), "-J")
	hideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		iperf.Unlock()
		cancel()
		return a.IperfState(), err
	}
	if err := cmd.Start(); err != nil {
		iperf.Unlock()
		cancel()
		return a.IperfState(), err
	}
	iperf.cmd, iperf.cancel, iperf.port, iperf.err = cmd, cancel, port, ""
	iperf.idle = time.AfterFunc(30*time.Minute, func() { a.IperfServerStop() })
	iperf.Unlock()
	go func() {
		readIperfTests(stdout, func(t IperfTest) {
			iperf.Lock()
			iperf.last = &t
			if iperf.idle != nil {
				iperf.idle.Reset(30 * time.Minute)
			}
			iperf.Unlock()
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, "iperf:test", t)
			}
		})
		werr := cmd.Wait()
		iperf.Lock()
		if iperf.cmd == cmd {
			iperf.cmd = nil
			if werr != nil && ctx.Err() == nil {
				iperf.err = "the iperf3 server stopped: " + werr.Error()
			}
		}
		iperf.Unlock()
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "iperf:state", a.IperfState())
		}
	}()
	return a.IperfState(), nil
}

// IperfServerStop stops the server.
func (a *App) IperfServerStop() {
	iperf.Lock()
	if iperf.cancel != nil {
		iperf.cancel()
	}
	if iperf.idle != nil {
		iperf.idle.Stop()
	}
	iperf.cmd, iperf.cancel = nil, nil
	iperf.Unlock()
}

// readIperfTests reads the JSON objects "iperf3 -s -J" prints, one per test.
func readIperfTests(r io.Reader, each func(IperfTest)) {
	dec := json.NewDecoder(bufio.NewReader(r))
	for {
		var v iperfJSON
		if err := dec.Decode(&v); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			// skip to the next object
			if _, err := dec.Token(); err != nil {
				return
			}
			continue
		}
		if t, ok := v.test(); ok {
			each(t)
		}
	}
}

type iperfJSON struct {
	Start struct {
		Connected []struct {
			RemoteHost string `json:"remote_host"`
		} `json:"connected"`
		Timestamp struct {
			Time string `json:"time"`
		} `json:"timestamp"`
	} `json:"start"`
	End struct {
		SumSent struct {
			Seconds     float64 `json:"seconds"`
			Bps         float64 `json:"bits_per_second"`
			Retransmits int     `json:"retransmits"`
		} `json:"sum_sent"`
		SumReceived struct {
			Bps float64 `json:"bits_per_second"`
		} `json:"sum_received"`
	} `json:"end"`
	Error string `json:"error"`
}

func (v iperfJSON) test() (IperfTest, bool) {
	if v.End.SumSent.Bps == 0 && v.End.SumReceived.Bps == 0 {
		return IperfTest{}, false
	}
	t := IperfTest{Seconds: v.End.SumSent.Seconds, SentBps: v.End.SumSent.Bps, ReceivedBps: v.End.SumReceived.Bps, Retransmits: v.End.SumSent.Retransmits, Time: v.Start.Timestamp.Time}
	if len(v.Start.Connected) > 0 {
		t.Peer = v.Start.Connected[0].RemoteHost
	}
	return t, true
}

// IperfClient measures to an iPerf3 server: this computer → server, then
// server → this computer (reverse) when both is set.
func (a *App) IperfClient(host string, seconds int, both bool) ([]IperfTest, error) {
	bin := iperfPath()
	if bin == "" {
		return nil, errors.New("iperf3 is not installed - macOS: brew install iperf3 · Windows: winget install iperf3 · Linux: the iperf3 package")
	}
	if seconds < 1 || seconds > 120 {
		seconds = 10
	}
	h, port := host, "5201"
	if hh, p, err := net.SplitHostPort(host); err == nil {
		h, port = hh, p
	}
	run := func(reverse bool) (IperfTest, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds+20)*time.Second)
		defer cancel()
		args := []string{"-c", h, "-p", port, "-t", strconv.Itoa(seconds), "-J"}
		if reverse {
			args = append(args, "-R")
		}
		out, _ := ncRun(ctx, bin, args...)
		var v iperfJSON
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			return IperfTest{}, errors.New("iperf3 gave no result: " + firstLine(out))
		}
		if v.Error != "" {
			return IperfTest{}, errors.New("iperf3: " + v.Error)
		}
		t, ok := v.test()
		if !ok {
			return IperfTest{}, errors.New("iperf3 measured nothing")
		}
		t.Peer = h
		return t, nil
	}
	up, err := run(false)
	if err != nil {
		return nil, err
	}
	res := []IperfTest{up}
	if both {
		down, err := run(true)
		if err != nil {
			return res, err
		}
		res = append(res, down)
	}
	return res, nil
}
