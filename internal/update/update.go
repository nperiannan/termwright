// Package update checks GitHub releases for a newer Termwright build and
// downloads the matching installer.
//
// Channels:
//   - "stable":  the latest non-prerelease release (tags vX.Y.Z), compared by version number.
//   - "nightly": the "nightly" prerelease built from main, compared by git commit.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// API is the GitHub API base URL (overridden in tests).
var API = "https://api.github.com"

// Info describes the newest available build on a channel.
type Info struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Version   string `json:"version"` // release version; for a nightly, the version it was built as
	Newer     bool   `json:"newer"`
	Notes     string `json:"notes"`
	Page      string `json:"page"` // release web page
	Published string `json:"published"`
	AssetName string `json:"assetName"`
	AssetURL  string `json:"assetUrl"` // API URL of the asset (works for private repos with a token)
	AssetSize int64  `json:"assetSize"`
}

type release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

var client = &http.Client{Timeout: 30 * time.Second}

func get(ctx context.Context, url, token, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "Termwright-updater")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return client.Do(req)
}

// ErrNoNightly means no nightly build is published.
var ErrNoNightly = errors.New("no nightly build is available")

// Status is the result of a full check: the newest stable release and, when
// one is published, the newest nightly build.
type Status struct {
	Current    string `json:"current"`
	Nightly    bool   `json:"runningNightly"` // this build is a nightly / development build
	Stable     *Info  `json:"stable"`
	StableErr  string `json:"stableErr"`
	Latest     *Info  `json:"nightly"`
	NightlyErr string `json:"nightlyErr"`
}

// CheckAll looks up both channels.
func CheckAll(ctx context.Context, repo, token, current string) Status {
	st := Status{Current: current, Nightly: shaOf(current) != ""}
	if i, err := Check(ctx, repo, "stable", token, current); err == nil {
		st.Stable = &i
	} else {
		st.StableErr = err.Error()
	}
	if i, err := Check(ctx, repo, "nightly", token, current); err == nil {
		st.Latest = &i
	} else if !errors.Is(err, ErrNoNightly) {
		st.NightlyErr = err.Error()
	}
	return st
}

// Check looks up the newest release of repo ("owner/name") on channel.
func Check(ctx context.Context, repo, channel, token, current string) (Info, error) {
	info := Info{Current: current}
	if !regexp.MustCompile(`^[\w.-]+/[\w.-]+$`).MatchString(repo) {
		return info, fmt.Errorf("invalid repository %q (expected owner/name)", repo)
	}
	url := API + "/repos/" + repo + "/releases/latest"
	if channel == "nightly" {
		url = API + "/repos/" + repo + "/releases/tags/nightly"
	}
	resp, err := get(ctx, url, token, "application/vnd.github+json")
	if err != nil {
		return info, fmt.Errorf("cannot reach the update server (check the network or proxy): %w", err)
	}
	if token != "" && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound) {
		// an expired token, or one limited to other repositories: public repositories need none
		resp.Body.Close()
		if resp, err = get(ctx, url, "", "application/vnd.github+json"); err != nil {
			return info, fmt.Errorf("cannot reach the update server (check the network or proxy): %w", err)
		}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound && channel == "nightly":
		return info, ErrNoNightly
	case resp.StatusCode == http.StatusNotFound:
		return info, errors.New("no stable release has been published yet")
	case resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return info, errors.New("GitHub's limit of 60 update checks per hour was reached - try again later")
	case resp.StatusCode != http.StatusOK:
		return info, fmt.Errorf("the update server answered %s - try again later", resp.Status)
	}
	var r release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return info, err
	}
	info.Notes, info.Page, info.Published = r.Body, r.HTMLURL, r.PublishedAt.Format(time.RFC3339)
	names := make([]string, len(r.Assets))
	for i, a := range r.Assets {
		names[i] = a.Name
	}
	if channel == "nightly" {
		sha := shaOf(r.Name)
		info.Latest = "nightly " + sha
		if m := verRe.FindStringSubmatch(strings.Join(names, " ")); m != nil {
			info.Version = m[1]
		}
		info.Newer = nightlyNewer(ctx, repo, token, current, sha, info.Version)
	} else {
		info.Version = strings.TrimPrefix(r.TagName, "v")
		info.Latest = strings.TrimPrefix(r.TagName, "v")
		info.Newer = Compare(info.Latest, current) > 0
	}
	if i := PickAsset(names, runtime.GOOS, runtime.GOARCH); i >= 0 {
		info.AssetName, info.AssetURL, info.AssetSize = r.Assets[i].Name, r.Assets[i].URL, r.Assets[i].Size
	}
	return info, nil
}

