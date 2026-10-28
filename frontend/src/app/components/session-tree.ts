import { AfterViewInit, ChangeDetectionStrategy, Component, ElementRef, OnDestroy, computed, effect, inject, signal, viewChild } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { SessionNode } from '../core/models';
import { Tabs } from '../core/tabs.service';
import { MenuItem, Ui } from '../core/ui.service';
import { Icon } from './icon';
import { ago, envInfo, envOf } from '../core/env';
import { SessionIcon, SessionKind } from './session-icon';
import { DeviceIcon, deviceKindOf } from './device-icon';
import { FolderIcon } from './folder-icon';

const EXPANDED_KEY = 'termwright.expanded';
const SECTIONS_KEY = 'termwright.treeSections';

/** Row heights in px (must match session-tree.scss). */
const ROW_C = 24, SEC_C = 28, ALL_C = 30;
interface TreeRow { kind: 'sec' | 'all' | 'item' | 'node'; key: string; h: number; n?: SessionNode; depth?: number; sec?: string; label?: string; }
interface Hover { n: SessionNode; x: number; y: number; pw: string; dtPw: string; }

/** One line about where a session goes: user@host[:port] · device type. */
export function sessionAddr(n: SessionNode): string {
  let a: string;
  if (n.protocol === 'Serial') a = n.hostname || 'Serial';
  else if (n.protocol === 'Console') a = n.console || 'Console';
  else if (n.protocol === 'Local Shell') a = 'Local shell';
  else {
    const def = n.protocol === 'Telnet' ? 23 : n.protocol === 'RDP' ? 3389 : n.protocol === 'VNC' ? 5900 : 22;
    a = (n.username ? n.username + '@' : '') + (n.hostname ?? '') + (n.port && n.port !== def ? ':' + n.port : '');
  }
  return a + (n.deviceType ? ' · ' + n.deviceType : n.protocol && n.protocol !== 'SSH2' && n.protocol !== 'Local Shell' ? ' · ' + n.protocol : '');
}

