import { ChangeDetectionStrategy, Component, ElementRef, OnDestroy, computed, effect, inject, signal, untracked, viewChild } from '@angular/core';
import { History } from '../core/history.service';
import { FormsModule } from '@angular/forms';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { Snippet, fillSnippet, snippetTypes, snippetVars } from '../core/models';

/** Command window: compose a command (or several lines) and send it to the
 *  active tab or broadcast it to every connected tab. */
@Component({
  selector: 'app-command-window',
  imports: [FormsModule],
  template: `
    <div class="cw-bar">
      <span class="title">Command Window</span>
      <label><input type="radio" name="cwTarget" value="active" [(ngModel)]="target" /> Active tab</label>
      <label><input type="radio" name="cwTarget" value="all" [(ngModel)]="target" /> All connected tabs ({{ tabs.connectedCount() }})</label>
      <label><input type="radio" name="cwTarget" value="selected" [(ngModel)]="target" /> Selected tabs ({{ tabs.targets('selected').length }})</label>
      <button class="pick" title="Choose the tabs to send to" (click)="pick($event)">Choose…</button>
      <label title="Send characters immediately as you type"><input type="checkbox" [(ngModel)]="charMode" /> Send characters immediately</label>
      <span class="spacer"></span>
      <button class="pick" (click)="ui.open('history')" title="Search the commands you typed (Mod+Y)">History…</button>
      <button class="pick" (click)="library($event)" [title]="tabs.activeDeviceType() ? 'Commands from the library, those for ' + tabs.activeDeviceType() + ' first' : 'Commands from the library'">
        Commands{{ tabs.activeDeviceType() ? ' (' + tabs.activeDeviceType() + ')' : '' }} ▾</button>
      <button (click)="send()" [disabled]="!text().length && !charMode">Send ⏎</button>
      <button class="icon-btn" title="Close" (click)="ui.patchSettings({ showCommandWindow: false })">✕</button>
    </div>
    <div class="boxwrap">
      @if (sugg().length) {
        <div class="sugg" role="listbox">
          @for (s of sugg(); track $index) {
            <div class="si" [class.on]="$index === sel()" (mousedown)="accept(s); $event.preventDefault()">
              <span class="c">{{ s.text }}</span><span class="h">{{ s.hint }}</span>
            </div>
          }
          <div class="sh">Tab or Enter takes it · ↑/↓ choose · Esc closes</div>
        </div>
      }
      <textarea #box spellcheck="false" placeholder="Type a command. Enter sends, Shift+Enter adds a line, ↑/↓ recall history; suggestions come from history and the Command Library."
        [ngModel]="text()" (ngModelChange)="onChange($event)" (keydown)="onKey($event)" (blur)="closeSugg()"></textarea>
    </div>
  `,
  // Typing only re-renders this component; the suggestion list is worked out
  // once the keys pause (holding Backspace does not rebuild it on every key).
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [`
    :host { display: flex; flex-direction: column; height: 100%; background: var(--panel); border-top: 1px solid var(--border); contain: layout style; }
    .cw-bar { display: flex; align-items: center; gap: 14px; padding: 3px 8px; font-size: calc(12px * var(--ui-scale, 1)); color: var(--muted);
      .title { font-weight: 600; text-transform: uppercase; letter-spacing: .04em; }
      label { display: flex; align-items: center; gap: 4px; cursor: pointer; }
      .pick { height: 20px; font-size: calc(11px * var(--ui-scale, 1)); padding: 0 8px; }
      .spacer { flex: 1; } }
    .boxwrap { flex: 1; display: flex; position: relative; min-height: 0; }
    .sugg { position: absolute; left: 8px; bottom: calc(100% - 4px); z-index: 20; min-width: 360px; max-width: 80%; background: var(--panel);
      border: 1px solid var(--border); border-radius: 6px; box-shadow: var(--shadow); padding: 4px 0; font-size: calc(12.5px * var(--ui-scale, 1));
      .si { display: flex; gap: 16px; padding: 3px 10px; cursor: pointer; .c { font-family: var(--mono); white-space: pre; overflow: hidden; text-overflow: ellipsis; }
        .h { margin-left: auto; color: var(--muted); font-size: calc(11px * var(--ui-scale, 1)); white-space: nowrap; } }
      .si.on, .si:hover { background: var(--selected); }
      .sh { color: var(--muted); font-size: calc(10.5px * var(--ui-scale, 1)); padding: 3px 10px 1px; border-top: 1px solid var(--border); margin-top: 3px; } }
    textarea { flex: 1; resize: none; border: 0; border-top: 1px solid var(--border); border-radius: 0; padding: 6px 10px;
      font-family: var(--mono); font-size: calc(13px * var(--ui-scale, 1)); background: var(--input-bg); color: var(--text); outline: none; }
  `],
})
export class CommandWindow implements OnDestroy {
  protected tabs = inject(Tabs);
  protected ui = inject(Ui);
  private box = viewChild.required<ElementRef<HTMLTextAreaElement>>('box');
  readonly text = signal('');
  target: 'active' | 'all' | 'selected' = 'active';
  charMode = false;
  private history: string[] = [];
  private hIdx = -1;
  private hist = inject(History);
  /** Suggestions are shown after typing (not after recall or accepting one). */
  private readonly typed = signal(false);
  readonly sel = signal(-1);
  /** The text the suggestions are for: follows the box 120 ms after the last key. */
  private readonly query = signal('');
  private qTimer?: ReturnType<typeof setTimeout>;
  /** While keys are pressed the last list stays (no rebuilding on every key). */
  readonly sugg = computed(() => this.text().trim().length < 2 ? [] : this.listFor());
  private readonly listFor = computed(() => {
    const t = this.query();
    if (!this.typed() || this.charMode || t.includes('\n') || t.trim().length < 2) return [];
    const q = t.trim().toLowerCase();
    const dt = this.tabs.activeDeviceType();
    const out: { text: string; hint: string; snippet?: Snippet }[] = [];
    const seen = new Set<string>([t.trim()]);
    const push = (text: string, hint: string, snippet?: Snippet) => { if (!seen.has(text)) { seen.add(text); out.push({ text, hint, snippet }); } };
    // in a tab with a device type: its commands first, then those without one (never another device type's)
    const all = this.hist.entries();
    const mine = dt ? all.filter(e => (e.deviceType ?? '').toLowerCase() === dt.toLowerCase()) : [];
    const hist = dt ? all.filter(e => !e.deviceType || (e.deviceType ?? '').toLowerCase() === dt.toLowerCase()) : all;
    for (const e of [...mine, ...hist]) {
      if (out.length >= 6) break;
      if (e.cmd.toLowerCase().startsWith(q)) push(e.cmd, (e.deviceType || e.title || '') + (e.count > 1 ? ' ×' + e.count : ''));
    }
    const known = (this.ui.settings()?.deviceTypes ?? []).map(d => d.name);
    for (const s of this.ui.settings()?.snippets ?? []) {
      if (out.length >= 8) break;
      const types = snippetTypes(s, known);
      if (dt && types.length && !types.some(x => x.toLowerCase() === dt.toLowerCase())) continue;
      const first = s.text.split('\n')[0];
      if (s.name.toLowerCase().includes(q) || first.toLowerCase().startsWith(q)) push(s.text.includes('\n') ? s.name : first, 'library: ' + s.name, s);
    }
    for (const e of hist) {
      if (out.length >= 8) break;
      if (e.cmd.toLowerCase().includes(q)) push(e.cmd, (e.deviceType || e.title || '') + (e.count > 1 ? ' ×' + e.count : ''));
    }
    return out;
  });

