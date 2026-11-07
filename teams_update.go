package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/team"
)

// Updating a team server: Termwright logs in to its host over SSH, gets the
// image of this Termwright's version there, and starts the team's container
// again with the same name, ports, network, data volume and settings. The old
// container is kept (stopped) until the new one answers, and comes back if it
// does not.

// TeamUpdateRequest: the team, the SSH session of its host and how the image
// gets there ("download" or "copy", as when setting up).
type TeamUpdateRequest struct {
	TeamID      string `json:"teamId"`
	SessionPath string `json:"sessionPath"`
	Image       string `json:"image"`
}

// TeamUpdateServer starts updating a team server and returns the run id;
// progress and the end come as the setup's events ("team:setup", "team:setup:done").
func (a *App) TeamUpdateServer(r TeamUpdateRequest) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	t, _, err := a.teamConn(r.TeamID)
	if err != nil {
		return "", err
	}
	s, err := a.store.Get(r.SessionPath)
	if err != nil {
		return "", err
	}
	s = a.withFolderDefaults(s)
	if s.Protocol != "SSH2" {
		return "", errors.New("pick an SSH session of the host")
	}
	id := randHex(8)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	teamSetups.Store(id, cancel)
	emit := func(step, state, text string) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "team:setup", TeamSetUpStep{Run: id, Step: step, State: state, Text: text})
		}
	}
	go func() {
		defer cancel()
		defer teamSetups.Delete(id)
		res := TeamSetUpResult{Run: id, TeamID: t.ID}
		if err := a.teamUpdate(ctx, id, t, r, s.Hostname, emit); err != nil {
			res.Error = err.Error()
			if ctx.Err() == context.Canceled {
				res.Error = "Cancelled."
			}
		}
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "team:setup:done", res)
		}
	}()
	return id, nil
}

func (a *App) teamUpdate(ctx context.Context, id string, t config.TeamConn, r TeamUpdateRequest, host string, emit func(step, state, text string)) error {
	sess, err := a.store.Get(r.SessionPath)
	if err != nil {
		return err
	}
	sess = a.withFolderDefaults(sess)
	failf := func(step, f string, v ...any) error {
		msg := fmt.Sprintf(f, v...)
		emit(step, "fail", msg)
		return errors.New(msg)
	}
	out, closeSSH, err := a.teamLogin(ctx, id, sess, host, emit, failf)
	if err != nil {
		return err
	}
	defer closeSSH()
	// steps that must run even when the update is cancelled (putting the old
	// container back)
	run, closeSSH2, err := a.terms.ExecSession(context.Background(), "teamupdate-"+id, sess)
	if err != nil {
		return failf("login", "Could not log in to %s: %v", sess.Name, err)
	}
	defer closeSSH2()
	must := func(cmd string) (string, int, error) {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var b strings.Builder
		code, err := run(c, cmd, nil, func(l string) { b.WriteString(l); b.WriteString("\n") })
		return strings.TrimSpace(b.String()), code, err
	}
	dk, err := teamDocker(out, emit, failf)
	if err != nil {
		return err
	}

	// the team's container: the one it was set up as, the team's name, or the
	// one on the team's port
	emit("find", "run", "Looking for the team server's container…")
	_, port, _ := net.SplitHostPort(t.Address)
	list, _, _ := out(dk+` ps -a --format '{{.Names}}|{{.Image}}|{{.Ports}}'`, nil, 30*time.Second)
	name, byPort, all := "", "", []string{}
	for _, l := range strings.Split(list, "\n") {
		f := strings.SplitN(l, "|", 3)
		if len(f) < 3 || !(strings.Contains(f[1], "termwright-team") || strings.HasPrefix(f[0], "termwright-team-")) {
			continue
		}
		all = append(all, f[0])
		switch {
		case t.Container != "" && f[0] == t.Container, f[0] == "termwright-team-"+teamSlug(t.Name):
			if name == "" || f[0] == t.Container {
				name = f[0]
			}
		case strings.Contains(f[2], ":"+port+"->"):
			byPort = f[0]
		}
	}
	if name == "" {
		name = byPort
	}
	if name == "" && len(all) == 1 {
		name = all[0]
	}
	if name == "" {
		return failf("find", "%s has no container of the team server %s (looked for termwright-team-%s, or one on port %s). Is it the host the team server runs on (%s)?",
			sess.Name, t.Name, teamSlug(t.Name), port, t.Address)
	}
	old, _, _ := out(dk+" exec "+name+" /termwright-team -version 2>/dev/null", nil, 30*time.Second)
	img, _, _ := out(dk+" inspect -f '{{.Config.Image}}' "+name, nil, 30*time.Second)
	emit("find", "ok", "Found "+name+" ("+firstLine(img)+", version "+orDash(firstLine(old))+")")

	// how it runs: network, ports, data and settings
	spec, code, err := out(dk+" inspect -f "+shQuote(inspectFormat)+" "+name, nil, 30*time.Second)
	if err != nil || code != 0 {
		return failf("find", "Could not read how %s runs: %s", name, firstLine(spec+errText(err)))
	}
	args, data := runArgs(spec)
	if !data {
		return failf("find", "%s keeps its data inside the container (no volume at /data): a new container would lose the team. Back it up and set it up with a volume (see the team server guide).", name)
	}

	ref, err := a.teamImage(ctx, out, dk, r.Image, emit, failf)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// swap: the old container is renamed and stopped, the new one started
	emit("start", "run", "Starting "+name+" with the new image…")
	prev := name + "-previous"
	_, _, _ = must(dk + " rm -f " + prev + " 2>&1") // from an earlier update
	if msg, code, err := must(dk + " rename " + name + " " + prev + " 2>&1"); err != nil || code != 0 {
		return failf("start", "Could not rename %s: %s", name, firstLine(msg+errText(err)))
	}
	_, _, _ = must(dk + " stop -t 10 " + prev + " 2>&1")
	back := func() {
		_, _, _ = must(dk + " rm -f " + name + " 2>&1")
		_, _, _ = must(dk + " rename " + prev + " " + name + " 2>&1")
		_, _, _ = must(dk + " start " + name + " 2>&1")
	}
	cmd := fmt.Sprintf("%s run -d --name %s --restart unless-stopped %s %s 2>&1", dk, name, strings.Join(args, " "), shQuote(ref))
	if msg, code, err := must(cmd); err != nil || code != 0 {
		back()
		return failf("start", "The new container did not start (%s); the old one runs again.", firstLine(msg+errText(err)))
	}
	emit("start", "ok", "Started "+name+" ("+ref+")")

	// the same server answers again: same certificate, the new version
	emit("reach", "run", "Waiting for the team server to answer at "+t.Address+"…")
	c := team.NewClient(t.Address, t.Fingerprint, "")
	var info team.Info
	for i := 0; i < 45; i++ {
		ic, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		info, err = c.Info(ic)
		cancel()
		if err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		logs, _, _ := must(dk + " logs --tail 3 " + name + " 2>&1")
		back()
		return failf("reach", "The new container did not answer at %s (%v; %s) - the old one runs again.", t.Address, err, firstLine(logs))
	}
	_, _, _ = must(dk + " rm " + prev + " 2>&1")
	emit("reach", "ok", "The team server runs "+info.Version+" - the team, its members, inventory, history and invite are kept")

	// remember the host and container for the next update
	cur := a.cfg.Get().Teams
	for i := range cur {
		if cur[i].ID == t.ID {
			cur[i].Host, cur[i].Container = r.SessionPath, name
		}
	}
	_ = a.cfg.SetTeams(cur)
	return nil
}

