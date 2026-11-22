package testrec

import (
	"context"
	"fmt"
	"strings"
)

// TGenStep runs traffic on a commercial traffic generator through its REST API: it loads a saved
// configuration, connects its ports to chassis ports, starts the protocols, sends traffic for Duration
// seconds and reads each stream's counters. The step's output is JSON - streams[i].name, tx, rx, loss
// (percent), latencyUs, and the totals tx, rx, loss - so json checks work on it (json: loss, less: 0.01).
type TGenStep struct {
	// Kind: stc (Spirent TestCenter) or ixnetwork (Keysight IxNetwork).
	Kind string `yaml:"kind" json:"kind"`
	// Server: an API session (its base URL and login) or the REST API's address.
	Server string `yaml:"server" json:"server"`
	// Config: the saved configuration in the tests folder (STC .xml / .tcc, IxNetwork .ixncfg / .json);
	// empty: what is loaded on the server already.
	Config string `yaml:"config,omitempty" json:"config,omitempty"`
	// Ports: chassis ports in the order of the configuration's ports, chassis/slot/port.
	Ports []string `yaml:"ports,omitempty" json:"ports,omitempty"`
	// Duration: seconds of traffic (default 10).
	Duration int `yaml:"duration,omitempty" json:"duration,omitempty"`
	// Protocols: start the configuration's protocols first and wait Settle seconds (default 5).
	Protocols bool `yaml:"protocols,omitempty" json:"protocols,omitempty"`
	Settle    int  `yaml:"settle,omitempty" json:"settle,omitempty"`
	// Session: an IxNetwork session id to use (Windows API servers: 1); empty makes one and removes it after.
	Session string `yaml:"session,omitempty" json:"session,omitempty"`
}

// TGenDriver is a Driver that can run traffic on a traffic generator.
type TGenDriver interface {
	TGen(ctx context.Context, t TGenStep) (string, error)
}

func fillTGen(t TGenStep, v map[string]string) TGenStep {
	t.Server, t.Config, t.Session, t.Kind = Fill(t.Server, v), Fill(t.Config, v), Fill(t.Session, v), Fill(t.Kind, v)
	ports := make([]string, len(t.Ports))
	for i, p := range t.Ports {
		ports[i] = Fill(p, v)
	}
	t.Ports = ports
	return t
}

// TGenText is a traffic run in a few words.
func TGenText(t TGenStep) string {
	k := map[string]string{"stc": "Spirent TestCenter", "ixnetwork": "IxNetwork"}[strings.ToLower(t.Kind)]
	if k == "" {
		k = t.Kind
	}
	d := t.Duration
	if d <= 0 {
		d = 10
	}
	s := fmt.Sprintf("%s traffic %d s", k, d)
	if t.Config != "" {
		s += " · " + t.Config
	}
	return s
}
