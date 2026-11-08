package main

import (
	"context"
	"errors"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/secrets"
	"github.com/termwright/termwright/internal/team"
)

// Single sign-on from Termwright: the sign-in happens in the browser at the
// company's identity provider; Termwright waits for the team server to say
// it worked and keeps the member's token, like a join with the invite.

var (
	ssoMu     sync.Mutex
	ssoCancel context.CancelFunc
)

// TeamSSOCancel stops waiting for a sign-in in the browser.
func (a *App) TeamSSOCancel() {
	ssoMu.Lock()
	defer ssoMu.Unlock()
	if ssoCancel != nil {
		ssoCancel()
	}
}

// TeamJoinSSO joins the team of an invite with the company account.
func (a *App) TeamJoinSSO(invite string) (config.TeamConn, error) {
	iv, err := team.ParseInvite(invite)
	if err != nil {
		return config.TeamConn{}, err
	}
	for _, t := range a.cfg.Get().Teams {
		if t.Address == iv.Address && t.Fingerprint == iv.Fingerprint {
			return t, errors.New("you are already in this team (" + t.Name + ")")
		}
	}
	name, user, machine := a.teamIdentity()
	j, err := a.ssoSignIn(team.NewClient(iv.Address, iv.Fingerprint, ""), team.SSOStart{Name: name, User: user, Machine: machine})
	if err != nil {
		return config.TeamConn{}, err
	}
	return a.teamJoined(iv.Address, iv.Fingerprint, j, "", "")
}

// TeamSSOAgain signs in again with the company account (the team asks every
// few weeks); the member and the team folder stay.
func (a *App) TeamSSOAgain(id string) error {
	t, _, err := a.teamConn(id)
	if err != nil {
		return err
	}
	_, user, machine := a.teamIdentity()
	j, err := a.ssoSignIn(team.NewClient(t.Address, t.Fingerprint, ""), team.SSOStart{Member: t.MemberID, User: user, Machine: machine})
	if err != nil {
		return err
	}
	if err := secrets.Set(teamTokenKey(t.ID), j.Token); err != nil {
		return err
	}
	a.startTeamSync()
	return nil
}

// ssoSignIn opens the sign-in in the browser and waits (5 minutes at most).
func (a *App) ssoSignIn(c *team.Client, st team.SSOStart) (team.Joined, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	ssoMu.Lock()
	if ssoCancel != nil {
		ssoCancel()
	}
	ssoCancel = cancel
	ssoMu.Unlock()
	defer cancel()
	u, req, secret, err := c.SSOBegin(ctx, st)
	if err != nil {
		return team.Joined{}, err
	}
	if a.ctx != nil {
		wruntime.BrowserOpenURL(a.ctx, u)
	}
	for {
		j, waiting, err := c.SSOCollect(ctx, req, secret)
		switch {
		case ctx.Err() == context.Canceled:
			return team.Joined{}, errors.New("cancelled")
		case ctx.Err() != nil:
			return team.Joined{}, errors.New("the sign-in in the browser did not finish in 5 minutes - try again")
		case err != nil:
			return team.Joined{}, err
		case !waiting:
			return j, nil
		}
	}
}
