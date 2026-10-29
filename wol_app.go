package main

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/wol"
)

// WakeResult tells where a Wake-on-LAN packet went and whether the device answered.
type WakeResult struct {
	Sent   []string `json:"sent"`   // broadcast addresses
	Via    string   `json:"via"`    // "" = from this computer, else the server it was sent from
	Target string   `json:"target"` // host:port that was watched
	Awake  bool     `json:"awake"`
	Waited int      `json:"waited"` // seconds until it answered (or gave up)
}

// WakeSession sends a Wake-on-LAN packet to the session's MAC address - from
// this computer, or from its jump host / jump station so it reaches the
// device's network - and with wait, watches the session's port for up to
// three minutes until the device answers.
func (a *App) WakeSession(tabID string, s session.Session, wait bool) (WakeResult, error) {
	if err := a.ready(); err != nil {
		return WakeResult{}, err
	}
	s = a.withFolderDefaults(s)
	if strings.TrimSpace(s.MAC) == "" {
		return WakeResult{}, errors.New("no MAC address saved for " + s.Name + " - add it in Session Options → Connection")
	}
	pkt, err := wol.Packet(strings.TrimSpace(s.MAC))
	if err != nil {
		return WakeResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	var res WakeResult
	var client *ssh.Client
	release := func() {}
	if s.WOLFrom != "local" {
		c, rel, via, err := a.terms.RouteClient(ctx, tabID, s, tabID)
		if err != nil {
			return WakeResult{}, err
		}
		client, release, res.Via = c, rel, via
	}
	defer release()
	if client != nil {
		cmd, err := wol.RemoteCommand(pkt, s.MAC, s.WOLBroadcast, s.WOLPort)
		if err != nil {
			return WakeResult{}, err
		}
		ss, err := client.NewSession()
		if err != nil {
			return WakeResult{}, err
		}
		out, err := ss.CombinedOutput(cmd)
		_ = ss.Close()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return WakeResult{}, errors.New("sending from " + res.Via + " failed (it needs python3 or wakeonlan): " + msg)
		}
		b := s.WOLBroadcast
		if b == "" {
			b = "255.255.255.255"
		}
		res.Sent = []string{b}
	} else if res.Sent, err = wol.Send(pkt, s.WOLBroadcast, s.WOLPort); err != nil {
		return WakeResult{}, err
	}
	host, port := wakeTarget(s)
	if host == "" {
		return res, nil
	}
	res.Target = net.JoinHostPort(host, strconv.Itoa(port))
	if !wait {
		return res, nil
	}
	start := time.Now()
	for time.Since(start) < 3*time.Minute {
		var c net.Conn
		if client != nil {
			c, err = client.Dial("tcp", res.Target)
		} else {
			c, err = net.DialTimeout("tcp", res.Target, 2*time.Second)
		}
		if err == nil {
			_ = c.Close()
			res.Awake = true
			break
		}
		select {
		case <-ctx.Done():
			res.Waited = int(time.Since(start).Seconds())
			return res, nil
		case <-time.After(3 * time.Second):
		}
	}
	res.Waited = int(time.Since(start).Seconds())
	return res, nil
}

// wakeTarget is the address and port that show the device is up.
func wakeTarget(s session.Session) (string, int) {
	if h, ok := session.HopOf(s); ok && s.Protocol == session.ProtoSSH2 {
		t := h.Target
		return t[strings.LastIndex(t, "@")+1:], 22
	}
	port := s.Port
	if port == 0 {
		port = map[string]int{"SSH2": 22, "Telnet": 23, "RDP": 3389, "VNC": 5900}[s.Protocol]
	}
	if s.Hostname == "" || port == 0 {
		return "", 0
	}
	return s.Hostname, port
}
