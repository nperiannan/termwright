package main

import (
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

// The Hosts × Images matrix: which image files each host has, and copying
// them so every host has the same ones (then switches move between hosts
// without waiting for an image copy).

// EmuMatrixHost is a host in the matrix.
type EmuMatrixHost struct {
	Session string `json:"session"`
	Name    string `json:"name"`
	KVM     bool   `json:"kvm"`
	Free    int64  `json:"free"`  // bytes free in its lab folder
	Error   string `json:"error"` // it could not be read
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

// EmuMatrix is the Hosts × Images view; Cells[host][image] is "ok", "missing",
// "different" (another file of that name), "partial" (a copy that broke off),
// "queued", "copying 41 %" or "failed: why".
type EmuMatrix struct {
	Hosts  []EmuMatrixHost              `json:"hosts"`
	Images []EmuMatrixImage             `json:"images"`
	Cells  map[string]map[string]string `json:"cells"`
	Busy   bool                         `json:"busy"`
}

var emuSync = struct {
	sync.Mutex
	state map[string]string // host|image → queued, copying N %, failed: …
	busy  int
}{state: map[string]string{}}

// emuSyncKey names a cell.
func emuSyncKey(host, id string) string { return host + "|" + id }

// emuSyncPct shows a copy's progress in its cell (when a sync copies it).
func emuSyncPct(host, id string, pct int) {
	emuSync.Lock()
	if _, ok := emuSync.state[emuSyncKey(host, id)]; ok {
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

// EmuImageMatrix reads which images each host has (one command per host, in parallel).
func (a *App) EmuImageMatrix() (EmuMatrix, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	hosts := emuMatrixHosts()
	dir := "termwright-lab"
	used := map[string]int{}
	if l := emuLab.f.Lab; l != nil {
		dir = l.Dir
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
		mi := EmuMatrixImage{ID: im.ID, Name: im.Name, Version: im.Version, FileName: im.FileName, Size: im.Size, Local: im.File != "", Switches: used[im.ID]}
		imgs = append(imgs, mi)
		files = append(files, file{im.FileName, im.SHA256, im.Size, im.ID})
	}
	emuLab.mu.Unlock()
	m := EmuMatrix{Cells: map[string]map[string]string{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	have := map[string]bool{} // an image any host has
	line := regexp.MustCompile(`^(\S+) (\d+) ([0-9a-f]{64})?$`)
	for _, h := range hosts {
		mh := EmuMatrixHost{Session: h.Session, Name: orStr(h.Name, h.Session[strings.LastIndex(h.Session, "/")+1:]), KVM: h.Info.KVM == "yes"}
		m.Hosts = append(m.Hosts, mh)
		wg.Add(1)
		go func(i int, host string) {
			defer wg.Done()
			// every file in the images folder: name, size, its checksum file
			out, err := a.emuRun(host, "d=\"$HOME/"+dir+"/images\"; mkdir -p \"$d\"; df -Pk \"$d\" | awk 'NR==2{print \"@free \" $4}'; cd \"$d\" && for f in *; do case \"$f\" in *.sha256|*.part|*.booted*) continue;; esac; [ -f \"$f\" ] || continue; s=$(stat -c %s \"$f\" 2>/dev/null || stat -f %z \"$f\"); h=$(head -c 64 \"$f.sha256\" 2>/dev/null); echo \"$f $s $h\"; done; for f in *.part; do [ -f \"$f\" ] && echo \"@part ${f%.part}\"; done; true", time.Minute)
			cells := map[string]string{}
			on := map[string][2]string{}
			parts := map[string]bool{}
			for _, ln := range strings.Split(out, "\n") {
				ln = strings.TrimSpace(ln)
				if f, ok := strings.CutPrefix(ln, "@free "); ok {
					kb, _ := strconv.ParseInt(f, 10, 64)
					mu.Lock()
					m.Hosts[i].Free = kb * 1024
					mu.Unlock()
				} else if f, ok := strings.CutPrefix(ln, "@part "); ok {
					parts[f] = true
				} else if g := line.FindStringSubmatch(ln); g != nil {
					on[g[1]] = [2]string{g[2], g[3]}
				}
			}
			for _, f := range files {
				st := "missing"
				if x, ok := on[f.name]; ok {
					sz, _ := strconv.ParseInt(x[0], 10, 64)
					switch {
					case f.sha != "" && x[1] == f.sha, f.sha == "" && (f.size <= 0 || sz == f.size):
						st = "ok"
					default:
						st = "different"
					}
				} else if parts[f.name] {
					st = "partial"
				}
				cells[f.id] = st
			}
			mu.Lock()
			if err != nil {
				m.Hosts[i].Error = err.Error()
			}
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
	for k, st := range emuSync.state { // copies going on (or failed) show over what the host has
		hi, id, _ := strings.Cut(k, "|")
		if c := m.Cells[hi]; c != nil && (c[id] != "ok" || !strings.HasPrefix(st, "failed")) {
			c[id] = st
		}
	}
	m.Busy = emuSync.busy > 0
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
// does not have the image yet (empty lists = all images in the matrix, all hosts).
// The file comes from this computer, or from a host that has it.
func (a *App) EmuSyncImages(hosts, images []string) (EmuMatrix, error) {
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
	type job struct{ host, id, from string }
	byHost := map[string][]job{}
	n := 0
	emuSync.Lock()
	for _, h := range hosts {
		for _, id := range images {
			st := m.Cells[h][id]
			if st == "ok" || strings.HasPrefix(st, "copying") || st == "queued" {
				continue
			}
			from := ""
			for _, o := range m.Hosts {
				if m.Cells[o.Session][id] == "ok" {
					from = o.Session
					break
				}
			}
			byHost[h] = append(byHost[h], job{h, id, from})
			emuSync.state[emuSyncKey(h, id)] = "queued"
			n++
		}
	}
	emuSync.busy += len(byHost)
	emuSync.Unlock()
	if n == 0 {
		return m, errors.New("every host has these images already")
	}
	emuLab.mu.Lock()
	emuEv("", "info", fmt.Sprintf("copying images so the hosts have the same ones: %d copies to %d hosts", n, len(byHost)))
	dir := "termwright-lab"
	if l := emuLab.f.Lab; l != nil {
		dir = l.Dir
	}
	emuLab.mu.Unlock()
	for _, js := range byHost { // one image after another per host, the hosts side by side
		go func(js []job) {
			defer func() { emuSync.Lock(); emuSync.busy--; emuSync.Unlock() }()
			for _, j := range js {
				err := a.emuSyncOne(j.host, j.id, j.from, dir)
				emuSync.Lock()
				if err != nil {
					emuSync.state[emuSyncKey(j.host, j.id)] = "failed: " + err.Error()
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
