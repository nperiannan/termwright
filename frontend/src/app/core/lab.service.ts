import { Injectable, computed, inject, signal, untracked } from '@angular/core';
import { Backend, errText } from './backend';
import { EmuImage, EmuRunRow, EmuPersona, EmuTemplate, EmuRequest, EmuSetupDone, EmuSetupLine, LabDevice, LabLink, LabOutput, LabRequest, LabView } from './models';
import { Ui } from './ui.service';
import { tr } from './i18n';

/** The labs for the Lab window and the Session Manager, refreshed while they run:
 *  the simulated lab (switches from a device pack, on this computer) and the
 *  emulated lab (the vendors' VM images on a Linux KVM host). `kind` says which
 *  one the Lab window shows; the actions go to that one. */
@Injectable({ providedIn: 'root' })
export class LabService {
  private backend = inject(Backend);
  private ui = inject(Ui);
  readonly sim = signal<LabView | null>(null);
  readonly emu = signal<LabView | null>(null);
  readonly kind = signal<'sim' | 'emu'>('sim');
  readonly state = computed(() => (this.kind() === 'emu' ? this.emu() : this.sim()));
  readonly busy = signal(false);
  /** The Lab window opens on this tab (and switch), from the workspace. */
  readonly dialogTab = signal<{ tab: string; focus: string; action?: 'pack' } | null>(null);
  /** The Lab window is open (refresh more often). */
  readonly watching = signal(false);
  private watchers = new Set<string>();
  /** The Lab window or a workspace tab shows the lab (who: its name). */
  watch(who: string, on: boolean) { if (on) this.watchers.add(who); else this.watchers.delete(who); this.watching.set(this.watchers.size > 0); if (on) this.schedule(); }
  private timer: ReturnType<typeof setTimeout> | null = null;

  readonly devices = computed(() => this.state()?.devices ?? []);
  readonly links = computed(() => this.state()?.links ?? []);
  /** "127.0.0.1:port" → simulated switch, for the Session Manager's dots. */
  readonly byPort = computed(() => new Map((this.sim()?.devices ?? []).map(d => [d.port, d] as [number, LabDevice])));
  /** session path → emulated switch (its SSH session or console). */
  readonly byPath = computed(() => {
    const e = this.emu(), m = new Map<string, LabDevice>();
    for (const d of e?.devices ?? []) { const s = e?.sessions?.[d.id], c = e?.consoles?.[d.id]; if (s) m.set(s, d); if (c) m.set(c, d); }
    return m;
  });
  readonly running = computed(() => this.devices().filter(d => d.running).length);
  readonly emuRunning = computed(() => (this.emu()?.devices ?? []).filter(d => d.state === 'running').length);
  readonly simRunning = computed(() => (this.sim()?.devices ?? []).filter(d => d.running).length);
  /** Switches running on the KVM hosts, every topology (read from the hosts every 30 s). */
  readonly runRows = signal<EmuRunRow[]>([]);
  /** The toolbar's Lab button: running switches (all hosts and topologies) and the open topology's ones that need a look. */
  readonly badge = computed(() => {
    const rows = this.runRows(), devs = this.emu()?.devices ?? [];
    const n = Math.max(rows.reduce((t, r) => t + r.switches.length, 0), this.emuRunning());
    const look = devs.filter(d => d.state === 'failed' || (d.state && d.state !== 'running' && d.state !== 'stopped') || (d.running && (d.error || d.personaError)));
    const lines = rows.map(r => '● ' + tr(r.open ? '{0} running on {1} · {2} (open)' : '{0} running on {1} · {2} (in the background)', r.switches.length, r.host.split('/').pop() ?? '', r.topology));
    for (const d of look) lines.push(`▲ ${d.hostname}: ${d.state === 'running' ? (d.error || d.personaError) : (d.stateText || d.state)}`);
    return { n, look: look.length, tip: [tr('Emulated lab'), ...lines].join('\n') };
  });
  async refreshRunning() {
    const e = this.emu();
    if (!e?.host && !(e?.hosts ?? []).length) { this.runRows.set([]); return; }
    try { this.runRows.set((await this.backend.emuRunning()) ?? []); } catch { /* hosts not reachable */ }
  }

