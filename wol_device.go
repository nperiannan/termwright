package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/discover"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/wol"
)

// A Wake-on-LAN session wakes its device and then watches it: with an IP
// address it pings until the device answers; with only a network it
// searches the network for the MAC address (every WOLScanSecs, WOLScans
// times) and saves the address it finds. Progress is sent as "wol:status".

// WakeStatus is one step of waking a device.
type WakeStatus struct {
	Path    string `json:"path"`
	State   string `json:"state"` // sent, waiting, scanning, up, down, notfound, error
	Message string `json:"message"`
	IP      string `json:"ip,omitempty"`
	Name    string `json:"name,omitempty"` // NetBIOS name
	Scan    int    `json:"scan,omitempty"`
	Scans   int    `json:"scans,omitempty"`
}

var (
	wakeMu   sync.Mutex
	wakeRuns = map[string]context.CancelFunc{}
)

// WakeDevice sends the magic packet for a Wake-on-LAN session and watches
// the device in the background; it returns once the packet is sent.
func (a *App) WakeDevice(s session.Session) (WakeResult, error) {
	if err := a.ready(); err != nil {
		return WakeResult{}, err
	}
	s = a.withFolderDefaults(s)
	if strings.TrimSpace(s.MAC) == "" {
		return WakeResult{}, errors.New("enter the device's MAC address")
	}
	pkt, err := wol.Packet(strings.TrimSpace(s.MAC))
	if err != nil {
		return WakeResult{}, err
	}
	if strings.TrimSpace(s.Hostname) == "" && strings.TrimSpace(s.WOLNetwork) == "" && s.WOLBroadcast == "" {
		return WakeResult{}, errors.New("enter the device's network (e.g. 192.168.1.0) or its IP address - the magic packet goes to that network only")
	}
	if n := strings.TrimSpace(s.WOLNetwork); n != "" {
		if _, err := discover.NetworkOf(n); err != nil {
			return WakeResult{}, err
		}
	}
	key := s.Path
	if key == "" {
		key = "mac:" + s.MAC
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	res, _, release, err := a.sendWake(ctx, "", s, pkt)
	if err != nil {
		cancel()
		return WakeResult{}, err
	}
	release() // the checks below run from this computer
	wakeMu.Lock()
	if old := wakeRuns[key]; old != nil {
		old()
	}
	wakeRuns[key] = cancel
	wakeMu.Unlock()
	go func() {
		defer func() {
			cancel()
			wakeMu.Lock()
			delete(wakeRuns, key)
			wakeMu.Unlock()
		}()
		a.watchWake(ctx, s, key)
	}()
	return res, nil
}

// WakeCancel stops watching a device that is being woken.
func (a *App) WakeCancel(path string) {
	wakeMu.Lock()
	c := wakeRuns[path]
	wakeMu.Unlock()
	if c != nil {
		c()
	}
}

func (a *App) watchWake(ctx context.Context, s session.Session, key string) {
	emit := func(st WakeStatus) {
		st.Path = key
		emitter{a}.Emit("wol:status", st)
	}
	scans, every := s.WOLScans, s.WOLScanSecs
	if scans <= 0 {
		scans = 4
	}
	if every <= 0 {
		every = 30
	}
	wait := func(d time.Duration) bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(d):
			return true
		}
	}
	up := func(ip string) bool {
		if !wol.Ping(ctx, ip, 2*time.Second) {
			return false
		}
		name := wol.NetBIOSName(ip, 1500*time.Millisecond)
		msg := s.Name + " is awake - " + ip + " answers ping"
		if name != "" {
			msg += " (NetBIOS name " + name + ")"
		}
		emit(WakeStatus{State: "up", IP: ip, Name: name, Message: msg})
		return true
	}
	ip := strings.TrimSpace(s.Hostname)
	if ip != "" {
		emit(WakeStatus{State: "waiting", IP: ip, Message: "Magic packet sent - waiting for " + ip + " to answer ping…"})
		limit := time.Duration(scans*every) * time.Second
		if limit < 2*time.Minute {
			limit = 2 * time.Minute
		}
		start := time.Now()
		for time.Since(start) < limit {
			if up(ip) {
				return
			}
			if !wait(5 * time.Second) {
				return
			}
		}
		emit(WakeStatus{State: "down", IP: ip, Message: s.Name + " (" + ip + ") has not answered ping in " + limit.Round(time.Second).String() + " - it is down, not woken or not present."})
		return
	}
	network := strings.TrimSpace(s.WOLNetwork)
	if network == "" {
		emit(WakeStatus{State: "sent", Message: "Magic packet sent to " + s.MAC + ". Add the device's IP address or network to see when it is up."})
		return
	}
	if s.JumpSession != "" && s.WOLFrom != "local" {
		emit(WakeStatus{State: "sent", Message: "Magic packet sent from the jump host. A network behind a jump host cannot be searched - add the device's IP address to see when it is up."})
		return
	}
	for i := 1; i <= scans; i++ {
		emit(WakeStatus{State: "scanning", Scan: i, Scans: scans,
			Message: "Magic packet sent - searching " + network + " for " + s.MAC + " in " + itoa(every) + " s (" + itoa(i) + " of " + itoa(scans) + ")…"})
		if !wait(time.Duration(every) * time.Second) {
			return
		}
		found, err := discover.FindMAC(ctx, network, s.MAC)
		if err != nil && ctx.Err() == nil {
			emit(WakeStatus{State: "error", Message: "Searching " + network + ": " + err.Error()})
			return
		}
		if found != "" {
			if s.Path != "" {
				if cur, err := a.store.Get(s.Path); err == nil && cur.Hostname != found {
					cur.Hostname = found
					_, _ = a.store.Save(cur, cur.Path)
				}
			}
			if !up(found) {
				emit(WakeStatus{State: "up", IP: found, Message: s.Name + " is on the network at " + found + " (found by its MAC address; it does not answer ping)"})
			}
			return
		}
	}
	emit(WakeStatus{State: "notfound", Scan: scans, Scans: scans,
		Message: s.MAC + " was not found on " + network + " in " + itoa(scans) + " searches - the device is not present, down or not woken."})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// PingResult is one host's answer.
type PingResult struct {
	Up   bool   `json:"up"`
	Name string `json:"name,omitempty"` // NetBIOS name when it answers
}

// PingHosts pings each target (at most 16 at a time) and, when it answers,
// asks for its NetBIOS name.
func (a *App) PingHosts(targets []ReachTarget) map[string]PingResult {
	out := make(map[string]PingResult, len(targets))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, t := range targets {
		if t.Host == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(t ReachTarget) {
			defer func() { <-sem; wg.Done() }()
			r := PingResult{Up: wol.Ping(context.Background(), t.Host, 2*time.Second)}
			if r.Up {
				r.Name = wol.NetBIOSName(t.Host, time.Second)
			}
			mu.Lock()
			out[t.Key] = r
			mu.Unlock()
		}(t)
	}
	wg.Wait()
	return out
}
