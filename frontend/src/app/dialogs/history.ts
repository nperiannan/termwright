import { AfterViewInit, Component, ElementRef, computed, inject, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { ago } from '../core/env';
import { History } from '../core/history.service';
import { HistoryEntry } from '../core/models';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';

/** Command History: search every command typed in terminals and the Command Window. */
@Component({
  selector: 'app-history',
  imports: [FormsModule],
  template: `
  <div class="modal-backdrop" (click)="ui.close()">
    <div class="modal hist" (click)="$event.stopPropagation()">
      <div class="modal-head">Command History</div>
      <div class="modal-body">
        <div class="row top">
          <input #q class="q" [ngModel]="query()" (ngModelChange)="query.set($event); sel.set(0)" (keydown)="key($event)"
            placeholder="Search commands, e.g. isis adj" spellcheck="false" autocomplete="off" />
          @if (dt()) {
            <label class="inline"><input type="checkbox" [ngModel]="onlyDt()" (ngModelChange)="onlyDt.set($event); sel.set(0)" /> Only {{ dt() }}</label>
          }
        </div>
        <div class="list" #list>
          @for (e of results(); track $index) {
            <div class="it" [class.on]="$index === sel()" (click)="sel.set($index)" (dblclick)="type(e)">
              <span class="c">{{ e.cmd }}</span>
              <span class="m">{{ e.deviceType || '' }}{{ e.deviceType && (e.title || e.session) ? ' · ' : '' }}{{ e.title || e.session }} · {{ when(e.time) }}{{ e.count > 1 ? ' · ×' + e.count : '' }}</span>
            </div>
          } @empty {
            <div class="empty">{{ hist.entries().length ? 'No command matches.' : 'Commands you type in terminals and the Command Window appear here.' }}</div>
          }
        </div>
        <p class="hint">Enter types the command in the active tab (press Enter there to run it) · Shift+Enter runs it ·
          Commands with passwords, secrets, SNMP communities or keys are never remembered.</p>
      </div>
      <div class="modal-foot">
        <button type="button" class="danger-text" (click)="clear()" [disabled]="!hist.entries().length">Clear History</button>
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Close</button>
        <button type="button" [disabled]="!current()" (click)="toWindow(current()!)">To Command Window</button>
        <button type="button" [disabled]="!current() || !tabs.active()" (click)="type(current()!)">Type in Tab</button>
        <button type="button" class="primary" [disabled]="!current() || !tabs.active()" (click)="run(current()!)">Run</button>
      </div>
    </div>
  </div>`,
  styles: [`
    .hist { width: min(820px, 94vw); }
    .top { display: flex; gap: 12px; align-items: center; margin-bottom: 8px; .q { flex: 1; } }
    .inline { display: inline-flex; gap: 6px; align-items: center; white-space: nowrap; font-size: 12.5px; }
    .list { height: 46vh; overflow: auto; border: 1px solid var(--border); border-radius: 6px; }
    .it { display: flex; flex-direction: column; padding: 5px 10px; cursor: default; border-bottom: 1px solid color-mix(in srgb, var(--border) 50%, transparent);
      .c { font-family: var(--mono); font-size: 13px; white-space: pre-wrap; word-break: break-all; } .m { color: var(--muted); font-size: 11px; } }
    .it.on { background: var(--selected); }
    .empty { padding: 16px; color: var(--muted); }
    .danger-text { color: var(--danger); } .spacer { flex: 1; }
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