  constructor() {
    void this.refresh();
    setTimeout(() => void this.refreshRunning(), 4000);
    setInterval(() => void this.refreshRunning(), 30000);
    this.backend.on('emu:error', (m: string) => this.ui.error(m));
    this.backend.on('emu:done', (m: string) => { this.ui.toast(m, 'success'); void this.refresh(); setTimeout(() => void this.refreshRunning(), 6000); });
    this.backend.on('emu:setup', (l: EmuSetupLine) => {
      const s = this.setup(); if (!s || s.run !== l.run) return;
      this.setup.set({ ...s, step: l.kind === 'step' ? l.text : s.step, lines: [...s.lines, (l.kind === 'line' ? '' : l.kind === 'step' ? '▸ ' : l.kind === 'fail' ? '✕ ' : '! ') + l.text].slice(-300) });
    });
    this.backend.on('emu:setup:done', (d: EmuSetupDone) => {
      const s = this.setup(); if (!s || s.run !== d.run) return;
      this.setup.set({ ...s, done: true, error: d.error });
      const e = this.emu(); if (d.host && e?.hosts) this.emu.set({ ...e, hosts: e.hosts.map(h => h.session === d.host!.session ? d.host! : h) }); // its checks at once
      if (d.error) this.ui.error(d.error); else this.ui.toast('The host is set up', 'success');
      void this.refresh();
    });
  }

  async refresh() {
    try { this.sim.set(await this.backend.labState()); } catch { /* not ready */ }
    try { this.emu.set(await this.backend.emuState()); } catch { /* not ready */ }
    if (!this.watching() && this.kind() === 'sim' && !this.sim()?.exists && this.emu()?.exists) this.kind.set('emu');
    this.schedule();
  }

  private schedule() {
    if (this.timer) clearTimeout(this.timer);
    const watching = this.watching();
    // an install of an ISO shows its progress: polled every few seconds (read untracked: schedule also runs inside effects)
    const busy = !watching && untracked(() => (this.emu()?.installs ?? []).some(j => ['copying', 'starting', 'installing', 'saving', 'fetching'].includes(j.stage)));
    const live = watching || (this.sim()?.running ?? false) || (this.emu()?.running ?? false) || busy;
    if (live) this.timer = setTimeout(() => void this.refresh(), watching || busy ? 2500 : 8000);
  }

  /** The simulated switch behind a session (127.0.0.1 on a lab port), or the emulated one (by its path). */
  deviceOf(host: string | undefined, port: number | undefined, path?: string): LabDevice | undefined {
    if (path) { const d = this.byPath().get(path); if (d) return d; }
    return host === '127.0.0.1' && port ? this.byPort().get(port) : undefined;
  }

  private async run(f: () => Promise<LabView>, ok?: string): Promise<boolean> {
    this.busy.set(true);
    try {
      const v = await f();
      (v?.kind === 'emu' ? this.emu : this.sim).set(v);
      if (ok) this.ui.toast(ok, 'success');
      return true;
    } catch (e) {
      this.ui.error(errText(e));
      await this.refresh();
      return false;
    } finally { this.busy.set(false); this.schedule(); }
  }
  private get e() { return this.kind() === 'emu'; }

