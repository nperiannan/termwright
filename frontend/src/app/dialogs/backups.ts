import { Component, OnDestroy, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { ago } from '../core/env';
import { BackupJob, BackupResult, BackupStatus, SessionNode } from '../core/models';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';

const NOW = '\u0000now';

/** Configuration Backups: jobs (sessions and folders, on demand or scheduled), results and what changed. */
@Component({
  selector: 'app-backups',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop">
    <div class="modal huge bk">
      <div class="modal-head"><span class="mh-ic"><app-icon name="backup" [size]="17" /></span>
        <span class="mh-t">Configuration Backups<span class="mh-s">Saved copies of each device's configuration, on a schedule or on demand</span></span></div>
      <div class="modal-split">
        <nav class="cat jobs">
          @if (adhoc().length) { <a [class.on]="sel() === NOW" (click)="sel.set(NOW)">Back up now<span>{{ adhoc().length }} selected</span></a> }
          @for (j of jobs(); track $index) {
            <a [class.on]="sel() === j.name" (click)="sel.set(j.name)">{{ j.name || '(no name)' }}<span>{{ scheduleText(j) }}</span></a>
          }
          <button type="button" class="add" (click)="addJob()">+ New backup job</button>
        </nav>
        <div class="modal-body">
          @if (job(); as j) {
            @if (sel() !== NOW) {
              <div class="grid">
                <label>Name</label><input [ngModel]="j.name" (ngModelChange)="rename(j, $event)" placeholder="e.g. Core switches nightly" />
                <label>Sessions</label>
                <div>
                  <div class="chips">
                    @for (t of j.targets; track t) {
                      <span class="chip">{{ t.endsWith('/') ? '📁 ' + t.slice(0, -1) : t }}<button type="button" (click)="removeTarget(j, t)" title="Remove">✕</button></span>
                    } @empty { <span class="hint">No sessions yet.</span> }
                  </div>
                  <div class="row addrow">
                    <input [(ngModel)]="pick" list="bk-targets" placeholder="Folder or session" (keydown.enter)="addTarget(j)" />
                    <datalist id="bk-targets">@for (o of choices(); track o) { <option [value]="o"></option> }</datalist>
                    <button type="button" (click)="addTarget(j)" [disabled]="!pick.trim()">Add</button>
                    <button type="button" (click)="addSelected(j)" [disabled]="!treeSelection().length"
                      title="The sessions or folder selected in the Session Manager">Add Selected ({{ treeSelection().length }})</button>
                  </div>
                </div>
                <label>Schedule</label>
                <div class="row">
                  <select [(ngModel)]="j.schedule" (ngModelChange)="dirty.set(true)">
                    <option value="">On demand (Run Now)</option>
                    <option value="daily">Every day</option>
                    <option value="weekdays">Monday to Friday</option>
                  </select>
                  @if (j.schedule) { <span>at</span><input type="time" class="time" [(ngModel)]="j.at" (ngModelChange)="dirty.set(true)" /> }
                  @if (j.schedule) { <span class="hint">while Termwright is open; missed runs start when it opens</span> }
                </div>
                <label>Keep</label>
                <div class="row">
                  <input type="number" class="num" min="1" max="1000" [(ngModel)]="j.keep" (ngModelChange)="dirty.set(true)" /><span>copies per device</span>
                  <label class="inline"><input type="checkbox" [(ngModel)]="j.saveAll" (ngModelChange)="dirty.set(true)" /> Save a copy every run (default: only when the configuration changed)</label>
                </div>
                <label>Commands</label>
                <div>
                  <textarea class="mono" rows="2" [(ngModel)]="j.commands" (ngModelChange)="dirty.set(true)"
                    placeholder="For sessions without a device type, e.g. show running-config"></textarea>
                  <div class="hint">Sessions with a device type use its backup commands (Global Options → Device Types): EXOS <code>show configuration</code>,
                    VOSS and Cisco IOS <code>show running-config</code>. Logins use saved passwords only - nothing is asked during a backup.</div>
                </div>
              </div>
            }
            <div class="devs">
              <div class="devhead">
                <b>{{ devices().length }} device{{ devices().length === 1 ? '' : 's' }}</b>
                @if (lastRun()) { <span class="hint">last run {{ lastRun() }}</span> }
                <span class="spacer"></span>
                @if (running()) { <progress [max]="total()" [value]="doneN()"></progress><span class="hint">{{ doneN() }} / {{ total() }}</span> }
              </div>
              <table class="list">
                <thead><tr><th>Session</th><th>Device type</th><th>Last backup</th><th>Result</th><th class="r">Copies</th><th></th></tr></thead>
                <tbody>
                  @for (p of devices(); track p) {
                    <tr>
                      <td>{{ p }}</td>
                      <td>{{ node(p)?.deviceType || '-' }}</td>
                      <td class="nw" [title]="status()[p]?.time ? full(status()[p]!.time) : ''">{{ status()[p]?.time ? ago(status()[p]!.time) : 'never' }}</td>
                      <td>
                        @if (busy().has(p)) { <span class="hint">backing up…</span> }
                        @else if (status()[p]; as st) {
                          @if (!st.ok) { <span class="bad">✕ {{ st.error }}</span>
                            @if (st.log) { <a class="log" (click)="openLog(st.log)" title="What the device sent before the backup failed">device output</a> } }
                          @else if (st.changed) { <span class="chg">● changed</span> } @else { <span class="ok">✓ no change</span> }
                        }
                      </td>
                      <td class="r">{{ status()[p]?.copies ?? 0 }}</td>
                      <td class="acts">
                        <button type="button" [disabled]="(status()[p]?.copies ?? 0) < 2" (click)="diff(p)" title="Compare the newest copy with the one before">What Changed</button>
                        <button type="button" [disabled]="!status()[p]?.file" (click)="open(p)">Open</button>
                        <button type="button" [disabled]="!status()[p]?.file" (click)="reveal(p)" title="Show in the file manager">Show</button>
                      </td>
                    </tr>
                  } @empty { <tr><td colspan="6" class="hint">Add folders or sessions above.</td></tr> }
                </tbody>
              </table>
            </div>
            @if (err()) { <p class="err">{{ err() }}</p> }
          } @else {
            <p class="hint">Back up the configuration of switches and routers: pick sessions or folders, run now or every day.
              Each device gets a folder of dated copies; <b>What Changed</b> compares the last two.</p>
          }
        </div>
      </div>
      <div class="modal-foot">
        <span class="hint dir" [title]="dir()">Saved in {{ dir() }}</span>
        <button type="button" (click)="chooseDir()">Change…</button>
        <button type="button" (click)="reveal('')">Open Folder</button>
        @if (job() && sel() !== NOW) { <button type="button" class="danger-text" (click)="removeJob()">Delete Job</button> }
        <span class="spacer"></span>
        <button type="button" (click)="close()">Close</button>
        @if (dirty()) { <button type="button" (click)="save()">Save</button> }
        @if (running()) { <button type="button" (click)="cancel()">Stop</button> }
        @else { <button type="button" class="primary" [disabled]="!devices().length" (click)="run()">Run Now</button> }
      </div>
    </div>
  </div>`,
  styles: [`
    .bk { width: min(1100px, 96vw); }
    .jobs { display: flex; flex-direction: column; overflow: auto; a { display: flex; flex-direction: column; line-height: 1.3; padding-top: 6px; padding-bottom: 6px; }
      a span { font-size: calc(11px * var(--ui-scale, 1)); color: var(--muted); } .add { margin: 8px; } }
    .chips { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 6px; }
    .chip { display: inline-flex; gap: 4px; align-items: center; padding: 1px 4px 1px 8px; border: 1px solid var(--border); border-radius: 12px; font-size: calc(12px * var(--ui-scale, 1));
      button { border: 0; background: none; height: auto; padding: 0 3px; color: var(--muted); cursor: pointer; } }
    .addrow input { max-width: 320px; }
    .time { width: 110px; } .num { width: 80px; }
    .inline { display: inline-flex; gap: 6px; align-items: center; margin-left: 12px; }
    textarea { width: 100%; resize: vertical; }
    .devs { margin-top: 14px; border: 1px solid var(--border); border-radius: 6px; max-height: 42vh; overflow: auto; }
    .devhead { display: flex; gap: 10px; align-items: center; padding: 6px 10px; position: sticky; top: 0; background: var(--panel); z-index: 2; border-bottom: 1px solid var(--border);
      .spacer { flex: 1; } progress { width: 160px; accent-color: var(--accent); } }
    table { width: 100%; } th { position: sticky; top: 35px; background: var(--panel); z-index: 1; }
    td { vertical-align: middle; } .r { text-align: right; } .nw { white-space: nowrap; }
    .acts { white-space: nowrap; text-align: right; button { height: 22px; font-size: calc(11.5px * var(--ui-scale, 1)); padding: 0 7px; margin-left: 4px; } }
    .bad { color: var(--danger); display: inline-block; max-width: 380px; white-space: normal; word-break: break-word; font-size: calc(12px * var(--ui-scale, 1)); }
    .log { display: block; font-size: calc(11.5px * var(--ui-scale, 1)); color: var(--accent); cursor: pointer; }
    .chg { color: #f59e0b; } .ok { color: var(--ok); }
    .dir { max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .err { color: var(--danger); }
    .danger-text { color: var(--danger); }
  `],
})
export class Backups implements OnDestroy {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  readonly NOW = NOW;
  readonly jobs = signal<BackupJob[]>(structuredClone(this.ui.settings()?.backupJobs ?? []));
  readonly adhoc = signal<string[]>(this.ui.backupTargets() ?? []);
  readonly sel = signal<string>(this.adhoc().length ? NOW : this.jobs()[0]?.name ?? '');
  readonly dirty = signal(false);
  readonly status = signal<Record<string, BackupStatus>>({});
  readonly jobRuns = signal<Record<string, string>>({});
  readonly devices = signal<string[]>([]);
  readonly busy = signal<Set<string>>(new Set());
  readonly runId = signal('');
  readonly running = computed(() => !!this.runId());
  readonly doneN = signal(0);
  readonly total = signal(0);
  readonly dir = signal('');
  readonly err = signal('');
  pick = '';
  private unsub: (() => void)[] = [];

  readonly job = computed<BackupJob | null>(() => {
    const s = this.sel();
    if (s === NOW) return { name: '', targets: this.adhoc(), schedule: '', at: '', keep: 30, saveAll: false, commands: '' };
    return this.jobs().find(j => j.name === s) ?? null;
  });
  readonly lastRun = computed(() => { const j = this.job(); return j?.name && this.jobRuns()[j.name] ? this.when(this.jobRuns()[j.name]) : ''; });
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
    this.ui.backupTargets.set(null);
    void this.refresh();
    this.backend.backupDir().then(d => this.dir.set(d)).catch(() => {});
    this.unsub.push(
      this.backend.on('backup:progress', (r: BackupResult) => {
        if (r.runId !== this.runId()) return;
        this.doneN.set(r.done);
        this.busy.update(s => { const n = new Set(s); n.delete(r.path); return n; });
        this.status.update(m => ({ ...m, [r.path]: { time: new Date().toISOString(), ok: r.ok, error: r.error, changed: r.changed, file: r.file, copies: m[r.path]?.copies ?? 0 } }));
      }),
      this.backend.on('backup:done', (d: { runId: string; failed: number; changed: number; total: number; cancelled: boolean }) => {
        if (d.runId !== this.runId()) return;
        this.runId.set('');
        this.busy.set(new Set());
        void this.refresh();
        this.ui.toast(d.cancelled ? 'Backup stopped' : `Backup done: ${d.total - d.failed} of ${d.total} saved, ${d.changed} changed` + (d.failed ? `, ${d.failed} failed` : ''),
          d.failed ? 'error' : 'success', 6000);
      }),
    );
    // keep the device list in step with the selected job
    let last = '';
    const tick = () => {
      const key = JSON.stringify(this.job()?.targets ?? []);
      if (key !== last) { last = key; void this.loadDevices(); }
    };
    const t = setInterval(tick, 300);
    this.unsub.push(() => clearInterval(t));
    tick();
    if (this.sel() === NOW) setTimeout(() => void this.run(), 200);
  }

  ngOnDestroy() { this.unsub.forEach(u => u()); }

  private async refresh() {
    this.status.set(await this.backend.backupStatus().catch(() => ({})) ?? {});
    this.jobRuns.set(await this.backend.backupJobRuns().catch(() => ({})) ?? {});
  }

  private async loadDevices() {
    const j = this.job();
    this.devices.set(j?.targets.length ? await this.backend.backupSessions(j.targets).catch(() => []) : []);
  }

  node(p: string) { return this.ui.findNode(p); }
  ago(t: string) { return ago(t); }
  full(t: string) { return new Date(t).toLocaleString(); }
  when(t: string) { return ago(t) + ' (' + new Date(t).toLocaleString([], { dateStyle: 'short', timeStyle: 'short' }) + ')'; }
  scheduleText(j: BackupJob) {
    const n = j.targets.length;
    const what = n + (n === 1 ? ' item' : ' items');
    return (j.schedule === 'daily' ? 'every day ' + j.at : j.schedule === 'weekdays' ? 'weekdays ' + j.at : 'on demand') + ' · ' + what;
  }

  addJob() {
    let name = 'Backup', i = 1;
    while (this.jobs().some(j => j.name === name)) name = 'Backup ' + ++i;
    const sel = this.treeSelection();
    this.jobs.update(l => [...l, { name, targets: sel, schedule: '', at: '02:00', keep: 30, saveAll: false, commands: '' }]);
    this.sel.set(name);
    this.dirty.set(true);
  }

  rename(j: BackupJob, name: string) {
    if (!name.trim() || this.jobs().some(x => x !== j && x.name === name)) return;
    j.name = name;
    this.sel.set(name);
    this.jobs.update(l => [...l]);
    this.dirty.set(true);
  }

  addTarget(j: BackupJob) {
    const v = this.pick.trim();
    if (!v || j.targets.includes(v)) return;
    j.targets = [...j.targets, v];
    this.pick = '';
    this.jobs.update(l => [...l]);
    this.dirty.set(true);
  }

  addSelected(j: BackupJob) {
    j.targets = [...new Set([...j.targets, ...this.treeSelection()])];
    this.jobs.update(l => [...l]);
    this.dirty.set(true);
  }

  removeTarget(j: BackupJob, t: string) {
    j.targets = j.targets.filter(x => x !== t);
    this.jobs.update(l => [...l]);
    this.dirty.set(true);
  }

  async removeJob() {
    const j = this.job();
    if (!j || !(await this.ui.confirm('Delete Backup Job', `Delete the job "${j.name}"? Saved copies stay in the backup folder.`, 'Delete', true))) return;
    this.jobs.update(l => l.filter(x => x !== j));
    this.sel.set(this.jobs()[0]?.name ?? '');
    await this.save();
  }

  async save() {
    const s = this.ui.settings();
    if (!s) return;
    await this.ui.saveSettings({ ...s, backupJobs: this.jobs().filter(j => j.name.trim()) });
    this.dirty.set(false);
  }

  async run() {
    const j = this.job();
    if (!j) return;
    if (this.dirty()) await this.save();
    this.err.set('');
    try {
      const id = await this.backend.backupRun(this.sel() === NOW ? '' : j.name, j.targets);
      this.runId.set(id);
      this.doneN.set(0);
      this.total.set(this.devices().length);
      this.busy.set(new Set(this.devices()));
    } catch (e) { this.err.set(errText(e)); }
  }

  cancel() { if (this.runId()) void this.backend.backupCancel(this.runId()); }

  async diff(p: string) {
    const files = await this.backend.backupFiles(p).catch(() => []);
    if (files.length < 2) return;
    this.ui.compareWith.set(['path:' + files[1].path, 'path:' + files[0].path]);
    this.ui.open('compare');
  }

  async openLog(f: string) { await this.backend.openPath(f).catch(e => this.ui.error(e)); }

  async open(p: string) {
    const f = this.status()[p]?.file;
    if (f) await this.backend.openPath(f).catch(e => this.ui.error(e));
  }

  async reveal(p: string) {
    await this.backend.showInFolder(p ? this.status()[p]?.file ?? '' : '').catch(e => this.ui.error(e));
  }

  async chooseDir() {
    const d = await this.backend.chooseDirectory('Folder for Configuration Backups').catch(() => '');
    if (!d) return;
    const s = this.ui.settings();
    if (s) await this.ui.saveSettings({ ...s, backupDir: d });
    this.dir.set(d);
    await this.refresh();
  }

  async close() {
    if (this.dirty() && await this.ui.confirm('Configuration Backups', 'Save the changes to the backup jobs?', 'Save')) await this.save();
    this.ui.close();
  }
}