var verRe = regexp.MustCompile(`(?i)termwright[-_](\d+\.\d+\.\d+)`)

// nightlyNewer reports whether the nightly commit sha is newer than the
// running build. GitHub's compare API tells whether the nightly commit is
// ahead of this build's commit (or of its release tag); when that is unknown
// (e.g. a local build whose commit was never pushed), only a higher version
// number counts. A different commit alone is not enough: an older nightly
// would otherwise be offered as an update.
func nightlyNewer(ctx context.Context, repo, token, current, sha, version string) bool {
	cur := shaOf(current)
	if sha == "" || (cur != "" && strings.HasPrefix(sha, cur[:min(len(cur), 7)])) {
		return false
	}
	ref := cur
	if ref == "" {
		ref = "v" + strings.TrimPrefix(current, "v")
		if i := strings.IndexAny(ref, "-~+ "); i >= 0 {
			ref = ref[:i]
		}
	}
	url := API + "/repos/" + repo + "/compare/" + ref + "..." + sha
	resp, err := get(ctx, url, token, "application/vnd.github+json")
	if err == nil && token != "" && resp.StatusCode != http.StatusOK {
		resp.Body.Close() // a stale token: try without it
		resp, err = get(ctx, url, "", "application/vnd.github+json")
	}
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var c struct {
				Status string `json:"status"`
			}
			if json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&c) == nil {
				switch c.Status {
				case "ahead":
					return true
				case "behind", "identical":
					return false
				}
			}
		}
	}
	return version != "" && Compare(version, current) > 0
}

var shaRe = regexp.MustCompile(`(?:~git|-git|\()([0-9a-f]{7,40})`)

// shaOf extracts a short commit id from "0.6.0~git9d57820" or "Termwright nightly (9d57820)".
func shaOf(s string) string {
	if m := shaRe.FindStringSubmatch(s); m != nil {
		return m[1][:7]
	}
	return ""
}

// Compare compares dotted versions ("0.6.10" > "0.6.9"); suffixes such as
// "-dev" or "~git…" are ignored. Returns -1, 0 or 1.
func Compare(a, b string) int {
	pa, pb := nums(a), nums(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] > pb[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

func nums(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-~+ "); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

// PickAsset returns the index of the installer for goos/goarch, or -1.
func PickAsset(names []string, goos, goarch string) int {
	type rule struct{ must []string }
	var rules []rule
	switch goos {
	case "darwin":
		rules = []rule{{[]string{".dmg"}}, {[]string{"macos", ".zip"}}}
	case "windows":
		rules = []rule{{[]string{"windows-" + goarch, "setup.exe"}}, {[]string{"windows-" + goarch, ".zip"}}}
	default:
		rules = []rule{{[]string{"_" + goarch + ".deb"}}, {[]string{"linux-" + goarch, ".tar.gz"}}}
	}
	for _, r := range rules {
		for i, n := range names {
			ln := strings.ToLower(n)
			ok := true
			for _, m := range r.must {
				if !strings.Contains(ln, m) {
					ok = false
					break
				}
			}
			if ok {
				return i
			}
		}
	}
	return -1
}

// Download saves the asset to dest, reporting progress.
func Download(ctx context.Context, assetURL, token, dest string, progress func(done, total int64)) error {
	if !strings.HasPrefix(assetURL, API+"/") {
		return errors.New("refusing to download from an unexpected URL")
	}
	dl := &http.Client{Timeout: 30 * time.Minute} // redirects to the storage host drop the token automatically
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "Termwright-updater")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := dl.Do(req)
	if err != nil {
		return err
	}
	if token != "" && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound) {
		resp.Body.Close() // public repository: try without the token
		req.Header.Del("Authorization")
		if resp, err = dl.Do(req); err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: %s", resp.Status)
	}
	f, err := os.Create(dest + ".part")
	if err != nil {
		return err
	}
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return err
			}
			done += int64(n)
			if progress != nil {
				progress(done, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(dest+".part", dest)
}
