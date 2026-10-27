import { Component, OnDestroy, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { ago } from '../core/env';
import { CheckRun, CollectColumn, Collection, SessionNode } from '../core/models';
import { Ui } from '../core/ui.service';

/** Commands and columns offered for well-known device types (matched by device type name). */
const PRESETS: Record<string, { commands: string; columns: { name: string; pattern: string }[] }> = {
  voss: { commands: 'show sys-info', columns: [
    { name: 'Model', pattern: 'ModelName\\s*:\\s*(\\S+)' },
    { name: 'Version', pattern: 'SysDescr\\s*:\\s*\\S+\\s*\\(([^)]+)\\)' },
    { name: 'Serial', pattern: 'Serial#\\s*:\\s*(\\S+)' },
    { name: 'Uptime', pattern: 'SysUpTime\\s*:\\s*(.+)' },
  ] },
  exos: { commands: 'show switch\nshow version', columns: [
    { name: 'Model', pattern: 'System Type:\\s*(.+)' },
    { name: 'Version', pattern: 'IMG:\\s*(\\S+)' },
    { name: 'Serial', pattern: '^Switch\\s*:\\s*\\S+\\s+(\\S+)' },
    { name: 'Uptime', pattern: 'System UpTime:\\s*(.+)' },
  ] },
  'cisco ios': { commands: 'show version', columns: [
    { name: 'Model', pattern: 'Model [Nn]umber\\s*:\\s*(\\S+)' },
    { name: 'Version', pattern: 'Version ([^,\\s]+)' },
    { name: 'Serial', pattern: 'System [Ss]erial [Nn]umber\\s*:\\s*(\\S+)' },
    { name: 'Uptime', pattern: 'uptime is (.+)' },
  ] },
};

interface Row { path: string; deviceType: string; ok: boolean | null; error: string; file: string; log: string; values: Record<string, string>; }

/** Run and Collect: the same commands on many devices, values from the output in a table (CSV export). */
@Component({
  selector: 'app-collect',
  imports: [FormsModule],
  template: `
  <div class="modal-backdrop">
    <div class="modal huge co">
      <div class="modal-head">Run and Collect</div>
      <div class="modal-split">
        <nav class="cat lists">
          @for (c of lists(); track $index) {
            <a [class.on]="sel() === c.name" (click)="pickList(c.name)">{{ c.name || '(no name)' }}<span>{{ c.targets.length }} item{{ c.targets.length === 1 ? '' : 's' }} · {{ colNames(c).length }} column{{ colNames(c).length === 1 ? '' : 's' }}</span></a>
          }
          <button type="button" class="add" (click)="addList()">+ New collection</button>
        </nav>
        <div class="modal-body">
          @if (cur(); as c) {
            <details class="setup" [open]="!runs().length || dirty()">
              <summary>Collection: {{ c.name }} - {{ devices().length }} device{{ devices().length === 1 ? '' : 's' }}, {{ colNames(c).length }} column{{ colNames(c).length === 1 ? '' : 's' }}</summary>
              <div class="grid">
                <label>Name</label><input [ngModel]="c.name" (ngModelChange)="rename(c, $event)" placeholder="e.g. Inventory" />
                <label>Devices</label>
                <div>
                  <div class="chips">
                    @for (t of c.targets; track t) {
                      <span class="chip">{{ t.endsWith('/') ? '📁 ' + t.slice(0, -1) : t }}<button type="button" (click)="removeTarget(c, t)" title="Remove">✕</button></span>
                    } @empty { <span class="hint">No devices yet.</span> }
                  </div>
                  <div class="row addrow">
                    <input [(ngModel)]="pick" list="co-targets" placeholder="Folder or session" (keydown.enter)="addTarget(c)" />
                    <datalist id="co-targets">@for (o of choices(); track o) { <option [value]="o"></option> }</datalist>
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
                      <textarea class="mono" rows="3" [(ngModel)]="g.commands" (ngModelChange)="dirty.set(true)" placeholder="One command per line, e.g. show version"></textarea>
                    </div>
                  }
                  <button type="button" (click)="addGroup(c)">+ Commands for a device type</button>
                </div>
                <label>Columns</label>
                <div class="cols">
                  <table>
                    <thead><tr><th>Column</th><th>Device type</th><th>Pattern (the first ( ) group is the value)</th><th></th></tr></thead>
                    <tbody>
                      @for (k of c.columns; track $index) {
                        <tr>
                          <td><input [(ngModel)]="k.name" (ngModelChange)="dirty.set(true)" placeholder="Version" /></td>
                          <td><select [(ngModel)]="k.deviceType" (ngModelChange)="dirty.set(true)">
                            <option value="">Any device</option>
                            @for (d of deviceTypes(); track d) { <option [value]="d">{{ d }}</option> }
                          </select></td>
                          <td><input class="mono" [class.bad]="!valid(k.pattern)" [(ngModel)]="k.pattern" (ngModelChange)="dirty.set(true)" placeholder="Version\\s+(\\S+)" spellcheck="false" /></td>
                          <td><button type="button" class="icon-btn" title="Remove" (click)="removeColumn(c, $index)">✕</button></td>
                        </tr>
                      }
                    </tbody>
                  </table>
                  <div class="row">
                    <button type="button" (click)="addColumn(c)">+ Column</button>
                    @if (presetTypes().length) { <button type="button" (click)="addPresets(c)" title="Model, version, serial number and uptime for {{ presetTypes().join(', ') }}">+ Inventory Columns</button> }
                    <span class="hint">Values update in the table as you edit - no need to run again.</span>
                  </div>
                </div>
              </div>
            </details>

            <div class="runbar">
              <b>Run</b>
              <select [ngModel]="runId()" (ngModelChange)="pickRun($event)" [disabled]="!runs().length">
                @for (r of runs(); track r.id) { <option [value]="r.id">{{ runLabel(r) }}</option> }
                @if (!runs().length) { <option value="">not run yet</option> }
              </select>
              <span class="spacer"></span>
              @if (busy()) {
                <progress [max]="total()" [value]="doneN()"></progress><span class="hint">{{ doneN() }} / {{ total() }}</span>
                <button type="button" (click)="cancel()">Stop</button>
              } @else {
                <button type="button" [disabled]="!rows().length" (click)="copy()">Copy Table</button>
                <button type="button" [disabled]="!rows().length" (click)="exportCsv()">Export CSV…</button>
                <button type="button" class="primary" (click)="runNow()" [disabled]="!devices().length || !c.commands.length">Run Now</button>
              }
            </div>
            <div class="res">
              <table class="list">
                <thead><tr><th>Device</th><th>Device type</th>@for (n of colNames(c); track n) { <th>{{ n }}</th> }<th></th></tr></thead>
                <tbody>
                  @for (r of rows(); track r.path) {
                    <tr>
                      <td>{{ r.path }}</td>
                      <td>{{ r.deviceType || '-' }}</td>
                      @if (r.ok === false) {
                        <td [attr.colspan]="colNames(c).length || 1" class="bad" [title]="r.error">✕ {{ r.error }}</td>
                      } @else if (r.ok === null) {
                        <td [attr.colspan]="colNames(c).length || 1" class="hint">{{ inProgress().has(r.path) ? 'running…' : '-' }}</td>
                      } @else {
                        @for (n of colNames(c); track n) { <td [class.miss]="!r.values[n]">{{ r.values[n] || '-' }}</td> }
                        @if (!colNames(c).length) { <td class="hint">✓ collected</td> }
                      }
                      <td class="acts">
                        @if (r.file) { <button type="button" (click)="open(r.file)">Output</button> }
                        @if (r.log) { <button type="button" (click)="open(r.log)">Device Output</button> }
                      </td>
                    </tr>
                  } @empty { <tr><td [attr.colspan]="colNames(c).length + 3" class="hint">Add folders or sessions to the collection.</td></tr> }
                </tbody>
              </table>
            </div>
            @if (run(); as r) { <div class="sum">{{ summary() }}</div> }
            @if (err()) { <p class="err">{{ err() }}</p> }
          } @else {
            <div class="intro">
              <p><b>Run commands on many devices</b> in the background (no tabs, four at a time) and keep each device's output.</p>
              <p><b>Columns</b> pull values out of the output with a pattern - model, version, serial number, uptime, anything -
                into one table you can copy or export as CSV.</p>
              <p class="hint">Logins use saved passwords and the device types' logon steps - nothing is asked.</p>
              <button type="button" class="primary" (click)="addList()">+ New collection</button>
            </div>
          }
        </div>
      </div>
      <div class="modal-foot">
        <span class="hint dir" [title]="dir()">Saved in {{ dir() }}</span>
        <button type="button" (click)="reveal()">Open Folder</button>
        @if (cur()) { <button type="button" class="danger-text" (click)="removeList()">Delete Collection</button> }
        <span class="spacer"></span>
        <button type="button" (click)="close()">Close</button>
        @if (dirty()) { <button type="button" class="primary" (click)="save()">Save</button> }
      </div>
    </div>
  </div>`,
  styles: [`
    .co { width: min(1180px, 96vw); }
    .lists { display: flex; flex-direction: column; overflow: auto; a { display: flex; flex-direction: column; line-height: 1.3; padding-top: 6px; padding-bottom: 6px; }
      a span { font-size: calc(11px * var(--ui-scale, 1)); color: var(--muted); } .add { margin: 8px; } }
    .setup { border: 1px solid var(--border); border-radius: 6px; padding: 6px 10px; margin-bottom: 10px;
      summary { cursor: pointer; font-weight: 600; padding: 2px 0; } .grid { margin-top: 8px; } }
    .chips { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 6px; }
    .chip { display: inline-flex; gap: 4px; align-items: center; padding: 1px 4px 1px 8px; border: 1px solid var(--border); border-radius: 12px; font-size: calc(12px * var(--ui-scale, 1));
      button { border: 0; background: none; height: auto; padding: 0 3px; color: var(--muted); cursor: pointer; } }
    .addrow input { max-width: 320px; }
    .cmds { display: flex; flex-direction: column; gap: 8px; align-items: flex-start; .cg { width: 100%; } textarea { width: 100%; resize: vertical; margin-top: 4px; } }
    .cols { display: flex; flex-direction: column; gap: 6px; table { width: 100%; } td { padding: 2px 4px; } td input, td select { width: 100%; }
      th { font-size: calc(11.5px * var(--ui-scale, 1)); color: var(--muted); text-align: left; font-weight: 500; } input.bad { border-color: var(--danger); } }
    .row { display: flex; gap: 8px; align-items: center; } .spacer { flex: 1; }
    .runbar { display: flex; gap: 8px; align-items: center; margin: 4px 0 6px; select { max-width: 360px; } progress { width: 140px; accent-color: var(--accent); } }
    .sum { font-size: calc(12px * var(--ui-scale, 1)); color: var(--muted); margin-top: 6px; }
    .res { border: 1px solid var(--border); border-radius: 6px; max-height: 46vh; overflow: auto; }
    .res table { width: 100%; } .res th { position: sticky; top: 0; background: var(--panel); z-index: 1; white-space: nowrap; }
    .res td { vertical-align: middle; font-size: calc(12.5px * var(--ui-scale, 1)); max-width: 280px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    td.miss { color: var(--muted); }
    .acts { white-space: nowrap; text-align: right; button { height: 22px; font-size: calc(11.5px * var(--ui-scale, 1)); padding: 0 7px; margin-left: 4px; } }
    .bad { color: var(--danger); }
    .dir { max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .intro { max-width: 640px; line-height: 1.55; }
    .err { color: var(--danger); } .danger-text { color: var(--danger); }
  `],
})
export class Collect implements OnDestroy {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  readonly lists = signal<Collection[]>(structuredClone(this.ui.settings()?.collections ?? []));
  readonly sel = signal(this.lists()[0]?.name ?? '');
  readonly dirty = signal(false);
  readonly runs = signal<CheckRun[]>([]);
  readonly runId = signal('');
  readonly devices = signal<string[]>([]);
  readonly texts = signal<Map<string, string>>(new Map());
  readonly inProgress = signal<Set<string>>(new Set());
  readonly busy = signal(false);
  readonly doneN = signal(0);
  readonly total = signal(0);
  readonly dir = signal('');
  readonly err = signal('');
  /** bumped on edits of the columns so the table is extracted again */
  private readonly rev = signal(0);
  pick = '';
  private unsub: (() => void)[] = [];
  private lastTargets = '';
  private lastCols = '';

  readonly cur = computed(() => this.lists().find(c => c.name === this.sel()) ?? null);
  readonly run = computed(() => this.runs().find(r => r.id === this.runId()) ?? null);
  readonly deviceTypes = computed(() => (this.ui.settings()?.deviceTypes ?? []).map(d => d.name));
  readonly presetTypes = computed(() => this.deviceTypes().filter(d => PRESETS[d.toLowerCase()]));
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

  readonly rows = computed<Row[]>(() => {
    this.rev();
    const c = this.cur();
    const r = this.run();
    const texts = this.texts();
    const paths = [...new Set([...this.devices(), ...Object.keys(r?.devices ?? {})])].sort();
    return paths.map(p => {
      const res = r?.devices[p]?.after;
      const dt = this.ui.findNode(p)?.deviceType ?? '';
      const row: Row = { path: p, deviceType: dt, ok: res ? res.ok : null, error: res?.error ?? '', file: res?.file ?? '', log: res?.log ?? '', values: {} };
      const txt = res?.file ? texts.get(res.file) ?? '' : '';
      if (txt && c) {
        for (const k of c.columns) {
          if (!k.name || !k.pattern || row.values[k.name]) continue;
          if (k.deviceType && k.deviceType.toLowerCase() !== dt.toLowerCase()) continue;
          const m = this.match(k.pattern, txt);
          if (m) row.values[k.name] = m;
        }
      }
      return row;
    });
  });

  constructor() {
    const want = this.ui.collectTargets();
    this.ui.collectTargets.set(null);
    if (want?.length) {
      const key = JSON.stringify([...want].sort());
      const same = this.lists().find(c => JSON.stringify([...c.targets].sort()) === key);
      if (same) this.sel.set(same.name); else this.addList(want);
    }
    this.backend.collectDir().then(d => this.dir.set(d)).catch(() => {});
    void this.loadRuns();
    this.unsub.push(
      this.backend.on('collect:progress', (r: { list: string; run: string; path: string; done: number }) => {
        if (r.list !== this.sel()) return;
        this.doneN.set(r.done);
        this.inProgress.update(s => { const n = new Set(s); n.delete(r.path); return n; });
        void this.loadRuns(false);
      }),
      this.backend.on('collect:done', (d: { list: string; total: number; failed: number; cancelled: boolean }) => {
        if (d.list !== this.sel()) return;
        this.busy.set(false);
        this.inProgress.set(new Set());
        void this.loadRuns(false);
        this.ui.toast(d.cancelled ? 'Run stopped' : `Collected from ${d.total - d.failed} of ${d.total} devices` + (d.failed ? `, ${d.failed} failed` : ''),
          d.failed ? 'error' : 'success', 6000);
      }),
    );
    const t = setInterval(() => {
      const c = this.cur();
      const key = JSON.stringify(c?.targets ?? []);
      if (key !== this.lastTargets) { this.lastTargets = key; void this.loadDevices(); }
      const cols = JSON.stringify(c?.columns ?? []);
      if (cols !== this.lastCols) { this.lastCols = cols; this.rev.update(v => v + 1); }
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
    const runs = c ? await this.backend.collectRuns(c.name).catch(() => []) ?? [] : [];
    this.runs.set(runs);
    if (selectNewest || !runs.some(r => r.id === this.runId())) this.runId.set(runs[0]?.id ?? '');
    await this.loadTexts();
  }

  /** Reads the output files of the selected run that are not read yet. */
  private async loadTexts() {
    const r = this.run();
    if (!r) return;
    const have = this.texts();
    const files = Object.values(r.devices).map(d => d.after?.file).filter((f): f is string => !!f && !have.has(f));
    if (!files.length) return;
    const got = await Promise.all(files.map(f => this.backend.readTextFile(f).then(t => [f, t] as const).catch(() => [f, ''] as const)));
    this.texts.update(m => { const n = new Map(m); for (const [f, t] of got) n.set(f, t); return n; });
  }

  private reCache = new Map<string, RegExp | null>();
  private re(p: string): RegExp | null {
    if (!this.reCache.has(p)) {
      let r: RegExp | null = null;
      try { r = new RegExp(p, 'im'); } catch { r = null; }
      this.reCache.set(p, r);
    }
    return this.reCache.get(p)!;
  }
  valid(p: string) { return !p || !!this.re(p); }
  private match(p: string, txt: string) {
    const m = this.re(p)?.exec(txt);
    return m ? (m[1] ?? m[0]).trim() : '';
  }

  colNames(c: Collection) { return [...new Set(c.columns.map(k => k.name.trim()).filter(Boolean))]; }

  pickList(name: string) { this.sel.set(name); void this.loadRuns(); }
  pickRun(id: string) { this.runId.set(id); void this.loadTexts(); }

  runLabel(r: CheckRun) {
    const d = new Date(r.created).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
    return d + (r.note ? ' - ' + r.note : '') + ' (' + ago(r.created) + ')';
  }

  summary() {
    const rows = this.rows();
    const ok = rows.filter(r => r.ok === true).length, bad = rows.filter(r => r.ok === false).length;
    return `${ok} collected` + (bad ? ` · ${bad} failed` : '') + ' · Output files are in the collection folder, one per device.';
  }

  addList(targets?: string[]) {
    const c: Collection = { name: '', targets: targets ?? this.treeSelection(), commands: [], columns: [] };
    this.applyPresets(c);
    const base = c.columns.length ? 'Inventory' : 'Collection';
    let name = base, i = 1;
    while (this.lists().some(x => x.name === name)) name = base + ' ' + ++i;
    c.name = name;
    if (!c.commands.length) c.commands = [{ deviceType: '', commands: 'show version' }];
    this.lists.update(l => [...l, c]);
    this.sel.set(name);
    this.runs.set([]);
    this.runId.set('');
    this.dirty.set(true);
  }

  private applyPresets(c: Collection) {
    for (const d of this.presetTypes()) {
      const p = PRESETS[d.toLowerCase()];
      if (!c.commands.some(g => g.deviceType === d)) c.commands = [...c.commands, { deviceType: d, commands: p.commands }];
      for (const k of p.columns) {
        if (!c.columns.some(x => x.name === k.name && x.deviceType === d)) c.columns = [...c.columns, { name: k.name, pattern: k.pattern, deviceType: d }];
      }
    }
  }

  addPresets(c: Collection) { this.applyPresets(c); this.lists.update(l => [...l]); this.dirty.set(true); }

  rename(c: Collection, name: string) {
    if (!name.trim() || this.lists().some(x => x !== c && x.name === name)) return;
    c.name = name;
    this.sel.set(name);
    this.lists.update(l => [...l]);
    this.dirty.set(true);
  }

  addTarget(c: Collection) {
    const v = this.pick.trim();
    if (!v || c.targets.includes(v)) return;
    c.targets = [...c.targets, v];
    this.pick = '';
    this.lists.update(l => [...l]);
    this.dirty.set(true);
  }

  addSelected(c: Collection) {
    c.targets = [...new Set([...c.targets, ...this.treeSelection()])];
    this.lists.update(l => [...l]);
    this.dirty.set(true);
  }

  removeTarget(c: Collection, t: string) {
    c.targets = c.targets.filter(x => x !== t);
    this.lists.update(l => [...l]);
    this.dirty.set(true);
  }

  addGroup(c: Collection) { c.commands = [...c.commands, { deviceType: '', commands: '' }]; this.lists.update(l => [...l]); this.dirty.set(true); }
  removeGroup(c: Collection, i: number) { c.commands = c.commands.filter((_, k) => k !== i); this.lists.update(l => [...l]); this.dirty.set(true); }
  addColumn(c: Collection) { c.columns = [...c.columns, { name: '', pattern: '', deviceType: '' } as CollectColumn]; this.lists.update(l => [...l]); this.dirty.set(true); }
  removeColumn(c: Collection, i: number) { c.columns = c.columns.filter((_, k) => k !== i); this.lists.update(l => [...l]); this.dirty.set(true); }

  async removeList() {
    const c = this.cur();
    if (!c || !(await this.ui.confirm('Delete Collection', `Delete the collection "${c.name}"? Collected outputs stay in the folder.`, 'Delete', true))) return;
    this.lists.update(l => l.filter(x => x !== c));
    this.sel.set(this.lists()[0]?.name ?? '');
    await this.save();
    await this.loadRuns();
  }

  async save() {
    const s = this.ui.settings();
    if (!s) return;
    const clean = this.lists().filter(c => c.name.trim()).map(c => ({ ...c, columns: c.columns.filter(k => k.name.trim() || k.pattern.trim()) }));
    await this.ui.saveSettings({ ...s, collections: clean });
    this.dirty.set(false);
  }

  async runNow() {
    const c = this.cur();
    if (!c) return;
    if (this.dirty()) await this.save();
    this.err.set('');
    try {
      const id = await this.backend.collectRun(c.name, '');
      await this.loadRuns(false);
      this.runId.set(id);
      this.busy.set(true);
      this.doneN.set(0);
      this.total.set(this.devices().length);
      this.inProgress.set(new Set(this.devices()));
    } catch (e) { this.err.set(errText(e)); }
  }

  cancel() { const c = this.cur(); if (c) void this.backend.collectCancel(c.name, this.runId()); }

  private table(sep: string) {
    const c = this.cur();
    if (!c) return '';
    const names = this.colNames(c);
    const cell = (v: string) => sep === ',' ? (/[",\n]/.test(v) ? '"' + v.replace(/"/g, '""') + '"' : v) : v.replace(/[\t\n]/g, ' ');
    const lines = [['Device', 'Device type', ...names, 'Error'].map(cell).join(sep)];
    for (const r of this.rows()) lines.push([r.path, r.deviceType, ...names.map(n => r.values[n] ?? ''), r.ok === false ? r.error : ''].map(cell).join(sep));
    return lines.join('\r\n') + '\r\n';
  }

  async copy() {
    try { await navigator.clipboard.writeText(this.table('\t')); this.ui.toast('Table copied - paste it into a spreadsheet', 'success'); }
    catch (e) { this.ui.error(e); }
  }

  async exportCsv() {
    const c = this.cur();
    if (!c) return;
    const d = new Date().toISOString().slice(0, 10);
    try {
      const p = await this.backend.saveTextAs('Export CSV', `${c.name} ${d}.csv`.replace(/[\\/:*?"<>|]/g, '-'), this.table(','));
      if (p) this.ui.toast('Saved ' + p, 'success');
    } catch (e) { this.ui.error(e); }
  }

  async open(f: string) { await this.backend.openPath(f).catch(e => this.ui.error(e)); }
  async reveal() { await this.backend.showCollectFolder().catch(e => this.ui.error(e)); }

  async close() {
    if (this.dirty() && await this.ui.confirm('Run and Collect', 'Save the changes to the collections?', 'Save')) await this.save();
    this.ui.close();
  }
}
