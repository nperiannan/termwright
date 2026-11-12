package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"strconv"
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
	Channel     string `json:"channel,omitempty"` // "" = this Termwright's version, "nightly" or "release" (also when the server runs the same version: a reinstall)
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
	out, must, closeSSH, err := a.teamLogin(ctx, id, sess, host, emit, failf)
	if err != nil {
		return err
	}
	defer closeSSH()
	dk, err := teamDocker(out, emit, failf)
	if err != nil {
		return err
	}

	// the team's container: the one whose certificate is the team's (another
	// team's server on the same host is never touched)
	emit("find", "run", "Looking for the team server's container…")
	_, port, _ := net.SplitHostPort(t.Address)
	list, _, _ := out(dk+` ps -a --format '{{.Names}}|{{.Image}}' 2>/dev/null`, nil, 30*time.Second)
	name, others := "", 0
	for _, l := range strings.Split(list, "\n") {
		f := strings.SplitN(strings.TrimSpace(l), "|", 2)
		if len(f) < 2 || !(strings.Contains(f[1], "termwright-team") || strings.HasPrefix(f[0], "termwright-team-")) || strings.HasSuffix(f[0], "-previous") {
			continue
		}
		pem, _, _ := out(dk+" cp "+f[0]+":/data/cert.pem - 2>/dev/null", nil, 30*time.Second)
		if fp := pemFingerprint(pem); fp != "" && fp == team.NormFingerprint(t.Fingerprint) {
			name = f[0]
			break
		}
		others++
	}
	if name == "" {
		// a team server installed without Docker
		if svc, _, _ := out("systemctl is-active termwright-team 2>/dev/null", nil, 20*time.Second); svc == "active" {
			return failf("find", "On %s the team server runs as the systemd service termwright-team, not in Docker. Update it by hand: download termwright-team-linux-<arch>.tar.gz of the release, then sudo cp termwright-team /usr/local/bin/ && sudo systemctl restart termwright-team.", sess.Name)
		}
		also := ""
		if others > 0 {
			also = fmt.Sprintf(" It runs %d other team server%s - not this team's (their certificates differ), so they are left alone.", others, map[bool]string{true: "", false: "s"}[others == 1])
		}
		return failf("find", "%s has no container of the team server %s.%s Pick the SSH session of the host at %s (port %s).", sess.Name, t.Name, also, strings.TrimSuffix(t.Address, ":"+port), port)
	}
	old := teamVersionNow(ctx, t)
	img, _, _ := out(dk+" inspect -f '{{.Config.Image}}' "+name, nil, 30*time.Second)
	emit("find", "ok", "Found "+name+" ("+firstLine(img)+", version "+orDash(old)+") - its certificate is the team's")

	// how it runs: network, ports, data, settings, limits, labels
	spec, code, err := out(dk+" inspect -f "+shQuote(inspectFormat)+" "+name, nil, 30*time.Second)
	if err != nil || code != 0 {
		return failf("find", "Could not read how %s runs: %s", name, firstLine(spec+errText(err)))
	}
	c := parseContainer(spec)
	tag := teamTagFor(r.Channel)

	// a server from Docker Compose is updated by Compose
	if c.compose.project != "" {
		cc := c.compose
		files := ""
		for _, f := range strings.Split(cc.files, ",") {
			if f = strings.TrimSpace(f); f != "" {
				files += " -f " + shQuote(f)
			}
		}
		cmp := dk + " compose -p " + shQuote(cc.project) + files
		emit("image", "run", "It runs from Docker Compose ("+cc.dir+") - pulling and starting it with Compose…")
		script := "cd " + shQuote(cc.dir) + " && " + cmp + " pull " + shQuote(cc.service) + " 2>&1 && " + cmp + " up -d " + shQuote(cc.service) + " 2>&1"
		if msg, code, err := out(script, nil, 8*time.Minute); err != nil || code != 0 {
			return failf("image", "Docker Compose could not update it: %s", lastLine(msg+errText(err)))
		}
		emit("image", "ok", "Started with Docker Compose")
		info, err := teamWaitFor(ctx, t)
		if err != nil {
			return failf("reach", "The team server did not answer at %s after the update (%v). On the host: cd %s && docker compose logs %s", t.Address, err, cc.dir, cc.service)
		}
		if old != "" && compareVersions(info.Version, old) <= 0 {
			return failf("reach", "The team server still runs %s: the Compose file names an image without a newer version. Set its image to ghcr.io/nperiannan/termwright-team:%s in %s, then update again.", info.Version, tag, cc.dir)
		}
		emit("reach", "ok", "The team server runs "+info.Version+" - the team, its members, inventory, history and invite are kept")
		a.teamRemember(t.ID, r.SessionPath, name)
		return nil
	}
	if !c.data {
		return failf("find", "%s keeps its data inside the container (no volume at /data): a new container would lose the team. Back it up and set it up with a volume (see the team server guide).", name)
	}

	ref, err := a.teamImage(ctx, out, dk, r.Image, tag, emit, failf)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// swap: the old container is renamed and stopped, the new one started; from
	// here on every step runs even when cancelled, so the team is never left without one
	emit("start", "run", "Starting "+name+" with the new image…")
	prev := name + "-previous"
	_, _, _ = must(dk+" rm -f "+prev+" 2>&1", nil, time.Minute) // from an earlier update
	if msg, code, err := must(dk+" rename "+name+" "+prev+" 2>&1", nil, time.Minute); err != nil || code != 0 {
		return failf("start", "Could not rename %s: %s", name, firstLine(msg+errText(err)))
	}
	_, _, _ = must(dk+" stop -t 10 "+prev+" 2>&1", nil, time.Minute)
	back := func() {
		_, _, _ = must(dk+" rm -f "+name+" 2>&1", nil, time.Minute)
		_, _, _ = must(dk+" rename "+prev+" "+name+" 2>&1", nil, time.Minute)
		_, _, _ = must(dk+" start "+name+" 2>&1", nil, time.Minute)
	}
	cmd := fmt.Sprintf("%s run -d --name %s %s %s 2>&1", dk, name, strings.Join(c.args, " "), shQuote(ref))
	if msg, code, err := must(cmd, nil, 2*time.Minute); err != nil || code != 0 {
		back()
		return failf("start", "The new container did not start (%s); the old one runs again.", firstLine(msg+errText(err)))
	}
	emit("start", "ok", "Started "+name+" ("+ref+")")

	// the same server answers again: the team's certificate (the client checks
	// it), and the new version
	emit("reach", "run", "Waiting for the team server to answer at "+t.Address+"…")
	info, err := teamWaitFor(context.Background(), t)
	if err == nil && tag != "nightly" && info.Version != tag {
		err = fmt.Errorf("it answers with version %s, not %s", info.Version, tag)
	}
	if err != nil {
		logs, _, _ := must(dk+" logs --tail 3 "+name+" 2>&1", nil, time.Minute)
		back()
		return failf("reach", "The new container did not answer as expected at %s (%v; %s) - the old one runs again.", t.Address, err, firstLine(logs))
	}
	_, _, _ = must(dk+" rm "+prev+" 2>&1", nil, time.Minute)
	emit("reach", "ok", "The team server runs "+info.Version+" - the team, its members, inventory, history and invite are kept")
	a.teamRemember(t.ID, r.SessionPath, name)
	return nil
}

