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
        <button type="button" class="danger-text" [class.danger-link]="modern()" (click)="clear()" [disabled]="!hist.entries().length">{{ modern() ? 'Clear history…' : 'Clear History' }}</button>
        <span class="spacer"></span>
        <span class="keys md-only"><kbd>↵</kbd> type in tab <kbd>⇧↵</kbd> run <kbd>esc</kbd> close</span>
        <button type="button" (click)="ui.close()">Close</button>
        <button type="button" [disabled]="!current()" (click)="toWindow(current()!)">To Command Window</button>
        <button type="button" class="cl-only" [disabled]="!current() || !tabs.active()" (click)="type(current()!)">Type in Tab</button>
        <button type="button" class="primary" [disabled]="!current() || !tabs.active()" (click)="run(current()!)">Run</button>
      </div>
    </div>
  </div>`,
  styles: [`
    .hist { width: min(820px, 94vw); }
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
      .hist { width: min(720px, 94vw); }
      .qi { position: absolute; left: 11px; color: var(--muted); pointer-events: none; }
      .q { height: 36px; padding-left: 34px; border-radius: var(--r-lg); font-family: var(--mono); &:focus { box-shadow: 0 0 0 3px var(--accent-soft); } }
      .list { height: 44vh; border: 0; border-radius: 0; display: flex; flex-direction: column; gap: 1px; }
      .it { flex-direction: row; align-items: center; gap: 10px; padding: 7px 10px; border: 0; border-radius: var(--r);
        .c { flex: 1; min-width: 0; font-size: calc(12.5px * var(--ui-scale, 1)); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; } }
      .it:hover { background: var(--surface-2); }
      .it.on { background: var(--accent-soft); }
      .mm { display: inline-flex !important; align-items: center; gap: 8px; flex: none; .n { min-width: 26px; text-align: right; } }
      .m { white-space: nowrap; font-size: calc(11.5px * var(--ui-scale, 1)); }
      .dt { display: inline-flex; align-items: center; height: 20px; padding: 0 7px; border-radius: 5px; font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600;
        color: var(--dt); border: 1px solid color-mix(in srgb, var(--dt) 40%, transparent); background: color-mix(in srgb, var(--dt) 12%, transparent); }
      .keys { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); margin-right: 6px;
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
    } else if (ev.key === 'Escape') this.ui.close();
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

  async clear() {
    if (!(await this.ui.confirm('Clear History', 'Forget every remembered command?', 'Clear', true))) return;
    await this.hist.clear().catch(e => this.ui.error(e));
  }
}
