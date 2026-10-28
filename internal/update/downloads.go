package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Downloads adds up the download counts of every file of every release of
// repo ("owner/name"), as GitHub counts them (installers fetched from the
// releases page and by the in-app updater).
func Downloads(ctx context.Context, repo, token string) (int, error) {
	repo = strings.Trim(strings.TrimSpace(repo), "/")
	if repo == "" {
		return 0, fmt.Errorf("no repository")
	}
	total := 0
	for page := 1; page <= 10; page++ {
		resp, err := get(ctx, fmt.Sprintf("%s/repos/%s/releases?per_page=100&page=%d", API, repo, page), token, "application/vnd.github+json")
		if err != nil {
			return 0, err
		}
		var rel []struct {
			Assets []struct {
				DownloadCount int `json:"download_count"`
			} `json:"assets"`
		}
		err = func() error {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("GitHub answered %s", resp.Status)
			}
			return json.NewDecoder(resp.Body).Decode(&rel)
		}()
		if err != nil {
			return 0, err
		}
		for _, r := range rel {
			for _, a := range r.Assets {
				total += a.DownloadCount
			}
		}
		if len(rel) < 100 {
			break
		}
	}
	return total, nil
}