// teamVersionNow asks the team server its version ("" when it does not answer).
func teamVersionNow(ctx context.Context, t config.TeamConn) string {
	ic, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	info, err := team.NewClient(t.Address, t.Fingerprint, "").Info(ic)
	if err != nil {
		return ""
	}
	return info.Version
}

// teamWaitFor waits up to 90 seconds for the team server to answer.
func teamWaitFor(ctx context.Context, t config.TeamConn) (team.Info, error) {
	c := team.NewClient(t.Address, t.Fingerprint, "")
	end := time.Now().Add(90 * time.Second)
	for {
		ic, cancel := context.WithTimeout(ctx, 4*time.Second)
		info, err := c.Info(ic)
		cancel()
		if err == nil || time.Now().After(end) || ctx.Err() != nil {
			return info, err
		}
		time.Sleep(2 * time.Second)
	}
}

// teamRemember keeps the host and container of a team for the next update.
func (a *App) teamRemember(id, sessionPath, container string) {
	cur := a.cfg.Get().Teams
	for i := range cur {
		if cur[i].ID == id {
			cur[i].Host, cur[i].Container = sessionPath, container
		}
	}
	_ = a.cfg.SetTeams(cur)
}

// pemFingerprint is the SHA-256 fingerprint of the first certificate in text
// (as the server and invites write it), or "".
func pemFingerprint(text string) string {
	i := strings.Index(text, "-----BEGIN CERTIFICATE-----")
	if i < 0 {
		return ""
	}
	b, _ := pem.Decode([]byte(text[i:]))
	if b == nil {
		return ""
	}
	sum := sha256.Sum256(b.Bytes)
	return hex.EncodeToString(sum[:])
}

