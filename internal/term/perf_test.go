package term

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/session"
)

// countEmitter counts the terminal bytes and events the UI would get.
type countEmitter struct {
	bytes, events atomic.Int64
}

func (c *countEmitter) Emit(name string, d any) {
	if strings.HasPrefix(name, "term:data:") {
		if s, ok := d.(string); ok {
			c.bytes.Add(int64(base64.StdEncoding.DecodedLen(len(s))))
			c.events.Add(1)
		}
	}
}

// TestPerfTerminalOutput: simulated devices (raw TCP) stream device-like
// output into tabs; measures the throughput to the UI and how many events it
// takes. TW_PERF_MB=64 TW_PERF_TABS=1 (or more tabs at once).
func TestPerfTerminalOutput(t *testing.T) {
	mb, _ := strconv.Atoi(os.Getenv("TW_PERF_MB"))
	if mb == 0 {
		t.Skip("set TW_PERF_MB")
	}
	tabs, _ := strconv.Atoi(os.Getenv("TW_PERF_TABS"))
	if tabs == 0 {
		tabs = 1
	}
	line := []byte("GigabitEthernet1/0/12   up      up      1000   full   a-1000   VLAN 120  \"uplink to core-sw2\"\r\n")
	total := mb << 20
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 0, 64<<10)
				for sent := 0; sent < total; {
					buf = buf[:0]
					for len(buf)+len(line) <= cap(buf) {
						buf = append(buf, line...)
					}
					n, err := c.Write(buf)
					if err != nil {
						return
					}
					sent += n
				}
				time.Sleep(200 * time.Millisecond)
			}(c)
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	em := &countEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	t0 := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < tabs; i++ {
		s := session.Defaults()
		s.Protocol, s.Hostname = session.ProtoRaw, host
		fmt.Sscan(port, &s.Port)
		ti := m.Create(s)
		if err := m.Start(ti.ID, 200, 50); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() { defer wg.Done() }()
	}
	want := int64(tabs) * int64(total)
	for em.bytes.Load() < want*99/100 && time.Since(t0) < 3*time.Minute {
		time.Sleep(20 * time.Millisecond)
	}
	d := time.Since(t0)
	got := em.bytes.Load()
	t.Logf("%d tab(s) × %d MB: %.1f s, %.0f MB/s to the UI, %d events (%.0f KB per event)",
		tabs, mb, d.Seconds(), float64(got)/1e6/d.Seconds(), em.events.Load(), float64(got)/1024/float64(max(em.events.Load(), 1)))
	wg.Wait()
}
