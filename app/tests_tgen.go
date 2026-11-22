package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/termwright/termwright/internal/testrec"
	"github.com/termwright/termwright/internal/tgen"
)

// tgenJob makes a traffic-generator job from a step: the server is an API session (its base URL, user
// name and secret) or an address; the configuration is a file of the tests folder.
func (a *App) tgenJob(t testrec.TGenStep) (tgen.Job, error) {
	j := tgen.Job{Kind: t.Kind, Server: t.Server, Ports: t.Ports, Duration: t.Duration, Protocols: t.Protocols, Settle: t.Settle, Session: t.Session}
	if s, err := a.store.Get(t.Server); err == nil {
		j.Server, j.Username, j.Insecure = s.APIBaseURL, s.Username, s.APIInsecure
		if j.Server == "" {
			j.Server = s.Hostname
		}
		j.Password, _ = a.secret(httpSecretKey(s.Path))
	}
	if t.Config != "" {
		f, err := a.testsFolder()
		if err != nil {
			return j, err
		}
		p := filepath.Join(f.Root, filepath.Clean("/"+t.Config))
		b, err := os.ReadFile(p)
		if err != nil {
			return j, fmt.Errorf("configuration %s: %v", t.Config, err)
		}
		j.Config, j.ConfigName = b, filepath.Base(p)
	}
	return j, nil
}

// TGen runs a traffic step on Spirent TestCenter or IxNetwork; its output is the result as JSON.
func (d *testDriver) TGen(ctx context.Context, t testrec.TGenStep) (string, error) {
	j, err := d.a.tgenJob(t)
	if err != nil {
		return "", err
	}
	res, err := tgen.Run(ctx, j)
	b, _ := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return string(b), fmt.Errorf("%s: %v", strings.ToUpper(res.Kind), err)
	}
	return string(b), nil
}
