import { Component, OnInit, inject, input, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { FileEntry, formatBytes } from '../core/models';
import { Tabs, TabView } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { Icon } from './icon';

interface Pane {
  side: 'local' | 'remote';
  dir: string;
  entries: FileEntry[];
  selected: Set<string>;
  loading: boolean;
  error: string;
}

/** Dual-pane SFTP file browser (local | remote) with transfer queue. */
@Component({
  selector: 'app-sftp-view',
  imports: [FormsModule, DatePipe, Icon],
  template: `
    <div class="panes">
      @for (p of [local(), remote()]; track p.side) {
        <section class="pane" [class.focus]="focus() === p.side" (mousedown)="focus.set(p.side)">
          <header>
            <span class="label" [title]="p.side === 'remote' && tab().hopTarget ? 'Files on ' + tab().hopTarget + ', reached through the jump station ' + tab().session.hostname : ''">{{ p.side === 'local' ? 'This computer' : (tab().root ? tab().runAsUser + ' @ ' + hostOnly() : tab().hopTarget || tab().session.hostname || 'Server') }}@if (p.side === 'remote' && tab().hopTarget) {<span class="via-jump"> via {{ tab().session.hostname }}</span>}</span>
            <button class="icon-btn" title="Up" (click)="up(p)"><app-icon name="up" /></button>
            <input class="path mono" [ngModel]="p.dir" (keydown.enter)="go(p, $any($event.target).value)" spellcheck="false" />
            <button class="icon-btn" title="Refresh" (click)="load(p)"><app-icon name="refresh" /></button>
            <button class="icon-btn" title="New folder" (click)="mkdir(p)"><app-icon name="folder-plus" /></button>
            @if (p.side === 'remote') {
              <button class="root-btn" [class.on]="tab().root" [disabled]="rootBusy()" (click)="runAsMenu($event)"
                [title]="tab().root ? 'Browsing as ' + tab().runAsUser + '. Click to switch back or to another user.' : 'Browse as root or another user (sudo, su, …)'">
                {{ rootBusy() ? '…' : tab().root ? 'as ' + tab().runAsUser + ' ▾' : 'Run as ▾' }}</button>
            }
            <button class="icon-btn bm" [class.on]="isBookmarked(p)" title="Bookmarks" (click)="bookmarkMenu($event, p)">{{ isBookmarked(p) ? '★' : '☆' }}</button>
          </header>
          <div class="list" tabindex="0" (keydown)="key($event, p)">
            <div class="row head"><span class="n">Name</span><span class="s">Size</span><span class="d">Modified</span><span class="m">Mode</span></div>
            @if (p.error) { <div class="err pad">{{ p.error }}</div> }
            @for (e of p.entries; track e.name) {
              <div class="row" [class.sel]="p.selected.has(e.name)" (click)="select(p, e, $event)" (dblclick)="open(p, e)"
                   (contextmenu)="menu($event, p, e)">
                <span class="n"><app-icon [name]="e.isDir ? 'folder' : 'file'" />{{ e.name }}</span>
                <span class="s">{{ e.isDir ? '' : size(e.size) }}</span>
                <span class="d">{{ e.modTime | date: 'yyyy-MM-dd HH:mm' }}</span>
                <span class="m mono">{{ e.mode }}</span>
              </div>
            }
          </div>
          <footer>{{ p.entries.length }} items @if (p.selected.size) { · {{ p.selected.size }} selected }</footer>
        </section>
        @if (p.side === 'local') {
          <div class="mid">
            <div class="via" role="radiogroup" title="Protocol used by → and ← (browsing always uses SFTP). SCP helps with devices whose SFTP server is slow or limited.">
              <span class="via-label">Copy via</span>
              <button type="button" role="radio" [class.on]="via() === 'sftp'" [attr.aria-checked]="via() === 'sftp'" (click)="setVia('sftp')">SFTP</button>
              <button type="button" role="radio" [class.on]="via() === 'scp'" [attr.aria-checked]="via() === 'scp'" (click)="setVia('scp')"
                [disabled]="tab().root" [attr.title]="tab().root ? 'SCP cannot copy as another user; SFTP is used' : null">SCP</button>
            </div>
            <button [title]="'Upload selected to server via ' + via().toUpperCase()" [disabled]="!local().selected.size" (click)="transfer('up')">→</button>
            <button [title]="'Download selected to this computer via ' + via().toUpperCase()" [disabled]="!remote().selected.size" (click)="transfer('down')">←</button>
            <span class="sync-label">Sync</span>
            <button class="sync" title="Sync folder contents up: copy new and changed files from this computer to the server (nothing is deleted)" (click)="sync('up')">⇉</button>
            <button class="sync" title="Sync folder contents down: copy new and changed files from the server to this computer (nothing is deleted)" (click)="sync('down')">⇇</button>
          </div>
        }
      }
    </div>
    @if (tabs.transfers().length) {
      <div class="queue">
        @for (t of tabs.transfers(); track t.id) {
          <div class="xfer" [class]="t.state">
            <span class="dir">{{ dirGlyph(t.direction) }}</span>
            <span class="file mono" [title]="t.file">{{ t.file }}</span>
            <progress [max]="t.total || 1" [value]="t.bytes"></progress>
            <span class="num">{{ size(t.bytes) }} / {{ size(t.total) }} · {{ t.filesDone }}/{{ t.filesTotal }}</span>
            <span class="st">{{ t.state === 'error' ? t.error : t.state }}</span>
            @if (t.state === 'running') { <button class="icon-btn" (click)="cancel(t.id)">✕</button> }
            @if (t.resumable && (t.state === 'error' || t.state === 'cancelled')) {
              <button class="icon-btn" title="Resume: continue partial files, skip completed ones" (click)="resume(t.id)">⟳</button>
            }
          </div>
        }
        <button class="clear" (click)="clearDone()">Clear finished</button>
      </div>
    }
  `,
  styleUrl: './sftp-view.scss',
})
export class SftpView implements OnInit {
  readonly tab = input.required<TabView>();
  protected tabs = inject(Tabs);
  private backend = inject(Backend);
  private ui = inject(Ui);
  readonly focus = signal<'local' | 'remote'>('remote');
  readonly local = signal<Pane>({ side: 'local', dir: '', entries: [], selected: new Set(), loading: false, error: '' });
  readonly remote = signal<Pane>({ side: 'remote', dir: '', entries: [], selected: new Set(), loading: false, error: '' });
  private lastClicked = '';
  /** Protocol used by → / ← (browsing always uses SFTP). */
  readonly via = signal<'sftp' | 'scp'>('sftp');
  readonly size = formatBytes;

  private viaKey() { return 'termwright.sftpVia:' + (this.tab().session.path || this.tab().session.hostname) + (this.tab().hopTarget ? '>' + this.tab().hopTarget : ''); }
  /** Remembers SFTP/SCP per session on this computer. */
  setVia(v: 'sftp' | 'scp') {
    this.via.set(v);
    try { if (v === 'scp') localStorage.setItem(this.viaKey(), v); else localStorage.removeItem(this.viaKey()); } catch { /* ignore */ }
  }

  readonly rootBusy = signal(false);
  hostOnly() { const h = this.tab().hopTarget || this.tab().session.hostname; return h.slice(h.lastIndexOf('@') + 1); }
  loginUser() { return this.tab().hopTarget || (this.tab().session.username ? this.tab().session.username + '@' : '') + this.tab().session.hostname; }

  private recentKey() { return 'termwright.runAs:' + (this.tab().session.path || this.tab().session.hostname) + (this.tab().hopTarget ? '>' + this.tab().hopTarget : ''); }
  private recent(): string[] {
    try { return JSON.parse(localStorage.getItem(this.recentKey()) || '[]'); } catch { return []; }
  }

  runAsMenu(ev: MouseEvent) {
    const t = this.tab();
    this.ui.showMenu(ev, [
      { label: 'root (sudo)', action: () => this.switchUser('sudo') },
      ...this.recent().map(c => ({ label: c, action: () => this.switchUser(c) })),
      { label: 'Other Command...', action: () => this.askCommand() },
      ...(t.root ? [{ separator: true }, { label: 'Back to ' + this.loginUser(), action: () => this.switchUser('') }] : []),
    ]);
  }

  async askCommand() {
    const c = await this.ui.input('Run As', 'Command that switches user, e.g. sudo su - oracle, su - admin, sudo -iu postgres', this.recent()[0] ?? 'sudo su - ');
    if (c?.trim()) await this.switchUser(c.trim());
  }

  /** '' = login user, 'sudo' = root via sudo sftp-server, otherwise a Run As command. */
  async switchUser(cmd: string) {
    this.rootBusy.set(true);
    try {
      let home: string, user: string;
      if (cmd === 'sudo') { home = await this.backend.sftpSetRoot(this.sid(), true); user = 'root'; }
      else if (cmd === '') { const r = await this.backend.sftpRunAs(this.sid(), ''); home = r.home; user = ''; }
      else {
        const r = await this.backend.sftpRunAs(this.sid(), cmd);
        home = r.home; user = r.user;
        try { localStorage.setItem(this.recentKey(), JSON.stringify([cmd, ...this.recent().filter(x => x !== cmd)].slice(0, 6))); } catch { /* ignore */ }
      }
      this.tabs.patch(this.sid(), { root: !!user, runAsUser: user });
      if (user) this.via.set('sftp');
      await this.go(this.remote(), home);
    } catch (e) { this.ui.error(e); }
    this.rootBusy.set(false);
  }

  async ngOnInit() {
    try { if (localStorage.getItem(this.viaKey()) === 'scp') this.via.set('scp'); } catch { /* ignore */ }
    const home = await this.backend.localHome().catch(() => '/');
    this.local.update(p => ({ ...p, dir: home }));
    this.remote.update(p => ({ ...p, dir: this.tab().home || '/' }));
    await Promise.all([this.load(this.local()), this.load(this.remote())]);
  }

  private sig(p: Pane) { return p.side === 'local' ? this.local : this.remote; }
  private sid() { return this.tab().id; }

  private join(p: Pane, name: string) {
    if (p.side === 'remote') return (p.dir.endsWith('/') ? p.dir : p.dir + '/') + name;
    const sep = p.dir.includes('\\') && !p.dir.includes('/') ? '\\' : '/';
    return (p.dir.endsWith(sep) ? p.dir : p.dir + sep) + name;
  }

  async load(p: Pane) {
    const s = this.sig(p);
    s.update(x => ({ ...x, loading: true, error: '' }));
    try {
      const list = p.side === 'local' ? await this.backend.localList(p.dir) : await this.backend.sftpList(this.sid(), p.dir);
      s.update(x => ({ ...x, entries: list ?? [], selected: new Set(), loading: false }));
      if (p.side === 'remote') this.tabs.sftpDirs.set(this.sid(), p.dir);
    } catch (e) {
      s.update(x => ({ ...x, entries: [], loading: false, error: e instanceof Error ? e.message : String(e) }));
    }
  }

  async go(p: Pane, dir: string) {
    this.sig(p).update(x => ({ ...x, dir }));
    await this.load(this.sig(p)());
  }

  async up(p: Pane) {
    const parent = p.side === 'local'
      ? await this.backend.localParent(p.dir)
      : await this.backend.sftpRealPath(this.sid(), this.join(p, '..')).catch(() => '/');
    await this.go(p, parent);
  }

  open(p: Pane, e: FileEntry) {
    if (e.isDir) void this.go(p, this.join(p, e.name));
    else void this.transfer(p.side === 'local' ? 'up' : 'down', [e.name], p.side);
  }

  select(p: Pane, e: FileEntry, ev: MouseEvent) {
    this.sig(p).update(x => {
      let sel = new Set(x.selected);
      if (ev.shiftKey && this.lastClicked) {
        const names = x.entries.map(f => f.name);
        const [a, b] = [names.indexOf(this.lastClicked), names.indexOf(e.name)].sort((m, n) => m - n);
        names.slice(a, b + 1).forEach(n => sel.add(n));
      } else if (ev.ctrlKey || ev.metaKey) {
        if (sel.has(e.name)) sel.delete(e.name); else sel.add(e.name);
      } else {
        sel = new Set([e.name]);
      }
      return { ...x, selected: sel };
    });
    this.lastClicked = e.name;
  }

  key(ev: KeyboardEvent, p: Pane) {
    if (ev.key === 'Delete') void this.remove(p);
    else if (ev.key === 'Backspace') void this.up(p);
    else if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === 'a') {
      ev.preventDefault();
      this.sig(p).update(x => ({ ...x, selected: new Set(x.entries.map(e => e.name)) }));
    }
  }

  dirGlyph(d: string) {
    const g: Record<string, string> = { up: '↑', down: '↓', s2s: '⇄', 'scp-up': '↑ SCP', 'scp-down': '↓ SCP' };
    return g[d] ?? 'X';
  }

  async transfer(dir: 'up' | 'down', names?: string[], side?: 'local' | 'remote') {
    const from = dir === 'up' ? this.local() : this.remote();
    const to = dir === 'up' ? this.remote() : this.local();
    const sel = names ?? [...from.selected];
    if (!sel.length || (side && side !== from.side)) return;
    if (this.via() === 'scp' && !this.tab().root) { await this.scp(dir, sel.map(n => this.join(from, n)), to); return; }
    try {
      await this.backend.startTransfer(this.sid(), dir, sel.map(n => this.join(from, n)), to.dir);
      const unwatch = this.backend.on('xfer:progress', t => {
        if (t.state !== 'running') { unwatch(); void this.load(this.sig(to)()); }
      });
    } catch (e) { this.ui.error(e); }
  }

  /** Copies with SCP over the same SSH connection, one transfer per item (folders recursively). */
  private async scp(dir: 'up' | 'down', srcs: string[], to: Pane) {
    const ids: string[] = [], ended = new Set<string>();
    let started = false;
    const check = () => {
      if (started && ids.every(id => ended.has(id))) { unwatch(); if (ids.length) void this.load(this.sig(to)()); }
    };
    const unwatch = this.backend.on('xfer:progress', t => { if (t.state !== 'running') { ended.add(t.id); check(); } });
    try {
      for (const src of srcs) ids.push(await this.backend.scpTransferSftp(this.sid(), dir, src, to.dir));
    } catch (e) { this.ui.error(e); }
    started = true;
    check();
  }

  async sync(dir: 'up' | 'down') {
    const l = this.local().dir, r = this.remote().dir;
    const msg = dir === 'up' ? `Copy new and changed files from\n${l}\nto the server folder\n${r}?` : `Copy new and changed files from the server folder\n${r}\nto\n${l}?`;
    if (!(await this.ui.confirm('Synchronize Folders', msg, 'Sync'))) return;
    try {
      await this.backend.startSync(this.sid(), dir, l, r);
      const target = dir === 'up' ? this.remote : this.local;
      const unwatch = this.backend.on('xfer:progress', t => {
        if (t.state !== 'running') { unwatch(); void this.load(target()); }
      });
    } catch (e) { this.ui.error(e); }
  }

  async mkdir(p: Pane) {
    const name = await this.ui.input('New Folder', 'Folder name');
    if (!name) return;
    try {
      const path = this.join(p, name);
      if (p.side === 'local') await this.backend.localMkdir(path); else await this.backend.sftpMkdir(this.sid(), path);
      await this.load(p);
    } catch (e) { this.ui.error(e); }
  }

  async remove(p: Pane) {
    const sel = [...p.selected];
    if (p.side !== 'remote' || !sel.length) return; // local deletion is intentionally not offered
    if (!(await this.ui.confirm('Delete', `Delete ${sel.length} item(s) on the server? Folders are deleted with their contents.`, 'Delete', true))) return;
    try {
      for (const n of sel) await this.backend.sftpRemove(this.sid(), this.join(p, n));
    } catch (e) { this.ui.error(e); }
    await this.load(p);
  }

  async rename(p: Pane, e: FileEntry) {
    const name = await this.ui.input('Rename', 'New name', e.name);
    if (!name || name === e.name) return;
    try {
      await this.backend.sftpRename(this.sid(), this.join(p, e.name), this.join(p, name));
      await this.load(p);
    } catch (err) { this.ui.error(err); }
  }

  menu(ev: MouseEvent, p: Pane, e: FileEntry) {
    if (!p.selected.has(e.name)) this.select(p, e, ev);
    const cur = this.sig(p)();
    this.ui.showMenu(ev, p.side === 'remote'
      ? [
          { label: this.via() === 'scp' ? 'Download (SCP)' : 'Download', action: () => this.transfer('down') },
          ...this.otherSftpTabs().map(o => ({
            label: `Copy to ${o.title.replace(/^SFTP: /, '')}: ${this.tabs.sftpDirs.get(o.id) || o.home}`,
            action: () => this.copyToServer(o),
          })),
          { label: 'Rename...', action: () => this.rename(cur, e) },
          { label: 'Delete', action: () => this.remove(cur) },
          { separator: true },
          { label: 'New Folder...', action: () => this.mkdir(cur) },
          { label: 'Refresh', action: () => this.load(cur) },
        ]
      : [
          { label: this.via() === 'scp' ? 'Upload (SCP)' : 'Upload', action: () => this.transfer('up') },
          { separator: true },
          { label: 'New Folder...', action: () => this.mkdir(cur) },
          { label: 'Refresh', action: () => this.load(cur) },
        ]);
  }

  cancel(id: string) { void this.backend.cancelTransfer(id); }

  // ---- server-to-server copy ----

  /** Other open SFTP tabs that can receive a server-to-server copy. */
  otherSftpTabs() { return this.tabs.tabs().filter(t => t.kind === 'sftp' && t.id !== this.sid()); }

  async copyToServer(target: TabView) {
    const p = this.remote();
    const sel = [...p.selected];
    if (!sel.length) return;
    const dest = this.tabs.sftpDirs.get(target.id) || target.home || '/';
    try {
      await this.backend.startRemoteCopy(this.sid(), target.id, sel.map(n => this.join(p, n)), dest);
      this.ui.toast(`Copying ${sel.length} item(s) to ${target.title.replace(/^SFTP: /, '')}:${dest} (the data passes through this computer)`);
    } catch (e) { this.ui.error(e); }
  }

  // ---- bookmarks ----

  private bmSession() { return (this.tab().session.path || this.tab().session.hostname) + (this.tab().hopTarget ? ' > ' + this.tab().hopTarget : ''); }

  private bookmarks(p: Pane) {
    return (this.ui.settings()?.bookmarks ?? []).filter(b => b.side === p.side && (p.side === 'local' || b.session === this.bmSession()));
  }

  isBookmarked(p: Pane) { return this.bookmarks(p).some(b => b.path === p.dir); }

  bookmarkMenu(ev: MouseEvent, p: Pane) {
    const list = this.bookmarks(p);
    const here = list.find(b => b.path === p.dir);
    this.ui.showMenu(ev, [
      ...list.map(b => ({ label: `${b.name}  —  ${b.path}`, action: () => this.go(this.sig(p)(), b.path) })),
      ...(list.length ? [{ separator: true }] : []),
      here
        ? { label: `Remove Bookmark "${here.name}"`, action: () => this.saveBookmarks((this.ui.settings()?.bookmarks ?? []).filter(b => b !== here)) }
        : { label: 'Bookmark This Folder...', action: () => this.addBookmark(p) },
    ]);
  }

  async addBookmark(p: Pane) {
    const dir = this.sig(p)().dir;
    const def = dir.split(/[\\/]/).filter(Boolean).pop() || dir;
    const name = await this.ui.input('Bookmark This Folder', 'Name', def);
    if (!name) return;
    const all = this.ui.settings()?.bookmarks ?? [];
    await this.saveBookmarks([...all, { name, side: p.side, session: p.side === 'remote' ? this.bmSession() : '', path: dir }]);
  }

  private async saveBookmarks(bookmarks: NonNullable<ReturnType<Ui['settings']>>['bookmarks']) {
    const s = this.ui.settings();
    if (s) await this.ui.saveSettings({ ...s, bookmarks });
  }

  async resume(id: string) {
    try {
      await this.backend.resumeTransfer(id);
      this.tabs.transfers.update(l => l.filter(t => t.id !== id));
    } catch (e) { this.ui.error(e); }
  }

  clearDone() { this.tabs.transfers.update(l => l.filter(t => t.state === 'running')); }
}
