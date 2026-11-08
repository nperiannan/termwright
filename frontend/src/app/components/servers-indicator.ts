import { Component, HostListener, inject } from '@angular/core';
import { FileServers, pct } from '../core/file-servers.service';
import { Ui } from '../core/ui.service';
import { Backend } from '../core/backend';

/** Status bar: which file servers run (only while one does), with a pop-up to stop them. */
@Component({
  selector: 'app-servers-indicator',
  template: `
  @if (fs.summary(); as s) {
    <button type="button" class="ind" (click)="toggle($event)" [attr.aria-expanded]="fs.popup()" title="File servers running - click for details">
      <span class="d"></span>{{ s }}</button>
    @if (fs.popup()) {
      <div class="pop" role="dialog" aria-label="File servers" (click)="$event.stopPropagation()">
        <div class="h"><span>File servers</span><span class="muted">{{ fs.running()[0]?.address || 'all addresses' }}</span></div>
        @for (sv of fs.running(); track sv.kind) {
          <div class="r"><span class="d"></span><b>{{ sv.kind.toUpperCase() }}</b> :{{ sv.port }}<span class="grow"></span>
            @if (!active(sv.kind).length) { <span class="muted">idle</span> }
            <button type="button" (click)="stop(sv.kind)">Stop</button></div>
          @for (t of active(sv.kind); track t.id) {
            <div class="x"><div class="muted">{{ t.put ? '↑' : '↓' }} {{ t.file }} {{ t.put ? '←' : '→' }} {{ t.peer }} · {{ pct(t) }}</div>
              <div class="bar"><i [style.width]="t.size > 0 ? pct(t) : '30%'"></i></div></div>
          }
        }
        <div class="ft"><button type="button" (click)="open()">Open File Servers…</button><span class="grow"></span><button type="button" (click)="stop('')">Stop All</button></div>
      </div>
    }
  }`,
  styles: [`
    :host { position: relative; display: inline-flex; }
    .ind { all: unset; cursor: pointer; color: var(--ok); display: inline-flex; align-items: center; gap: 5px; white-space: pre; }
    .ind:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; border-radius: 3px; }
    .d { width: 7px; height: 7px; border-radius: 50%; background: var(--ok); display: inline-block; flex: none; }
    .pop { position: fixed; right: 10px; bottom: 34px; width: 340px; z-index: 90; background: var(--panel); color: var(--text); border: 1px solid var(--border);
      border-radius: var(--r-lg); box-shadow: var(--shadow); padding: 10px 12px; font-size: calc(12.5px * var(--ui-scale, 1)); white-space: normal; }
    .h { font-weight: 600; margin-bottom: 8px; display: flex; gap: 8px; span:first-child { flex: 1; } }
    .muted { color: var(--muted); font-weight: 400; }
    .r { display: flex; align-items: center; gap: 8px; padding: 5px 0; border-top: 1px solid var(--line); }
    .grow { flex: 1; }
    .x { padding: 0 0 6px 15px; .muted { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } }
    .bar { height: 5px; border-radius: 3px; background: var(--surface-2); overflow: hidden; margin-top: 4px; i { display: block; height: 100%; background: var(--accent); } }
    .ft { display: flex; gap: 6px; margin-top: 8px; }
    button:not(.ind) { padding: 2px 10px; font-size: calc(12px * var(--ui-scale, 1)); }
  `],
})
export class ServersIndicator {
  protected fs = inject(FileServers);
  private ui = inject(Ui);
  private backend = inject(Backend);
  protected readonly pct = pct;

  active(kind: string) { return this.fs.active().filter(t => t.server.toLowerCase() === kind); }
  toggle(e: Event) { e.stopPropagation(); this.fs.popup.set(!this.fs.popup()); }
  open() { this.fs.popup.set(false); this.ui.open('fileServers'); }
  async stop(kind: string) { await this.backend.fileServersStop(kind); }

  @HostListener('document:click') outside() { if (this.fs.popup()) this.fs.popup.set(false); }
  @HostListener('document:keydown.escape') esc() { if (this.fs.popup()) this.fs.popup.set(false); }
}
