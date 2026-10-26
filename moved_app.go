package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/discover"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/term"
)

// Devices that got a new address from DHCP: sessions remember the MAC
// address Discover Devices saw, and are found again by it.

// MovedDevice is a session whose device answers at another address.
type MovedDevice struct {
	Path    string `json:"path"`
	Old     string `json:"old"`
	New     string `json:"new"`
	MAC     string `json:"mac"`
	HostKey string `json:"hostKey"` // same, different, unknown (no saved key), none (not SSH or no answer)
	Scanned string `json:"scanned,omitempty"`
}

func sessionPort(s session.Session) int {
	if s.Port > 0 {
		return s.Port
	}
	if s.Protocol == session.ProtoTelnet {
		return 23
	}
	return 22
}

// hostKeyStatus compares the SSH host key at newHost with the key saved for
// the session's current address.
func (a *App) hostKeyStatus(ctx context.Context, s session.Session, newHost string) string {
	if s.Protocol != session.ProtoSSH2 || s.JumpSession != "" || s.ProxyType != "" {
		return "none"
	}
	port := strconv.Itoa(sessionPort(s))
	key, err := term.FetchHostKey(ctx, net.JoinHostPort(newHost, port))
	if err != nil {
		return "none"
	}
	return a.terms.HostKeyStatus(net.JoinHostPort(s.Hostname, port), key)
}

// CheckMovedDevices takes sessions with the address their MAC answers at now
// (from a Discover Devices scan) and checks their SSH host keys.
func (a *App) CheckMovedDevices(moves []MovedDevice) ([]MovedDevice, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out := make([]MovedDevice, len(moves))
	var wg sync.WaitGroup
	for i, mv := range moves {
		out[i] = mv
		s, err := a.store.Get(mv.Path)
		if err != nil {
			out[i].HostKey = "none"
			continue
		}
		out[i].Old, out[i].MAC = s.Hostname, discover.NormalizeMAC(s.MAC)
		wg.Add(1)
		go func(i int, s session.Session) {
			defer wg.Done()
			out[i].HostKey = a.hostKeyStatus(ctx, s, out[i].New)
		}(i, s)
	}
	wg.Wait()
	return out, nil
}

// FindByMAC looks for a session's device on this computer's network by the
// MAC address saved in the session.
func (a *App) FindByMAC(path string) (*MovedDevice, error) {
	s, err := a.store.Get(path)
	if err != nil {
		return nil, err
	}
	mac := discover.NormalizeMAC(s.MAC)
	if mac == "" {
		return nil, errors.New("no MAC address is saved for this session - it is saved when Discover Devices adds or finds the device")
	}
	if s.JumpSession != "" || s.ProxyType != "" {
		return nil, errors.New("the device is reached through a jump host or proxy: its MAC address cannot be seen from this computer")
	}
	old, err := netip.ParseAddr(strings.TrimSpace(s.Hostname))
	if err != nil {
		ips, lerr := net.LookupIP(s.Hostname)
		if lerr != nil || len(ips) == 0 {
			return nil, fmt.Errorf("%s is not an IP address and cannot be resolved", s.Hostname)
		}
		old, _ = netip.AddrFromSlice(ips[0])
	}
	old = old.Unmap()
	pfx, ok := discover.LocalPrefix(old)
	if !ok {
		return nil, fmt.Errorf("%s is not on this computer's network: MAC addresses can only be seen on the local network", old)
	}
	if pfx.Bits() < 22 { // scan at most 1024 addresses around the old one
		pfx = netip.PrefixFrom(old, 22).Masked()
	}
	res := &MovedDevice{Path: path, Old: s.Hostname, MAC: mac, Scanned: pfx.String()}
	addrs, err := discover.ParseTargets(pfx.String())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var mu sync.Mutex
	discover.Scan(ctx, addrs, discover.Options{Probes: []discover.Probe{{Port: sessionPort(s), Service: "ssh"}}, Timeout: 500 * time.Millisecond,
		Parallel: 128, NoDNS: true, NoWeb: true, SIPPort: -1, MDNSPort: -1, CoAPPort: -1},
		func(r discover.Result) {
			if discover.NormalizeMAC(r.MAC) == mac {
				mu.Lock()
				if res.New == "" {
					res.New = r.IP
					cancel()
				}
				mu.Unlock()
			}
		}, nil)
	if res.New == "" {
		return res, fmt.Errorf("no device with MAC %s answered on %s - it may be switched off or on another network", mac, pfx)
	}
	hctx, hcancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer hcancel()
	res.HostKey = a.hostKeyStatus(hctx, s, res.New)
	return res, nil
}

// MoveSession changes a session's address to its device's new one. When the
// SSH host key there is the one saved for the old address, it is saved for
// the new address too (no new-key question).
func (a *App) MoveSession(path, newHost string) (session.Session, error) {
	s, err := a.store.Get(path)
	if err != nil {
		return s, err
	}
	newHost = strings.TrimSpace(newHost)
	if _, err := netip.ParseAddr(newHost); err != nil {
		return s, fmt.Errorf("%q is not an IP address", newHost)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if s.Protocol == session.ProtoSSH2 && s.JumpSession == "" && s.ProxyType == "" {
		port := strconv.Itoa(sessionPort(s))
		if key, err := term.FetchHostKey(ctx, net.JoinHostPort(newHost, port)); err == nil &&
			a.terms.HostKeyStatus(net.JoinHostPort(s.Hostname, port), key) == "same" {
			_ = a.terms.TrustHostKey(net.JoinHostPort(newHost, port), key)
		}
	}
	s.Hostname = newHost
	return a.SaveSession(s, path)
}

// RememberMACs saves MAC addresses seen by Discover Devices in sessions that
// have none yet ({session path: MAC}).
func (a *App) RememberMACs(macs map[string]string) (int, error) {
	n := 0
	for p, mac := range macs {
		mac = discover.NormalizeMAC(mac)
		if mac == "" {
			continue
		}
		s, err := a.store.Get(p)
		if err != nil || s.MAC != "" {
			continue
		}
		s.MAC = mac
		if _, err := a.SaveSession(s, p); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
