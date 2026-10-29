import { AfterViewInit, Component, ElementRef, computed, inject, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { ago } from '../core/env';
import { History } from '../core/history.service';
import { HistoryEntry } from '../core/models';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';

/** Command History: search every command typed in terminals and the Command Window. */
@Component({
  selector: 'app-history',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop" (click)="ui.close()">
    <div class="modal hist" (click)="$event.stopPropagation()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="history" [size]="17" /></span>
        <span class="mh-t">Command History<span class="mh-s">What you typed, per session - search by any part</span></span></div>
      <div class="modal-body">
        <div class="row top">
          <span class="qwrap"><app-icon name="search" [size]="15" class="md-only qi" /><input #q class="q" [ngModel]="query()" (ngModelChange)="query.set($event); sel.set(0)" (keydown)="key($event)"
            placeholder="Search commands, e.g. isis adj" spellcheck="false" autocomplete="off" /></span>
          @if (dt()) {
            <label class="inline"><input type="checkbox" [ngModel]="onlyDt()" (ngModelChange)="onlyDt.set($event); sel.set(0)" /> Only {{ dt() }}</label>
          }
        </div>
        <div class="list" #list>
          @for (e of results(); track $index) {
            <div class="it" [class.on]="$index === sel()" (click)="sel.set($index)" (dblclick)="type(e)">
              <span class="c">{{ e.cmd }}</span>
              <span class="m cl-only">{{ e.deviceType || '' }}{{ e.deviceType && (e.title || e.session) ? ' · ' : '' }}{{ e.title || e.session }} · {{ when(e.time) }}{{ e.count > 1 ? ' · ×' + e.count : '' }}</span>
              <span class="mm md-only">@if (e.deviceType) { <span class="dt" [style.--dt]="dtColor(e.deviceType)">{{ e.deviceType }}</span> }
                <span class="m">{{ e.title || e.session }} · {{ when(e.time) }}</span>@if (e.count > 1) { <span class="m n">×{{ e.count }}</span> }</span>
              <button type="button" class="icon-btn rm" [title]="onlyDt() && dt() ? 'Remove this command from the ' + dt() + ' history' : 'Remove this command from history'" (click)="$event.stopPropagation(); remove(e)" (dblclick)="$event.stopPropagation()"><app-icon name="trash" [size]="14" /></button>
            </div>
          } @empty {
            <div class="empty">{{ hist.entries().length ? 'No command matches.' : 'Commands you type in terminals and the Command Window appear here.' }}</div>
          }
        </div>
        <p class="hint md-only">Commands with passwords, secrets, SNMP communities or keys are never remembered.</p>
        <p class="hint cl-only">Enter types the command in the active tab (press Enter there to run it) · Shift+Enter runs it ·
          Commands with passwords, secrets, SNMP communities or keys are never remembered.</p>
      </div>
      <div class="modal-foot">
        <button type="button" class="danger" (click)="openClear()" [disabled]="!hist.entries().length"><app-icon name="trash" [size]="14" /> Clear History…</button>
        <span class="spacer"></span>
        <span class="keys md-only"><kbd>↵</kbd> type <kbd>⇧↵</kbd> run <kbd>esc</kbd> close</span>
        <button type="button" (click)="ui.close()">Close</button>
        <button type="button" [disabled]="!current()" (click)="toWindow(current()!)">To Command Window</button>
        <button type="button" class="cl-only" [disabled]="!current() || !tabs.active()" (click)="type(current()!)">Type in Tab</button>
        <button type="button" class="primary" [disabled]="!current() || !tabs.active()" (click)="run(current()!)">Run</button>
      </div>
      @if (clearing()) {
        <div class="cl-back" (click)="clearing.set(false)">
          <div class="cl-card" (click)="$event.stopPropagation()">
            <div class="ct"><span class="cic"><app-icon name="trash" [size]="16" /></span><span>Clear Command History<span class="cs">Forget the commands typed on these device types</span></span></div>
            <label class="ck all"><input type="checkbox" [checked]="picked().size === types().length" [indeterminate]="picked().size > 0 && picked().size < types().length" (change)="pickAll($any($event.target).checked)" /><span class="tl">All device types</span><span class="n">{{ hist.entries().length }}</span></label>
            <div class="cl-list">
              @for (t of types(); track t.type) {
                <label class="ck"><input type="checkbox" [checked]="picked().has(t.type)" (change)="pick(t.type, $any($event.target).checked)" />
                  <span class="tl">@if (t.type) { <span class="dt" [style.--dt]="dtColor(t.type)">{{ t.type }}</span> } @else { No device type (local shells, Quick Connect) }</span><span class="n">{{ t.n }}</span></label>
              }
            </div>
            <div class="cf">
              <button type="button" (click)="clearing.set(false)">Cancel</button>
              <button type="button" class="danger" [disabled]="!picked().size" (click)="doClear()">Clear {{ pickedCount() }} {{ pickedCount() === 1 ? 'Command' : 'Commands' }}</button>
            </div>
          </div>
        </div>
      }
    </div>
  </div>`,
  styles: [`
    .hist { width: min(880px, 94vw); position: relative; }
    .modal-foot { flex-wrap: nowrap; > button { white-space: nowrap; flex: none; } button.danger { display: inline-flex; align-items: center; gap: 6px; } }
    .it { position: relative; }
    .rm { flex: none; width: 26px; height: 24px; padding: 0; justify-content: center; visibility: hidden; &:hover { color: var(--danger) !important; } }
    .it:hover .rm, .it.on .rm { visibility: visible; }
    .it > .rm { position: absolute; right: 6px; top: 4px; }
    .cl-back { position: absolute; inset: 0; z-index: 5; display: grid; place-items: center; background: color-mix(in srgb, var(--bg) 55%, transparent); border-radius: inherit; }
    .cl-card { width: min(420px, 90%); max-height: 80%; display: flex; flex-direction: column; gap: 6px; padding: 16px; border: 1px solid var(--border); border-radius: 10px; background: var(--panel); box-shadow: 0 12px 40px rgb(0 0 0 / .35);
      .ct { display: flex; gap: 10px; align-items: center; font-weight: 650; margin-bottom: 6px; > span:last-child { display: flex; flex-direction: column; } }
      .cs { font-weight: 400; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); }
      .cic { width: 30px; height: 30px; border-radius: 8px; display: grid; place-items: center; flex: none; color: var(--danger); background: color-mix(in srgb, var(--danger) 14%, transparent); }
      .cl-list { overflow: auto; display: flex; flex-direction: column; gap: 2px; }
      .ck { display: flex; align-items: center; gap: 10px; min-height: 30px; padding: 0 8px; border-radius: 6px; cursor: pointer; &:hover { background: var(--hover); }
        input { margin: 0; accent-color: var(--danger); } .tl { flex: 1; } .n { color: var(--muted); font-variant-numeric: tabular-nums; } }
      .ck.all { border-bottom: 1px solid var(--border); border-radius: 6px 6px 0 0; font-weight: 600; }
      .cf { display: flex; justify-content: flex-end; gap: 8px; margin-top: 10px; } }
    .dt { display: inline-flex; align-items: center; height: 20px; padding: 0 7px; border-radius: 5px; font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600;
      color: var(--dt); border: 1px solid color-mix(in srgb, var(--dt) 40%, transparent); background: color-mix(in srgb, var(--dt) 12%, transparent); }
    .top { display: flex; gap: 12px; align-items: center; margin-bottom: 8px; .q { flex: 1; } }
    .inline { display: inline-flex; gap: 6px; align-items: center; white-space: nowrap; font-size: calc(12.5px * var(--ui-scale, 1)); }
    .list { height: 46vh; overflow: auto; border: 1px solid var(--border); border-radius: 6px; }
    .it { display: flex; flex-direction: column; padding: 5px 10px; cursor: default; border-bottom: 1px solid color-mix(in srgb, var(--border) 50%, transparent);
      .c { font-family: var(--mono); font-size: calc(13px * var(--ui-scale, 1)); white-space: pre-wrap; word-break: break-all; } .m { color: var(--muted); font-size: calc(11px * var(--ui-scale, 1)); } }
    .it.on { background: var(--selected); }
    .empty { padding: 16px; color: var(--muted); }
    .danger-text { color: var(--danger); } .spacer { flex: 1; }
    .qwrap { flex: 1; display: flex; position: relative; align-items: center; }
    :host-context(.modern) {
      .hist { width: min(880px, 94vw); }
      .qi { position: absolute; left: 11px; color: var(--muted); pointer-events: none; }
      .q { height: 36px; padding-left: 34px; border-radius: var(--r-lg); font-family: var(--mono); &:focus { box-shadow: 0 0 0 3px var(--accent-soft); } }
      .list { height: 44vh; border: 0; border-radius: 0; display: flex; flex-direction: column; gap: 1px; }
      .it { flex-direction: row; align-items: center; gap: 10px; padding: 7px 10px; border: 0; border-radius: var(--r);
        .c { flex: 1; min-width: 0; font-size: calc(12.5px * var(--ui-scale, 1)); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; } }
      .it > .rm { position: static; }
      .it:hover { background: var(--surface-2); }
      .it.on { background: var(--accent-soft); }
      .mm { display: inline-flex !important; align-items: center; gap: 8px; flex: none; .n { min-width: 26px; text-align: right; } }
      .m { white-space: nowrap; font-size: calc(11.5px * var(--ui-scale, 1)); }
      .dt { display: inline-flex; align-items: center; height: 20px; padding: 0 7px; border-radius: 5px; font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600;
        color: var(--dt); border: 1px solid color-mix(in srgb, var(--dt) 40%, transparent); background: color-mix(in srgb, var(--dt) 12%, transparent); }
      .cl-card { background: var(--surface); border-color: var(--line); border-radius: var(--r-lg); }
      .keys { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); margin-right: 6px; white-space: nowrap;
        kbd { display: inline-flex; align-items: center; height: 20px; padding: 0 6px; margin: 0 2px 0 6px; border-radius: 5px; border: 1px solid var(--line); background: var(--surface-2); font: inherit; font-size: 11px; } }
    }
  `],
})
export class HistoryDialog implements AfterViewInit {
  protected ui = inject(Ui);
  protected tabs = inject(Tabs);
  protected hist = inject(History);
  private backend = inject(Backend);
  private q = viewChild.required<ElementRef<HTMLInputElement>>('q');
  private list = viewChild.required<ElementRef<HTMLDivElement>>('list');
  readonly query = signal('');
  readonly sel = signal(0);
  readonly dt = this.tabs.activeDeviceType;
  readonly onlyDt = signal(!!this.tabs.activeDeviceType());
  readonly results = computed<HistoryEntry[]>(() => this.hist.search(this.query(), this.onlyDt() ? this.dt() : '', 300));
  readonly current = computed(() => this.results()[this.sel()] ?? null);
  readonly modern = () => this.ui.settings()?.layout !== 'classic';
  /** A steady colour per device type for its badge. */
  dtColor(name: string) {
    const fixed: Record<string, string> = { voss: '#0d9488', exos: '#7c3aed', 'cisco ios': '#2563eb', linux: '#d97706' };
    const f = fixed[name.toLowerCase()];
    if (f) return f;
    let h = 0; for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) % 360;
    return `hsl(${h} 55% 48%)`;
  }

  constructor() { void this.hist.load(); }

  ngAfterViewInit() { setTimeout(() => this.q().nativeElement.focus()); }

  when(t: string) { return ago(t); }

  key(ev: KeyboardEvent) {
    const n = this.results().length;
    if (ev.key === 'ArrowDown' || ev.key === 'ArrowUp') {
      ev.preventDefault();
      if (!n) return;
      this.sel.set(ev.key === 'ArrowDown' ? Math.min(this.sel() + 1, n - 1) : Math.max(this.sel() - 1, 0));
      setTimeout(() => this.list().nativeElement.querySelector('.on')?.scrollIntoView({ block: 'nearest' }));
    } else if (ev.key === 'Enter' && this.current()) {
      ev.preventDefault();
      if (ev.shiftKey) this.run(this.current()!); else this.type(this.current()!);
    } else if (ev.key === 'Delete' && ev.shiftKey && this.current()) {
      ev.preventDefault();
      void this.remove(this.current()!);
    } else if (ev.key === 'Escape') {
      if (this.clearing()) this.clearing.set(false); else this.ui.close();
    }
  }

  type(e: HistoryEntry) {
    const t = this.tabs.active();
    if (!t) return;
    this.ui.close();
    void this.backend.sendInput(t.id, e.cmd);
    setTimeout(() => this.tabs.handles.get(t.id)?.term.focus());
  }

  run(e: HistoryEntry) {
    const t = this.tabs.active();
    if (!t) return;
    this.ui.close();
    void this.tabs.send(e.cmd + '\r', 'active', true);
    this.hist.add(e.cmd, { session: t.session.path, title: t.title, deviceType: t.session.deviceType });
    setTimeout(() => this.tabs.handles.get(t.id)?.term.focus());
  }

  toWindow(e: HistoryEntry) {
    this.ui.close();
    if (!this.ui.settings()?.showCommandWindow) this.ui.patchSettings({ showCommandWindow: true });
    this.ui.cwInsert.set(e.cmd);
  }

  readonly clearing = signal(false);
  readonly types = signal<{ type: string; n: number }[]>([]);
  readonly picked = signal<Set<string>>(new Set());
  readonly pickedCount = computed(() => this.types().filter(t => this.picked().has(t.type)).reduce((n, t) => n + t.n, 0));

  /** Clear History…: choose the device types (the active tab's is ticked). */
  openClear() {
    const types = this.hist.typeCounts();
    this.types.set(types);
    const mine = types.find(t => this.dt() && t.type.toLowerCase() === this.dt()!.toLowerCase());
    this.picked.set(new Set(mine ? [mine.type] : types.map(t => t.type)));
    this.clearing.set(true);
  }
  pick(type: string, on: boolean) {
    this.picked.update(s => { const n = new Set(s); if (on) n.add(type); else n.delete(type); return n; });
  }
  pickAll(on: boolean) { this.picked.set(new Set(on ? this.types().map(t => t.type) : [])); }

  async doClear() {
    const types = [...this.picked()];
    const all = types.length === this.types().length;
    try {
      if (all) await this.hist.clear(); else await this.hist.clearTypes(types);
    } catch (e) { this.ui.error(e); return; }
    this.clearing.set(false);
    this.sel.set(0);
    setTimeout(() => this.q().nativeElement.focus());
  }

  /** Forgets one command (only for the device type when Only … is ticked). */
  async remove(e: HistoryEntry) {
    const dt = this.onlyDt() ? this.dt() ?? '' : '';
    await this.hist.remove(e.cmd, dt).catch(err => this.ui.error(err));
    this.sel.update(i => Math.min(i, Math.max(0, this.results().length - 1)));
    this.ui.toast(`Removed "${e.cmd.length > 60 ? e.cmd.slice(0, 57) + '…' : e.cmd}" from the command history.`);
  }
}
