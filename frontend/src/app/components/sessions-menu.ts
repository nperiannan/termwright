import { AfterViewInit, Component, ElementRef, computed, inject, input, output, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { connectString, looksLikeConnect } from '../core/connect-string';
import { SessionNode } from '../core/models';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { DeviceIcon, deviceKindOf } from './device-icon';
import { SessionIcon, SessionKind } from './session-icon';

interface Row { head?: string; n?: SessionNode; connect?: string }

/**
 * Title-bar session list: every session with its small icon, grouped by
 * folder (favourites first), a filter, and Enter to connect. Typing an
 * address that is no session connects to it.
 */
@Component({
  selector: 'app-sessions-menu',
  imports: [FormsModule, DeviceIcon, SessionIcon],
  template: `
    <div class="back" (mousedown)="closed.emit()"></div>
    <div class="sm" [style.top.px]="top()" [style.right.px]="right()" (mousedown)="$event.stopPropagation()">
      <input #q class="q" [ngModel]="query()" (ngModelChange)="query.set($event); sel.set(0)" (keydown)="key($event)"
        placeholder="Filter sessions - or type an address to connect" spellcheck="false" />
      <div class="list" #list>
        @for (r of rows(); track $index) {
          @if (r.head) { <div class="h">{{ r.head }}</div> }
          @else if (r.connect) {
            <div class="r" [class.on]="idx(r) === sel()" (mouseenter)="sel.set(idx(r))" (click)="choose(r, $event.altKey)">
              <span class="ic conn">↵</span><span class="nm">Connect to {{ r.connect }}</span></div>
          } @else {
            <div class="r" [class.on]="idx(r) === sel()" [attr.data-i]="idx(r)" (mouseenter)="sel.set(idx(r))" (click)="choose(r, $event.altKey)" [title]="r.n!.path">
              @if (kind(r.n!); as k) { <app-device-icon [kind]="k" [size]="16" /> } @else { <app-session-icon [kind]="proto(r.n!)" [size]="16" /> }
              <span class="nm">{{ r.n!.name }}</span><span class="ad">{{ r.n!.hostname }}</span></div>
          }
        } @empty { <div class="empty">No sessions match</div> }
      </div>
      <div class="ft"><span><kbd>↵</kbd> connect</span><span><kbd>Alt</kbd>+<kbd>↵</kbd> SFTP</span><span><kbd>Esc</kbd> close</span></div>
    </div>`,
  styles: [`
    .back { position: fixed; inset: 0; z-index: 900; }
    .sm { position: fixed; z-index: 901; width: 380px; max-width: calc(100vw - 24px); display: flex; flex-direction: column; max-height: min(70vh, 620px);
      background: var(--surface, var(--panel)); border: 1px solid var(--line, var(--border)); border-radius: var(--r-lg, 8px); box-shadow: 0 12px 36px rgba(0, 0, 0, .35); overflow: hidden; }
    .q { margin: 8px; height: 30px; padding: 0 10px; border-radius: var(--r, 6px); background: var(--surface-2); }
    .list { flex: 1; min-height: 0; overflow: auto; padding: 0 6px 6px; }
    .h { padding: 8px 8px 3px; font-size: calc(10.5px * var(--ui-scale, 1)); font-weight: 600; letter-spacing: .05em; text-transform: uppercase; color: var(--faint, var(--muted));
      white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .r { display: flex; align-items: center; gap: 8px; height: 28px; padding: 0 8px; border-radius: var(--r-sm, 5px); cursor: default; white-space: nowrap;
      &.on { background: var(--accent-soft, var(--selected)); }
      .nm { min-width: 0; overflow: hidden; text-overflow: ellipsis; }
      .ad { margin-left: auto; padding-left: 10px; color: var(--muted); font-family: var(--mono); font-size: calc(11px * var(--ui-scale, 1)); overflow: hidden; text-overflow: ellipsis; max-width: 48%; }
      .ic.conn { width: 16px; text-align: center; color: var(--accent); font-weight: 700; } }
    .empty { padding: 10px 8px; color: var(--muted); }
    .ft { display: flex; gap: 12px; padding: 6px 12px; border-top: 1px solid var(--line, var(--border)); color: var(--muted); font-size: calc(11px * var(--ui-scale, 1));
      kbd { font: inherit; padding: 0 4px; border: 1px solid var(--line, var(--border)); border-radius: 4px; } }
  `],
})
export class SessionsMenu implements AfterViewInit {
  private ui = inject(Ui);
  private tabs = inject(Tabs);
  private backend = inject(Backend);
  /** Position under the title-bar button. */
  readonly top = input(48);
  readonly right = input(12);
  readonly closed = output<void>();
  private q = viewChild.required<ElementRef<HTMLInputElement>>('q');
  private list = viewChild<ElementRef<HTMLElement>>('list');
  readonly query = signal('');
  readonly sel = signal(0);

  /** Sessions grouped by folder, favourites first; filtered by the query. */
  readonly rows = computed<Row[]>(() => {
    const root = this.ui.tree();
    const q = this.query().trim().toLowerCase();
    const words = q.split(/\s+/).filter(Boolean);
    const hit = (n: SessionNode) => !words.length || words.every(w => (n.name + ' ' + (n.hostname ?? '') + ' ' + n.path).toLowerCase().includes(w));
    const out: Row[] = [];
    const byPath = new Map<string, SessionNode>();
    const groups: { folder: string; items: SessionNode[] }[] = [];
    const walk = (n: SessionNode, folder: string) => {
      const mine: SessionNode[] = [];
      for (const c of n.children ?? []) if (!c.isFolder) { byPath.set(c.path, c); if (hit(c)) mine.push(c); }
      if (mine.length) groups.push({ folder, items: mine });
      for (const c of n.children ?? []) if (c.isFolder) walk(c, c.path);
    };
    if (root) walk(root, '');
    const favs = (this.ui.settings()?.favorites ?? []).map(p => byPath.get(p)).filter((n): n is SessionNode => !!n && hit(n));
    const text = this.query().trim();
    const exact = groups.some(g => g.items.some(n => n.name.toLowerCase() === q));
    if (text && !exact && looksLikeConnect(text, this.ui)) out.push({ connect: text });
    if (favs.length && !words.length) { out.push({ head: 'Favourites' }); favs.forEach(n => out.push({ n })); }
    // top-level sessions go last, under "Sessions" (as in the Session Manager)
    for (const g of [...groups.filter(g => g.folder), ...groups.filter(g => !g.folder)]) {
      out.push({ head: g.folder ? g.folder.split('/').join(' / ') : 'Sessions' });
      g.items.forEach(n => out.push({ n }));
    }
    return out;
  });
  /** The rows that can be chosen, in order. */
  private readonly pickable = computed(() => this.rows().filter(r => !r.head));
  idx(r: Row) { return this.pickable().indexOf(r); }

  kind(n: SessionNode) { return n.protocol === 'WOL' ? null : deviceKindOf(n.deviceType) || null; }
  proto(n: SessionNode): SessionKind {
    switch (n.protocol) {
      case 'Serial': return 'serial'; case 'Console': return 'console'; case 'Local Shell': return 'shell';
      case 'Telnet': return 'telnet'; case 'Raw': return 'raw'; case 'RDP': return 'rdp'; case 'VNC': return 'vnc'; case 'WOL': return 'wol';
      default: return n.openAs === 'sftp' ? 'sftp' : 'ssh';
    }
  }

  ngAfterViewInit() { setTimeout(() => this.q().nativeElement.focus(), 0); }

  key(ev: KeyboardEvent) {
    const n = this.pickable().length;
    if (ev.key === 'ArrowDown' || ev.key === 'ArrowUp') {
      ev.preventDefault();
      this.sel.set(n ? (this.sel() + (ev.key === 'ArrowDown' ? 1 : n - 1)) % n : 0);
      setTimeout(() => this.list()?.nativeElement.querySelector('.r.on')?.scrollIntoView({ block: 'nearest' }), 0);
    } else if (ev.key === 'Escape') this.closed.emit();
    else if (ev.key === 'Enter') {
      ev.preventDefault();
      const r = this.pickable()[this.sel()];
      if (r) this.choose(r, ev.altKey);
    }
  }

  choose(r: Row, alt: boolean) {
    this.closed.emit();
    if (r.connect) void connectString(r.connect, this.ui, this.tabs, this.backend);
    else if (r.n) void this.tabs.openSession(r.n.path, alt ? 'sftp' : 'auto');
  }
}
