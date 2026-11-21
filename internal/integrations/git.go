package integrations

import (
	"net/url"
	"strings"
)

// GitArgs returns the git options and environment that give git the saved user and token for the
// configured host, through a credential helper that reads them from the environment: the token is
// never in the command line, the remote's address or the repository's config. For other hosts git
// uses its own saved logins.
func GitArgs(c Git, token string) (args []string, env []string) {
	h := strings.TrimSpace(c.Host)
	if h == "" || token == "" {
		return nil, nil
	}
	if u, err := url.Parse(h); err == nil && u.Host != "" {
		h = u.Host
	}
	user := c.User
	if user == "" {
		user = "git"
	}
	ctx := "credential.https://" + h + ".helper"
	return []string{"-c", ctx + "=", "-c", ctx + `=!f() { test "$1" = get && echo "username=$TW_GIT_USER" && echo "password=$TW_GIT_TOKEN"; }; f`},
		[]string{"TW_GIT_USER=" + user, "TW_GIT_TOKEN=" + token}
}

// GitCheckURL is the address "Test connection" lists the user's repositories at (GitHub, GitLab and
// Gitea/Bitbucket-like hosts answer at one of these with the token).
func GitCheckURL(c Git) string {
	h := strings.TrimSpace(c.Host)
	if u, err := url.Parse(h); err == nil && u.Host != "" {
		h = u.Host
	}
	if h == "github.com" {
		return "https://api.github.com/user"
	}
	if strings.Contains(h, "gitlab") {
		return "https://" + h + "/api/v4/user"
	}
	return "https://" + h + "/api/v1/user"
}
