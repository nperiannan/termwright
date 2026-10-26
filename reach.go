package main

import (
	"net"
	"strconv"
	"sync"
	"time"
)

// ReachTarget is one session to check: Key identifies it in the result.
type ReachTarget struct {
	Key  string `json:"key"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

// CheckReachable opens a TCP connection to each target (at most 32 at a
// time, 1.5 s each) and reports which answered. Nothing is sent: the
// connection is closed as soon as it is open.
func (a *App) CheckReachable(targets []ReachTarget) map[string]bool {
	out := make(map[string]bool, len(targets))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)
	for _, t := range targets {
		if t.Host == "" || t.Port <= 0 {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(t ReachTarget) {
			defer func() { <-sem; wg.Done() }()
			ok := false
			if c, err := net.DialTimeout("tcp", net.JoinHostPort(t.Host, strconv.Itoa(t.Port)), 1500*time.Millisecond); err == nil {
				ok = true
				_ = c.Close()
			}
			mu.Lock()
			out[t.Key] = ok
			mu.Unlock()
		}(t)
	}
	wg.Wait()
	return out
}
