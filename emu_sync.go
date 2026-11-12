package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// The Images on the Hosts page: which image files each host has, how each
// host is (CPU, memory, disk, what is installed, which other hosts it
// reaches), and copying images so every host has every image (then switches
// move between hosts without waiting for an image copy).

// EmuMatrixHost is a host in the matrix, read fresh.
type EmuMatrixHost struct {
	Session   string   `json:"session"`
	Name      string   `json:"name"`
	Addr      string   `json:"addr"`
	KVM       string   `json:"kvm"` // yes, none, noaccess, … (as the host check says)
	VT        bool     `json:"vt"`
	Virt      string   `json:"virt"`
	QEMU      string   `json:"qemu"`
	QemuImg   bool     `json:"qemuImg"`
	Guestfs   bool     `json:"guestfs"`
	Sudo      bool     `json:"sudo"`
	CanSetup  bool     `json:"canSetup"`
	Problems  []string `json:"problems"`
	CPUs      int      `json:"cpus"`
	Load      float64  `json:"load"`
	MemTotal  int64    `json:"memTotal"`  // bytes
	MemFree   int64    `json:"memFree"`   // bytes, available
	DiskTotal int64    `json:"diskTotal"` // bytes, where its images are
	DiskFree  int64    `json:"diskFree"`
	ImagesDir string   `json:"imagesDir"` // where its images really are (a link followed)
	Error     string   `json:"error"`     // it could not be read
}

// EmuMatrixImage is an image (a file of it) in the matrix.
type EmuMatrixImage struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	FileName string `json:"fileName"`
	Size     int64  `json:"size"`
	Local    bool   `json:"local"` // the file is on this computer
	Switches int    `json:"switches"`
}

// EmuReach says whether one host reaches another: UDP for the cables between
// hosts (20000-29999), TCP for live moves (29400-29999). "ok", "blocked", "" = not checked.
type EmuReach struct {
	From string `json:"from"`
	To   string `json:"to"`
	UDP  string `json:"udp"`
	TCP  string `json:"tcp"`
	Text string `json:"text"`
	At   string `json:"at"`
}

// EmuMatrix is the Images on the Hosts view; Cells[host][image] is "ok",
// "missing", "different" (another file of that name), "partial" (a copy that
// broke off), "queued", "copying 41 % · …", "low: …" (it would leave less
// than a fifth of the disk free) or "failed: why".
type EmuMatrix struct {
	Hosts    []EmuMatrixHost              `json:"hosts"`
	Images   []EmuMatrixImage             `json:"images"`
	Cells    map[string]map[string]string `json:"cells"`
	Reach    []EmuReach                   `json:"reach"`
	Busy     bool                         `json:"busy"`
	Checking bool                         `json:"checking"` // the reach check runs
	SyncAll  bool                         `json:"syncAll"`  // copy new images to every host
	Keep20   bool                         `json:"keep20"`   // keep a fifth of each disk free
}

var emuSync = struct {
	sync.Mutex
	state    map[string]string // host|image → queued, copying …, low: …, failed: …
	busy     int
	reach    map[string]EmuReach // from|to
	checking bool
}{state: map[string]string{}, reach: map[string]EmuReach{}}

// emuSyncKey names a cell.
func emuSyncKey(host, id string) string { return host + "|" + id }

// emuSyncPct shows a copy's progress in its cell (when a sync copies it).
func emuSyncPct(host, id string, pct int) {
	emuSync.Lock()
	if st, ok := emuSync.state[emuSyncKey(host, id)]; ok && !strings.Contains(st, "·") {
		emuSync.state[emuSyncKey(host, id)] = "copying " + strconv.Itoa(pct) + " %"
	}
	emuSync.Unlock()
}

// emuSyncText shows a copy's progress with its speed and time left in its cell.
func emuSyncText(host, id, t string) {
	emuSync.Lock()
	if _, ok := emuSync.state[emuSyncKey(host, id)]; ok {
		emuSync.state[emuSyncKey(host, id)] = "copying " + t
	}
	emuSync.Unlock()
}

