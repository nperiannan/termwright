package main

import (
	"context"
	"hash/fnv"
	"io/fs"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/session"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// hookStore makes the store tell the window at once about every added, changed,
// moved or deleted session (changes made in a burst are sent as one).
func (a *App) hookStore(st *session.Store) {
	var mu sync.Mutex
	var pending bool
	st.OnChange = func() {
		mu.Lock()
		defer mu.Unlock()
		if pending {
			return
		}
		pending = true
		time.AfterFunc(150*time.Millisecond, func() {
			mu.Lock()
			pending = false
			mu.Unlock()
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, "sessions:changed")
			}
		})
	}
}

// watchSessions tells the window when the session files change on disk - a sync
// tool, a git pull, another copy of Termwright, an import by hand - so the
// Session Manager shows them without a manual Refresh. It compares a cheap
// signature (names, sizes and times) of the sessions folder every few seconds.
func (a *App) watchSessions(ctx context.Context) {
	var last uint64
	t := time.NewTicker(4 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		st := a.store
		if st == nil {
			continue
		}
		sig := folderSignature(st.Root())
		if last != 0 && sig != last {
			wruntime.EventsEmit(ctx, "sessions:changed")
		}
		last = sig
	}
}

// folderSignature is a hash of the names, sizes and modification times of all
// files and folders below root.
func folderSignature(root string) uint64 {
	h := fnv.New64a()
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		_, _ = h.Write([]byte(p + "|" + strconv.FormatInt(info.Size(), 10) + "|" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + "\n"))
		return nil
	})
	s := h.Sum64()
	if s == 0 {
		s = 1
	}
	return s
}