  constructor() {
    void this.hist.load();
    effect(() => {
      const t = this.ui.cwInsert();
      if (t === null) return;
      untracked(() => { this.text.set(t); this.typed.set(false); this.ui.cwInsert.set(null); setTimeout(() => this.box().nativeElement.focus()); });
    });
  }

  closeSugg() { this.typed.set(false); this.sel.set(-1); clearTimeout(this.qTimer); }

  ngOnDestroy() { clearTimeout(this.qTimer); }

  accept(s: { text: string; snippet?: Snippet }) {
    this.closeSugg();
    if (s.snippet) { void this.insert(s.snippet); return; }
    this.text.set(s.text);
    this.box().nativeElement.focus();
  }

  onChange(v: string) {
    if (this.charMode) {
      const prev = this.text();
      if (v.startsWith(prev) && v.length > prev.length) void this.tabs.send(v.slice(prev.length).replace(/\n/g, '\r'), this.target, true);
    }
    this.text.set(v);
    this.typed.set(true);
    this.sel.set(-1);
    clearTimeout(this.qTimer);
    this.qTimer = setTimeout(() => this.query.set(this.text()), 120);
  }

  onKey(ev: KeyboardEvent) {
    const list = this.sugg();
    if (list.length) {
      if (ev.key === 'ArrowDown' || ev.key === 'ArrowUp') {
        ev.preventDefault();
        const n = list.length;
        this.sel.set(ev.key === 'ArrowDown' ? (this.sel() + 1) % n : (this.sel() <= 0 ? n - 1 : this.sel() - 1));
        return;
      }
      if (ev.key === 'Escape') { ev.preventDefault(); this.closeSugg(); return; }
      if (ev.key === 'Tab' || (ev.key === 'Enter' && !ev.shiftKey && this.sel() >= 0)) {
        ev.preventDefault();
        this.accept(list[Math.max(this.sel(), 0)]);
        return;
      }
    }
    if (ev.key === 'Enter' && !ev.shiftKey) {
      ev.preventDefault();
      if (this.charMode) { void this.tabs.send('\r', this.target, true); this.text.set(''); return; }
      this.send();
      return;
    }
    const single = !this.text().includes('\n');
    if ((ev.key === 'ArrowUp' || ev.key === 'ArrowDown') && single && this.history.length) {
      ev.preventDefault();
      this.hIdx = ev.key === 'ArrowUp' ? Math.min(this.hIdx + 1, this.history.length - 1) : Math.max(this.hIdx - 1, -1);
      this.text.set(this.hIdx < 0 ? '' : this.history[this.hIdx]);
      this.typed.set(false);
    }
  }