// emuMatrixHosts are the checked hosts, this lab's main host first.
func emuMatrixHosts() []EmuHost {
	var out []EmuHost
	for _, h := range emuLab.f.Hosts {
		if h.Session == emuLab.f.Host {
			out = append([]EmuHost{h}, out...)
		} else {
			out = append(out, h)
		}
	}
	return out
}

// emuLabDir is the lab folder on the hosts (under their home).
func emuLabDir() string {
	if l := emuLab.f.Lab; l != nil && l.Dir != "" {
		return l.Dir
	}
	return "termwright-lab"
}

// emuMatrixScript reads a host: its numbers now, where its images are and every image file there.
func emuMatrixScript(dir string) string {
	return `d="$HOME/` + dir + `/images"; mkdir -p "$d"; cd "$d" || exit 0
echo "@dir $(pwd -P)"
df -Pk . | awk 'NR==2{print "@disk " $2 " " $4}'
echo "@cpus $(nproc 2>/dev/null || sysctl -n hw.ncpu)"
echo "@load $(cut -d' ' -f1 /proc/loadavg 2>/dev/null)"
awk '/^MemTotal:/{t=$2} /^MemAvailable:/{a=$2} END{print "@mem " t " " a}' /proc/meminfo 2>/dev/null
for f in *; do case "$f" in *.sha256|*.part|*.booted*|'*') continue;; esac; [ -f "$f" ] || continue; s=$(stat -c %s "$f" 2>/dev/null || stat -f %z "$f"); h=$(head -c 64 "$f.sha256" 2>/dev/null); echo "$f $s $h"; done
for f in *.part; do [ -f "$f" ] && echo "@part ${f%.part}"; done; true`
}

