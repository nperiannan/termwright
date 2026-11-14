package main

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Who caused an Events line: "user" (Termwright's own screens), "ai" (an AI
// agent through the connector) or "system" (Termwright on its own: watching
// the switches, retrying a copy...). It follows the goroutine that does the
// work, and the goroutines it starts (emuGo).
var emuTrig sync.Map // goroutine id -> who

func emuGoid() uint64 {
	var b [40]byte
	s := strings.TrimPrefix(string(b[:runtime.Stack(b[:], false)]), "goroutine ")
	if i := strings.IndexByte(s, ' '); i > 0 {
		n, _ := strconv.ParseUint(s[:i], 10, 64)
		return n
	}
	return 0
}

// emuWho is who caused what this goroutine does ("system" when nobody).
func emuWho() string {
	if v, ok := emuTrig.Load(emuGoid()); ok {
		return v.(string)
	}
	return "system"
}

// emuAs marks this goroutine's work as by who until the returned func runs.
// "user" does not replace a mark already there (an AI agent's call goes
// through the same methods as the screens).
func emuAs(who string) func() {
	id := emuGoid()
	old, had := emuTrig.Load(id)
	if had && who == "user" {
		return func() {}
	}
	emuTrig.Store(id, who)
	return func() {
		if had {
			emuTrig.Store(id, old)
		} else {
			emuTrig.Delete(id)
		}
	}
}

// emuGo starts f in a goroutine that keeps who caused it.
func emuGo(f func()) {
	who := emuWho()
	go func() {
		defer emuAs(who)()
		f()
	}()
}
