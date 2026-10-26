import { Component, OnDestroy, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { ago } from '../core/env';
import { CheckResult, CheckRun, Checklist, SessionNode } from '../core/models';
import { Ui } from '../core/ui.service';

/** Change Checklists: the same show commands before and after a change, compared per device. */
@Component({
  selector: 'app-checklists',
  imports: [FormsModule],
  template: `
  <div class="modal-backdrop">
    <div class="modal huge ck">
      <div class="modal-head">Change Checklists</div>
      <div class="modal-split">
        <nav class="cat lists">
          @for (c of lists(); track $index) {
            <a [class.on]="sel() === c.name" (click)="pickList(c.name)">{{ c.name || '(no name)' }}<span>{{ c.targets.length }} item{{ c.targets.length === 1 ? '' : 's' }}</span></a>
          }
          <button type="button" class="add" (click)="addList()">+ New checklist</button>
        </nav>
        <div class="modal-body">
          @if (cur(); as c) {
            <details class="setup" [open]="!runs().length || dirty()">
              <summary>Checklist: {{ c.name }} - {{ devices().length }} device{{ devices().length === 1 ? '' : 's' }}, {{ c.commands.length }} command set{{ c.commands.length === 1 ? '' : 's' }}</summary>
              <div class="grid">
                <label>Name</label><input [ngModel]="c.name" (ngModelChange)="rename(c, $event)" placeholder="e.g. Core upgrade" />
                <label>Devices</label>
                <div>
                  <div class="chips">
                    @for (t of c.targets; track t) {
                      <span class="chip">{{ t.endsWith('/') ? '📁 ' + t.slice(0, -1) : t }}<button type="button" (click)="removeTarget(c, t)" title="Remove">✕</button></span>
                    } @empty { <span class="hint">No devices yet.</span> }
                  </div>
                  <div class="row addrow">
                    <input [(ngModel)]="pick" list="ck-targets" placeholder="Folder or session" (keydown.enter)="addTarget(c)" />
                    <datalist id="ck-targets">@for (o of choices(); track o) { <option [value]="o"></option> }</datalist>
                    <button type="button" (click)="addTarget(c)" [disabled]="!pick.trim()">Add</button>
                    <button type="button" (click)="addSelected(c)" [disabled]="!treeSelection().length">Add Selected ({{ treeSelection().length }})</button>
                  </div>
                </div>
                <label>Commands</label>
                <div class="cmds">
                  @for (g of c.commands; track $index) {
                    <div class="cg">
                      <div class="row">
                        <select [(ngModel)]="g.deviceType" (ngModelChange)="dirty.set(true)">
                          <option value="">Any other device</option>
                          @for (d of deviceTypes(); track d) { <option [value]="d">{{ d }}</option> }
                        </select>
                        <span class="spacer"></span>
                        <button type="button" class="icon-btn" title="Remove" (click)="removeGroup(c, $index)">✕</button>
                      </div>
                      <textarea class="mono" rows="4" [(ngModel)]="g.commands" (ngModelChange)="dirty.set(true)"
                        placeholder="One show command per line, e.g. show isis adjacency"></textarea>
                    </div>
                  }
                  <button type="button" (click)="addGroup(c)">+ Commands for a device type</button>
                  <div class="hint">Use show commands only: they run on every device before and after the change. Logins use saved passwords - nothing is asked.</div>
                </div>
              </div>
            </details>

            <div class="runbar">
              <b>Change</b>
              <select [ngModel]="runId()" (ngModelChange)="runId.set($event)" [disabled]="!runs().length">
                @for (r of runs(); track r.id) { <option [value]="r.id">{{ runLabel(r) }}</option> }
                @if (!runs().length) { <option value="">no change recorded yet</option> }
              </select>
              <span class="spacer"></span>
              @if (busy()) {
                <progress [max]="total()" [value]="doneN()"></progress><span class="hint">{{ phaseRunning() }} {{ doneN() }} / {{ total() }}</span>
                <button type="button" (click)="cancel()">Stop</button>
              } @else {
                <button type="button" (click)="before(true)" [disabled]="!devices().length || !c.commands.length">New Change: Take Before</button>
                @if (run()) {
                  <button type="button" (click)="before(false)" title="Take the before snapshot again for this change">Retake Before</button>
                  <button type="button" class="primary" (click)="after()">Take After</button>
                }
              }
            </div>
            @if (run(); as r) {
              <div class="sum">{{ summary(r) }}</div>
            }
            <div class="devs">
              <table class="list">
                <thead><tr><th>Device</th><th>Device type</th><th>Before</th><th>After</th><th>Result</th><th></th></tr></thead>
                <tbody>
                  @for (p of rows(); track p) {
                    <tr>
                      <td>{{ p }}</td>
                      <td>{{ node(p)?.deviceType || '-' }}</td>
                      <td>@if (inProgress().has(p + ':before')) { <span class="hint">running…</span> } @else {
                        @if (dev(p)?.before; as b) { <span [class.bad]="!b.ok" [title]="b.error || ''">{{ b.ok ? '✓ ' + ago(b.time) : '✕ ' + b.error }}</span>
                          @if (b.log) { <a class="log" (click)="open(b.log)">device output</a> } } @else { <span class="hint">-</span> } }</td>
                      <td>@if (inProgress().has(p + ':after')) { <span class="hint">running…</span> } @else {
                        @if (dev(p)?.after; as a) { <span [class.bad]="!a.ok" [title]="a.error || ''">{{ a.ok ? '✓ ' + ago(a.time) : '✕ ' + a.error }}</span>
                          @if (a.log) { <a class="log" (click)="open(a.log)">device output</a> } } @else { <span class="hint">-</span> } }</td>
                      <td>
                        @if (dev(p)?.changed === true) { <span class="chg">● changed</span> }
                        @else if (dev(p)?.changed === false) { <span class="ok">✓ same</span> }
                      </td>
                      <td class="acts">
                        <button type="button" [disabled]="!both(p)" (click)="compare(p)" title="Before and after side by side">Compare</button>
                        <button type="button" [disabled]="!dev(p)?.before?.file" (click)="open(dev(p)!.before!.file!)">Before</button>
                        <button type="button" [disabled]="!dev(p)?.after?.file" (click)="open(dev(p)!.after!.file!)">After</button>
                      </td>
                    </tr>
                  } @empty { <tr><td colspan="6" class="hint">Add folders or sessions to the checklist.</td></tr> }
                </tbody>
              </table>
            </div>
            @if (err()) { <p class="err">{{ err() }}</p> }
          } @else {
            <div class="intro">
              <p><b>Before a change</b>, take a snapshot: Termwright runs your show commands on every device of the checklist (in the background, no tabs).</p>
              <p><b>After the change</b>, take the after snapshot: each device is marked <i>same</i> or <i>changed</i>, and <b>Compare</b> shows the difference.</p>
              <p class="hint">Times, dates and uptime counters are ignored when deciding what changed.</p>
              <button type="button" class="primary" (click)="addList()">+ New checklist</button>
            </div>
          }
        </div>
      </div>
      <div class="modal-foot">
        <span class="hint dir" [title]="dir()">Saved in {{ dir() }}</span>
        <button type="button" (click)="reveal()">Open Folder</button>
        @if (cur()) { <button type="button" class="danger-text" (click)="removeList()">Delete Checklist</button> }
        <span class="spacer"></span>
        <button type="button" (click)="close()">Close</button>
        @if (dirty()) { <button type="button" class="primary" (click)="save()">Save</button> }
      </div>
    </div>
  </div>`,
  styles: [`
    .ck { width: min(1100px, 96vw); }
    .lists { display: flex; flex-direction: column; overflow: auto; a { display: flex; flex-direction: column; line-height: 1.3; padding-top: 6px; padding-bottom: 6px; }
      a span { font-size: 11px; color: var(--muted); } .add { margin: 8px; } }
    .setup { border: 1px solid var(--border); border-radius: 6px; padding: 6px 10px; margin-bottom: 10px;
      summary { cursor: pointer; font-weight: 600; padding: 2px 0; } .grid { margin-top: 8px; } }
    .chips { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 6px; }
    .chip { display: inline-flex; gap: 4px; align-items: center; padding: 1px 4px 1px 8px; border: 1px solid var(--border); border-radius: 12px; font-size: 12px;
      button { border: 0; background: none; height: auto; padding: 0 3px; color: var(--muted); cursor: pointer; } }
    .addrow input { max-width: 320px; }
    .cmds { display: flex; flex-direction: column; gap: 8px; align-items: flex-start; .cg { width: 100%; } textarea { width: 100%; resize: vertical; margin-top: 4px; } }
    .row { display: flex; gap: 8px; align-items: center; } .spacer { flex: 1; }
    .runbar { display: flex; gap: 8px; align-items: center; margin: 4px 0 6px; select { max-width: 360px; } progress { width: 140px; accent-color: var(--accent); } }
    .sum { font-size: 12.5px; color: var(--muted); margin-bottom: 6px; }
    .devs { border: 1px solid var(--border); border-radius: 6px; max-height: 44vh; overflow: auto; }
    table { width: 100%; } th { position: sticky; top: 0; background: var(--panel); z-index: 1; } td { vertical-align: middle; font-size: 12.5px; }
    .acts { white-space: nowrap; text-align: right; button { height: 22px; font-size: 11.5px; padding: 0 7px; margin-left: 4px; } }
    .bad { color: var(--danger); } .chg { color: #f59e0b; } .ok { color: var(--ok); }
    .log { display: block; font-size: 11px; color: var(--accent); cursor: pointer; }
    .dir { max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .intro { max-width: 640px; line-height: 1.55; }
    .err { color: var(--danger); } .danger-text { color: var(--danger); }
  `],
})
export class Checklists implements OnDestroy {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  readonly lists = signal<Checklist[]>(structuredClone(this.ui.settings()?.checklists ?? []));
  readonly sel = signal(this.lists()[0]?.name ?? '');
  readonly dirty = signal(false);
  readonly runs = signal<CheckRun[]>([]);
  readonly runId = signal('');
  readonly devices = signal<string[]>([]);
  readonly inProgress = signal<Set<string>>(new Set());
  readonly busy = signal(false);
  readonly phaseRunning = signal('');
  readonly doneN = signal(0);
  readonly total = signal(0);
  readonly dir = signal('');
  readonly err = signal('');
  pick = '';
  private unsub: (() => void)[] = [];
  private lastTargets = '';

  readonly cur = computed(() => this.lists().find(c => c.name === this.sel()) ?? null);
  readonly run = computed(() => this.runs().find(r => r.id === this.runId()) ?? null);
  readonly deviceTypes = computed(() => (this.ui.settings()?.deviceTypes ?? []).map(d => d.name));
  readonly rows = computed(() => [...new Set([...this.devices(), ...Object.keys(this.run()?.devices ?? {})])].sort());
  readonly choices = computed(() => {
    const out: string[] = [];
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => { if (c.isFolder) { out.push(c.path + '/'); walk(c); } else out.push(c.path); });
    walk(this.ui.tree());
    return out;
  });
  readonly treeSelection = computed(() => {
    const multi = [...this.ui.selectedPaths()];
    if (multi.length) return multi;
    const p = this.ui.selectedPath();
    return p ? [this.ui.selectedIsFolder() ? p + '/' : p] : [];
  });

  constructor() {
    // opened from the Session Manager: the checklist of exactly these devices, or a new one
    const want = this.ui.checklistTargets();
    this.ui.checklistTargets.set(null);
    if (want?.length) {
      const key = JSON.stringify([...want].sort());
      const same = this.lists().find(c => JSON.stringify([...c.targets].sort()) === key);
      if (same) this.sel.set(same.name); else this.addList(want);
    }
    this.backend.checklistDir().then(d => this.dir.set(d)).catch(() => {});
    void this.loadRuns();
    this.unsub.push(
      this.backend.on('check:progress', (r: CheckResult) => {
        if (r.list !== this.sel()) return;
        this.doneN.set(r.done);
        this.inProgress.update(s => { const n = new Set(s); n.delete(r.path + ':' + r.phase); return n; });
        void this.loadRuns(false);
      }),
      this.backend.on('check:done', (d: { list: string; phase: string; total: number; failed: number; changed: number; cancelled: boolean }) => {
        if (d.list !== this.sel()) return;
        this.busy.set(false);
        this.inProgress.set(new Set());
        void this.loadRuns(false);
        const what = d.phase === 'before' ? 'Before snapshot' : 'After snapshot';
        this.ui.toast(d.cancelled ? what + ' stopped' : `${what}: ${d.total - d.failed} of ${d.total} devices` +
          (d.phase === 'after' ? `, ${d.changed} changed` : '') + (d.failed ? `, ${d.failed} failed` : ''), d.failed ? 'error' : 'success', 6000);
      }),
    );
    const t = setInterval(() => {
      const key = JSON.stringify(this.cur()?.targets ?? []);
      if (key !== this.lastTargets) { this.lastTargets = key; void this.loadDevices(); }
    }, 300);
    this.unsub.push(() => clearInterval(t));
  }

  ngOnDestroy() { this.unsub.forEach(u => u()); }

  private async loadDevices() {
    const c = this.cur();
    this.devices.set(c?.targets.length ? await this.backend.backupSessions(c.targets).catch(() => []) : []);
  }

  private async loadRuns(selectNewest = true) {
    const c = this.cur();
    const runs = c ? await this.backend.checklistRuns(c.name).catch(() => []) ?? [] : [];
    this.runs.set(runs);
    if (selectNewest || !runs.some(r => r.id === this.runId())) this.runId.set(runs[0]?.id ?? '');
  }

  pickList(name: string) { this.sel.set(name); void this.loadRuns(); }
  node(p: string) { return this.ui.findNode(p); }
  dev(p: string) { return this.run()?.devices[p]; }
  both(p: string) { const d = this.dev(p); return !!(d?.before?.file && d?.after?.file); }
  ago(t: string) { return ago(t); }

  runLabel(r: CheckRun) {
    const d = new Date(r.created).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
    return d + (r.note ? ' - ' + r.note : '');
  }

  summary(r: CheckRun) {
    const ds = Object.values(r.devices);
    const n = (f: (x: typeof ds[number]) => boolean) => ds.filter(f).length;
    const before = n(d => !!d.before?.ok), after = n(d => !!d.after?.ok), ch = n(d => d.changed === true), same = n(d => d.changed === false);
    const failed = n(d => d.before?.ok === false || d.after?.ok === false);
    if (!after) return `Before snapshot of ${before} device${before === 1 ? '' : 's'} - make the change, then Take After.` + (failed ? ` ${failed} failed.` : '');
    return `${ch} changed · ${same} the same` + (failed ? ` · ${failed} failed` : '');
  }

  addList(targets?: string[]) {
    let name = 'Checklist', i = 1;
    while (this.lists().some(c => c.name === name)) name = 'Checklist ' + ++i;
    const dts = this.deviceTypes();
    const defaults: Record<string, string> = {
      voss: 'show isis adjacency\nshow vlan basic\nshow interfaces gigabitEthernet state\nshow ip route',
      exos: 'show vlan\nshow ports no-refresh\nshow iproute',
      'cisco ios': 'show ip interface brief\nshow vlan brief\nshow ip route summary',
    };
    const commands = dts.filter(d => defaults[d.toLowerCase()]).map(d => ({ deviceType: d, commands: defaults[d.toLowerCase()] }));
    this.lists.update(l => [...l, { name, targets: targets ?? this.treeSelection(), commands: commands.length ? commands : [{ deviceType: '', commands: '' }] }]);
    this.sel.set(name);
    this.runs.set([]);
    this.runId.set('');
    this.dirty.set(true);
  }

  rename(c: Checklist, name: string) {
    if (!name.trim() || this.lists().some(x => x !== c && x.name === name)) return;
    c.name = name;
    this.sel.set(name);
    this.lists.update(l => [...l]);
    this.dirty.set(true);
  }

  addTarget(c: Checklist) {
    const v = this.pick.trim();
    if (!v || c.targets.includes(v)) return;
    c.targets = [...c.targets, v];
    this.pick = '';
    this.lists.update(l => [...l]);
    this.dirty.set(true);
  }

  addSelected(c: Checklist) {
    c.targets = [...new Set([...c.targets, ...this.treeSelection()])];
    this.lists.update(l => [...l]);
    this.dirty.set(true);
  }

  removeTarget(c: Checklist, t: string) {
    c.targets = c.targets.filter(x => x !== t);
    this.lists.update(l => [...l]);
    this.dirty.set(true);
  }

  addGroup(c: Checklist) { c.commands = [...c.commands, { deviceType: '', commands: '' }]; this.lists.update(l => [...l]); this.dirty.set(true); }
  removeGroup(c: Checklist, i: number) { c.commands = c.commands.filter((_, k) => k !== i); this.lists.update(l => [...l]); this.dirty.set(true); }

  async removeList() {
    const c = this.cur();
    if (!c || !(await this.ui.confirm('Delete Checklist', `Delete the checklist "${c.name}"? Recorded snapshots stay in the checklist folder.`, 'Delete', true))) return;
    this.lists.update(l => l.filter(x => x !== c));
    this.sel.set(this.lists()[0]?.name ?? '');
    await this.save();
    await this.loadRuns();
  }

  async save() {
    const s = this.ui.settings();
    if (!s) return;
    await this.ui.saveSettings({ ...s, checklists: this.lists().filter(c => c.name.trim()) });
    this.dirty.set(false);
  }

  private async start(runId: string, phase: 'before' | 'after', note: string) {
    const c = this.cur();
    if (!c) return;
    if (this.dirty()) await this.save();
    this.err.set('');
    try {
      const id = await this.backend.checklistRun(c.name, runId, phase, note);
      await this.loadRuns(false);
      this.runId.set(id);
      this.busy.set(true);
      this.phaseRunning.set(phase === 'before' ? 'Before:' : 'After:');
      this.doneN.set(0);
      this.total.set(this.devices().length);
      this.inProgress.set(new Set(this.devices().map(p => p + ':' + phase)));
    } catch (e) { this.err.set(errText(e)); }
  }

  async before(fresh: boolean) {
    if (fresh) {
      const note = await this.ui.input('New Change', 'Change ticket or note (optional), e.g. CHG-1234 core upgrade');
      if (note === null) return;
      await this.start('', 'before', note.trim());
    } else await this.start(this.runId(), 'before', '');
  }

  async after() { await this.start(this.runId(), 'after', ''); }

  cancel() { const c = this.cur(); if (c) void this.backend.checklistCancel(c.name, this.runId()); }

  compare(p: string) {
    const d = this.dev(p);
    if (!d?.before?.file || !d.after?.file) return;
    this.ui.compareWith.set(['path:' + d.before.file, 'path:' + d.after.file]);
    this.ui.open('compare');
  }

  async open(f: string) { await this.backend.openPath(f).catch(e => this.ui.error(e)); }
  async reveal() { await this.backend.showChecklistFolder().catch(e => this.ui.error(e)); }

  async close() {
    if (this.dirty() && await this.ui.confirm('Change Checklists', 'Save the changes to the checklists?', 'Save')) await this.save();
    this.ui.close();
  }
}

