package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// EmuRoutingView is a computer's route generator as it runs.
type EmuRoutingView struct {
	emu.Routing
	State    string `json:"state"`    // the neighbour as FRR has it (Full/DR, Established, Active …) or why not
	Up       bool   `json:"up"`       // the adjacency or session is up
	Prefixes int    `json:"prefixes"` // prefixes it has to advertise
	First    string `json:"first"`    // the first address to send traffic to
	Step     int    `json:"step"`     // addresses from one prefix to the next
	Raw      string `json:"raw"`      // what FRR printed
}

// emuComputerScript runs a shell script on a running computer through its console (in pieces, base64).
func (a *App) emuComputerScript(id, script string, d time.Duration) (string, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	var v *emu.VM
	if emuLab.f.Lab != nil {
		v = emuLab.f.Lab.VMByID(id)
	}
	if v == nil || !emuLab.ready[id] {
		emuLab.mu.Unlock()
		return "", errors.New("it is not running and ready")
	}
	if emuLab.day0[id] {
		emuLab.mu.Unlock()
		return "", errors.New("Termwright is typing its configuration: try again in a moment")
	}
	vv, h := *v, emuHostOf(*v)
	emuLab.mu.Unlock()
	enc := base64.StdEncoding.EncodeToString([]byte(script))
	var out string
	err := a.emuOnVMConsole(h, vv, d, func(ctx context.Context, c *emu.Console, im emu.Image, _ map[int]string) error {
		prompt, err := emu.Login(ctx, c, &im)
		if err != nil {
			return err
		}
		if _, err := emu.Run(ctx, c, &im, prompt, ": > /tmp/tw.b64"); err != nil {
			return err
		}
		for i := 0; i < len(enc); i += 300 {
			if _, err := emu.Run(ctx, c, &im, prompt, "printf %s "+enc[i:min(len(enc), i+300)]+" >> /tmp/tw.b64"); err != nil {
				return err
			}
		}
		o, err := emu.Run(ctx, c, &im, prompt, "base64 -d /tmp/tw.b64 > /tmp/tw.sh && sh /tmp/tw.sh 2>&1 | tail -n 20")
		out = emu.CleanConsole(o)
		return err
	})
	return out, err
}

// EmuSetRouting makes a Linux computer a route generator (or stops it: proto "").
func (a *App) EmuSetRouting(id string, r emu.Routing) (EmuRoutingView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	var v *emu.VM
	if emuLab.f.Lab != nil {
		v = emuLab.f.Lab.VMByID(id)
	}
	if v == nil {
		emuLab.mu.Unlock()
		return EmuRoutingView{}, errors.New("no such computer")
	}
	im := emuImage(v.Image)
	if im == nil || len(im.PortAddr) == 0 {
		emuLab.mu.Unlock()
		return EmuRoutingView{}, errors.New("only a Linux computer can be a route generator")
	}
	if r.Proto == "" {
		v.Routing = nil
		_ = a.emuSave()
		emuLab.mu.Unlock()
		_, err := a.emuComputerScript(id, emu.RoutingOffScript(), 60*time.Second)
		return EmuRoutingView{}, err
	}
	if _, _, _, err := r.Plan(); err != nil {
		emuLab.mu.Unlock()
		return EmuRoutingView{}, err
	}
	addr := v.Addrs[r.Port]
	iface := im.PortNameOf(r.Port)
	name := v.Name
	if addr == "" || addr == "dhcp" {
		emuLab.mu.Unlock()
		return EmuRoutingView{}, fmt.Errorf("give its port %s a fixed IPv4 address first", iface)
	}
	script, err := emu.RoutingScript(r, name, iface, addr)
	if err != nil {
		emuLab.mu.Unlock()
		return EmuRoutingView{}, err
	}
	rr := r
	v.Routing = &rr
	emuEv(id, "info", fmt.Sprintf("route generator: %s with %d prefixes from %s on %s", strings.ToUpper(r.Proto), r.Routes, r.Prefix, iface))
	_ = a.emuSave()
	emuLab.mu.Unlock()
	out, err := a.emuComputerScript(id, script, 5*time.Minute)
	if err == nil && !strings.Contains(out, "tw-routing-ok") {
		err = errors.New("setting up FRR did not finish: " + strings.TrimSpace(out))
	}
	if err != nil {
		emuLab.mu.Lock()
		emuEv(id, "warn", "route generator: "+err.Error())
		emuLab.mu.Unlock()
		return EmuRoutingView{}, err
	}
	time.Sleep(2 * time.Second)
	return a.EmuRouting(id)
}

// EmuRouting reads a computer's route generator: its neighbour and prefixes.
func (a *App) EmuRouting(id string) (EmuRoutingView, error) {
	emuLab.mu.Lock()
	var r *emu.Routing
	if emuLab.f.Lab != nil {
		if v := emuLab.f.Lab.VMByID(id); v != nil && v.Routing != nil {
			c := *v.Routing
			r = &c
		}
	}
	emuLab.mu.Unlock()
	if r == nil {
		return EmuRoutingView{}, errors.New("it is not a route generator")
	}
	p, step, _, err := r.Plan()
	if err != nil {
		return EmuRoutingView{}, err
	}
	rv := EmuRoutingView{Routing: *r, Step: int(step), First: p.Addr().Next().String()}
	if step == 1 {
		rv.First = p.Addr().String()
	}
	out, err := a.emuComputerScript(id, emu.RoutingShowScript(*r), 60*time.Second)
	if err != nil {
		rv.State = err.Error()
		return rv, nil
	}
	rv.Raw = strings.TrimSpace(out)
	rv.State, rv.Up, rv.Prefixes = emu.RoutingState(*r, out)
	return rv, nil
}

// emuRoutingStream adds an ICMP stream from a test port to every advertised prefix (one address in each, in
// turn): its replies show the switch forwards to the routes.
func (a *App) emuRoutingStream(id, from string, pps int) (EmuTrafficView, error) {
	rv, err := a.EmuRouting(id)
	if err != nil && rv.First == "" {
		return EmuTrafficView{}, err
	}
	emuLab.mu.Lock()
	name := id
	if v := emuLab.f.Lab.VMByID(id); v != nil {
		name = v.Name
	}
	emuLab.mu.Unlock()
	s := emu.Stream{Name: "routes of " + name, From: from, Kind: "icmp", DstIP: rv.First, Size: 128, PPS: max(1, min(pps, 100000)), Seconds: 60, MaxLoss: 0,
		Mods: []emu.Mod{{Field: "dstIp", Mode: "inc", Count: rv.Routes, Step: rv.Step}}}
	if s.PPS == 1 && pps == 0 {
		s.PPS = 1000
	}
	return a.EmuStreamSave(s)
}

// emuRoutingText is a route generator for the connector.
func emuRoutingText(name string, rv EmuRoutingView) string {
	t := fmt.Sprintf("%s: %s, %d prefixes from %s (next one every %s addresses), %s", name, strings.ToUpper(rv.Proto), rv.Routes, rv.Prefix, strconv.Itoa(rv.Step), rv.State)
	if rv.Up {
		t += " (up)"
	}
	return t + fmt.Sprintf(". Traffic: an ICMP stream from a test port to %s, dstIp +%d for %d prefixes (action stream adds it).", rv.First, rv.Step, rv.Routes)
}