// inspectFormat reads how a container runs, for runArgs.
const inspectFormat = `{{.HostConfig.NetworkMode}}{{"\n"}}{{range $p, $b := .HostConfig.PortBindings}}{{range $b}}P|{{.HostIp}}|{{.HostPort}}|{{$p}}{{"\n"}}{{end}}{{end}}` +
	`{{range .Mounts}}M|{{.Type}}|{{.Name}}|{{.Source}}|{{.Destination}}{{"\n"}}{{end}}{{range .Config.Env}}E|{{.}}{{"\n"}}{{end}}`

// runArgs turns a container's settings (inspectFormat) into
// docker run arguments: its network, published ports, volumes and TW_ settings.
// data tells whether /data is a volume or a host folder.
func runArgs(spec string) (args []string, data bool) {
	lines := strings.Split(spec, "\n")
	switch mode := strings.TrimSpace(lines[0]); mode {
	case "", "default", "bridge":
	case "host":
		args = append(args, "--network host")
	default:
		args = append(args, "--network "+shQuote(mode))
	}
	seen := map[string]bool{}
	for _, l := range lines[1:] {
		f := strings.Split(strings.TrimSpace(l), "|")
		switch {
		case f[0] == "P" && len(f) == 4:
			ip, hp, cp := f[1], f[2], strings.TrimSuffix(f[3], "/tcp")
			p := hp + ":" + cp
			if ip != "" && ip != "0.0.0.0" && ip != "::" {
				p = ip + ":" + p
			}
			if !seen[p] {
				seen[p] = true
				args = append(args, "-p "+shQuote(p))
			}
		case f[0] == "M" && len(f) == 5:
			src := f[3]
			if f[1] == "volume" {
				src = f[2]
			}
			if src == "" {
				continue
			}
			args = append(args, "-v "+shQuote(src+":"+f[4]))
			if f[4] == "/data" {
				data = true
			}
		case f[0] == "E" && len(f) >= 2:
			e := strings.Join(f[1:], "|")
			if strings.HasPrefix(e, "TW_") {
				args = append(args, "-e "+shQuote(e))
			}
		}
	}
	return args, data
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
