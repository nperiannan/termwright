package app

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// EmuHostStat is how busy a KVM host is now, for choosing where the next device goes: CPU, memory, the
// disk of the lab folder and the network port the switches use (percentages; -1 = not known).
type EmuHostStat struct {
	Host      string  `json:"host"` // session path
	Name      string  `json:"name"`
	Addr      string  `json:"addr"`
	Switches  int     `json:"switches"` // of this topology on it
	Running   int     `json:"running"`
	CPU       float64 `json:"cpu"`
	Cores     int     `json:"cores"`
	Mem       float64 `json:"mem"`
	MemTotal  int64   `json:"memTotal"` // MB
	MemUsed   int64   `json:"memUsed"`
	Disk      float64 `json:"disk"`
	DiskTotal int64   `json:"diskTotal"` // MB
	DiskUsed  int64   `json:"diskUsed"`
	Net       float64 `json:"net"` // the busier direction against the port's speed
	If        string  `json:"if"`
	Speed     int     `json:"speed"` // Mb/s (0 = not known: a virtual NIC)
	Rx        float64 `json:"rx"`    // bits/s
	Tx        float64 `json:"tx"`
	Err       string  `json:"err,omitempty"`
}

// emuStatScript reads two samples one second apart: CPU and the port's byte counters (/proc), memory, the disk.
func emuStatScript(dir, nic string) string {
	return fmt.Sprintf(`IF=%q; { [ -n "$IF" ] && [ -e "/sys/class/net/$IF" ]; } || IF=$(ip route show default 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="dev"){print $(i+1); exit}}')
n() { awk -v i="$IF" '{sub(/^ +/,"")} index($0, i":")==1 {sub(/^[^:]*: */,""); print $1, $9}' /proc/net/dev; }
echo "a $(head -1 /proc/stat) | $(n)"; sleep 1; echo "b $(head -1 /proc/stat) | $(n)"
grep -E '^(MemTotal|MemAvailable):' /proc/meminfo; echo "cores $(nproc 2>/dev/null)"
d="$HOME/%s"; [ -d "$d" ] || d="$HOME"; echo "df $(df -Pk "$d" 2>/dev/null | tail -1)"
echo "if $IF $(cat "/sys/class/net/$IF/speed" 2>/dev/null)"`, nic, dir)
}

// emuParseStat reads what emuStatScript printed into s.
func emuParseStat(out string, s *EmuHostStat) {
	s.CPU, s.Mem, s.Disk, s.Net = -1, -1, -1, -1
	type smp struct{ tot, idle, rx, tx float64 }
	read := func(l string) (smp, bool) {
		cpu, nic, _ := strings.Cut(l, "|")
		f := strings.Fields(cpu)
		if len(f) < 6 || f[1] != "cpu" {
			return smp{}, false
		}
		var x smp
		for i, v := range f[2:min(len(f), 10)] {
			n, _ := strconv.ParseFloat(v, 64)
			x.tot += n
			if i == 3 || i == 4 { // idle, iowait
				x.idle += n
			}
		}
		if g := strings.Fields(nic); len(g) == 2 {
			x.rx, _ = strconv.ParseFloat(g[0], 64)
			x.tx, _ = strconv.ParseFloat(g[1], 64)
		}
		return x, true
	}
	var a, b smp
	var okA, okB bool
	var memAvail int64
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		f := strings.Fields(l)
		switch {
		case strings.HasPrefix(l, "a cpu"):
			a, okA = read(l)
		case strings.HasPrefix(l, "b cpu"):
			b, okB = read(l)
		case len(f) >= 2 && f[0] == "MemTotal:":
			k, _ := strconv.ParseInt(f[1], 10, 64)
			s.MemTotal = k / 1024
		case len(f) >= 2 && f[0] == "MemAvailable:":
			k, _ := strconv.ParseInt(f[1], 10, 64)
			memAvail = k / 1024
		case len(f) == 2 && f[0] == "cores":
			s.Cores, _ = strconv.Atoi(f[1])
		case len(f) >= 7 && f[0] == "df":
			t, _ := strconv.ParseInt(f[2], 10, 64)
			u, _ := strconv.ParseInt(f[3], 10, 64)
			s.DiskTotal, s.DiskUsed = t/1024, u/1024
			if t > 0 {
				s.Disk = float64(u) * 100 / float64(t)
			}
		case len(f) >= 2 && f[0] == "if":
			s.If = f[1]
			if len(f) >= 3 {
				if sp, err := strconv.Atoi(f[2]); err == nil && sp > 0 {
					s.Speed = sp
				}
			}
		}
	}
	if okA && okB && b.tot > a.tot {
		s.CPU = 100 * (1 - (b.idle-a.idle)/(b.tot-a.tot))
		s.Rx, s.Tx = max(0, b.rx-a.rx)*8, max(0, b.tx-a.tx)*8
		if s.Speed > 0 {
			s.Net = 100 * max(s.Rx, s.Tx) / (float64(s.Speed) * 1e6)
		}
	}
	if s.MemTotal > 0 {
		s.MemUsed = s.MemTotal - memAvail
		s.Mem = float64(s.MemUsed) * 100 / float64(s.MemTotal)
	}
}

// EmuHostStats reads every KVM host of the lab at once (about a second): CPU, memory, disk and network.
func (a *App) EmuHostStats() []EmuHostStat {
	emuLab.mu.Lock()
	a.emuLoad()
	dir := emuLabDir()
	var out []EmuHostStat
	var nics []string
	for _, h := range emuLab.f.Hosts {
		if h.Session == emu.LocalHost {
			continue
		}
		s := EmuHostStat{Host: h.Session, Name: h.Name, Addr: strings.Split(h.Info.LanAddr, "/")[0]}
		if s.Name == "" {
			s.Name = h.Session[strings.LastIndex(h.Session, "/")+1:]
		}
		if l := emuLab.f.Lab; l != nil {
			for _, v := range l.VMs {
				if emuHostOf(v) == h.Session {
					s.Switches++
					if _, up := emuLab.hostStatus[h.Session].Up[v.ID]; up {
						s.Running++
					}
				}
			}
		}
		out = append(out, s)
		nics = append(nics, orStr(h.Info.Uplink, h.Info.LanIf))
	}
	emuLab.mu.Unlock()
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func(s *EmuHostStat, nic string) {
			defer wg.Done()
			o, err := a.emuRun(s.Host, emuStatScript(dir, nic), 10*time.Second)
			emuParseStat(o, s)
			if err != nil {
				s.Err = err.Error()
			}
		}(&out[i], nics[i])
	}
	wg.Wait()
	return out
}
