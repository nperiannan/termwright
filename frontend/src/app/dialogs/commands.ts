import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Snippet, fillSnippet, snippetTypes, snippetVars } from '../core/models';
import { SendTarget, Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';

/** Asks for snippet variables one by one, then sends the snippet. */
export async function sendSnippet(ui: Ui, tabs: Tabs, s: Snippet, target: SendTarget = 'active') {
  const values: Record<string, string> = {};
  for (const v of snippetVars(s.text)) {
    const val = await ui.input(s.name, v.name, v.def);
    if (val === null) return;
    values[v.name] = val;
  }
  const text = fillSnippet(s.text, values).replace(/\r?\n$/, '');
  if (!tabs.targets(target).length) { ui.toast('No connected tab to send to.', 'error'); return; }
  await tabs.send(text.split(/\r?\n/).map(l => l + '\r').join(''), target, true);
}

/** Command Library: saved commands in folders, with {{variables}}, sent to one, selected or all tabs. */
@Component({
  selector: 'app-commands',
  imports: [FormsModule],
  template: `
  <div class="modal-backdrop">
    <div class="modal wide">
      <div class="modal-head">Command Library</div>
      <div class="modal-split">
        <nav class="cat lib">
          <input class="filter" [ngModel]="filter()" (ngModelChange)="filter.set($event)" placeholder="Filter commands" />
          @if (dt()) {
            <label class="only"><input type="checkbox" [ngModel]="onlyDt()" (ngModelChange)="onlyDt.set($event)" /> Only commands for {{ dt() }}</label>
          }
          @for (g of groups(); track g.folder) {
            <div class="folder" [class.mine]="g.mine">{{ g.folder || 'General' }}</div>
            @for (c of g.items; track c.i) {
              <a [class.on]="sel() === c.i" (click)="select(c.i)" (dblclick)="send()" [title]="c.s.text">{{ c.s.name }}</a>
            }
          }
          <button type="button" class="add" (click)="add()">+ New command</button>
        </nav>
        <div class="modal-body">
          @if (cur(); as c) {
            <div class="grid">
              <label>Name</label><input [(ngModel)]="c.name" (ngModelChange)="dirty = true" />
              <label>Folder</label><input [(ngModel)]="c.folder" (ngModelChange)="dirty = true" placeholder="e.g. VOSS / Troubleshooting" />
              <label>Description</label><input [(ngModel)]="c.description" (ngModelChange)="dirty = true" />
              <label>For device types</label>
              <div class="chips">
                @for (t of typeNames(); track t) {
                  <label class="chip" [class.on]="(c.deviceTypes ?? []).includes(t)"><input type="checkbox" [checked]="(c.deviceTypes ?? []).includes(t)" (change)="toggleType(c, t)" /> {{ t }}</label>
                }
                <span class="hint">{{ (c.deviceTypes ?? []).length ? 'Suggested first in tabs of these device types.' : 'None ticked: for any device' + (folderType(c) ? ' (the folder name says ' + folderType(c) + ')' : '') + '.' }}</span>
              </div>
              <label>Commands</label>
              <textarea class="mono" rows="8" [(ngModel)]="c.text" (ngModelChange)="dirty = true"
                [placeholder]="ph"></textarea>
              @if (varList(c); as v) {
                <label>Asks for</label><div class="hint">{{ v }}</div>
              }
              <label>Send to</label>
              <div class="checks row">
                <label><input type="radio" name="tgt" value="active" [(ngModel)]="target" /> Active tab</label>
                <label><input type="radio" name="tgt" value="selected" [(ngModel)]="target" /> Selected tabs ({{ tabs.targets('selected').length }})</label>
                <label><input type="radio" name="tgt" value="all" [(ngModel)]="target" /> All connected ({{ tabs.connectedCount() }})</label>
              </div>
            </div>
          } @else {
            <p class="hint">Save commands you use often - per device type, troubleshooting steps, config snippets. Double-click a command
              to send it. Also available from Quick Open ({{ mod }}G).</p>
          }
        </div>
      </div>
      <div class="modal-foot">
        @if (cur()) { <button type="button" class="danger-text" (click)="remove()">Delete</button> }
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Close</button>
        @if (cur()) {
          <button type="button" (click)="save()" [disabled]="!dirty">Save</button>
          <button type="button" class="primary" (click)="send()">Send</button>
        }
      </div>
    </div>
  </div>`,
  styles: [`
    .lib { display: flex; flex-direction: column; overflow: auto; }
    .lib .filter { margin: 6px 8px; }
    .lib .folder { font-size: calc(11px * var(--ui-scale, 1)); text-transform: uppercase; color: var(--muted); padding: 8px 12px 2px; }
    .lib a { padding-left: 20px; }
    .lib .only { display: flex; gap: 6px; align-items: center; font-size: calc(12px * var(--ui-scale, 1)); padding: 0 12px 4px; color: var(--muted); }
    .lib .folder.mine { color: var(--accent); }
    .chips { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
    .chip { display: inline-flex; gap: 5px; align-items: center; padding: 1px 8px; border: 1px solid var(--border); border-radius: 12px; cursor: pointer; font-size: calc(12px * var(--ui-scale, 1)); }
    .chip.on { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 15%, transparent); }
    .lib .add { margin: 8px; }
    textarea { width: 100%; resize: vertical; }
    .row label { margin-right: 12px; }
    .danger-text { color: var(--danger); }
  `],
})
export class Commands {
  protected ui = inject(Ui);
  protected tabs = inject(Tabs);
  readonly mod = navigator.platform.toLowerCase().includes('mac') ? '⌘' : 'Ctrl+Shift+';
  readonly list = signal<Snippet[]>(structuredClone(this.ui.settings()?.snippets ?? []));
  readonly sel = signal(-1);
  readonly filter = signal('');
  readonly ph = 'One command per line. Use {{name}} or {{name:default}} for values asked when sending.';
  target: 'active' | 'selected' | 'all' = 'active';
  dirty = false;

  readonly cur = computed(() => this.list()[this.sel()] ?? null);
  /** Device type of the active tab: its commands are listed first. */
  readonly dt = this.tabs.activeDeviceType;
  readonly typeNames = computed(() => (this.ui.settings()?.deviceTypes ?? []).map(d => d.name));
  readonly onlyDt = signal(true);
  readonly groups = computed(() => {
    const f = this.filter().toLowerCase();
    const dt = this.dt().toLowerCase();
    const known = this.typeNames();
    const mine: { i: number; s: Snippet }[] = [];
    const map = new Map<string, { i: number; s: Snippet }[]>();
    this.list().forEach((s, i) => {
      if (f && !(s.name + ' ' + s.folder + ' ' + s.text).toLowerCase().includes(f)) return;
      const types = snippetTypes(s, known).map(t => t.toLowerCase());
      if (dt && types.includes(dt)) { mine.push({ i, s }); return; }
      if (dt && this.onlyDt() && types.length) return; // meant for another device type
      const k = s.folder.trim();
      if (!map.has(k)) map.set(k, []);
      map.get(k)!.push({ i, s });
    });
    const rest = [...map].sort((a, b) => a[0].localeCompare(b[0])).map(([folder, items]) => ({ folder, items, mine: false }));
    return mine.length ? [{ folder: 'For ' + this.dt() + ' (this tab)', items: mine, mine: true }, ...rest] : rest;
  });

  folderType(c: Snippet) { return (c.deviceTypes ?? []).length ? '' : snippetTypes(c, this.typeNames())[0] ?? ''; }

  toggleType(c: Snippet, t: string) {
    const l = c.deviceTypes ?? [];
    c.deviceTypes = l.includes(t) ? l.filter(x => x !== t) : [...l, t];
    this.dirty = true;
    this.list.update(x => [...x]);
  }

  varList(c: Snippet) { return snippetVars(c.text).map(v => v.name + (v.def ? ` (default ${v.def})` : '')).join(', '); }

  select(i: number) { this.sel.set(i); }

  add() {
    this.list.update(l => [...l, { name: 'New command', folder: this.cur()?.folder ?? '', text: '', description: '', deviceTypes: this.dt() ? [this.dt()] : [] }]);
    this.select(this.list().length - 1);
    this.dirty = true;
  }

  async save() {
    const s = this.ui.settings();
    if (!s) return;
    await this.ui.saveSettings({ ...s, snippets: this.list().filter(c => c.name.trim()) });
    this.dirty = false;
    this.ui.toast('Command library saved', 'success');
  }

  async remove() {
    const c = this.cur();
    if (!c || !(await this.ui.confirm('Delete Command', `Delete "${c.name}"?`, 'Delete', true))) return;
    this.list.update(l => l.filter((_, i) => i !== this.sel()));
    this.sel.set(-1);
    await this.save();
  }

  async send() {
    const c = this.cur();
    if (!c) return;
    if (this.dirty) await this.save();
    this.ui.close();
    await sendSnippet(this.ui, this.tabs, c, this.target);
  }
}
