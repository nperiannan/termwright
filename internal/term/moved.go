package term

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/termwright/termwright/internal/discover"
	"github.com/termwright/termwright/internal/session"
)

// ErrOtherDevice: the session's address answers with another MAC address
// than the one saved for the device (DHCP gave the address to another device).
var ErrOtherDevice = errors.New("another device answers at this address")

// macOf looks up the MAC address of a local address (ARP cache); tests replace it.
var macOf = discover.MACOf

// checkMAC compares the MAC address of a direct connection's peer with the
// one saved in the session (devices added by Discover Devices). It does
// nothing when no MAC is saved or the peer is not on a local network.
func (m *Manager) checkMAC(ctx context.Context, t *Tab, s session.Session, c net.Conn) error {
	want := discover.NormalizeMAC(s.MAC)
	if want == "" || c == nil {
		return nil
	}
	ta, ok := c.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return nil
	}
	ip, ok := netip.AddrFromSlice(ta.IP)
	if !ok {
		return nil
	}
	ip = ip.Unmap()
	got := macOf(ip)
	if got == "" || got == want {
		return nil
	}
	name := s.Name
	if name == "" {
		name = s.Hostname
	}
	r, err := m.prompts.Ask(ctx, Prompt{TabID: t.id, Kind: "hostkey", Title: "Another Device at This Address", Danger: true,
		Message: fmt.Sprintf("%s answers with a different MAC address than %s:\n\n  now:   %s\n  saved: %s\n\n"+
			"The address may have been given to another device (DHCP). Termwright can look for %s by its MAC address.", ip, name, got, want, name),
		Choices: []string{"Find the Device by MAC", "Connect Anyway"}})
	if err == nil && r.Choice == "Connect Anyway" {
		return nil
	}
	if err == nil && s.Path != "" {
		m.emit.Emit("device:find", map[string]string{"path": s.Path, "tab": t.id})
	}
	return fmt.Errorf("%w: %s has MAC %s, %s has %s - find the device by its MAC address", ErrOtherDevice, ip, got, name, want)
}

// FetchHostKey reads the SSH host key of addr (host:port) without logging in.
func FetchHostKey(ctx context.Context, addr string) (ssh.PublicKey, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(8 * time.Second))
	var key ssh.PublicKey
	stop := errors.New("got the key")
	cfg := &ssh.ClientConfig{User: "termwright", ClientVersion: "SSH-2.0-Termwright",
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error { key = k; return stop }}
	_, _, _, err = ssh.NewClientConn(c, addr, cfg)
	if key != nil {
		return key, nil
	}
	if err == nil {
		err = errors.New("no host key")
	}
	return nil, err
}

// HostKeyStatus compares key with the key saved for oldAddr (host:port):
// "same", "different" or "unknown" (no saved key).
func (m *Manager) HostKeyStatus(oldAddr string, key ssh.PublicKey) string {
	cb, err := knownhosts.New(m.knownHostsFiles()...)
	if err != nil {
		return "unknown"
	}
	err = cb(oldAddr, &net.TCPAddr{}, key)
	if err == nil {
		return "same"
	}
	var ke *knownhosts.KeyError
	if errors.As(err, &ke) && len(ke.Want) > 0 {
		return "different"
	}
	return "unknown"
}

// TrustHostKey saves key as the host key of addr (host:port), replacing
// older entries (used when a device moved to a new address).
func (m *Manager) TrustHostKey(addr string, key ssh.PublicKey) error {
	return m.saveHostKey(addr, key, true)
}