// EmuImageMatrix reads which images each host has and how each host is (one command per host, side by side).
func (a *App) EmuImageMatrix() (EmuMatrix, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	hosts := emuMatrixHosts()
	dir := emuLabDir()
	used := map[string]int{}
	if l := emuLab.f.Lab; l != nil {
		for _, v := range l.VMs {
			used[v.Image]++
		}
	}
	type file struct {
		name, sha string
		size      int64
		id        string
	}
	var files []file
	var imgs []EmuMatrixImage
	for _, im := range emuLab.f.Images {
		imgs = append(imgs, EmuMatrixImage{ID: im.ID, Name: im.Name, Version: im.Version, FileName: im.FileName, Size: im.Size, Local: im.File != "", Switches: used[im.ID]})
		files = append(files, file{im.FileName, im.SHA256, im.Size, im.ID})
	}
	m := EmuMatrix{Cells: map[string]map[string]string{}, SyncAll: emuLab.f.SyncAll, Keep20: !emuLab.f.NoKeep20}
	emuLab.mu.Unlock()
	var mu sync.Mutex
	var wg sync.WaitGroup
	have := map[string]bool{} // an image any host has
	line := regexp.MustCompile(`^(.+) (\d+) ?([0-9a-f]{64})?$`)
	for _, h := range hosts {
		in := h.Info
		mh := EmuMatrixHost{Session: h.Session, Name: orStr(h.Name, h.Session[strings.LastIndex(h.Session, "/")+1:]), Addr: strings.Split(in.LanAddr, "/")[0],
			KVM: in.KVM, VT: in.VT, Virt: in.Virt, QEMU: in.QEMU, QemuImg: in.QemuImg, Guestfs: in.Guestfs, Sudo: in.Sudo, CanSetup: in.CanSetup, Problems: in.Problems,
			CPUs: in.CPUs, Load: in.Load, MemTotal: in.MemTotal << 20, MemFree: in.MemFree << 20, DiskFree: in.DiskFree << 20}
		m.Hosts = append(m.Hosts, mh)
		wg.Add(1)
		go func(i int, host string) {
			defer wg.Done()
			out, err := a.emuRun(host, emuMatrixScript(dir), time.Minute)
			cells := map[string]string{}
			on := map[string][2]string{}
			parts := map[string]bool{}
			mu.Lock()
			hm := &m.Hosts[i]
			for _, ln := range strings.Split(out, "\n") {
				ln = strings.TrimSpace(ln)
				f := strings.Fields(ln)
				switch {
				case strings.HasPrefix(ln, "@dir "):
					hm.ImagesDir = strings.TrimPrefix(ln, "@dir ")
				case strings.HasPrefix(ln, "@disk ") && len(f) == 3:
					t, _ := strconv.ParseInt(f[1], 10, 64)
					fr, _ := strconv.ParseInt(f[2], 10, 64)
					hm.DiskTotal, hm.DiskFree = t<<10, fr<<10
				case strings.HasPrefix(ln, "@cpus ") && len(f) == 2:
					if n, e := strconv.Atoi(f[1]); e == nil {
						hm.CPUs = n
					}
				case strings.HasPrefix(ln, "@load ") && len(f) == 2:
					if n, e := strconv.ParseFloat(f[1], 64); e == nil {
						hm.Load = n
					}
				case strings.HasPrefix(ln, "@mem ") && len(f) == 3:
					t, _ := strconv.ParseInt(f[1], 10, 64)
					av, _ := strconv.ParseInt(f[2], 10, 64)
					if t > 0 {
						hm.MemTotal, hm.MemFree = t<<10, av<<10
					}
				case strings.HasPrefix(ln, "@part "):
					parts[strings.TrimPrefix(ln, "@part ")] = true
				default:
					if g := line.FindStringSubmatch(ln); g != nil {
						on[g[1]] = [2]string{g[2], g[3]}
					}
				}
			}
			if err != nil {
				hm.Error = err.Error()
			}
			mu.Unlock()
			for _, f := range files {
				st := "missing"
				if x, ok := on[f.name]; ok {
					sz, _ := strconv.ParseInt(x[0], 10, 64)
					if f.sha != "" && x[1] == f.sha || f.sha == "" && (f.size <= 0 || sz == f.size) {
						st = "ok"
					} else {
						st = "different"
					}
				} else if parts[f.name] {
					st = "partial"
				}
				if err != nil {
					st = "unknown"
				}
				cells[f.id] = st
			}
			mu.Lock()
			m.Cells[host] = cells
			for id, st := range cells {
				if st == "ok" {
					have[id] = true
				}
			}
			mu.Unlock()
		}(len(m.Hosts)-1, h.Session)
	}
	wg.Wait()
	emuSync.Lock()
	for k, st := range emuSync.state { // copies going on (or held back, or failed) show over what the host has
		hi, id, _ := strings.Cut(k, "|")
		if c := m.Cells[hi]; c != nil && c[id] != "ok" {
			c[id] = st
		} else if c != nil && c[id] == "ok" && !strings.HasPrefix(st, "copying") && st != "queued" {
			delete(emuSync.state, k) // it is there now
		}
	}
	for _, f := range m.Hosts {
		for _, t := range m.Hosts {
			if r, ok := emuSync.reach[f.Session+"|"+t.Session]; ok {
				m.Reach = append(m.Reach, r)
			}
		}
	}
	m.Busy, m.Checking = emuSync.busy > 0, emuSync.checking
	emuSync.Unlock()
	// the images worth a row: on this computer, on a host, or used by a switch
	for _, im := range imgs {
		if im.Local || have[im.ID] || im.Switches > 0 {
			m.Images = append(m.Images, im)
		}
	}
	sort.SliceStable(m.Images, func(i, j int) bool {
		if (m.Images[i].Switches > 0) != (m.Images[j].Switches > 0) {
			return m.Images[i].Switches > 0
		}
		return m.Images[i].Name+m.Images[i].Version < m.Images[j].Name+m.Images[j].Version
	})
	return m, nil
}

