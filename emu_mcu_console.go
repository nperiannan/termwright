package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// Microcontroller consoles. Espressif's UART throws away what the firmware prints while nothing is connected to
// its socket (and its log gets nothing either), so Termwright stays connected from the first instruction: QEMU
// starts paused (-S), Termwright connects to the UART, then continues it. Termwright passes the console on: a
// relay on this computer, where the console sessions connect, shows what the board printed so far, then what it
// prints, and types what they type (more than one tab can watch).

type mcuKeeper struct {
	id   string
	conn net.Conn     // the UART (through the host)
	ln   net.Listener // the relay on this computer
	mu   sync.Mutex
	hist []byte // what it printed (the last mcuHistMax bytes)
	subs map[chan []byte]bool
	done chan struct{}
}

const mcuHistMax = 64 << 10

var mcuKeepers = struct {
	sync.Mutex
	m    map[string]*mcuKeeper
	busy map[string]bool
}{m: map[string]*mcuKeeper{}, busy: map[string]bool{}}

// emuMCUKeeper is a microcontroller's console keeper when Termwright is connected to it.
func emuMCUKeeper(id string) *mcuKeeper {
	mcuKeepers.Lock()
	defer mcuKeepers.Unlock()
	return mcuKeepers.m[id]
}

// emuMCUKeep connects Termwright to a running microcontroller's UART (once), opens its console relay and continues
// it when it waits paused (a microcontroller started by Termwright does, until this).
func (a *App) emuMCUKeep(id string) (*mcuKeeper, error) {
	for wait := 0; ; wait++ {
		mcuKeepers.Lock()
		if k := mcuKeepers.m[id]; k != nil {
			mcuKeepers.Unlock()
			return k, nil
		}
		if !mcuKeepers.busy[id] {
			mcuKeepers.busy[id] = true
			mcuKeepers.Unlock()
			break
		}
		mcuKeepers.Unlock()
		if wait == 150 {
			return nil, errors.New("its console is being connected")
		}
		time.Sleep(100 * time.Millisecond) // another caller connects it: its keeper then
	}
	defer func() {
		mcuKeepers.Lock()
		delete(mcuKeepers.busy, id)
		mcuKeepers.Unlock()
	}()
	emuLab.mu.Lock()
	var v *emu.VM
	if emuLab.f.Lab != nil {
		v = emuLab.f.Lab.VMByID(id)
	}
	if v == nil {
		emuLab.mu.Unlock()
		return nil, errors.New("no such microcontroller")
	}
	host := emuHostOf(*v)
	lh := a.emuFor(host)
	uart, mon, relay := lh.SSHPort(*v), lh.MonitorPort(*v), lh.ConsolePort(*v)
	name := v.Name
	emuLab.mu.Unlock()
	cl, err := a.emuClient(context.Background(), host)
	if err != nil {
		return nil, err
	}
	var conn net.Conn
	for try := 0; ; try++ { // QEMU may still be starting
		if conn, err = cl.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(uart))); err == nil || try == 20 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err != nil {
		return nil, fmt.Errorf("its UART: %w", err)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(relay)))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("its console port %d on this computer: %w", relay, err)
	}
	k := &mcuKeeper{id: id, conn: conn, ln: ln, subs: map[chan []byte]bool{}, done: make(chan struct{})}
	mcuKeepers.Lock()
	mcuKeepers.m[id] = k
	mcuKeepers.Unlock()
	go k.read()
	go k.serve()
	if h, err := a.emuMonitor(host, mon); err == nil { // it waits paused for this
		_, _ = h.do("cont", 3*time.Second)
		h.close()
	}
	emuLogf(emuLvDebug, host, name, "console kept by Termwright: its UART on port %d, the console sessions on port %d here", uart, relay)
	return k, nil
}

