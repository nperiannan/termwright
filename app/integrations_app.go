package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/integrations"
	"github.com/termwright/termwright/internal/secrets"
)

// integrationsCfg is the Integrations page of the settings.
func (a *App) integrationsCfg() integrations.Config {
	s, _ := a.GetSettings()
	return s.Integrations
}

func integrationToken(kind string) string {
	t, _ := secrets.Lookup(integrations.TokenKey(kind))
	return t
}

func (a *App) qtestClient() integrations.QTestClient {
	return integrations.QTestClient{Cfg: a.integrationsCfg().QTest, Token: integrationToken(integrations.KindQTest)}
}

func (a *App) jiraClient() integrations.JiraClient {
	return integrations.JiraClient{Cfg: a.integrationsCfg().Jira, Token: integrationToken(integrations.KindJira)}
}

// gitCreds: git options and environment with the saved Git host token (nothing when none is saved).
func (a *App) gitCreds() ([]string, []string) {
	return integrations.GitArgs(a.integrationsCfg().Git, integrationToken(integrations.KindGit))
}

func validKind(kind string) error {
	switch kind {
	case integrations.KindQTest, integrations.KindJira, integrations.KindGit:
		return nil
	}
	return errors.New("unknown integration " + kind)
}

// IntegrationTokens says which integrations have a token saved (the tokens are never returned).
func (a *App) IntegrationTokens() map[string]bool {
	m := map[string]bool{}
	for _, k := range []string{integrations.KindQTest, integrations.KindJira, integrations.KindGit} {
		m[k] = integrationToken(k) != ""
	}
	return m
}

// IntegrationSetToken saves a token in the OS credential store ("" forgets it).
func (a *App) IntegrationSetToken(kind, token string) error {
	if err := validKind(kind); err != nil {
		return err
	}
	if token = strings.TrimSpace(token); token == "" {
		return secrets.Delete(integrations.TokenKey(kind))
	}
	return secrets.Set(integrations.TokenKey(kind), token)
}

// IntegrationTest checks an integration's address and user (cfg: the values in the options dialog,
// nil: the saved ones) with its saved token.
func (a *App) IntegrationTest(kind string, cfg *integrations.Config) (string, error) {
	c := a.integrationsCfg()
	if cfg != nil {
		c = *cfg
	}
	if err := validKind(kind); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch kind {
	case integrations.KindQTest:
		return integrations.QTestClient{Cfg: c.QTest, Token: integrationToken(kind)}.Check(ctx)
	case integrations.KindJira:
		return integrations.JiraClient{Cfg: c.Jira, Token: integrationToken(kind)}.Check(ctx)
	}
	g := c.Git
	tok := integrationToken(integrations.KindGit)
	if g.Host == "" || tok == "" {
		return "", errors.New("set the Git host and save a token first")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", integrations.GitCheckURL(g), nil)
	req.Header.Set("Authorization", "token "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	res.Body.Close()
	switch {
	case res.StatusCode == 401 || res.StatusCode == 403:
		return "", errors.New(g.Host + " refused the token")
	case res.StatusCode >= 300:
		return "the token is saved; " + g.Host + " has no user API to check it (" + res.Status + ") - Clone, Pull and Push use it", nil
	}
	return "connected to " + g.Host + " - Clone, Pull and Push of the tests folder use this token", nil
}