// EmuSyncImages copies images to hosts in the background: each pair whose host
// does not have the image yet (empty lists = all images in the matrix, all
// hosts). The file comes from this computer, or from a host that has it.
// Unless force, a copy that would leave less than a fifth of the host's disk
// free is held back (when Keep 20 % is on) and its cell says so.
func (a *App) EmuSyncImages(hosts, images []string, force bool) (EmuMatrix, error) {
	m, err := a.EmuImageMatrix()
	if err != nil {
		return m, err
	}
	if len(hosts) == 0 {
		for _, h := range m.Hosts {
			if h.Error == "" {
				hosts = append(hosts, h.Session)
			}
		}
	}
	if len(images) == 0 {
		for _, im := range m.Images {
			images = append(images, im.ID)
		}
	}
	size := map[string]int64{}
	for _, im := range m.Images {
		size[im.ID] = im.Size
	}
	hinfo := map[string]EmuMatrixHost{}
	for _, h := range m.Hosts {
		hinfo[h.Session] = h
	}
	type job struct{ host, id, from string }
	byHost := map[string][]job{}
	n, held := 0, 0
	emuSync.Lock()
	for _, h := range hosts {
		free := hinfo[h].DiskFree
		for _, id := range images {
			st := m.Cells[h][id]
			if st == "ok" || st == "unknown" || strings.HasPrefix(st, "copying") || st == "queued" {
				continue
			}
			from := ""
			for _, o := range m.Hosts {
				if m.Cells[o.Session][id] == "ok" {
					from = o.Session
					break
				}
			}
			if t := hinfo[h].DiskTotal; !force && m.Keep20 && t > 0 && size[id] > 0 {
				if left := free - size[id]; left*5 < t { // less than a fifth free after it
					emuSync.state[emuSyncKey(h, id)] = fmt.Sprintf("low: %d %% free after it (keep 20 %%)", max(left, 0)*100/t)
					held++
					continue
				}
			}
			free -= size[id]
			byHost[h] = append(byHost[h], job{h, id, from})
			emuSync.state[emuSyncKey(h, id)] = "queued"
			n++
		}
	}
	emuSync.busy += len(byHost)
	emuSync.Unlock()
	if n == 0 {
		if held > 0 {
			return a.EmuImageMatrix() // the held-back cells say why
		}
		return m, errors.New("every host has these images already")
	}
	emuLab.mu.Lock()
	emuEv("", "info", fmt.Sprintf("copying images so the hosts have the same ones: %d copies to %d hosts", n, len(byHost)))
	dir := emuLabDir()
	emuLab.mu.Unlock()
	for _, js := range byHost { // one image after another per host, the hosts side by side
		go func(js []job) {
			defer func() { emuSync.Lock(); emuSync.busy--; emuSync.Unlock() }()
			for _, j := range js {
				err := a.emuSyncOne(j.host, j.id, j.from, dir)
				emuSync.Lock()
				if err != nil {
					emuSync.state[emuSyncKey(j.host, j.id)] = "failed: " + emuCopyWhy(err)
				} else {
					delete(emuSync.state, emuSyncKey(j.host, j.id))
				}
				emuSync.Unlock()
				if err != nil {
					emuLab.mu.Lock()
					emuEvOn(j.host, "", "error", err.Error())
					emuLab.mu.Unlock()
				}
			}
		}(js)
	}
	time.Sleep(100 * time.Millisecond)
	return a.EmuImageMatrix()
}

// emuSyncOne copies one image (and its other disks) to a host: from this
// computer when it has the file, else from a host that has it.
func (a *App) emuSyncOne(host, id, from, dir string) error {
	emuLab.mu.Lock()
	ip := emuImage(id)
	var im emu.Image
	if ip != nil {
		im = *ip
	}
	emuLab.mu.Unlock()
	if ip == nil {
		return errors.New("no such image")
	}
	emuSyncPct(host, id, 0)
	if im.File != "" {
		return a.emuUpload(host, im, dir)
	}
	if from == "" {
		return fmt.Errorf("%s is neither on this computer nor on a host: Images → Download or Choose", im.Name)
	}
	// host to host (through this computer's two connections), then its checksum file
	src := dir + "/images/" + im.FileName
	if err := a.emuCopyFile(from, host, src, src, func(t string) { emuSyncText(host, id, t) }); err != nil {
		return fmt.Errorf("copying %s from %s: %w", im.Name, from[strings.LastIndex(from, "/")+1:], err)
	}
	if im.SHA256 == "" {
		return nil
	}
	_, err := a.emuRun(host, "printf '%s\\n' "+emu.Q(im.SHA256)+" > "+emu.Q(src+".sha256"), 30*time.Second)
	return err
}