  send() {
    const t = this.text();
    if (this.charMode) return;
    const lines = t.split('\n');
    void this.tabs.send(lines.map(l => l + '\r').join(''), this.target, true);
    if (t.trim()) this.history = [t, ...this.history.filter(h => h !== t)].slice(0, 100);
    const targets = this.tabs.targets(this.target);
    for (const l of lines) for (const tv of targets.slice(0, 1)) this.hist.add(l, { session: tv.session.path, title: tv.title, deviceType: tv.session.deviceType });
    this.hIdx = -1;
    this.closeSugg();
    this.text.set('');
    this.box().nativeElement.focus();
  }

  focus() { this.box().nativeElement.focus(); }

  /** Library commands, those for the active tab's device type first; the chosen one goes into the box to check before sending. */
  library(ev: MouseEvent) {
    const all = this.ui.settings()?.snippets ?? [];
    const dt = this.tabs.activeDeviceType().toLowerCase();
    const known = (this.ui.settings()?.deviceTypes ?? []).map(d => d.name);
    const mine = dt ? all.filter(s => snippetTypes(s, known).some(t => t.toLowerCase() === dt)) : [];
    const other = all.filter(s => !mine.includes(s) && !(dt && snippetTypes(s, known).length));
    const item = (s: Snippet) => ({ label: s.name, hint: s.folder, action: () => void this.insert(s) });
    this.ui.showMenu(ev, [
      ...(mine.length ? [{ label: 'For ' + this.tabs.activeDeviceType(), disabled: true }, ...mine.slice(0, 20).map(item), { separator: true }] : []),
      ...other.slice(0, 20).map(item),
      ...(all.length ? [{ separator: true }] : [{ label: 'No saved commands yet', disabled: true }]),
      { label: 'Command Library...', action: () => this.ui.open('commands') },
    ]);
  }

  private async insert(s: Snippet) {
    const values: Record<string, string> = {};
    for (const v of snippetVars(s.text)) {
      const val = await this.ui.input(s.name, v.name, v.def);
      if (val === null) return;
      values[v.name] = val;
    }
    this.text.set(fillSnippet(s.text, values).replace(/\r?\n$/, ''));
    this.box().nativeElement.focus();
  }

  /** Menu of connected tabs to tick for "Selected tabs". */
  pick(ev: MouseEvent) {
    const live = this.tabs.tabs().filter(t => t.kind === 'terminal' && t.state === 'connected');
    this.ui.showMenu(ev, live.length ? [
      ...live.map(t => ({ label: (this.tabs.selected().has(t.id) ? '☑ ' : '☐ ') + t.title, action: () => { this.tabs.toggleSelected(t.id); this.target = 'selected'; this.pick(ev); } })),
      { separator: true },
      { label: 'Select All', action: () => { this.tabs.selected.set(new Set(live.map(t => t.id))); this.target = 'selected'; } },
      { label: 'Clear', action: () => this.tabs.selected.set(new Set()) },
    ] : [{ label: 'No connected tabs', disabled: true }]);
  }
}