// read takes what the board prints: kept, and passed to whoever watches. When the UART closes (it stopped), the
// relay closes too.
func (k *mcuKeeper) read() {
	buf := make([]byte, 4096)
	for {
		n, err := k.conn.Read(buf)
		if n > 0 {
			b := append([]byte{}, buf[:n]...)
			k.mu.Lock()
			k.hist = append(k.hist, b...)
			if len(k.hist) > mcuHistMax {
				k.hist = append([]byte{}, k.hist[len(k.hist)-mcuHistMax:]...)
			}
			for c := range k.subs {
				select {
				case c <- b:
				default: // a watcher that does not keep up misses some
				}
			}
			k.mu.Unlock()
		}
		if err != nil {
			k.close()
			return
		}
	}
}

func (k *mcuKeeper) close() {
	mcuKeepers.Lock()
	if mcuKeepers.m[k.id] == k {
		delete(mcuKeepers.m, k.id)
	}
	mcuKeepers.Unlock()
	k.mu.Lock()
	select {
	case <-k.done:
	default:
		close(k.done)
		for c := range k.subs {
			close(c)
		}
		k.subs = map[chan []byte]bool{}
	}
	k.mu.Unlock()
	k.conn.Close()
	k.ln.Close()
}

// watch is what the board printed so far and a channel of what it prints from now (closed when it stops).
func (k *mcuKeeper) watch() ([]byte, chan []byte, func()) {
	c := make(chan []byte, 256)
	k.mu.Lock()
	defer k.mu.Unlock()
	select {
	case <-k.done:
		close(c)
		return append([]byte{}, k.hist...), c, func() {}
	default:
	}
	k.subs[c] = true
	return append([]byte{}, k.hist...), c, func() {
		k.mu.Lock()
		if k.subs[c] {
			delete(k.subs, c)
			close(c)
		}
		k.mu.Unlock()
	}
}

// Write types on the UART.
func (k *mcuKeeper) Write(b []byte) (int, error) { return k.conn.Write(b) }

// serve is the relay: each console session gets what the board printed so far, then what it prints; what it
// types goes to the UART (telnet's option talk left out). It asks the terminal for character mode (echo by the
// board), as QEMU's own telnet console does.
func (k *mcuKeeper) serve() {
	for {
		c, err := k.ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			hist, ch, stop := k.watch()
			defer stop()
			if _, err := c.Write(append([]byte{255, 251, 1, 255, 251, 3}, hist...)); err != nil { // IAC WILL ECHO, IAC WILL SGA
				return
			}
			go func() {
				buf := make([]byte, 1024)
				var t telnetStrip
				for {
					n, err := c.Read(buf)
					if n > 0 {
						if b := t.strip(buf[:n]); len(b) > 0 {
							if _, err := k.Write(b); err != nil {
								return
							}
						}
					}
					if err != nil {
						stop()
						return
					}
				}
			}()
			for b := range ch {
				if _, err := c.Write(b); err != nil {
					return
				}
			}
		}(c)
	}
}

// telnetStrip takes telnet's commands out of what a terminal sends (IAC WILL/WONT/DO/DONT x, IAC SB … IAC SE,
// IAC IAC = a 255 byte), across reads.
type telnetStrip struct{ state int }

func (t *telnetStrip) strip(in []byte) []byte {
	out := in[:0:0]
	for _, b := range in {
		switch t.state {
		case 0:
			if b == 255 {
				t.state = 1
			} else {
				out = append(out, b)
			}
		case 1: // after IAC
			switch {
			case b == 255:
				out = append(out, 255)
				t.state = 0
			case b >= 251 && b <= 254:
				t.state = 2
			case b == 250:
				t.state = 3
			default:
				t.state = 0
			}
		case 2: // the option of WILL/WONT/DO/DONT
			t.state = 0
		case 3: // in a subnegotiation, until IAC SE
			if b == 255 {
				t.state = 4
			}
		case 4:
			if b == 240 {
				t.state = 0
			} else {
				t.state = 3
			}
		}
	}
	return out
}