// EmuSyncSettings sets: copy new images to every host, and keep a fifth of each disk free.
func (a *App) EmuSyncSettings(syncAll, keep20 bool) (EmuMatrix, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	emuLab.f.SyncAll, emuLab.f.NoKeep20 = syncAll, !keep20
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuMatrix{}, err
	}
	if syncAll {
		if m, err := a.EmuSyncImages(nil, nil, false); err == nil {
			return m, nil
		}
	}
	return a.EmuImageMatrix()
}

// emuSyncAfter: with "keep all hosts the same", an image just copied to one host (or downloaded) goes to the others too.
func (a *App) emuSyncAfter(id string) {
	emuLab.mu.Lock()
	on := emuLab.f.SyncAll
	emuLab.mu.Unlock()
	if on {
		go func() { _, _ = a.EmuSyncImages(nil, []string{id}, false) }()
	}
}

// EmuCheckReach checks every pair of hosts, both ways and side by side: UDP as
// the cables between hosts use it, and TCP as a live move does.
func (a *App) EmuCheckReach() (EmuMatrix, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	hosts := emuMatrixHosts()
	emuLab.mu.Unlock()
	if len(hosts) < 2 {
		return a.EmuImageMatrix()
	}
	emuSync.Lock()
	if emuSync.checking {
		emuSync.Unlock()
		return a.EmuImageMatrix()
	}
	emuSync.checking = true
	emuSync.Unlock()
	go func() {
		defer func() { emuSync.Lock(); emuSync.checking = false; emuSync.Unlock() }()
		var wg sync.WaitGroup
		k := 0
		for _, f := range hosts {
			for _, t := range hosts {
				if f.Session == t.Session {
					continue
				}
				k++
				wg.Add(1)
				go func(f, t EmuHost, k int) {
					defer wg.Done()
					r := a.emuReachOne(f, t, 29400+k*2) // the ports of live moves: TCP p, UDP p+1 (in the cables' range too)
					emuSync.Lock()
					emuSync.reach[f.Session+"|"+t.Session] = r
					emuSync.Unlock()
				}(f, t, k)
			}
		}
		wg.Wait()
		emuLab.mu.Lock()
		emuSync.Lock()
		for _, r := range emuSync.reach {
			if r.UDP != "ok" || r.TCP != "ok" {
				emuEv("", "warn", "hosts: "+r.Text)
			}
		}
		emuSync.Unlock()
		emuLab.mu.Unlock()
	}()
	time.Sleep(100 * time.Millisecond)
	return a.EmuImageMatrix()
}

// emuReachOne: the receiver listens on TCP p and UDP p+1 for 8 s; the sender connects and sends meanwhile.
func (a *App) emuReachOne(f, t EmuHost, p int) EmuReach {
	name := func(h EmuHost) string { return orStr(h.Name, h.Session[strings.LastIndex(h.Session, "/")+1:]) }
	r := EmuReach{From: f.Session, To: t.Session, At: time.Now().Format("15:04")}
	to := strings.Split(t.Info.LanAddr, "/")[0]
	if to == "" {
		r.Text = "no address known for " + name(t) + ": check it under Hosts"
		return r
	}
	recv := fmt.Sprintf(`timeout 10 python3 -c "
import socket,select
u=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);u.bind(('0.0.0.0',%d))
s=socket.socket();s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1);s.bind(('0.0.0.0',%d));s.listen(1)
got=set()
import time;end=time.time()+8
while time.time()<end and len(got)<2:
  r,_,_=select.select([u,s],[],[],0.5)
  for x in r:
    if x is u: u.recvfrom(64);got.add('udp')
    else: c,_=s.accept();c.close();got.add('tcp')
print('got',' '.join(sorted(got)) or 'none')
" 2>&1 || echo nothing`, p+1, p)
	done := make(chan string, 1)
	go func() { o, err := a.emuRun(t.Session, recv, 20*time.Second); done <- o + errText(err) }()
	time.Sleep(2 * time.Second)
	_, _ = a.emuRun(f.Session, fmt.Sprintf("bash -c 'for i in 1 2 3 4 5 6; do echo tw > /dev/udp/%s/%d; (exec 3<>/dev/tcp/%s/%d) 2>/dev/null; sleep 0.5; done' 2>/dev/null; true", to, p+1, to, p), 15*time.Second)
	got := <-done
	if strings.Contains(got, "python3") && strings.Contains(got, "not found") {
		r.Text = name(t) + " has no python3 to listen with: not checked"
		return r
	}
	r.UDP, r.TCP = "blocked", "blocked"
	if m := regexp.MustCompile(`got ([a-z ]+)`).FindStringSubmatch(got); m != nil {
		if strings.Contains(m[1], "udp") {
			r.UDP = "ok"
		}
		if strings.Contains(m[1], "tcp") {
			r.TCP = "ok"
		}
	}
	switch {
	case r.UDP == "ok" && r.TCP == "ok":
		r.Text = fmt.Sprintf("%s reaches %s (%s): cables and live moves work", name(f), name(t), to)
	case r.UDP == "ok":
		r.Text = fmt.Sprintf("%s reaches %s (%s) on UDP, not on TCP 29400-29999: cables work, live moves do not", name(f), name(t), to)
	case r.TCP == "ok":
		r.Text = fmt.Sprintf("%s reaches %s (%s) on TCP, not on UDP 20000-29999: live moves work, cables between them do not", name(f), name(t), to)
	default:
		r.Text = fmt.Sprintf("%s does not reach %s (%s) on UDP 20000-29999 or TCP 29400-29999: a firewall between them or on %s", name(f), name(t), to, name(t))
	}
	return r
}

