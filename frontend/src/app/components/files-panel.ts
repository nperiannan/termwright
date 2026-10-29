import { Component, OnDestroy, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { SUDO_ERR } from '../core/fixes';
import { FileEntry, formatBytes } from '../core/models';
import { Tabs, TabView } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { Autofocus } from '../core/autofocus';
import { FileIcon } from './file-icon';
import { Icon } from './icon';

interface Opened { remote: string; local: string; mod: number; changed: boolean; busy?: boolean; }

const store = {
  get(k: string, d: string) { try { return localStorage.getItem(k) ?? d; } catch { return d; } },
  set(k: string, v: string) { try { localStorage.setItem(k, v); } catch { /* ignore */ } },
};

/**
 * Files panel: a small SFTP browser beside an SSH terminal (opposite the
 * Session Manager). It follows the terminal's folder, a folder opened in it is
 * cd'd to in the terminal (when the shell is at a prompt), and a double-clicked
 * file opens on this computer with the system's program - saved changes are
 * offered for upload.
 */
@Component({
  selector: 'app-files-panel',
  imports: [FormsModule, Autofocus, FileIcon, Icon],
  host: { '[style.width.px]': 'width()', '[class.modern]': "ui.settings()?.layout !== 'classic'" },
  template: `
    <div class="grip" title="Drag to resize" (mousedown)="startResize($event)"></div>
    <div class="dh">
      <span class="t">Files</span><span class="h" [title]="hostTitle()">{{ hostShort() }}</span>@if (asUser()) { <b class="as" [title]="'Browsing as ' + asUser() + ', like the terminal'">{{ asUser() }}</b> }
      <button type="button" class="fol" [class.on]="follow() && followable()" [disabled]="!followable()"
        [title]="!followable() ? 'The terminal is on another host' : follow() ? 'Following the terminal folder - click to stop' : 'Follow the terminal folder'"
        (click)="setFollow(!follow())"><app-icon name="sync-right" [size]="12" />{{ follow() && followable() ? 'Following' : 'Follow' }}</button>
      <button type="button" class="ib" title="More" (click)="moreMenu($event)">⋯</button>
      <button type="button" class="ib" title="Hide the Files panel" (click)="hide()">✕</button>
    </div>
    @if (!followable() && sid()) {
      <div class="away">The terminal is on {{ termHost() }}.
        <a (click)="reopen()">Show {{ shortOf(termHost()) }} here</a></div>
    }
    @if (editing()) {
      <input class="path mono" [ngModel]="dir()" appAutofocus spellcheck="false"
        (keydown.enter)="editing.set(false); go($any($event.target).value)" (keydown.escape)="editing.set(false)" (blur)="editing.set(false)" />
    } @else {
      <div class="crumb mono" title="Click to type a path" (click)="editing.set(true)">
        @for (c of crumbs(); track $index) {
          @if (!$first && crumbs()[$index - 1].label !== '/') { <span class="cs">/</span> }
          <button type="button" class="cb" [class.last]="$last" [title]="c.path" (click)="$event.stopPropagation(); go(c.path)">{{ c.label }}</button>
        }
      </div>
    }
    <div class="tools">
      <button type="button" class="ib" title="Up" (click)="up()"><app-icon name="up" [size]="15" /></button>
      <button type="button" class="ib" title="Refresh" (click)="refresh()"><app-icon name="refresh" [size]="15" /></button>
      <button type="button" class="ib" title="New folder" (click)="mkdir()"><app-icon name="folder-plus" [size]="15" /></button>
      <button type="button" class="ib" title="Upload files here" (click)="upload()"><app-icon name="upload" [size]="15" /></button>
      <button type="button" class="ib" [disabled]="!selected().size" title="Download the selected files" (click)="download()"><app-icon name="download" [size]="15" /></button>
      <button type="button" class="ib del" [disabled]="!selected().size" title="Delete the selected files" (click)="remove()"><app-icon name="trash" [size]="15" /></button>
      <span class="grow"></span>
      <button type="button" class="ib" title="Open a full SFTP tab here" (click)="openTab()"><app-icon name="sftp" [size]="15" /></button>
    </div>
    <div class="list" tabindex="0" (keydown)="key($event)">
      @if (error()) { <div class="err">{{ error() }}</div> }
      @if (!sid() && !error()) { <div class="hint">{{ term().state === 'connected' ? 'Opening…' : 'Not connected' }}</div> }
      @if (dir() && dir() !== '/') {
        <div class="row" title="Up one folder" (dblclick)="up()"><span class="fold"></span><span class="n">..</span></div>
      }
      @for (e of shown(); track e.name) {
        <div class="row" [attr.data-n]="e.name" [class.sel]="selected().has(e.name)" (click)="select(e, $event)" (dblclick)="open(e)" (contextmenu)="menu($event, e)"
          [title]="e.name + (e.isDir ? '' : ' - ' + size(e.size)) + ' - ' + e.mode">
          @if (e.isDir) { <span class="fold" [class.dot]="e.name.startsWith('.')"></span> } @else { <app-file-icon [name]="e.name" [link]="e.isLink" [badge]="true" /> }
          <span class="n">{{ e.name }}</span>
          <span class="s">{{ e.isDir ? '' : size(e.size) }}</span>
        </div>
      }
    </div>
    @for (o of opened(); track o.local) {
      @if (o.changed) {
        <div class="edit">
          <div><b>{{ base(o.remote) }}</b> was saved on this computer.</div>
          <div class="es">Upload it to {{ o.remote }}?</div>
          <div class="eb"><button type="button" class="primary" [disabled]="o.busy" (click)="uploadOpened(o)">{{ o.busy ? 'Uploading…' : 'Upload Now' }}</button>
            <button type="button" (click)="ignoreChange(o)">Not Now</button>
            <button type="button" class="lnk" (click)="unwatch(o)">Stop Watching</button></div>
        </div>
      }
    }
    @if (xfer(); as x) {
      <div class="xf" [class.done]="x.state === 'done'" [class.error]="x.state === 'error'">
        <span>{{ x.direction === 'up' ? '↑' : '↓' }} {{ base(x.file) }}</span>
        @if (x.state === 'running') { <span class="bar"><i [style.width.%]="x.total ? x.bytes * 100 / x.total : 0"></i></span> }
        <span class="st">{{ x.state === 'running' ? (x.total ? pct(x.bytes, x.total) + '%' : '') : x.state === 'done' ? 'Done' : x.error || x.state }}</span>
      </div>
    }
    <div class="foot">{{ shown().length }} items@if (selected().size) { · {{ selected().size }} selected }@if (opened().length) { · watching {{ opened().length }} }</div>
  `,
  styles: [`
    :host { position: relative; flex: none; display: flex; flex-direction: column; min-width: 0; min-height: 0; background: var(--panel); border-left: 1px solid var(--border); font-size: calc(12.5px * var(--ui-scale, 1)); }
    :host-context(.tdock.left) { border-left: 0; border-right: 1px solid var(--border); .grip { left: auto; right: -3px; } }
    .grip { position: absolute; left: -3px; top: 0; bottom: 0; width: 6px; cursor: col-resize; z-index: 3; }
    .dh { flex: none; height: 38px; display: flex; align-items: center; gap: 6px; padding: 0 6px 0 12px; border-bottom: 1px solid var(--border);
      .t { font-weight: 650; } .as { flex: none; margin-left: -2px; padding: 0 6px; border-radius: 8px; background: var(--danger); color: #fff; font-weight: 600; font-size: calc(10.5px * var(--ui-scale, 1)); }
      .h { flex: 1; min-width: 0; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; } }
    .fol { height: 22px; padding: 0 8px; border-radius: 11px; display: inline-flex; align-items: center; gap: 4px; font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; white-space: nowrap; color: var(--muted);
      &.on { color: var(--ok, #22c55e); border-color: color-mix(in srgb, var(--ok, #22c55e) 45%, transparent); background: color-mix(in srgb, var(--ok, #22c55e) 13%, transparent); } }
    .ib { width: 26px; height: 26px; padding: 0; display: inline-grid; place-items: center; border: 0 !important; background: transparent !important; color: var(--muted); border-radius: 6px; flex: none;
      &:not(:disabled):hover { color: var(--text); background: var(--hover) !important; } &:disabled { opacity: .4; } }
    .del:not(:disabled):hover { color: var(--danger) !important; }
    .away { flex: none; margin: 8px 8px 0; padding: 7px 9px; border-radius: 6px; font-size: calc(12px * var(--ui-scale, 1)); line-height: 1.4;
      border: 1px dashed var(--border); color: var(--muted); a { color: var(--accent); cursor: pointer; display: block; } }
    .crumb, .path { flex: none; margin: 8px 8px 4px; height: 28px; border: 1px solid var(--border); border-radius: 6px; background: var(--input-bg); }
    .path { padding: 0 8px; font-size: calc(12px * var(--ui-scale, 1)); }
    .crumb { display: flex; align-items: center; gap: 1px; padding: 0 4px; overflow: hidden; white-space: nowrap; cursor: text; font-size: calc(12px * var(--ui-scale, 1)); color: var(--muted);
      .cs { color: var(--faint, var(--muted)); padding: 0 1px; }
      .cb { height: 22px; padding: 0 3px; border: 0 !important; background: transparent !important; color: var(--muted); font: inherit; flex: 0 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; border-radius: 4px;
        &:hover { color: var(--text); background: var(--hover) !important; } &.last { color: var(--text); font-weight: 500; } } }
    .tools { flex: none; display: flex; align-items: center; gap: 1px; padding: 0 6px 6px; border-bottom: 1px solid var(--border); .grow { flex: 1; } }
    .list { flex: 1; min-height: 0; overflow: auto; outline: none; user-select: none; padding: 2px 0; }
    .row { height: 28px; display: flex; align-items: center; gap: 8px; padding: 0 10px; cursor: default; white-space: nowrap;
      .n { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; } .s { color: var(--muted); font-size: calc(11px * var(--ui-scale, 1)); font-variant-numeric: tabular-nums; }
      &:hover { background: var(--hover); } &.sel { background: var(--selected); } }
    .fold { width: 18px; height: 14px; flex: none; background: #e5a93b; border-radius: 3px 3px 4px 4px; clip-path: polygon(0 18%, 38% 18%, 48% 0, 100% 0, 100% 100%, 0 100%); &.dot { opacity: .65; } }
    .err { padding: 10px 12px; color: var(--danger); } .hint { padding: 10px 12px; color: var(--muted); }
    .edit { flex: none; margin: 6px 8px; padding: 8px 10px; border-radius: 8px; font-size: calc(12px * var(--ui-scale, 1)); line-height: 1.4;
      background: color-mix(in srgb, var(--accent) 11%, transparent); border: 1px solid color-mix(in srgb, var(--accent) 40%, transparent);
      .es { color: var(--muted); word-break: break-all; } .eb { display: flex; gap: 6px; margin-top: 6px; flex-wrap: wrap; button { height: 24px; font-size: calc(11.5px * var(--ui-scale, 1)); } .lnk { border: 0; background: none; color: var(--muted); } } }
    .xf { flex: none; display: flex; align-items: center; gap: 8px; padding: 5px 10px; border-top: 1px solid var(--border); font-size: calc(11.5px * var(--ui-scale, 1)); white-space: nowrap;
      > span:first-child { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; }
      .bar { width: 70px; height: 5px; border-radius: 3px; background: var(--hover); overflow: hidden; i { display: block; height: 100%; background: var(--accent); } }
      .st { color: var(--muted); } &.done .st { color: var(--ok, #22c55e); } &.error .st { color: var(--danger); } }
    .foot { flex: none; height: 28px; display: flex; align-items: center; padding: 0 12px; border-top: 1px solid var(--border); color: var(--muted); font-size: calc(11px * var(--ui-scale, 1)); white-space: nowrap; overflow: hidden; }
    :host(.modern) { background: var(--surface); border-color: var(--line);
      .dh, .tools, .xf, .foot { border-color: var(--line); } .crumb, .path { border-color: var(--line); border-radius: var(--r); }
      .row:hover { background: var(--surface-2); } .row.sel { background: var(--accent-soft); } }
  `],
})
export class FilesPanel implements OnDestroy {
  readonly term = input.required<TabView>();
  protected ui = inject(Ui);
  private tabs = inject(Tabs);
  private backend = inject(Backend);

  readonly sid = signal('');
  /** The hop target this panel browses ('' = the host logged in to). */
  private target = signal('');
  readonly dir = signal('');
  readonly entries = signal<FileEntry[]>([]);
  readonly selected = signal<Set<string>>(new Set());
  readonly error = signal('');
  readonly editing = signal(false);
  readonly follow = signal(store.get('termwright.filesFollow', '1') === '1');
  readonly cdTerm = signal(store.get('termwright.filesCd', '1') === '1');
  readonly hidden = signal(store.get('termwright.filesHidden', '1') === '1');
  readonly width = signal(Math.min(640, Math.max(220, Number(store.get('termwright.filesWidth', '320')) || 320)));
  readonly opened = signal<Opened[]>([]);
  private xferIds = new Set<string>();
  private home = '';
  private last = '';
  private opening = false;
  /** Opening failed while connected: not retried until the terminal reconnects (Refresh retries). */
  private failed = false;
  private poll?: ReturnType<typeof setInterval>;
  private offClosed = this.backend.on('sftp:closed', (id: string) => { if (id === this.sid()) { this.sid.set(''); this.entries.set([]); this.asUser.set(''); this.triedUser = ''; } });
  /** The user this panel browses as when not the login user (root after "sudo su -" in the terminal). */
  readonly asUser = signal('');
  private userBusy = false;
  private triedUser = '';

  readonly shown = computed(() => (this.hidden() ? this.entries() : this.entries().filter(e => !e.name.startsWith('.'))));
  /** The live terminal tab. */
  private live = computed(() => this.tabs.get(this.term().id) ?? this.term());
  /** The terminal's shell is on the host this panel browses. */
  readonly followable = computed(() => {
    const t = this.live();
    return !t.hopTarget || !!this.target() === !this.tabs.onJump(t);
  });
  readonly termHost = computed(() => { const t = this.live(); return this.tabs.onJump(t) ? t.session.hostname : t.hopTarget || t.session.hostname; });
  readonly hostTitle = computed(() => this.target() ? this.target() + ' (through ' + this.live().session.hostname + ')' : this.live().session.hostname);
  readonly hostShort = computed(() => this.shortOf(this.target() || this.live().session.hostname));
  readonly xfer = computed(() => this.tabs.transfers().find(x => this.xferIds.has(x.id)));

  constructor() {
    // Open (and reopen after a reconnect) while the terminal is connected.
    effect(() => {
      const t = this.live();
      if (t.state !== 'connected') { this.failed = false; return; }
      if (!this.sid() && !this.failed) untracked(() => void this.start());
    });
    // Follow the terminal's folder.
    effect(() => {
      const cwd = this.live().cwd;
      if (!cwd || !this.follow() || !this.followable() || !this.sid()) return;
      untracked(() => { if (cwd !== this.dir() && !this.userBusy && (this.userChange() === null || this.triedUser === (this.live().shellUser ?? ''))) void this.load(cwd); });
    });
    // Follow the terminal's user: after "sudo su -" the panel browses as root
    // too (sudo sftp-server), after "exit" as the login user again.
    effect(() => {
      const u = this.live().shellUser;
      if (!u || !this.sid() || !this.followable() || this.ui.settings()?.sftpFollowUser === false) return;
      untracked(() => {
        const c = this.userChange();
        if (c === null || u === this.triedUser || this.userBusy) return;
        this.triedUser = u;
        void this.switchUser(c);
      });
    });
  }

  ngOnDestroy() {
    this.offClosed();
    clearInterval(this.poll);
    if (this.sid()) void this.backend.sftpClose(this.sid()).catch(() => {});
  }

  shortOf(h: string) { h = h.slice(h.lastIndexOf('@') + 1); return /^[\d.]+$|:/.test(h) ? h : h.split('.')[0]; }
  base(p: string) { return p.slice(Math.max(p.lastIndexOf('/'), p.lastIndexOf('\\')) + 1); }
  size(n: number) { return formatBytes(n); }
  pct(a: number, b: number) { return Math.round(a * 100 / b); }

  private async start(mode?: 'jump' | 'target') {
    if (this.opening) return;
    this.opening = true;
    this.error.set('');
    const t = this.live();
    if (!mode) mode = t.hopTarget ? (this.tabs.onJump(t) ? 'jump' : 'target') : undefined;
    try {
      let r: { id: string; home: string; target: string };
      try { r = await this.backend.sftpOpen(t.id, mode ?? ''); }
      catch (e) {
        if (mode !== 'target') throw e;
        r = await this.backend.sftpOpen(t.id, 'jump');
      }
      this.target.set(r.target || '');
      this.home = r.home;
      this.asUser.set('');
      this.triedUser = '';
      this.sid.set(r.id);
      const cwd = this.follow() && this.followable() ? this.live().cwd : '';
      const c = this.userChange();
      if (c !== null) { this.triedUser = this.live().shellUser ?? ''; await this.switchUser(c); }
      else await this.load(cwd || this.dir() || r.home);
      void this.tabs.detectCwd(this.live(), r.id);
    } catch (e) { this.failed = true; this.error.set('Files: ' + errText(e)); }
    finally { this.opening = false; }
  }

  /** The login name on the host this panel browses ('' when not known). */
  private loginName() {
    const t = this.target();
    if (t) return t.includes('@') ? t.slice(0, t.lastIndexOf('@')) : '';
    return this.live().session.username || '';
  }

  /** How to switch to the terminal's user: '' = back to the login user,
   *  'sudo' = root, a Run As command; null = nothing to change. */
  private userChange(): string | null {
    const u = this.live().shellUser;
    if (!u || this.ui.settings()?.sftpFollowUser === false) return null;
    const login = this.loginName();
    if (u === (this.asUser() || login) || (!login && u !== 'root')) return null;
    return u === login ? '' : u === 'root' ? 'sudo' : 'sudo su - ' + u;
  }

  /** Browses as another user: '' = the login user, 'sudo' = root, otherwise a Run As command. */
  async switchUser(cmd: string) {
    const id = this.sid();
    if (!id) return;
    this.userBusy = true;
    try {
      let home: string, user: string;
      if (cmd === 'sudo') { home = await this.backend.sftpSetRoot(id, true); user = 'root'; }
      else { const r = await this.backend.sftpRunAs(id, cmd); home = r.home; user = r.user; }
      this.asUser.set(user);
      const cwd = this.follow() && this.followable() ? this.live().cwd : '';
      await this.load(cwd || home);
      if (cwd && this.error()) await this.load(home);
    } catch (e) {
      const msg = errText(e);
      this.error.set(SUDO_ERR.test(msg) ? 'Cannot browse as ' + (cmd === 'sudo' ? 'root' : cmd.replace(/^sudo su - /, '')) + ': ' + msg : msg);
    }
    this.userBusy = false;
  }

  /** Reopens the panel on the host the terminal is on now. */
  async reopen() {
    const old = this.sid();
    this.sid.set('');
    if (old) await this.backend.sftpClose(old).catch(() => {});
    this.dir.set('');
    await this.start(this.tabs.onJump(this.live()) ? 'jump' : 'target');
  }

  refresh() { if (this.sid()) void this.load(this.dir()); else { this.failed = false; void this.start(); } }

  async load(d: string) {
    const id = this.sid();
    if (!id || !d) return;
    try {
      const list = (await this.backend.sftpList(id, d)) ?? [];
      this.dir.set(d);
      this.entries.set(list);
      this.error.set('');
      if (d !== this.last) this.selected.set(new Set());
      this.last = d;
    } catch (e) { this.error.set(errText(e)); }
  }

  /** Opens a folder; with "cd the terminal" on, the terminal goes there too. */
  async go(d: string) {
    d = d.trim();
    if (!d) return;
    if (!d.startsWith('/')) d = await this.backend.sftpRealPath(this.sid(), this.join(d)).catch(() => d);
    await this.load(d);
    const t = this.live();
    if (this.cdTerm() && this.followable() && t.state === 'connected' && this.dir() === d && t.cwd !== d && this.tabs.atPrompt(t.id))
      void this.backend.sendInput(t.id, ` cd '${d.replace(/'/g, `'\\''`)}'\r`);
  }
  async up() {
    const d = this.dir();
    if (!d || d === '/') return;
    await this.go(d.replace(/\/[^/]+\/?$/, '') || '/');
  }
  private join(n: string) { const d = this.dir(); return (d.endsWith('/') ? d : d + '/') + n; }

  crumbs(): { label: string; path: string }[] {
    const d = this.dir();
    if (!d) return [];
    const parts = d.split('/').filter(Boolean);
    const out = [{ label: '/', path: '/' }, ...parts.map((x, i) => ({ label: x, path: '/' + parts.slice(0, i + 1).join('/') }))];
    return out.length > 4 ? [out[0], { label: '…', path: out[out.length - 3].path }, ...out.slice(-2)] : out;
  }

  private anchor = '';
  select(e: FileEntry, ev: MouseEvent) {
    this.selected.update(s => {
      let n = new Set(s);
      if (ev.shiftKey && this.anchor) {
        const names = this.shown().map(f => f.name);
        const [a, b] = [names.indexOf(this.anchor), names.indexOf(e.name)].sort((x, y) => x - y);
        if (a >= 0) names.slice(a, b + 1).forEach(x => n.add(x));
      } else if (ev.metaKey || ev.ctrlKey) { if (n.has(e.name)) n.delete(e.name); else n.add(e.name); }
      else n = new Set([e.name]);
      return n;
    });
    if (!ev.shiftKey) this.anchor = e.name;
  }

  key(ev: KeyboardEvent) {
    const names = this.shown().map(e => e.name);
    if (ev.key === 'ArrowDown' || ev.key === 'ArrowUp') {
      ev.preventDefault();
      if (!names.length) return;
      const cur = names.indexOf(this.anchor);
      const i = cur < 0 ? 0 : Math.min(names.length - 1, Math.max(0, cur + (ev.key === 'ArrowDown' ? 1 : -1)));
      this.anchor = names[i];
      this.selected.set(new Set([names[i]]));
      const list = ev.currentTarget as HTMLElement;
      queueMicrotask(() => list.querySelector(`[data-n="${CSS.escape(names[i])}"]`)?.scrollIntoView({ block: 'nearest' }));
    } else if (ev.key === 'Enter') {
      const e = this.shown().find(x => x.name === this.anchor);
      if (e) void this.open(e);
    } else if (ev.key === 'Backspace') void this.up();
    else if (ev.key === 'Delete') void this.remove();
  }

  /** Double-click: a folder opens, a file opens on this computer. */
  async open(e: FileEntry) {
    if (e.isDir) { await this.go(this.join(e.name)); return; }
    const remote = this.join(e.name);
    this.ui.toast('Opening ' + e.name + '…', 'info', 2500);
    try {
      const r = await this.backend.sftpOpenFile(this.sid(), remote);
      this.opened.update(l => [...l.filter(o => o.remote !== remote), { remote, local: r.local, mod: r.mod, changed: false }]);
      this.watch();
    } catch (err) { this.ui.error(err); }
  }

  private watch() {
    if (this.poll) return;
    this.poll = setInterval(async () => {
      const l = this.opened();
      if (!l.length) { clearInterval(this.poll); this.poll = undefined; return; }
      for (const o of l) {
        if (o.changed || o.busy) continue;
        const m = await this.backend.openedFileMod(o.local).catch(() => 0);
        if (m && m !== o.mod) this.patchOpened(o, { changed: true, mod: m });
      }
    }, 2000);
  }
  private patchOpened(o: Opened, p: Partial<Opened>) { this.opened.update(l => l.map(x => (x.local === o.local ? { ...x, ...p } : x))); }
  async uploadOpened(o: Opened) {
    this.patchOpened(o, { busy: true });
    try {
      await this.backend.sftpUploadOpened(this.sid(), o.local, o.remote);
      this.patchOpened(o, { busy: false, changed: false });
      this.ui.toast('Uploaded ' + this.base(o.remote), 'success');
      if (o.remote.startsWith(this.dir())) void this.load(this.dir());
    } catch (e) { this.patchOpened(o, { busy: false }); this.ui.error(e); }
  }
  ignoreChange(o: Opened) { this.patchOpened(o, { changed: false }); }
  unwatch(o: Opened) { this.opened.update(l => l.filter(x => x.local !== o.local)); }

  async mkdir() {
    const n = await this.ui.input('New Folder', 'Folder name');
    if (!n) return;
    try { await this.backend.sftpMkdir(this.sid(), this.join(n)); await this.load(this.dir()); } catch (e) { this.ui.error(e); }
  }
  async upload() {
    const files = await this.backend.chooseOpenFiles('Upload to ' + this.dir()).catch(() => null);
    if (files?.length) await this.transfer('up', files, this.dir());
  }
  async download() {
    const names = [...this.selected()];
    if (!names.length) return;
    const dest = await this.backend.chooseDirectory('Download to').catch(() => '');
    if (dest) await this.transfer('down', names.map(n => this.join(n)), dest);
  }
  private async transfer(dir: 'up' | 'down', src: string[], dest: string) {
    try {
      const id = await this.backend.startTransfer(this.sid(), dir, src, dest);
      if (id) this.xferIds = new Set([id]);
      const off = this.backend.on('xfer:progress', (t: { id: string; state: string }) => {
        if (t.id === id && t.state !== 'running') { off(); if (dir === 'up') void this.load(this.dir()); }
      });
    } catch (e) { this.ui.error(e); }
  }
  async remove(names = [...this.selected()]) {
    if (!names.length) return;
    const what = names.length === 1 ? names[0] : names.length + ' items';
    if (!(await this.ui.confirm('Delete', `Delete ${what} on the server? This cannot be undone; folders are deleted with their contents.`, 'Delete', true))) return;
    for (const n of names) await this.backend.sftpRemove(this.sid(), this.join(n)).catch(e => this.ui.error(e));
    await this.load(this.dir());
  }
  async rename(e: FileEntry) {
    const n = await this.ui.input('Rename', 'New name', e.name);
    if (!n || n === e.name) return;
    try { await this.backend.sftpRename(this.sid(), this.join(e.name), this.join(n)); await this.load(this.dir()); } catch (err) { this.ui.error(err); }
  }

  menu(ev: MouseEvent, e: FileEntry) {
    ev.preventDefault();
    if (!this.selected().has(e.name)) { this.selected.set(new Set([e.name])); this.anchor = e.name; }
    const path = this.join(e.name);
    const t = this.live();
    this.ui.showMenu(ev, [
      { label: 'Open', hint: 'double-click', action: () => void this.open(e) },
      { label: 'Download…', action: () => void this.download() },
      { separator: true },
      { label: 'cd Here in the Terminal', disabled: !this.followable() || t.state !== 'connected',
        action: () => { void this.backend.sendInput(t.id, ` cd '${(e.isDir ? path : this.dir()).replace(/'/g, `'\\''`)}'\r`); this.tabs.handles.get(t.id)?.term.focus(); } },
      { label: 'Copy Path', action: () => void this.backend.clipboardSet(path) },
      { label: 'Rename…', action: () => void this.rename(e) },
      { label: 'Delete…', action: () => void this.remove() },
    ]);
  }

  moreMenu(ev: MouseEvent) {
    this.ui.showMenu(ev, [
      { label: (this.follow() ? '✓ ' : '') + 'Follow the Terminal Folder', action: () => this.setFollow(!this.follow()) },
      { label: (this.cdTerm() ? '✓ ' : '') + 'cd the Terminal When I Open a Folder Here', action: () => { this.cdTerm.set(!this.cdTerm()); store.set('termwright.filesCd', this.cdTerm() ? '1' : '0'); } },
      { label: (this.hidden() ? '✓ ' : '') + 'Show Hidden Files', action: () => { this.hidden.set(!this.hidden()); store.set('termwright.filesHidden', this.hidden() ? '1' : '0'); } },
      { separator: true },
      { label: 'Go to Home Folder', action: () => void this.go(this.home || '~') },
      ...(this.asUser() ? [{ label: 'Browse as ' + (this.loginName() || 'the Login User'), action: () => { this.triedUser = this.live().shellUser ?? ''; void this.switchUser(''); } }]
        : [{ label: 'Browse as root (sudo)', action: () => { this.triedUser = this.live().shellUser ?? ''; void this.switchUser('sudo'); } }]),
      { label: 'Turn On Folder Detection in the Terminal', action: () => void this.tabs.enableCwdReporting(this.live().id, false, true) },
      { label: 'Open a Full SFTP Tab', action: () => this.openTab() },
      { separator: true },
      { label: 'Hide the Files Panel', action: () => this.hide() },
    ]);
  }

  setFollow(v: boolean) {
    this.follow.set(v);
    store.set('termwright.filesFollow', v ? '1' : '0');
    const cwd = this.live().cwd;
    if (v && cwd && this.followable()) void this.load(cwd);
    else if (v && !cwd && this.sid()) void this.tabs.detectCwd(this.live(), this.sid());
  }
  openTab() { void this.tabs.openSftp(this.live().id, this.target() ? 'target' : this.live().hopTarget ? 'jump' : ''); }
  hide() { this.tabs.patch(this.live().id, { filesPanel: false }); }

  startResize(ev: MouseEvent) {
    ev.preventDefault();
    const x0 = ev.clientX, w0 = this.width();
    const left = !!(ev.currentTarget as HTMLElement).closest('.tdock.left');
    const move = (e: MouseEvent) => this.width.set(Math.min(640, Math.max(220, w0 + (left ? e.clientX - x0 : x0 - e.clientX))));
    const upFn = () => { window.removeEventListener('mousemove', move); window.removeEventListener('mouseup', upFn); store.set('termwright.filesWidth', String(this.width())); };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', upFn);
  }
}
