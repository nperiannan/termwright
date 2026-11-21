package app

import (
	"context"
	"fmt"

	"github.com/termwright/termwright/internal/netconf"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/testrec"
)

// Netconf runs a test's NETCONF step: the session's SSH login on the NETCONF port (830 unless the step
// says otherwise), the netconf subsystem, then the operation (and a commit for a candidate edit).
func (d *testDriver) Netconf(ctx context.Context, n testrec.NetconfStep) (string, error) {
	s, err := d.a.store.Get(n.Session)
	if err != nil {
		return "", fmt.Errorf("no saved session %q", n.Session)
	}
	if s.Protocol != "" && s.Protocol != session.ProtoSSH2 {
		return "", fmt.Errorf("%s is not an SSH session: NETCONF needs SSH", n.Session)
	}
	rpcs, err := testrec.NetconfRPC(n)
	if err != nil {
		return "", err
	}
	s.Port = n.Port
	if s.Port == 0 {
		s.Port = 830
	}
	client, closeFn, err := d.a.terms.Client(ctx, "netconf:"+s.Path, s)
	if err != nil {
		return "", fmt.Errorf("NETCONF login on port %d: %v", s.Port, err)
	}
	defer closeFn()
	ss, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer ss.Close()
	in, err := ss.StdinPipe()
	if err != nil {
		return "", err
	}
	out, err := ss.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := ss.RequestSubsystem("netconf"); err != nil {
		return "", fmt.Errorf("the device has no NETCONF on port %d: %v", s.Port, err)
	}
	c, err := netconf.Open(out, in)
	if err != nil {
		return "", err
	}
	var reply string
	for _, op := range rpcs {
		r, err := c.RPC(ctx, op)
		if err != nil {
			return netconf.Pretty(r), err
		}
		reply = r
	}
	_, _ = c.RPC(ctx, "<close-session/>")
	return netconf.Pretty(reply), nil
}