// EmuSetImagesDir moves a host's images to another folder there (a bigger
// disk) and leaves a link in their old place, so everything finds them.
func (a *App) EmuSetImagesDir(host, to string) (EmuMatrix, error) {
	to = strings.TrimSpace(to)
	if to == "" || strings.ContainsAny(to, "'\"`$\\\n;|&<>") {
		return EmuMatrix{}, errors.New("a folder path without quotes or shell characters")
	}
	emuLab.mu.Lock()
	dir := emuLabDir()
	busy := false
	if l := emuLab.f.Lab; l != nil {
		for _, v := range l.VMs {
			if emuHostOf(v) == host {
				if _, up := emuLab.status.Up[v.ID]; up {
					busy = true
				}
			}
		}
	}
	emuLab.mu.Unlock()
	if busy {
		return EmuMatrix{}, errors.New("stop the switches on that host first: their images move")
	}
	emuSync.Lock()
	for k, st := range emuSync.state {
		if strings.HasPrefix(k, host+"|") && (st == "queued" || strings.HasPrefix(st, "copying")) {
			emuSync.Unlock()
			return EmuMatrix{}, errors.New("images are being copied to that host: wait for them")
		}
	}
	emuSync.Unlock()
	if !strings.HasPrefix(to, "/") && !strings.HasPrefix(to, "~/") {
		to = "~/" + to
	}
	tgt := "\"$HOME\"/" + strings.TrimPrefix(to, "~/")
	if strings.HasPrefix(to, "/") {
		tgt = "'" + to + "'"
	}
	// the files move (mv keeps them when it cannot), the old folder becomes a link
	script := `set -e
old="$HOME/` + dir + `/images"; new=` + tgt + `
mkdir -p "$new" || { echo "fail cannot make $new (sudo mkdir -p it and give it to $(id -un))"; exit 0; }
touch "$new/.tw-write" 2>/dev/null && rm -f "$new/.tw-write" || { echo "fail $new is not writable for $(id -un)"; exit 0; }
[ "$(cd "$old" 2>/dev/null && pwd -P)" = "$(cd "$new" && pwd -P)" ] && { echo "same"; exit 0; }
if [ -d "$old" ] && [ ! -L "$old" ]; then
  for f in "$old"/* "$old"/.[!.]*; do [ -e "$f" ] || continue; mv -n "$f" "$new"/ || { echo "fail could not move $f"; exit 0; }; done
  rmdir "$old" || { echo "fail $old is not empty (files with the same names in $new?)"; exit 0; }
elif [ -L "$old" ]; then
  src=$(readlink "$old"); for f in "$src"/* ; do [ -e "$f" ] || continue; mv -n "$f" "$new"/ || true; done; rm -f "$old"
fi
mkdir -p "$(dirname "$old")"; ln -s "$new" "$old"; echo moved`
	out, err := a.emuRun(host, script, 30*time.Minute)
	if err != nil {
		return EmuMatrix{}, err
	}
	if i := strings.Index(out, "fail "); i >= 0 {
		return EmuMatrix{}, errors.New(strings.TrimSpace(out[i+5:]))
	}
	emuLab.mu.Lock()
	emuEvOn(host, "", "ok", "its images are in "+to+" now (a link in ~/"+dir+"/images points there)")
	emuLab.mu.Unlock()
	return a.EmuImageMatrix()
}