/** Session Manager: folder tree with search, context menu and drag & drop. */
@Component({
  selector: 'app-session-tree',
  imports: [NgTemplateOutlet, FormsModule, Icon, SessionIcon, DeviceIcon, FolderIcon],
  host: { '[class.roomy]': 'roomy()' },
  template: `
    @if (!roomy()) {
    <div class="sm-head">
      <span class="title" title="Session Manager">Sessions</span>
      <button class="icon-btn" title="New session" (click)="ui.newSession(ui.currentFolder())"><app-icon name="plus" /></button>
      <button class="icon-btn" title="New folder" (click)="newFolder(ui.currentFolder())"><app-icon name="folder-plus" /></button>
      <button class="icon-btn" title="Refresh" (click)="ui.refreshTree(); checkNow()"><app-icon name="refresh" /></button>
      <button class="icon-btn" [class.on]="reach()" [title]="reach() ? 'Hide which devices answer' : 'Show which devices answer (a quick TCP check of the port of each session, every minute)'"
        (click)="ui.patchSettings({ showReachability: !reach() })"><app-icon name="pulse" /></button>
      <button class="icon-btn" [title]="ui.settings()?.sessionManagerRight ? 'Move the Session Manager to the left' : 'Move the Session Manager to the right'"
        (click)="ui.actionHandler('toggle-session-manager-side')"><app-icon [name]="ui.settings()?.sessionManagerRight ? 'dock-left' : 'dock-right'" /></button>
      <button class="icon-btn" title="Hide the Session Manager (View → Session Manager or the toolbar shows it again)"
        (click)="ui.actionHandler('toggle-session-manager')"><app-icon name="hide-panel" /></button>
    </div>
    }
    <div class="sm-search">
      @if (roomy()) { <app-icon name="find" [size]="14" /> }
      <input type="search" placeholder="Filter sessions" [(ngModel)]="filterText" (ngModelChange)="filter.set($event); showAll.set(false)" />
      @if (roomy()) {
        <button class="icon-btn more" title="New folder, refresh, reachability, dock side, hide" (click)="headMenu($event)"><app-icon name="more" /></button>
      }
    </div>
    <div class="sm-body" #body (scroll)="scrollTop.set(body.scrollTop)" (contextmenu)="rootMenu($event)"
         (dragover)="allowDrop($event)" (drop)="dropOn($event, '')">
      <!-- only the rows in view (and a margin) are in the page: large trees stay fast -->
      @if (win().top) { <div [style.height.px]="win().top"></div> }
      @for (r of win().rows; track r.key) {
        @switch (r.kind) {
          @case ('sec') {
            <div class="sec" (click)="toggleSection(r.sec!)"><span class="twisty" [class.open]="!closedSec().has(r.sec!)"><svg viewBox="0 0 12 12" aria-hidden="true"><path d="M4 2.5 L8 6 L4 9.5" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg></span>{{ r.label }}</div>
          }
          @case ('all') { <div class="sec all">All sessions @if (roomy()) { <span class="total">{{ counts().get('') ?? 0 }}</span> }</div> }
          @case ('item') {
            <div class="row session" [class.sel]="isSel(r.n!)" [style.padding-left.px]="r.sec ? 22 : 8"
                 (click)="select(r.n!, $event)" (dblclick)="connect(r.n!)" (contextmenu)="nodeMenu($event, r.n!)"
                 (mouseenter)="hoverIn($event, r.n!)" (mouseleave)="hoverOut()">
              <ng-container [ngTemplateOutlet]="sessTpl" [ngTemplateOutletContext]="{ $implicit: r.n }" />
              <span class="host">{{ r.sec === 'fav' ? parentOf(r.n!.path) : r.sec === 'recent' ? lastAgo(r.n!.path) : r.n!.path }}</span>
            </div>
          }
          @case ('node') {
            @let n = r.n!;
            <div class="row" [class.folder]="n.isFolder" [class.session]="!n.isFolder"
                 [class.sel]="n.isFolder ? ui.selectedPath() === n.path : isSel(n)" [class.drop]="dropTarget() === n.path"
                 [style.padding-left.px]="8 + r.depth! * 14" [title]="n.isFolder ? n.path : ''"
                 (mouseenter)="n.isFolder || hoverIn($event, n)" (mouseleave)="hoverOut()"
                 draggable="true" (dragstart)="dragStart($event, n)" (dragend)="dropTarget.set('')"
                 (dragover)="allowDrop($event, n.isFolder ? n.path : parentOf(n.path))" (dragleave)="dropTarget.set('')"
                 (drop)="dropOn($event, n.isFolder ? n.path : parentOf(n.path))"
                 (click)="select(n, $event)" (dblclick)="n.isFolder ? toggle(n.path) : connect(n)"
                 (contextmenu)="nodeMenu($event, n)">
              @if (n.isFolder) {
                <span class="twisty" [class.open]="expanded().has(n.path)" (click)="toggle(n.path); $event.stopPropagation()"><svg viewBox="0 0 12 12" aria-hidden="true"><path d="M4 2.5 L8 6 L4 9.5" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg></span>
                @if (roomy()) { <app-folder-icon [depth]="r.depth ?? 0" [env]="folderEnv(n.path)" [open]="expanded().has(n.path)" [size]="22" /> }
                @else { <app-session-icon [kind]="expanded().has(n.path) ? 'folder-open' : 'folder'" /> }
                <span class="name">{{ n.name }}</span>
                @if (env(n); as e) { <span class="env" [style.background]="e.color" [title]="e.label + ' (folder)'">{{ e.short }}</span> }
                @if (roomy()) { <span class="count">{{ counts().get(n.path) ?? 0 }}</span> }
              } @else {
                <span class="twisty"></span>
                <ng-container [ngTemplateOutlet]="sessTpl" [ngTemplateOutletContext]="{ $implicit: n }" />
              }
            </div>
          }
        }
      }
      @if (win().bottom) { <div [style.height.px]="win().bottom"></div> }
      @if (filter()) {
        @if (!matches().length) { <div class="empty">No matching sessions</div> }
        @if (hiddenMatches() > 0) {
          <div class="more"><a (click)="showAll.set(true)">{{ hiddenMatches() }} more session{{ hiddenMatches() === 1 ? '' : 's' }} containing "{{ filter().trim() }}"</a></div>
        }
      } @else if (ui.tree() && !(ui.tree()!.children ?? []).length) {
        <div class="empty">
          No sessions yet.<br />
          <a (click)="ui.newSession('')">Create a session</a> or
          <a (click)="ui.open('import')">import sessions from another terminal program</a>.
        </div>
      }
    </div>
    <ng-template #sessTpl let-n>
      @if (roomy()) {
        @if (devKind(n); as k) { <app-device-icon [kind]="k" [size]="24" /> } @else { <app-session-icon [kind]="protoIcon(n)" [via]="!!n.hopTarget || n.protocol === 'Console'" [size]="24" /> }
        <span class="txt">
          <span class="line"><span class="name">{{ n.name }}</span>
            @if (isFav(n.path)) { <span class="star" title="Favourite">★</span> }
            @if (env(n); as e) { <span class="env" [style.background]="e.color" [title]="e.label">{{ e.short }}</span> }</span>
          <span class="addr">{{ addr(n) }}</span>
        </span>
        @if (reach()) { <span class="dot" [class]="dotClass(n)"></span> }
        @else if (liveState().get(n.path); as st) { <span class="dot" [class]="'dot ' + (st === 'connected' ? 'up' : st === 'error' ? 'down' : 'unk')" [title]="'Open in a tab - ' + st"></span> }
      } @else {
        <app-session-icon [kind]="protoIcon(n)" [via]="!!n.hopTarget || n.protocol === 'Console'" />
        @if (reach()) { <span class="dot" [class]="dotClass(n)"></span> }
        <span class="name">{{ n.name }}</span>
        @if (isFav(n.path)) { <span class="star" title="Favourite">★</span> }
        @if (env(n); as e) { <span class="env" [style.background]="e.color" [title]="e.label">{{ e.short }}</span> }
      }
    </ng-template>
    @if (roomy()) {
      <div class="sm-foot">
        <button class="primary" title="New session in the selected folder" (click)="ui.newSession(ui.currentFolder())"><app-icon name="plus" [size]="14" /> New</button>
        <button title="Import sessions from another terminal program or a file" (click)="ui.open('import')"><app-icon name="import" [size]="14" /> Import</button>
        <button title="Discover devices on a network" (click)="ui.open('discover')"><app-icon name="pulse" [size]="14" /> Scan</button>
      </div>
    }

    @if (hover(); as h) {
      <div class="hcard" [style.left.px]="h.x" [style.top.px]="h.y">
        <div class="hname">{{ h.n.name }}
          @if (env(h.n); as e) { <span class="env" [style.background]="e.color">{{ e.label }}</span> }</div>
        <div class="hrow mono">{{ target(h.n) }}</div>
        @if (h.n.jump) { <div class="hrow">through jump host <b>{{ h.n.jump }}</b></div> }
        @if (h.n.proxy) { <div class="hrow">through a proxy</div> }
        @if (h.n.deviceType) { <div class="hrow">Device type <b>{{ h.n.deviceType }}</b></div> }
        @if (h.n.mac) { <div class="hrow">MAC address <span class="mono">{{ h.n.mac }}</span></div> }
        <div class="hrow">{{ loginText(h) }}</div>
        @if (reach() && reachable().has(h.n.path)) { <div class="hrow">{{ reachable().get(h.n.path) ? 'Answers on port ' + portOf(h.n) : 'Does not answer on port ' + portOf(h.n) }}</div> }
        <div class="hrow muted">{{ lastAgo(h.n.path) ? 'Last connected ' + lastAgo(h.n.path) : 'Not connected yet from this computer' }}</div>
        @if (h.n.description) { <div class="hdesc">{{ h.n.description }}</div> }
      </div>
    }
  `,
  styleUrl: './session-tree.scss',
  // Only re-rendered when its signals change: typing elsewhere (Command Window,
  // dialogs) does not walk a large tree on every key.
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SessionTree implements AfterViewInit, OnDestroy {
  protected ui = inject(Ui);
  private tabs = inject(Tabs);
  private backend = inject(Backend);

  filterText = '';
  readonly filter = signal('');
  readonly dropTarget = signal('');
  readonly expanded = signal<Set<string>>(this.loadExpanded());
  private dragged: SessionNode | null = null;
  readonly closedSec = signal<Set<string>>(this.loadSections());
  readonly hover = signal<Hover | null>(null);
  private hoverTimer?: ReturnType<typeof setTimeout>;
  /** Session path → answered the last TCP check. */
  readonly reachable = signal<Map<string, boolean>>(new Map());
  private reachTimer?: ReturnType<typeof setInterval>;
  private checking = false;
  readonly reach = computed(() => !!this.ui.settings()?.showReachability);

  /** Every session node by path. */
  private readonly byPath = computed(() => {
    const m = new Map<string, SessionNode>();
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => (c.isFolder ? walk(c) : m.set(c.path, c)));
    walk(this.ui.tree());
    return m;
  });
  readonly favorites = computed(() => (this.ui.settings()?.favorites ?? []).map(p => this.byPath().get(p)).filter((n): n is SessionNode => !!n));
  readonly recents = computed(() => (this.ui.settings()?.recentSessions ?? []).slice(0, 5).map(p => this.byPath().get(p)).filter((n): n is SessionNode => !!n));
  /** Session paths in the order they are shown (for Shift-click ranges). */
  private readonly visibleOrder = computed(() => {
    if (this.filter()) return this.matches().map(n => n.path);
    const out: string[] = [];
    const ex = this.expanded();
    const walk = (n: SessionNode) => n.children?.forEach(c => { if (!c.isFolder) out.push(c.path); else if (ex.has(c.path)) walk(c); });
    const root = this.ui.tree();
    if (root) walk(root);
    return out;
  });
  private anchor = '';

  // ---- rows in view ----
  readonly scrollTop = signal(0);
  readonly viewH = signal(800);
  private body = viewChild<ElementRef<HTMLDivElement>>('body');
  private ro?: ResizeObserver;

  /** Everything the body shows, as flat rows (sections, sessions, tree nodes). */
  /** Modern layout: two-line session rows with device icons, folder counts and a footer. */
  readonly roomy = computed(() => this.ui.settings()?.layout !== 'classic' && !this.ui.settings()?.compactRows);
  /** Sessions under each folder (all levels). */
  readonly counts = computed(() => {
    const m = new Map<string, number>();
    const walk = (n: SessionNode): number => {
      let c = 0;
      for (const x of n.children ?? []) c += x.isFolder ? walk(x) : 1;
      m.set(n.path, c);
      return c;
    };
    const root = this.ui.tree();
    if (root) walk(root);
    return m;
  });
  /** Session path → state of its open tab (connected wins). */
  readonly liveState = computed(() => {
    const m = new Map<string, string>();
    for (const t of this.tabs.tabs()) {
      const p = t.session?.path;
      if (p && m.get(p) !== 'connected') m.set(p, t.state);
    }
    return m;
  });

  headMenu(ev: MouseEvent) {
    const r = (ev.currentTarget as HTMLElement).getBoundingClientRect();
    const s = this.ui.settings();
    this.ui.showMenuAt(r.left, r.bottom + 4, [
      { label: 'New Session...', action: () => void this.ui.newSession(this.ui.currentFolder()) },
      { label: 'New Folder...', action: () => void this.newFolder(this.ui.currentFolder()) },
      { label: 'Refresh', action: () => { void this.ui.refreshTree(); void this.checkNow(); } },
      { separator: true },
      { label: (this.reach() ? '✓ ' : '') + 'Show Which Devices Answer', action: () => this.ui.patchSettings({ showReachability: !this.reach() }) },
      { label: (s?.compactRows ? '✓ ' : '') + 'Compact Rows', action: () => this.ui.patchSettings({ compactRows: !s?.compactRows }) },
      { label: s?.sessionManagerRight ? 'Move to the Left' : 'Move to the Right', action: () => this.ui.actionHandler('toggle-session-manager-side') },
      { label: 'Hide Session Manager', action: () => this.ui.actionHandler('toggle-session-manager') },
    ]);
  }

  devKind(n: SessionNode) { return deviceKindOf(n.deviceType); }
  addr(n: SessionNode) { return sessionAddr(n); }

  private readonly rows = computed<TreeRow[]>(() => {
    const out: TreeRow[] = [];
    const roomy = this.roomy();
    const ROW = roomy ? 40 : ROW_C, SEC = roomy ? 30 : SEC_C, ALL = roomy ? 32 : ALL_C, FOLD = roomy ? 30 : ROW_C;
    if (this.filter()) {
      for (const n of this.matches()) out.push({ kind: 'item', key: 'm:' + n.path, n, h: ROW });
      return out;
    }
    const closed = this.closedSec();
    const fav = this.favorites(), rec = this.recents();
    if (fav.length) {
      out.push({ kind: 'sec', key: 's:fav', sec: 'fav', label: 'Favourites', h: SEC });
      if (!closed.has('fav')) for (const n of fav) out.push({ kind: 'item', key: 'f:' + n.path, n, sec: 'fav', h: ROW });
    }
    if (rec.length) {
      out.push({ kind: 'sec', key: 's:recent', sec: 'recent', label: 'Recent', h: SEC });
      if (!closed.has('recent')) for (const n of rec) out.push({ kind: 'item', key: 'r:' + n.path, n, sec: 'recent', h: ROW });
    }
    if (fav.length || rec.length) out.push({ kind: 'all', key: 'all', h: ALL });
    const ex = this.expanded();
    const walk = (n: SessionNode, depth: number) => {
      for (const c of n.children ?? []) {
        out.push({ kind: 'node', key: 'n:' + c.path, n: c, depth, h: c.isFolder ? FOLD : ROW });
        if (c.isFolder && ex.has(c.path)) walk(c, depth + 1);
      }
    };
    const root = this.ui.tree();
    if (root) walk(root, 0);
    return out;
  });
  private readonly offsets = computed(() => {
    const rows = this.rows();
    const off = new Array<number>(rows.length + 1);
    off[0] = 0;
    for (let i = 0; i < rows.length; i++) off[i + 1] = off[i] + rows[i].h;
    return off;
  });
  /** The rows to render with the space above and below them. */
  readonly win = computed(() => {
    const rows = this.rows();
    if (rows.length <= 250) return { rows, top: 0, bottom: 0 };
    const off = this.offsets();
    const from = Math.max(0, this.scrollTop() - 600), to = this.scrollTop() + this.viewH() + 600;
    let lo = 0, hi = rows.length;
    while (lo < hi) { const m = (lo + hi) >> 1; if (off[m + 1] <= from) lo = m + 1; else hi = m; }
    let end = lo;
    while (end < rows.length && off[end] < to) end++;
    return { rows: rows.slice(lo, end), top: off[lo], bottom: off[rows.length] - off[end] };
  });

  ngAfterViewInit() {
    const el = this.body()?.nativeElement;
    if (!el) return;
    this.viewH.set(el.clientHeight || 800);
    this.ro = new ResizeObserver(() => this.viewH.set(el.clientHeight || 800));
    this.ro.observe(el);
  }

  constructor() {
    effect(() => {
      const on = this.reach();
      this.byPath();
      clearInterval(this.reachTimer);
      if (!on) { this.reachable.set(new Map()); return; }
      void this.checkNow();
      this.reachTimer = setInterval(() => void this.checkNow(), 60000);
    });
  }

  /** Ticks every minute so "12 minutes ago" stays current. */
  private readonly minute = signal(0);
  private minuteTimer = setInterval(() => this.minute.update(n => n + 1), 60000);
  ngOnDestroy() { this.ro?.disconnect(); clearInterval(this.reachTimer); clearInterval(this.minuteTimer); clearTimeout(this.hoverTimer); }

  /** TCP check of every session that connects directly (not through a jump host or proxy). */
  async checkNow() {
    if (!this.reach() || this.checking) return;
    const targets = [...this.byPath().values()]
      .filter(n => !n.jump && !n.proxy && n.hostname && (n.protocol === 'SSH2' || n.protocol === 'Telnet' || n.protocol === 'Raw'))
      .map(n => ({ key: n.path, host: n.hostname!, port: this.portOf(n) }));
    if (!targets.length) return;
    this.checking = true;
    try {
      const r = await this.backend.checkReachable(targets);
      this.reachable.set(new Map(Object.entries(r ?? {})));
    } catch { /* ignore */ }
    this.checking = false;
  }

  portOf(n: SessionNode) { return n.port || (n.protocol === 'Telnet' ? 23 : 22); }

  dotClass(n: SessionNode) {
    const v = this.reachable().get(n.path);
    return 'dot ' + (v === true ? 'up' : v === false ? 'down' : 'unk');
  }

  /** Environment of a folder, set on it or on a folder above it. */
  folderEnv(path: string) { return envOf(path + '/x', '', this.ui.settings()?.folderDefaults); }

  /** Marks a folder (and everything in it) as production, or takes the mark off. */
  setProduction(path: string, on: boolean) {
    const list = [...(this.ui.settings()?.folderDefaults ?? [])];
    const i = list.findIndex(d => d.folder === path);
    if (i >= 0) list[i] = { ...list[i], environment: on ? 'production' : '' };
    else if (on) list.push({ folder: path, username: '', deviceType: '', jumpSession: '', identityFile: '', keywordSet: '', colorScheme: '', environment: 'production' });
    this.ui.patchSettings({ folderDefaults: list });
  }

  env(n: SessionNode) {
    const s = this.ui.settings();
    return envInfo(n.isFolder ? (s?.folderDefaults ?? []).find(d => d.folder === n.path)?.environment : envOf(n.path, n.environment, s?.folderDefaults));
  }

  isFav(p: string) { return (this.ui.settings()?.favorites ?? []).includes(p); }

  setFav(paths: string[], on: boolean) {
    const cur = this.ui.settings()?.favorites ?? [];
    const next = on ? [...cur, ...paths.filter(p => !cur.includes(p))] : cur.filter(p => !paths.includes(p));
    this.ui.patchSettings({ favorites: next });
  }

  lastAgo(p: string) { this.minute(); return ago(this.ui.settings()?.lastConnected?.[p]); }

  private loadSections(): Set<string> {
    try { return new Set(JSON.parse(localStorage.getItem(SECTIONS_KEY) ?? '[]')); } catch { return new Set(); }
  }

  toggleSection(k: string) {
    const s = new Set(this.closedSec());
    if (s.has(k)) s.delete(k); else s.add(k);
    this.closedSec.set(s);
    try { localStorage.setItem(SECTIONS_KEY, JSON.stringify([...s])); } catch { /* ignore */ }
  }

  isSel(n: SessionNode) { return this.ui.selectedPaths().has(n.path) || (this.ui.selectedPaths().size === 0 && this.ui.selectedPath() === n.path); }

  hoverIn(ev: MouseEvent, n: SessionNode) {
    clearTimeout(this.hoverTimer);
    const r = (ev.currentTarget as HTMLElement).getBoundingClientRect();
    this.hoverTimer = setTimeout(async () => {
      const x = Math.min(r.right + 8, window.innerWidth - 300), y = Math.min(r.top, window.innerHeight - 220);
      const h: Hover = { n, x: x < r.right && r.left > 310 ? r.left - 300 : x, y, pw: '…', dtPw: '' };
      this.hover.set(h);
      const pw = await this.backend.sessionPasswordState(n.path).catch(() => '');
      const dtPw = n.deviceType ? await this.backend.deviceTypePasswordState(n.deviceType).catch(() => '') : '';
      if (this.hover()?.n === n) this.hover.set({ ...h, pw, dtPw });
    }, 650);
  }

  hoverOut() { clearTimeout(this.hoverTimer); this.hover.set(null); }

  target(n: SessionNode) {
    if (n.protocol === 'Serial') return 'Serial ' + (n.hostname ?? '');
    if (n.protocol === 'Console') return 'Console ' + (n.console ? n.console.replace(/ port (\d+)$/, ', port $1') : '(no console server)');
    if (n.protocol === 'Local Shell') return 'Local shell';
    const who = n.username ? n.username + '@' : '';
    return `${n.protocol ?? ''} ${who}${n.hostname ?? ''}:${this.portOf(n)}` + (n.hopTarget ? ` → ${n.hopTarget}` : '');
  }

  loginText(h: Hover) {
    const st = (s: string) => s === '' ? 'asked when connecting' : s === 'empty' ? 'no password' : s.startsWith('ref:') ? 'from a password manager' : 'saved';
    if (h.pw === '…') return 'Login: …';
    if (h.n.deviceType && h.pw === '') return `Login from the device type: password ${st(h.dtPw)}`;
    return 'Password ' + st(h.pw);
  }

  /** Sessions containing the filter text, best first: exact host or name, then starting with it, then the rest. */
  readonly partial = computed(() => {
    const q = this.filter().toLowerCase().trim();
    const out: { n: SessionNode; rank: number }[] = [];
    const walk = (n: SessionNode) => {
      for (const c of n.children ?? []) {
        if (c.isFolder) { walk(c); continue; }
        const host = c.hostname?.toLowerCase() ?? '', name = c.name.toLowerCase();
        if (host === q || name === q) out.push({ n: c, rank: 0 });
        else if (host.startsWith(q) || name.startsWith(q)) out.push({ n: c, rank: 1 });
        else if ([c.name, c.hostname, c.username, c.path].some(v => v?.toLowerCase().includes(q))) out.push({ n: c, rank: 2 });
      }
    };
    const root = this.ui.tree();
    if (root) walk(root);
    return out.sort((a, b) => a.rank - b.rank);
  });
  /** Shown matches: only the exact ones when a host or name equals the filter, unless "show all" was clicked. */
  readonly matches = computed(() => {
    const all = this.partial();
    const exact = all.filter(m => m.rank === 0);
    return (exact.length && !this.showAll() ? exact : all).map(m => m.n);
  });
  readonly hiddenMatches = computed(() => this.partial().length - this.matches().length);
  readonly showAll = signal(false);

  private loadExpanded(): Set<string> {
    try { return new Set(JSON.parse(localStorage.getItem(EXPANDED_KEY) ?? '[]')); } catch { return new Set(); }
  }

  toggle(path: string) {
    const s = new Set(this.expanded());
    if (s.has(path)) s.delete(path); else s.add(path);
    this.expanded.set(s);
    try { localStorage.setItem(EXPANDED_KEY, JSON.stringify([...s])); } catch { /* storage unavailable */ }
  }

  select(n: SessionNode, ev?: MouseEvent) {
    this.hoverOut();
    const multi = ev && (ev.metaKey || ev.ctrlKey);
    if (!n.isFolder && ev?.shiftKey && this.anchor) {
      const order = this.visibleOrder();
      const a = order.indexOf(this.anchor), b = order.indexOf(n.path);
      if (a >= 0 && b >= 0) {
        this.ui.selectedPaths.set(new Set(order.slice(Math.min(a, b), Math.max(a, b) + 1)));
        this.ui.selectedPath.set(n.path);
        this.ui.selectedIsFolder.set(false);
        return;
      }
    }
    if (!n.isFolder && multi) {
      const s = new Set(this.ui.selectedPaths());
      if (s.size === 0 && this.ui.selectedPath() && !this.ui.selectedIsFolder()) s.add(this.ui.selectedPath());
      if (s.has(n.path)) s.delete(n.path); else s.add(n.path);
      this.ui.selectedPaths.set(s);
    } else {
      this.ui.selectedPaths.set(new Set());
    }
    if (!n.isFolder) this.anchor = n.path;
    this.ui.selectedPath.set(n.path);
    this.ui.selectedIsFolder.set(n.isFolder);
  }

  /** The selected sessions when the menu is opened on one of them. */
  private group(n: SessionNode): string[] {
    const s = this.ui.selectedPaths();
    return !n.isFolder && s.size > 1 && s.has(n.path) ? [...s] : [];
  }

  parentOf(p: string): string { return p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : ''; }

  connect(n: SessionNode) { void this.tabs.openSession(n.path); }

  protoIcon(n: SessionNode): SessionKind {
    switch (n.protocol) {
      case 'Serial': return 'serial';
      case 'Console': return 'console';
      case 'Local Shell': return 'shell';
      case 'Telnet': return 'telnet';
      case 'Raw': return 'raw';
      case 'RDP': return 'rdp';
      case 'VNC': return 'vnc';
      default: return n.openAs === 'sftp' ? 'sftp' : 'ssh';
    }
  }

  async newFolder(parent: string) {
    const name = await this.ui.input('New Folder', 'Folder name');
    if (!name) return;
    const path = parent ? parent + '/' + name : name;
    try {
      await this.backend.createFolder(path);
      if (parent) this.expanded.update(s => new Set(s).add(parent));
      await this.ui.refreshTree();
    } catch (e) { this.ui.error(e); }
  }

  rootMenu(ev: MouseEvent) {
    this.ui.showMenu(ev, [
      { label: 'New Session...', action: () => this.ui.newSession('') },
      { label: 'New Folder...', action: () => this.newFolder('') },
      { separator: true },
      { label: 'Import Sessions...', action: () => this.ui.open('import') },
      { label: 'Export Sessions...', action: () => this.ui.open('export') },
      { label: 'Refresh', action: () => this.ui.refreshTree() },
    ]);
  }

  nodeMenu(ev: MouseEvent, n: SessionNode) {
    this.hoverOut();
    const g = this.group(n);
    if (g.length) {
      const allFav = g.every(p => this.isFav(p));
      this.ui.showMenu(ev, [
        { label: `Connect ${g.length} Sessions`, action: () => g.forEach(p => void this.tabs.openSession(p)) },
        { separator: true },
        { label: `Edit ${g.length} Sessions...`, action: () => this.bulkEdit(g) },
        { label: `Back Up ${g.length} Configurations`, action: () => this.backUp(g) },
        { label: 'Change Checklist...', action: () => this.checklist(g) },
        { label: 'Run and Collect...', action: () => this.collect(g) },
        { label: allFav ? 'Remove from Favourites' : 'Add to Favourites', action: () => this.setFav(g, !allFav) },
        { separator: true },
        { label: `Delete ${g.length} Sessions`, action: () => this.deleteMany(g) },
      ]);
      return;
    }
    this.select(n);
    const items: MenuItem[] = n.isFolder
      ? [
          { label: 'Connect All in Folder', action: () => this.connectAll(n) },
          { label: 'Edit All Sessions in Folder...', action: () => this.bulkEdit(this.pathsIn(n)) },
          { label: 'Back Up Configurations', action: () => this.backUp([n.path + '/']) },
          { label: 'Change Checklist...', action: () => this.checklist([n.path + '/']) },
          { label: 'Run and Collect...', action: () => this.collect([n.path + '/']) },
          { separator: true },
          { label: 'New Session...', action: () => this.ui.newSession(n.path) },
          { label: 'New Folder...', action: () => this.newFolder(n.path) },
          { label: 'Rename...', action: () => this.renameFolder(n) },
          { label: 'Folder Defaults...', action: () => { this.ui.folderDefaultsFor.set(n.path); this.ui.open('folderDefaults'); } },
          ...((this.ui.settings()?.folderDefaults ?? []).find(d => d.folder === n.path)?.environment === 'production'
            ? [{ label: 'Unmark Production', action: () => this.setProduction(n.path, false) }]
            : this.folderEnv(n.path) === 'production' ? [] : [{ label: 'Mark as Production', action: () => this.setProduction(n.path, true) }]),
          { label: 'Export Folder...', action: () => this.ui.open('export') },
          { separator: true },
          { label: 'Delete Folder', action: () => this.deleteFolder(n) },
        ]
      : [
          ...(n.protocol === 'SSH2' ? [
            { label: 'Connect Terminal', action: () => void this.tabs.openSession(n.path, 'terminal') },
            ...(n.hopTarget ? [
              { label: `Connect SFTP Session to ${n.hostname}`, action: () => void this.tabs.openSession(n.path, 'sftp', 'jump') },
              { label: `Connect SFTP Session to ${n.hopTarget} (through ${n.hostname})`, action: () => void this.tabs.openSession(n.path, 'sftp', 'target') },
            ] : [{ label: 'Connect SFTP Session', action: () => void this.tabs.openSession(n.path, 'sftp') }]),
          ] : [{ label: 'Connect', action: () => this.connect(n) }]),
          { separator: true },
          { label: 'Properties...', action: () => this.ui.editSession(n.path) },
          { label: this.isFav(n.path) ? 'Remove from Favourites' : 'Add to Favourites', action: () => this.setFav([n.path], !this.isFav(n.path)) },
          ...(n.protocol === 'RDP' || n.protocol === 'VNC' || n.protocol === 'Local Shell' ? [] : [
            { label: 'Back Up Configuration', action: () => this.backUp([n.path]) },
            { label: 'Change Checklist...', action: () => this.checklist([n.path]) },
          ]),
          ...(n.protocol === 'RDP' || n.protocol === 'VNC' || n.protocol === 'Local Shell' || n.protocol === 'Serial' ? [] : [
            { label: 'Run and Collect...', action: () => this.collect([n.path]) },
          ]),
          ...(n.protocol === 'SSH2' || n.protocol === 'Telnet' ? [
            { label: 'Console Server Ports...', action: () => { this.ui.consoleServerFor.set(n.path); this.ui.open('consolePorts'); } },
          ] : []),
          { label: 'Duplicate', action: () => this.duplicate(n) },
          { label: 'Rename...', action: () => this.renameSession(n) },
          { label: 'Forget Saved Password', action: () => this.backend.forgetPassword(n.path).then(() => this.ui.toast('Password removed')) },
          { separator: true },
          { label: 'Delete', action: () => this.deleteSession(n) },
        ];
    this.ui.showMenu(ev, items);
  }

  checklist(targets: string[]) {
    this.ui.checklistTargets.set(targets);
    this.ui.open('checklists');
  }

  collect(targets: string[]) {
    this.ui.collectTargets.set(targets);
    this.ui.open('collect');
  }

  backUp(targets: string[]) {
    this.ui.backupTargets.set(targets);
    this.ui.open('backups');
  }

  bulkEdit(paths: string[]) {
    if (!paths.length) return;
    this.ui.bulkPaths.set(paths);
    this.ui.open('bulkEdit');
  }

  private pathsIn(n: SessionNode): string[] {
    const out: string[] = [];
    const walk = (x: SessionNode) => (x.children ?? []).forEach(c => (c.isFolder ? walk(c) : out.push(c.path)));
    walk(n);
    return out;
  }

  private async deleteMany(paths: string[]) {
    if (!(await this.ui.confirm('Delete Sessions', `Delete ${paths.length} sessions?\n\n${paths.slice(0, 12).join('\n')}${paths.length > 12 ? '\n…' : ''}`, 'Delete', true))) return;
    for (const p of paths) await this.backend.deleteSession(p).catch(e => this.ui.error(e));
    this.ui.selectedPaths.set(new Set());
    await this.ui.refreshTree();
  }

  private connectAll(n: SessionNode) {
    const walk = (x: SessionNode) => (x.children ?? []).forEach(c => (c.isFolder ? walk(c) : void this.tabs.openSession(c.path)));
    walk(n);
  }

  private async renameFolder(n: SessionNode) {
    const name = await this.ui.input('Rename Folder', 'Folder name', n.name);
    if (!name || name === n.name) return;
    const parent = n.path.includes('/') ? n.path.slice(0, n.path.lastIndexOf('/')) : '';
    try {
      await this.backend.renameFolder(n.path, parent ? parent + '/' + name : name);
      await this.ui.refreshTree();
    } catch (e) { this.ui.error(e); }
  }

  private async deleteFolder(n: SessionNode) {
    const count = this.countSessions(n);
    if (!(await this.ui.confirm('Delete Folder', `Delete folder "${n.path}" and ${count} session(s) in it?`, 'Delete', true))) return;
    try { await this.backend.deleteFolder(n.path); await this.ui.refreshTree(); } catch (e) { this.ui.error(e); }
  }

  private countSessions(n: SessionNode): number {
    return (n.children ?? []).reduce((a, c) => a + (c.isFolder ? this.countSessions(c) : 1), 0);
  }

  private async duplicate(n: SessionNode) {
    try {
      const p = await this.backend.duplicateSession(n.path);
      await this.ui.refreshTree();
      this.ui.selectedPath.set(p);
    } catch (e) { this.ui.error(e); }
  }

  private async renameSession(n: SessionNode) {
    const name = await this.ui.input('Rename Session', 'Session name', n.name);
    if (!name || name === n.name) return;
    try {
      const s = await this.backend.getSession(n.path);
      s.name = name;
      await this.backend.saveSession(s, n.path);
      await this.ui.refreshTree();
    } catch (e) { this.ui.error(e); }
  }

  private async deleteSession(n: SessionNode) {
    if (!(await this.ui.confirm('Delete Session', `Delete session "${n.path}"?`, 'Delete', true))) return;
    try { await this.backend.deleteSession(n.path); await this.ui.refreshTree(); } catch (e) { this.ui.error(e); }
  }

  dragStart(ev: DragEvent, n: SessionNode) {
    this.dragged = n;
    ev.dataTransfer?.setData('text/plain', n.path);
    if (ev.dataTransfer) ev.dataTransfer.effectAllowed = 'move';
  }

  allowDrop(ev: DragEvent, path = '') {
    if (!this.dragged) return;
    ev.preventDefault();
    ev.stopPropagation();
    this.dropTarget.set(path);
  }

  async dropOn(ev: DragEvent, folder: string) {
    ev.preventDefault();
    ev.stopPropagation();
    this.dropTarget.set('');
    const n = this.dragged;
    this.dragged = null;
    if (!n) return;
    const parent = n.path.includes('/') ? n.path.slice(0, n.path.lastIndexOf('/')) : '';
    if (parent === folder || n.path === folder) return;
    try {
      if (n.isFolder) {
        await this.backend.renameFolder(n.path, folder ? folder + '/' + n.name : n.name);
      } else {
        const sel = this.ui.selectedPaths();
        for (const p of sel.size > 1 && sel.has(n.path) ? [...sel] : [n.path]) {
          if (this.parentOf(p) === folder) continue;
          const s = await this.backend.getSession(p);
          s.folder = folder;
          await this.backend.saveSession(s, p);
        }
        this.ui.selectedPaths.set(new Set());
      }
      if (folder) this.expanded.update(s => new Set(s).add(folder));
      await this.ui.refreshTree();
    } catch (e) { this.ui.error(e); }
  }
}