  async add(r: LabRequest) {
    const ok = await this.run(() => this.backend.labAdd(r), r.groups.reduce((n, g) => n + g.count, 0) + ' simulated switches added' + (r.start ? ' and started' : ''));
    if (ok) await this.ui.refreshTree();
    return ok;
  }
  async addEmu(r: EmuRequest) {
    const n = r.groups.reduce((k, g) => k + g.count, 0);
    const ok = await this.run(() => this.backend.emuAdd(r), n + ' emulated switches added' + (r.start ? ' - they boot on the host now' : ''));
    if (ok) { this.kind.set('emu'); await this.ui.refreshTree(); }
    return ok;
  }
  async start(ids: string[] = []) { const ok = await this.run(() => (this.e ? this.backend.emuStart(ids) : this.backend.labStart(ids))); await this.ui.refreshTree(); return ok; }
  shutDown(ids: string[] = []) { return this.run(() => this.backend.emuShutDown(ids)); }
  setFast(on: boolean) { return this.run(() => this.backend.emuSetFast(on), on ? 'Fast start on - Stop saves the switches’ running state' : 'Fast start off - the switches boot each time'); }
  stop(ids: string[] = []) { return this.run(() => (this.e ? this.backend.emuStop(ids) : this.backend.labStop(ids))); }
  restart(ids: string[]) { return this.run(() => this.backend.emuRestart(ids)); }
  setMgmt(mgmt: string, mgmtNet = '') { return this.run(() => this.backend.emuSetMgmt(mgmt, mgmtNet), 'Management changed - it applies when the switches start'); }
  reset(ids: string[]) { return this.run(() => this.backend.emuReset(ids), 'Back to day 0: the switches boot as new'); }
  async remove(ids: string[], sessions: boolean) {
    const ok = await this.run(() => (this.e ? this.backend.emuRemove(ids, sessions) : this.backend.labRemove(ids, sessions)));
    if (ok) await this.ui.refreshTree();
    return ok;
  }
  connect(l: LabLink) {
    // drawn at once; the lab's answer replaces it (or the error takes it away again)
    const sig = this.e ? this.emu : this.sim, v = sig();
    if (v) sig.set({ ...v, links: [...(v.links ?? []), { ...l, up: false }] });
    return this.run(() => (this.e ? this.backend.emuConnect(l) : this.backend.labConnect(l)));
  }
  disconnect(id: string, port: string) { return this.run(() => (this.e ? this.backend.emuDisconnect(id, port) : this.backend.labDisconnect(id, port))); }
  setLink(id: string, port: string, up: boolean) { return this.run(() => this.backend.emuSetLink(id, port, up)); }
  setHostMgmt(host: string, mgmt: string, mgmtNet = '') { return this.run(() => this.backend.emuSetHostMgmt(host, mgmt, mgmtNet), 'Management changed - it applies when the switches start'); }
  moveVM(id: string, host: string) { return this.run(() => this.backend.emuMoveVM(id, host), 'Moved - it starts there from its image (day 0)'); }
  day0Again(id: string) { return this.run(() => this.backend.emuDay0Again(id), 'Typing day 0 again - see the Events'); }
  migrate(id: string, host: string, live: boolean) { return this.run(() => this.backend.emuMigrate(id, host, live), live ? 'Moving live - see the Events' : 'Moving with its disk - see the Events'); }
  setLag(vm: string, name: string, mode: string, ports: string[]) { return this.run(() => this.backend.emuSetLag(vm, name, mode, ports)); }
  saveTopology(name: string) { return this.run(() => this.backend.emuSaveTopology(name), 'Topology saved'); }
  openTopology(name: string, start: boolean, hosts: Record<string, string> | null = null) { return this.run(() => (hosts ? this.backend.emuOpenTopologyOn(name, start, hosts) : this.backend.emuOpenTopology(name, start)), start ? 'Opened - the switches start' : 'Opened'); }
  newTopology(name: string) { return this.run(() => this.backend.emuNewTopology(name), 'New topology - add switches'); }
  applyLag(vm: string, name: string, remove: boolean) { return this.run(() => this.backend.emuApplyLag(vm, name, remove), remove ? 'LAG removed from the switch' : 'LAG set on the switch'); }
  checkCabling() { return this.run(() => this.backend.emuCheckCabling(), 'Cabling checked with LLDP - see the cables and the Events'); }
  readLags(ids: string[] = []) { return this.run(() => this.backend.emuReadLags(ids), 'LAGs read from the switches'); }
  place(id: string, x: number, y: number) {
    void (this.e ? this.backend.emuPlace(id, Math.round(x), Math.round(y)) : this.backend.labPlace(id, Math.round(x), Math.round(y))).catch(() => {});
  }
  freePorts(id: string) { return (this.e ? this.backend.emuFreePorts(id) : this.backend.labFreePorts(id)).then(p => p ?? []).catch(() => [] as string[]); }
  show(id: string, cmd: string) { return this.backend.labShow(id, cmd).catch(e => errText(e)); }
  consoleTail(id: string) { return this.backend.emuConsoleTail(id).catch(() => ''); }
  setIdentity(id: string, serial: string, mac: string) { return this.run(() => (this.e ? this.backend.emuSetIdentity(id, serial, mac) : this.backend.labSetIdentity(id, serial, mac)), this.e ? 'Saved - the switch gets it when it starts next' : 'Saved'); }
  setSerials(ids: string[], serials: string[]) { return this.run(() => (this.e ? this.backend.emuSetSerials(ids, serials, false) : this.backend.labSetSerials(ids, serials)), 'Serial numbers set'); }
  newSerials(ids: string[]) { return this.run(() => (this.e ? this.backend.emuSetSerials(ids, [], true) : this.backend.labNewSerials(ids)), 'New serial numbers set'); }
  setOutputs(o: LabOutput[]) { return this.run(() => this.backend.labSetOutputs(o), 'Outputs saved'); }
  /** Reads a device pack for the consent step (nothing is kept). */
  packInfo(path: string) { return this.backend.labPackInfo(path); }
  acceptPack(path: string, hash: string) { return this.run(() => this.backend.labPackAccept(path, hash), 'Device pack loaded'); }
  removePack() { return this.run(() => this.backend.labPackRemove(), 'Device pack removed'); }
  // emulated lab: hosts and images
  async checkHost(session: string) {
    this.busy.set(true);
    try { const h = await this.backend.emuCheckHost(session); this.emu.set(await this.backend.emuState()); return h; }
    catch (e) { this.ui.error(errText(e)); return null; } finally { this.busy.set(false); }
  }
  /** A host to set up when the Lab window opens (from the wizard). */
  readonly setupFor = signal('');
  /** The running (or last) host set-up. */
  readonly setup = signal<{ run: string; host: string; step: string; lines: string[]; done: boolean; error: string } | null>(null);
  async setupHost(host: string, password: string) {
    try { const run = await this.backend.emuSetupHost(host, password); this.setup.set({ run, host, step: '', lines: [], done: false, error: '' }); }
    catch (e) { this.ui.error(errText(e)); }
  }
  saveTemplate(t: EmuTemplate) { return this.run(() => this.backend.emuSaveTemplate(t), 'Template saved'); }
  removeTemplate(id: string) { return this.run(() => this.backend.emuRemoveTemplate(id)); }
  importProfiles(path: string) { return this.run(() => this.backend.emuImportProfiles(path), 'Image profiles imported - choose their image files'); }
  setPersona(id: string, p: EmuPersona) { return this.run(() => this.backend.emuSetPersona(id, p), 'Saved - Apply writes it into the switch'); }
  applyPersona(ids: string[]) { return this.run(() => this.backend.emuApplyPersona(ids), 'Writing the persona file - the switch boots again'); }
  setResources(id: string, cpus: number, ram: number, diskGb: number) { return this.run(() => this.backend.emuSetResources(id, cpus, ram, diskGb), 'Saved - the switch gets them when it starts next'); }
  setMACs(ids: string[], start: string) { return this.run(() => (this.e ? this.backend.emuSetMACs(ids, start) : this.backend.labSetMACs(ids, start)), 'MAC addresses set'); }
  /** The template to preselect in New Lab / Add Switches. */
  readonly wizardTemplate = signal('');
  removeHost(session: string) { return this.run(() => this.backend.emuRemoveHost(session)); }
  addImage(path: string) { return this.run(() => this.backend.emuAddImage(path), 'Image added - check its settings'); }
  importAppliance(path: string) { return this.run(() => this.backend.emuImportAppliance(path), 'Appliance imported'); }
  setImageFile(id: string, path: string) { return this.run(() => this.backend.emuSetImageFile(id, path)); }
  saveImage(im: EmuImage, clearPassword = false) { return this.run(() => this.backend.emuSaveImage(im, clearPassword), 'Image saved'); }
  removeImage(id: string) { return this.run(() => this.backend.emuRemoveImage(id)); }
  addFromCatalog(file: string, name: string) { return this.run(() => this.backend.emuAddFromCatalog(file), name + ' added - its versions are in the image list'); }
  download(ids: string[]) { return this.run(() => this.backend.emuDownload(ids), ids.length === 1 ? 'Downloading the image…' : 'Downloading ' + ids.length + ' images one after the other…'); }
  upload(id: string, host = '') { return this.run(() => this.backend.emuUpload(id, host), 'Copying the image to the host…'); }
  osShort(os: string) { return this.state()?.os?.[os]?.short || os.toUpperCase(); }
  sessionOf(id: string) { return this.state()?.sessions?.[id] ?? ''; }
  consoleOf(id: string) { return this.state()?.consoles?.[id] ?? ''; }
}
