package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/secrets"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/team"
)

// Teams: the team servers this Termwright joined, to see who uses which
// device. Nothing happens - no connection, nothing sent - until a team is
// joined or set up.

// TeamView is a joined team with how its server answers now.
type TeamView struct {
	config.TeamConn
	Reachable bool   `json:"reachable"`
	Error     string `json:"error"`
	Members   int    `json:"members"`
	Online    int    `json:"online"`
	Version   string `json:"version"`
	// Update is the version this Termwright would update the team server to,
	// when the server is older ("" when it is current or newer)
	Update string `json:"update,omitempty"`
	// Newer: the team server is newer than this Termwright
	Newer bool `json:"newer,omitempty"`
	// SignIn: single sign-on asks to sign in again
	SignIn bool `json:"signIn,omitempty"`
}

// TeamDetails is a team with its members and, for owners and co-owners, the invite link.
type TeamDetails struct {
	team.Team
	ID         string `json:"id"`
	Address    string `json:"address"`
	InviteLink string `json:"inviteLink"`
}

func teamTokenKey(id string) string { return "team:" + id }

// teamIdentity is how the teams see this person: a display name, the OS user and the computer.
func (a *App) teamIdentity() (name, usr, machine string) {
	machine, _ = os.Hostname()
	machine = strings.TrimSuffix(machine, ".local")
	if u, err := user.Current(); err == nil {
		usr = u.Username
		if i := strings.LastIndexAny(usr, `\`); i >= 0 { // DOMAIN\user on Windows
			usr = usr[i+1:]
		}
		name = strings.TrimSpace(u.Name)
	}
	if n := strings.TrimSpace(a.cfg.Get().TeamDisplayName); n != "" {
		name = n
	}
	if name == "" {
		name = usr
	}
	return
}

// TeamIdentity is the name the teams see and "user@computer".
func (a *App) TeamIdentity() map[string]string {
	n, u, m := a.teamIdentity()
	return map[string]string{"name": n, "user": u, "machine": m}
}

func (a *App) teamConn(id string) (config.TeamConn, *team.Client, error) {
	for _, t := range a.cfg.Get().Teams {
		if t.ID == id {
			tok, _ := secrets.Lookup(teamTokenKey(id))
			return t, team.NewClient(t.Address, t.Fingerprint, tok), nil
		}
	}
	return config.TeamConn{}, nil, errors.New("no such team")
}

func teamCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 12*time.Second)
}

// Teams lists the joined teams, asking each server how it is (at the same time).
func (a *App) Teams() []TeamView {
	ts := a.cfg.Get().Teams
	out := make([]TeamView, len(ts))
	var wg sync.WaitGroup
	for i, t := range ts {
		out[i] = TeamView{TeamConn: t}
		wg.Add(1)
		go func(v *TeamView) {
			defer wg.Done()
			_, c, _ := a.teamConn(v.ID)
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			tm, err := c.Team(ctx)
			if err != nil {
				v.Error = err.Error()
				switch {
				case team.NeedsSSO(err):
					v.Error, v.SignIn = "Sign in again with your company account - the team asks for it every few weeks.", true
				case team.Unauthorized(err):
					v.Error = "You are no longer a member of this team."
				}
				return
			}
			v.Reachable, v.Members, v.Version, v.Role = true, len(tm.Members), tm.Version, tm.Role
			cmp := compareVersions(tm.Version, Version)
			if cmp > 0 && sameNumbers(tm.Version, Version) {
				cmp = 0 // a nightly Termwright and the release of its numbers
			}
			switch {
			case cmp < 0:
				v.Update = teamImageTag()
			case cmp > 0:
				v.Newer = true
			}
			for _, m := range tm.Members {
				if m.Online {
					v.Online++
				}
			}
		}(&out[i])
	}
	wg.Wait()
	// keep the role the server reports (an owner may have changed it)
	changed := false
	cur := a.cfg.Get().Teams
	for i := range cur {
		for _, v := range out {
			if v.ID == cur[i].ID && v.Reachable && v.Role != cur[i].Role {
				cur[i].Role, changed = v.Role, true
			}
		}
	}
	if changed {
		_ = a.cfg.SetTeams(cur)
	}
	return out
}

// TeamDetails is a team's members (and the invite link for owners and co-owners).
func (a *App) TeamDetails(id string) (TeamDetails, error) {
	t, c, err := a.teamConn(id)
	if err != nil {
		return TeamDetails{}, err
	}
	ctx, cancel := teamCtx()
	defer cancel()
	tm, err := c.Team(ctx)
	if err != nil {
		return TeamDetails{}, err
	}
	d := TeamDetails{Team: tm, ID: id, Address: t.Address}
	if tm.Invite != "" {
		d.InviteLink = team.Invitation{Address: t.Address, Key: tm.Invite, Fingerprint: t.Fingerprint, Name: tm.Name}.String()
	}
	return d, nil
}

// TeamInviteInfo checks an invite before joining: the team's name and size.
func (a *App) TeamInviteInfo(invite string) (team.Info, error) {
	iv, err := team.ParseInvite(invite)
	if err != nil {
		return team.Info{}, err
	}
	ctx, cancel := teamCtx()
	defer cancel()
	return team.NewClient(iv.Address, iv.Fingerprint, "").Info(ctx)
}

// TeamJoin joins the team of an invite link.
func (a *App) TeamJoin(invite string) (config.TeamConn, error) {
	iv, err := team.ParseInvite(invite)
	if err != nil {
		return config.TeamConn{}, err
	}
	for _, t := range a.cfg.Get().Teams {
		if t.Address == iv.Address && t.Fingerprint == iv.Fingerprint {
			return t, errors.New("you are already in this team (" + t.Name + ")")
		}
	}
	return a.teamJoin(iv.Address, iv.Fingerprint, team.JoinRequest{Invite: iv.Key}, "", "")
}

func (a *App) teamJoin(addr, fp string, req team.JoinRequest, host, container string) (config.TeamConn, error) {
	req.Name, req.User, req.Machine = a.teamIdentity()
	ctx, cancel := teamCtx()
	defer cancel()
	j, err := team.NewClient(addr, fp, "").Join(ctx, req)
	if err != nil {
		return config.TeamConn{}, err
	}
	return a.teamJoined(addr, fp, j, host, container)
}

// teamJoined keeps a team this Termwright joined (the member's token in the keychain).
func (a *App) teamJoined(addr, fp string, j team.Joined, host, container string) (config.TeamConn, error) {
	t := config.TeamConn{ID: randHex(8), Name: j.Team, Address: addr, Fingerprint: team.NormFingerprint(fp), MemberID: j.MemberID, Role: j.Role, Host: host, Container: container}
	// the same team again (set up again, or reconnected as its owner): it keeps
	// its id, so its team folder stays the same
	var others []config.TeamConn
	for _, o := range a.cfg.Get().Teams {
		if o.Address == t.Address && o.Fingerprint == t.Fingerprint {
			t.ID = o.ID
		} else {
			others = append(others, o)
		}
	}
	if err := secrets.Set(teamTokenKey(t.ID), j.Token); err != nil {
		return config.TeamConn{}, fmt.Errorf("the keychain could not keep the team key: %w", err)
	}
	if err := a.cfg.SetTeams(append(others, t)); err != nil {
		return config.TeamConn{}, err
	}
	a.startTeamSync()
	return t, nil
}

// TeamRotateInvite makes a new invite (the old link stops working) and returns it.
func (a *App) TeamRotateInvite(id string) (string, error) {
	t, c, err := a.teamConn(id)
	if err != nil {
		return "", err
	}
	ctx, cancel := teamCtx()
	defer cancel()
	k, err := c.RotateInvite(ctx)
	if err != nil {
		return "", err
	}
	return team.Invitation{Address: t.Address, Key: k, Fingerprint: t.Fingerprint, Name: t.Name}.String(), nil
}

// TeamSetRole makes a member co-owner or member.
func (a *App) TeamSetRole(id, memberID, role string) error {
	_, c, err := a.teamConn(id)
	if err != nil {
		return err
	}
	ctx, cancel := teamCtx()
	defer cancel()
	return c.SetRole(ctx, memberID, role)
}

// TeamRemoveMember takes a member out of a team.
func (a *App) TeamRemoveMember(id, memberID string) error {
	_, c, err := a.teamConn(id)
	if err != nil {
		return err
	}
	ctx, cancel := teamCtx()
	defer cancel()
	return c.Remove(ctx, memberID)
}

// TeamLeave leaves a team. With force, the team is forgotten here even when its
// server cannot be reached (or no longer knows this member).
func (a *App) TeamLeave(id string, force bool) error {
	_, c, err := a.teamConn(id)
	if err != nil {
		return err
	}
	ctx, cancel := teamCtx()
	defer cancel()
	if err := c.Leave(ctx); err != nil && !force && !team.Unauthorized(err) {
		return err
	}
	_ = secrets.Delete(teamTokenKey(id))
	var keep []config.TeamConn
	var gone config.TeamConn
	for _, t := range a.cfg.Get().Teams {
		if t.ID != id {
			keep = append(keep, t)
		} else {
			gone = t
		}
	}
	if err := a.cfg.SetTeams(keep); err != nil {
		return err
	}
	a.startTeamSync()
	a.removeTeamFolder(gone)
	return nil
}

// ---- Setting a team server up on a Linux host with Docker ----

// TeamSetUpRequest: the SSH session of the host, the team's name, the port
// (0 = the first free one from 7443) and how the image gets there
// ("download" from GitHub by the host, falling back to "copy" over SSH).
type TeamSetUpRequest struct {
	SessionPath string `json:"sessionPath"`
	Name        string `json:"name"`
	Port        int    `json:"port"`
	Image       string `json:"image"`
	HostNetwork bool   `json:"hostNetwork"` // the container on the host's network: scans see MAC addresses
}

// TeamSetUpStep is one line of the setup's progress (event "team:setup").
type TeamSetUpStep struct {
	Run   string `json:"run"`
	Step  string `json:"step"`
	State string `json:"state"` // run, ok, fail, info
	Text  string `json:"text"`
}

// TeamSetUpResult ends a setup (event "team:setup:done").
type TeamSetUpResult struct {
	Run    string `json:"run"`
	TeamID string `json:"teamId"`
	Invite string `json:"invite"`
	Error  string `json:"error"`
}

var teamSetups sync.Map // run id → cancel

// TeamSetUp starts setting a team server up and returns the run id; progress
// and the end come as events.
func (a *App) TeamSetUp(r TeamSetUpRequest) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	if strings.TrimSpace(r.Name) == "" {
		return "", errors.New("give the team a name")
	}
	if teamSlug(r.Name) == "" {
		return "", errors.New("the team name needs letters or digits")
	}
	if r.Port != 0 && (r.Port < 1024 || r.Port > 65535) {
		return "", errors.New("pick a port from 1024 to 65535 (or leave it empty for a free one)")
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
		res := TeamSetUpResult{Run: id}
		tc, invite, err := a.teamSetUp(ctx, id, r, s.Hostname, emit)
		if err != nil {
			res.Error = err.Error()
			if ctx.Err() == context.Canceled {
				res.Error = "Cancelled."
			}
		} else {
			res.TeamID, res.Invite = tc.ID, invite
		}
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "team:setup:done", res)
		}
	}()
	return id, nil
}

// TeamSetUpCancel stops a setup.
func (a *App) TeamSetUpCancel(id string) {
	if c, ok := teamSetups.Load(id); ok {
		c.(context.CancelFunc)()
	}
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// teamSlug makes the container name part of a team name: "Switching Lab B" → "switching-lab-b".
func teamSlug(name string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

// shQuote quotes a value for the remote shell.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// teamImageTag: the release's version, or "nightly" for nightly and development
// builds (the nightly build publishes the server too).
func teamImageTag() string {
	if strings.ContainsAny(Version, "-~+") || Version == "" {
		return "nightly"
	}
	return Version
}

var portRe = regexp.MustCompile(`:(\d+)(?:->|\s|$)`)

// freePort is the first port from 7443 to 7499 that nothing listens on.
func freePort(used map[int]bool) int {
	for p := team.DefaultPort; p < 7500; p++ {
		if !used[p] {
			return p
		}
	}
	return 0
}

func (a *App) teamSetUp(ctx context.Context, id string, r TeamSetUpRequest, host string, emit func(step, state, text string)) (config.TeamConn, string, error) {
	sess, err := a.store.Get(r.SessionPath)
	if err != nil {
		return config.TeamConn{}, "", err
	}
	sess = a.withFolderDefaults(sess)
	failf := func(step, f string, v ...any) error {
		msg := fmt.Sprintf(f, v...)
		emit(step, "fail", msg)
		return errors.New(msg)
	}

	out, _, closeSSH, err := a.teamLogin(ctx, id, sess, host, emit, failf)
	if err != nil {
		return config.TeamConn{}, "", err
	}
	defer closeSSH()
	dk, err := teamDocker(out, emit, failf)
	if err != nil {
		return config.TeamConn{}, "", err
	}

	// one container per team; a second setup of the same team would only clash
	slug := teamSlug(r.Name)
	name := "termwright-team-" + slug
	// the owner setup code the server prints: the (after+1)th one in its log
	ownerCodeOf := func(after int) (code, fp string) {
		for i := 0; i < 30 && ctx.Err() == nil; i++ {
			logs, _, _ := out(dk+" logs "+name+" 2>&1 | grep 'TERMWRIGHT-TEAM owner-code='", nil, 20*time.Second)
			lines := strings.Split(strings.TrimSpace(logs), "\n")
			if logs != "" && len(lines) > after {
				for _, f := range strings.Fields(lines[len(lines)-1]) {
					if v, ok := strings.CutPrefix(f, "owner-code="); ok {
						code = v
					} else if v, ok := strings.CutPrefix(f, "fingerprint="); ok {
						fp = v
					}
				}
				return
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
		return
	}
	if have, _, _ := out(dk+" ps -a --filter "+shQuote("name=^/"+name+"$")+" --format '{{.Status}} {{.Ports}}'", nil, 30*time.Second); have != "" {
		// the team's server runs here already: this Termwright lost the team (or
		// it is set up again) - reconnect as its owner; its data, members,
		// certificate and invite stay
		emit("port", "info", "This host runs "+name+" already ("+firstLine(have)+") - reconnecting to it as its owner")
		if !strings.HasPrefix(have, "Up") {
			_, _, _ = out(dk+" start "+name+" 2>&1", nil, time.Minute)
		}
		port := 0
		if ps, _, _ := out(dk+" port "+name+" 7443/tcp 2>/dev/null", nil, 20*time.Second); ps != "" {
			if m := portRe.FindStringSubmatch(firstLine(ps) + "\n"); m != nil {
				port, _ = strconv.Atoi(m[1])
			}
		}
		if port == 0 { // host network: the port it listens on
			env, _, _ := out(dk+" inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "+name, nil, 20*time.Second)
			port = team.DefaultPort
			for _, l := range strings.Split(env, "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(l), "TW_LISTEN="); ok {
					if i := strings.LastIndex(v, ":"); i >= 0 {
						if p, err := strconv.Atoi(v[i+1:]); err == nil {
							port = p
						}
					}
				}
			}
		}
		before, _, _ := out(dk+" logs "+name+" 2>&1 | grep -c 'TERMWRIGHT-TEAM owner-code='", nil, 20*time.Second)
		n, _ := strconv.Atoi(strings.TrimSpace(before))
		if msg, code, err := out(dk+" exec "+name+" /termwright-team -recover-owner -data /data 2>&1", nil, 30*time.Second); err != nil || code != 0 {
			return config.TeamConn{}, "", failf("start", "The team server %s is too old to reconnect to (%s). Update it (docker pull, then start it again with the same volume) or remove it, then set up again.", name, firstLine(msg+errText(err)))
		}
		ownerCode, fp := ownerCodeOf(n)
		if ownerCode == "" || fp == "" {
			return config.TeamConn{}, "", failf("start", "The team server %s did not give a new owner code - is it running? (docker logs %s)", name, name)
		}
		emit("start", "ok", "The team server answered with a new owner code")
		return a.teamReachAndJoin(ctx, r, host, name, port, ownerCode, fp, emit, failf)
	}

	// the port
	emit("port", "run", "Finding a free port…")
	lst, _, _ := out(`(ss -Htln 2>/dev/null || netstat -tln 2>/dev/null) | awk '{print $4}'; `+dk+` ps --format '{{.Ports}}' 2>/dev/null`, nil, 30*time.Second)
	used := map[int]bool{}
	for _, m := range portRe.FindAllStringSubmatch(lst+"\n", -1) {
		if p, err := strconv.Atoi(m[1]); err == nil {
			used[p] = true
		}
	}
	port := r.Port
	if port == 0 {
		if port = freePort(used); port == 0 {
			return config.TeamConn{}, "", failf("port", "No free port from 7443 to 7499 - type one.")
		}
	} else if used[port] {
		return config.TeamConn{}, "", failf("port", "Port %d is in use on this host - pick another one, or leave it empty.", port)
	}
	note := ""
	if used[80] || used[8080] {
		note = " (80 and 8080 are in use - not touched)"
	}
	emit("port", "ok", fmt.Sprintf("Port %d free%s", port, note))

	// the image: downloaded by the host, or copied over SSH and built there
	ref, err := a.teamImage(ctx, out, dk, r.Image, emit, failf)
	if err != nil {
		return config.TeamConn{}, "", err
	}

	// start it
	emit("start", "run", "Starting "+name+" (restarts with the host)…")
	netArgs := fmt.Sprintf("-p %d:%d", port, team.DefaultPort)
	if r.HostNetwork {
		netArgs = fmt.Sprintf("--network host -e TW_LISTEN=:%d", port)
	}
	cmd := fmt.Sprintf("%s run -d --name %s --restart unless-stopped %s -v %s:/data -e TW_TEAM_NAME=%s %s 2>&1",
		dk, name, netArgs, name, shQuote(strings.TrimSpace(r.Name)), shQuote(ref))
	if msg, code, err := out(cmd, nil, 2*time.Minute); err != nil || code != 0 {
		return config.TeamConn{}, "", failf("start", "The container did not start: %s", firstLine(msg+errText(err)))
	}
	ownerCode, fp := ownerCodeOf(0)
	if ctx.Err() != nil {
		return config.TeamConn{}, "", ctx.Err()
	}
	if ownerCode == "" || fp == "" {
		logs, _, _ := out(dk+" logs --tail 5 "+name+" 2>&1", nil, 20*time.Second)
		return config.TeamConn{}, "", failf("start", "The team server started but did not report its setup code: %s", firstLine(logs))
	}
	emit("start", "ok", "Started "+name)

	return a.teamReachAndJoin(ctx, r, host, name, port, ownerCode, fp, emit, failf)
}

// remoteRun runs a command on the host: its output, exit code and error.
type remoteRun func(cmd string, stdin []byte, timeout time.Duration) (string, int, error)

// teamLogin logs in to the host of a team server over SSH: out runs a command
// (stopped when ctx ends), must runs one even after ctx ended (to put things back).
func (a *App) teamLogin(ctx context.Context, id string, sess session.Session, host string, emit func(step, state, text string),
	failf func(step, f string, v ...any) error) (out, must remoteRun, closeSSH func(), err error) {
	emit("login", "run", "Logging in to "+sess.Name+"…")
	run, closeSSH, err := a.terms.ExecSession(ctx, "teamsetup-"+id, sess)
	if err != nil {
		return nil, nil, nil, failf("login", "Could not log in to %s: %v", sess.Name, err)
	}
	with := func(parent context.Context) remoteRun {
		return func(cmd string, stdin []byte, timeout time.Duration) (string, int, error) {
			c, cancel := context.WithTimeout(parent, timeout)
			defer cancel()
			var b strings.Builder
			code, err := run(c, cmd, stdin, func(l string) { b.WriteString(l); b.WriteString("\n") })
			return strings.TrimSpace(b.String()), code, err
		}
	}
	out, must = with(ctx), with(context.Background())
	who, _, _ := out("id -un", nil, 20*time.Second)
	emit("login", "ok", "Logged in as "+who+"@"+host)
	return out, must, closeSSH, nil
}

// teamDocker checks Docker on the host and whether this user may use it: the
// docker command to use ("docker" or "sudo -n docker").
func teamDocker(out remoteRun, emit func(step, state, text string), failf func(step, f string, v ...any) error) (string, error) {
	emit("docker", "run", "Checking Docker…")
	who, _, _ := out("id -un", nil, 20*time.Second)
	dk := "docker"
	ver, code, err := out(`docker version --format '{{.Server.Version}}' 2>&1`, nil, 30*time.Second)
	if err != nil {
		return "", failf("docker", "Docker could not be checked: %v", err)
	}
	if code != 0 {
		low := strings.ToLower(ver)
		switch {
		case strings.Contains(low, "not found"):
			return "", failf("docker", "Docker is not installed on this host. Install Docker (or Podman with its docker command), then try again.")
		case strings.Contains(low, "permission denied"):
			if v2, c2, _ := out(`sudo -n docker version --format '{{.Server.Version}}' 2>&1`, nil, 30*time.Second); c2 == 0 {
				dk, ver = "sudo -n docker", v2
			} else {
				return "", failf("docker", "%s may not use Docker. On the host: sudo usermod -aG docker %s (then log in again), or allow sudo without a password for docker.", who, who)
			}
		default:
			return "", failf("docker", "Docker does not answer: %s", firstLine(ver))
		}
	}
	emit("docker", "ok", "Docker "+firstLine(ver)+" - "+who+" may use it")
	return dk, nil
}

// teamImage gets the team server's image for this Termwright onto the host:
// downloaded by the host ("download", falling back to copying), or copied over
// SSH and built there ("copy"). It returns the image's name.
func (a *App) teamImage(ctx context.Context, out remoteRun, dk, mode string, emit func(step, state, text string),
	failf func(step, f string, v ...any) error) (string, error) {
	tag := teamImageTag()
	owner := strings.ToLower(strings.SplitN(a.cfg.Get().UpdateRepo, "/", 2)[0])
	if owner == "" {
		owner = "nperiannan"
	}
	ref := "ghcr.io/" + owner + "/termwright-team:" + tag
	copied := mode == "copy"
	if !copied {
		emit("image", "run", "Downloading "+ref+" on the host…")
		if msg, code, err := out(dk+" pull -q "+shQuote(ref)+" 2>&1", nil, 6*time.Minute); err != nil || code != 0 {
			emit("image", "info", "The host could not download the image ("+firstLine(msg)+") - copying it over SSH instead")
			copied = true
		} else {
			emit("image", "ok", "Image "+ref)
		}
	}
	if copied {
		arch, _, _ := out("uname -m", nil, 20*time.Second)
		ga := map[string]string{"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[strings.TrimSpace(arch)]
		if ga == "" {
			return "", failf("image", "The team server is built for x86-64 and ARM64 Linux; this host is %s.", arch)
		}
		emit("image", "run", "Getting the team server for linux/"+ga+"…")
		bin, err := teamBinarySource(a, ctx, ga, tag)
		if err != nil {
			return "", failf("image", "Could not get the team server program: %v", err)
		}
		emit("image", "run", fmt.Sprintf("Copying it to the host (%.1f MB) and building the image…", float64(len(bin))/1e6))
		ref = "termwright-team:" + tag
		script := `set -e; d="$HOME/.termwright-team/image"; mkdir -p "$d"; cat > "$d/termwright-team"; chmod 755 "$d/termwright-team"; ` +
			`printf '%s\n' 'FROM scratch' 'COPY termwright-team /termwright-team' 'VOLUME /data' 'EXPOSE 7443' 'ENTRYPOINT ["/termwright-team"]' > "$d/Dockerfile"; ` +
			dk + ` build -q -t ` + shQuote(ref) + ` "$d" 2>&1`
		if msg, code, err := out(script, bin, 6*time.Minute); err != nil || code != 0 {
			return "", failf("image", "The image could not be built on the host: %s", firstLine(msg+errText(err)))
		}
		emit("image", "ok", "Image "+ref+" (copied over SSH)")
	}
	return ref, nil
}

// sameNumbers: two versions with the same numbers ("0.30.0" and "0.30.0~git1234567").
func sameNumbers(a, b string) bool {
	core := func(v string) string {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		if i := strings.IndexAny(v, "-~+"); i >= 0 {
			v = v[:i]
		}
		return v
	}
	return core(a) != "" && compareVersions(core(a), core(b)) == 0
}

// compareVersions compares two Termwright versions by their numbers: -1, 0 or
// 1. Of the same numbers, a release is newer than a development or nightly
// build ("0.30.0-dev~git1234567", "0.30.0~git1234567"); "" and "dev" are oldest.
func compareVersions(a, b string) int {
	parse := func(v string) (n [3]int, pre bool, ok bool) {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		core := v
		if i := strings.IndexAny(v, "-~+"); i >= 0 {
			core, pre = v[:i], true
		}
		f := strings.Split(core, ".")
		if len(f) == 0 || len(f) > 3 {
			return n, pre, false
		}
		for i, x := range f {
			d, err := strconv.Atoi(x)
			if err != nil {
				return n, pre, false
			}
			n[i] = d
		}
		return n, pre, true
	}
	na, pa, oa := parse(a)
	nb, pb, ob := parse(b)
	switch {
	case !oa && !ob:
		return 0
	case !oa:
		return -1
	case !ob:
		return 1
	}
	for i := range na {
		if na[i] != nb[i] {
			if na[i] < nb[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case pa == pb:
		return 0
	case pa:
		return -1
	}
	return 1
}

// teamReachAndJoin reaches the team server from this computer and joins it as its owner.
func (a *App) teamReachAndJoin(ctx context.Context, r TeamSetUpRequest, host, name string, port int, ownerCode, fp string,
	emit func(step, state, text string), failf func(step, f string, v ...any) error) (config.TeamConn, string, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	emit("reach", "run", "Reaching "+addr+" from this computer…")
	c := team.NewClient(addr, fp, "")
	var ierr error
	for i := 0; i < 8; i++ {
		ic, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, ierr = c.Info(ic)
		cancel()
		if ierr == nil || ctx.Err() != nil {
			break
		}
		time.Sleep(time.Second)
	}
	if ierr != nil {
		return config.TeamConn{}, "", failf("reach", "The team server runs, but this computer cannot reach %s (%v). A firewall on the host may block port %d - e.g. sudo ufw allow %d/tcp, or sudo firewall-cmd --add-port=%d/tcp --permanent && sudo firewall-cmd --reload. The server keeps running: set it up again to finish, or join with an invite.",
			addr, ierr, port, port, port, port)
	}
	tc, err := a.teamJoin(addr, fp, team.JoinRequest{OwnerCode: ownerCode}, r.SessionPath, name)
	if err != nil {
		return config.TeamConn{}, "", failf("reach", "Could not join as the owner: %v", err)
	}
	emit("reach", "ok", "Reached "+addr+" - you are the owner of "+tc.Name)
	d, err := a.TeamDetails(tc.ID)
	if err != nil {
		return tc, "", nil
	}
	return tc, d.InviteLink, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 240 {
		s = s[:240] + "…"
	}
	return s
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return " " + err.Error()
}

// teamBinarySource gets the server program for the copy over SSH (tests replace it).
var teamBinarySource = (*App).teamServerBinary

// teamServerBinary downloads the team server program for linux/arch from the
// GitHub release of this version (the latest release for development builds).
func (a *App) teamServerBinary(ctx context.Context, arch, tag string) ([]byte, error) {
	repo := a.cfg.Get().UpdateRepo
	if repo == "" {
		repo = "nperiannan/termwright"
	}
	file := "termwright-team-linux-" + arch + ".tar.gz"
	u := "https://github.com/" + repo + "/releases/download/v" + tag + "/" + file
	if tag == "nightly" {
		u = "https://github.com/" + repo + "/releases/download/nightly/" + file
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", u, resp.Status)
	}
	return teamBinaryFromTarGz(resp.Body)
}

func teamBinaryFromTarGz(r io.Reader) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("the package has no termwright-team program")
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && (h.Name == "termwright-team" || strings.HasSuffix(h.Name, "/termwright-team")) {
			return io.ReadAll(io.LimitReader(tr, 64<<20))
		}
	}
}
