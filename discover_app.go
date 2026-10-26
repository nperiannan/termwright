package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/discover"
)

// ---- device discovery ----

var (
	discoverSeq  atomic.Int64
	discoverMu   sync.Mutex
	discoverRuns = map[string]context.CancelFunc{}
)

// DiscoverStart scans an address range in the background. Devices are sent
// as "discover:found:<id>" events, progress as "discover:progress:<id>" and
// the end as "discover:done:<id>". community "" skips the SNMP lookup.
// It returns the scan id and the number of addresses.
func (a *App) DiscoverStart(spec, community string) (map[string]any, error) {
	addrs, err := discover.ParseTargets(spec)
	if err != nil {
		return nil, err
	}
	if len(community) > 64 {
		return nil, errors.New("SNMP community is too long")
	}
	id := strconv.FormatInt(discoverSeq.Add(1), 10)
	ctx, cancel := context.WithCancel(context.Background())
	discoverMu.Lock()
	discoverRuns[id] = cancel
	discoverMu.Unlock()
	go func() {
		defer func() {
			discoverMu.Lock()
			delete(discoverRuns, id)
			discoverMu.Unlock()
			cancel()
			wruntime.EventsEmit(a.ctx, "discover:done:"+id, ctx.Err() != nil)
		}()
		discover.Scan(ctx, addrs, discover.Options{Community: community, OUI: a.ouiTable()},
			func(r discover.Result) { wruntime.EventsEmit(a.ctx, "discover:found:"+id, r) },
			func(done, total int) {
				wruntime.EventsEmit(a.ctx, "discover:progress:"+id, map[string]int{"done": done, "total": total})
			})
	}()
	return map[string]any{"id": id, "total": len(addrs)}, nil
}

// DiscoverStop stops a running scan.
func (a *App) DiscoverStop(id string) {
	discoverMu.Lock()
	defer discoverMu.Unlock()
	if c, ok := discoverRuns[id]; ok {
		c()
	}
}

// ---- MAC vendor list (IEEE OUI registry) ----

const ouiURL = "https://standards-oui.ieee.org/oui/oui.csv"

var (
	ouiMu    sync.Mutex
	ouiCache discover.OUI
)

func (a *App) ouiPath() string { return filepath.Join(a.cfg.DataDir(), "oui.csv") }

// ouiTable loads the downloaded MAC vendor list (nil when there is none).
func (a *App) ouiTable() discover.OUI {
	ouiMu.Lock()
	defer ouiMu.Unlock()
	if ouiCache != nil {
		return ouiCache
	}
	f, err := os.Open(a.ouiPath())
	if err != nil {
		return nil
	}
	defer f.Close()
	if t, err := discover.LoadOUI(f); err == nil && len(t) > 0 {
		ouiCache = t
	}
	return ouiCache
}

// DiscoverOUIStatus reports whether the MAC vendor list is present ("" when not) and its date.
func (a *App) DiscoverOUIStatus() string {
	if st, err := os.Stat(a.ouiPath()); err == nil && st.Size() > 0 {
		return st.ModTime().Format("2006-01-02")
	}
	return ""
}

// DiscoverDownloadOUI downloads the IEEE MAC vendor registry (about 3 MB) into the data folder.
func (a *App) DiscoverDownloadOUI() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ouiURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Termwright)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("cannot download the MAC vendor list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("cannot download the MAC vendor list: %s", resp.Status)
	}
	tmp := a.ouiPath() + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, 64<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, err
	}
	rf, err := os.Open(tmp)
	if err != nil {
		return 0, err
	}
	t, err := discover.LoadOUI(rf)
	rf.Close()
	if err != nil || len(t) < 1000 {
		return 0, errors.New("the downloaded MAC vendor list is not valid")
	}
	if err := os.Rename(tmp, a.ouiPath()); err != nil {
		return 0, err
	}
	ouiMu.Lock()
	ouiCache = t
	ouiMu.Unlock()
	return len(t), nil
}