// EmuOpenPortsText is what to run on a host so the others reach it (Termwright does not change firewalls itself).
func (a *App) EmuOpenPortsText(host string) string {
	return "sudo ufw allow 20000:29999/udp && sudo ufw allow 29400:29999/tcp\n\n" +
		"sudo firewall-cmd --permanent --add-port=20000-29999/udp --add-port=29400-29999/tcp && sudo firewall-cmd --reload"
}

// EmuRemoveInfo is what Remove Host can take back or wipe on a host.
type EmuRemoveInfo struct {
	Switches    int      `json:"switches"`    // switches of the open topology on it
	ImagesBytes int64    `json:"imagesBytes"` // its images
	ImagesDir   string   `json:"imagesDir"`
	LabBytes    int64    `json:"labBytes"` // the switches' own disks (all topologies) in its lab folder
	LabDirs     int      `json:"labDirs"`
	SudoRule    bool     `json:"sudoRule"` // Set Up wrote its sudo rule
	Group       bool     `json:"group"`    // Set Up put the user in the kvm group
	Pkgs        []string `json:"pkgs"`     // packages Set Up installed
	FullSudo    bool     `json:"fullSudo"` // sudo without a password for anything (else the password is needed to take back)
	Error       string   `json:"error"`    // the host could not be reached: it can only be forgotten
}

// EmuRemoveHostInfo reads what Remove Host can take back or wipe on a host.
func (a *App) EmuRemoveHostInfo(host string) (EmuRemoveInfo, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	dir := emuLabDir()
	var r EmuRemoveInfo
	if l := emuLab.f.Lab; l != nil {
		for _, v := range l.VMs {
			if emuHostOf(v) == host {
				r.Switches++
			}
		}
	}
	emuLab.mu.Unlock()
	out, err := a.emuRun(host, `d="$HOME/`+dir+`"; [ -e "$d/images" ] && echo "@img $(du -sbL "$d/images" 2>/dev/null | cut -f1) $(readlink -f "$d/images")"
n=0; t=0; for l in "$d"/lab-*; do [ -d "$l" ] || continue; n=$((n+1)); s=$(du -sb "$l" 2>/dev/null | cut -f1); t=$((t+${s:-0})); done; echo "@lab $n $t"
cat /var/lib/termwright/setup 2>/dev/null; sudo -n true 2>/dev/null && echo "@fullsudo"; true`, time.Minute)
	if err != nil {
		r.Error = err.Error()
		return r, nil
	}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(strings.TrimSpace(ln))
		switch {
		case len(f) >= 2 && f[0] == "@img":
			r.ImagesBytes, _ = strconv.ParseInt(f[1], 10, 64)
			if len(f) >= 3 {
				r.ImagesDir = f[2]
			}
		case len(f) == 3 && f[0] == "@lab":
			r.LabDirs, _ = strconv.Atoi(f[1])
			r.LabBytes, _ = strconv.ParseInt(f[2], 10, 64)
		case len(f) == 1 && f[0] == "@fullsudo":
			r.FullSudo = true
		case len(f) == 1 && f[0] == "sudo":
			r.SudoRule = true
		case len(f) == 2 && f[0] == "group":
			r.Group = true
		case len(f) == 2 && f[0] == "pkg":
			r.Pkgs = append(r.Pkgs, f[1])
		}
	}
	return r, nil
}

