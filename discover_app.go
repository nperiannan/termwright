package main

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"

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
		discover.Scan(ctx, addrs, discover.Options{Community: community},
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