func lastLine(s string) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(l[len(l)-1])
}

// inspectFormat reads how a container runs, for parseContainer.
const inspectFormat = `{{.HostConfig.NetworkMode}}{{"\n"}}{{range $p, $b := .HostConfig.PortBindings}}{{range $b}}P|{{.HostIp}}|{{.HostPort}}|{{$p}}{{"\n"}}{{end}}{{end}}` +
	`{{range .Mounts}}M|{{.Type}}|{{.Name}}|{{.Source}}|{{.Destination}}{{"\n"}}{{end}}{{range .Config.Env}}E|{{.}}{{"\n"}}{{end}}` +
	`R|{{.HostConfig.RestartPolicy.Name}}|{{.HostConfig.RestartPolicy.MaximumRetryCount}}{{"\n"}}L|{{.HostConfig.Memory}}|{{.HostConfig.NanoCpus}}{{"\n"}}` +
	`{{range $k, $v := .Config.Labels}}B|{{$k}}={{$v}}{{"\n"}}{{end}}`

// container is how a team server's container runs: docker run arguments for
// a new one, whether /data is kept outside it, and its Compose project.
type container struct {
	args    []string
	data    bool
	compose struct{ project, dir, files, service string }
}

// parseContainer reads inspectFormat's output: the network, published ports,
// volumes, environment (but PATH), restart policy, memory and CPU limits and
// labels.
func parseContainer(spec string) (c container) {
	lines := strings.Split(spec, "\n")
	switch mode := strings.TrimSpace(lines[0]); mode {
	case "", "default", "bridge":
	case "host":
		c.args = append(c.args, "--network host")
	default:
		c.args = append(c.args, "--network "+shQuote(mode))
	}
	restart := "--restart unless-stopped"
	seen := map[string]bool{}
	for _, l := range lines[1:] {
		f := strings.Split(strings.TrimRight(l, "\r"), "|")
		switch {
		case f[0] == "P" && len(f) == 4:
			ip, hp, cp := f[1], f[2], strings.TrimSuffix(f[3], "/tcp")
			p := hp + ":" + cp
			if ip != "" && ip != "0.0.0.0" && ip != "::" {
				p = ip + ":" + p
			}
			if !seen[p] {
				seen[p] = true
				c.args = append(c.args, "-p "+shQuote(p))
			}
		case f[0] == "M" && len(f) == 5:
			src := f[3]
			if f[1] == "volume" {
				src = f[2]
			}
			if src == "" {
				continue
			}
			c.args = append(c.args, "-v "+shQuote(src+":"+f[4]))
			if f[4] == "/data" {
				c.data = true
			}
		case f[0] == "E" && len(f) >= 2:
			if e := strings.Join(f[1:], "|"); e != "" && !strings.HasPrefix(e, "PATH=") {
				c.args = append(c.args, "-e "+shQuote(e))
			}
		case f[0] == "R" && len(f) == 3:
			switch f[1] {
			case "always", "unless-stopped":
				restart = "--restart " + f[1]
			case "on-failure":
				restart = "--restart on-failure"
				if f[2] != "" && f[2] != "0" {
					restart += ":" + f[2]
				}
			case "no":
				restart = ""
			}
		case f[0] == "L" && len(f) == 3:
			if f[1] != "" && f[1] != "0" {
				c.args = append(c.args, "--memory "+f[1])
			}
			if n, err := strconv.ParseInt(f[2], 10, 64); err == nil && n > 0 {
				c.args = append(c.args, fmt.Sprintf("--cpus %g", float64(n)/1e9))
			}
		case f[0] == "B" && len(f) >= 2:
			kv := strings.Join(f[1:], "|")
			k, v, _ := strings.Cut(kv, "=")
			switch k {
			case "com.docker.compose.project":
				c.compose.project = v
			case "com.docker.compose.project.working_dir":
				c.compose.dir = v
			case "com.docker.compose.project.config_files":
				c.compose.files = v
			case "com.docker.compose.service":
				c.compose.service = v
			}
			if !strings.HasPrefix(k, "com.docker.compose.") && !strings.HasPrefix(k, "org.opencontainers.") {
				c.args = append(c.args, "--label "+shQuote(kv))
			}
		}
	}
	if restart != "" {
		c.args = append(c.args, restart)
	}
	return c
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