// EmuRemoveHostReq says what Remove Host does on the host before it forgets it.
type EmuRemoveHostReq struct {
	Host      string `json:"host"`
	Images    bool   `json:"images"`    // delete its images
	Labs      bool   `json:"labs"`      // delete the switches' own disks (all topologies) in its lab folder
	SudoRule  bool   `json:"sudoRule"`  // remove Set Up's sudo rule
	Group     bool   `json:"group"`     // take the user out of the kvm group (when Set Up put it there)
	Pkgs      bool   `json:"pkgs"`      // remove the packages Set Up installed
	Password  string `json:"password"`  // the sudo password, when sudo asks for one
	ForgetOff bool   `json:"forgetOff"` // a host that cannot be reached: only forget it
}

// EmuRemoveHostWith takes back or wipes what was chosen on the host, then forgets it.
func (a *App) EmuRemoveHostWith(r EmuRemoveHostReq) (EmuView, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	dir := emuLabDir()
	if l := emuLab.f.Lab; l != nil {
		for _, v := range l.VMs {
			if emuHostOf(v) == r.Host {
				emuLab.mu.Unlock()
				return EmuView{}, errors.New("switches of this topology run on this host: move or remove them first")
			}
		}
	}
	emuLab.mu.Unlock()
	var notes []string
	if !r.ForgetOff && (r.Images || r.Labs) {
		script := `d="$HOME/` + dir + `"`
		if r.Labs {
			script += `; for l in "$d"/lab-*; do [ -d "$l" ] || continue; for p in "$l"/*.pid; do [ -f "$p" ] && kill "$(cat "$p")" 2>/dev/null; done; rm -rf "$l"; done; echo "@@note the switches' disks are deleted"`
		}
		if r.Images {
			script += `; if [ -L "$d/images" ]; then t=$(readlink -f "$d/images"); rm -f "$d/images"; [ -n "$t" ] && [ "$t" != / ] && [ "$t" != "$HOME" ] && rm -rf "$t"; else rm -rf "$d/images"; fi; echo "@@note the images are deleted"`
		}
		script += `; rmdir "$d" 2>/dev/null; true`
		out, err := a.emuRun(r.Host, script, 10*time.Minute)
		if err != nil {
			return EmuView{}, fmt.Errorf("deleting on the host: %w", err)
		}
		notes = append(notes, emuNotes(out)...)
	}
	if !r.ForgetOff && (r.SudoRule || r.Group || r.Pkgs) {
		cl, err := a.emuClient(context.Background(), r.Host)
		if err != nil {
			return EmuView{}, err
		}
		s, err := cl.NewSession()
		if err != nil {
			return EmuView{}, err
		}
		if r.Password != "" {
			s.Stdin = strings.NewReader(r.Password + "\n")
		}
		out, err := s.CombinedOutput(emu.UndoCommand(r.SudoRule, r.Group, r.Pkgs, r.Password != ""))
		s.Close()
		o := string(out)
		if strings.Contains(o, "a password is required") || strings.Contains(o, "incorrect password") || strings.Contains(o, "Sorry, try again") {
			return EmuView{}, errors.New("sudo wants the password: type it and try again (nothing was taken back)")
		}
		if !strings.Contains(o, "@@done") {
			return EmuView{}, fmt.Errorf("taking back Set Up: %v %s", err, strings.TrimSpace(o))
		}
		notes = append(notes, emuNotes(o)...)
	}
	emuLab.mu.Lock()
	if c := emuLab.conns[r.Host]; c != nil {
		c.close()
		delete(emuLab.conns, r.Host)
	}
	for _, n := range notes {
		emuEvOn(r.Host, "", "info", "removing the host: "+n)
	}
	emuEvOn(r.Host, "", "ok", "the host is removed from Termwright")
	emuLab.mu.Unlock()
	return a.EmuRemoveHost(r.Host)
}

// emuNotes are the "@@note" lines of a script.
func emuNotes(out string) []string {
	var n []string
	for _, ln := range strings.Split(out, "\n") {
		if t, ok := strings.CutPrefix(strings.TrimSpace(ln), "@@note "); ok {
			n = append(n, t)
		}
	}
	return n
}
