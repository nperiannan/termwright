package standalone

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/termwright/termwright/internal/tgen"
)

// TGenStep is a traffic run on Spirent TestCenter or IxNetwork (as in the test file).
type TGenStep struct {
	Kind      string   `json:"kind"`
	Server    string   `json:"server"`
	Config    string   `json:"config,omitempty"`
	Ports     []string `json:"ports,omitempty"`
	Duration  int      `json:"duration,omitempty"`
	Protocols bool     `json:"protocols,omitempty"`
	Settle    int      `json:"settle,omitempty"`
	Session   string   `json:"session,omitempty"`
}

// TGen runs traffic: the server is a device of the inventory (base_url, username, its password) or an
// address; the configuration a file of the suite. The output is the result as JSON.
func (e *Engine) TGen(ctx context.Context, t TGenStep) (string, error) {
	j := tgen.Job{Kind: t.Kind, Server: t.Server, Ports: t.Ports, Duration: t.Duration, Protocols: t.Protocols, Settle: t.Settle, Session: t.Session}
	if d, ok := e.devs[t.Server]; ok {
		j.Server, j.Username, j.Insecure = orStr(d.BaseURL, d.Host), d.Username, d.Insecure
		if d.Username != "" {
			pw, err := e.Secret(d, "password")
			if err != nil {
				return "", err
			}
			j.Password = pw
		}
	}
	if t.Config != "" {
		p := filepath.Join(e.Root, filepath.Clean("/"+t.Config))
		b, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("configuration %s: %v", t.Config, err)
		}
		j.Config, j.ConfigName = b, filepath.Base(p)
	}
	res, err := tgen.Run(ctx, j)
	b, _ := json.MarshalIndent(res, "", "  ")
	return string(b), err
}
