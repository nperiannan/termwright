import { Component, OnInit, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { SUDO_ERR } from '../core/fixes';
import { FileEntry, Transfer, formatBytes } from '../core/models';
import { Tabs, TabView } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { FileIcon } from './file-icon';
import { Icon } from './icon';
import { SessionIcon } from './session-icon';
import { Autofocus } from '../core/autofocus';

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
  imports: [FormsModule, DatePipe, Icon, SessionIcon, FileIcon, Autofocus],
  host: { '[class.modern]': 'modern()' },
  template: `
    @if (shell() && !shell()!.cwd && !cwdHintClosed()) {
      <div class="cwd-hint">
        <div><b>Working directory detection</b> - the shell on {{ tab().hopTarget || tab().session.hostname }} does not report its folder,
          so this tab cannot open or follow the terminal's folder. Turn it on for bash or zsh:</div>
        <button type="button" (click)="enableCwd(false)">Enable in this shell</button>
        <button type="button" (click)="enableCwd(true)">Enable permanently…</button>
        <button type="button" class="icon-btn" title="Hide" (click)="closeCwdHint()">✕</button>
      </div>
    }
    @if (modern()) {
      <div class="stb">
        <span class="stic"><app-icon name="sftp" [size]="16" /></span>
        <b class="stt">SFTP · {{ tab().session.name || hostOnly() }}</b>
        <span class="sth">{{ (tab().session.username ? tab().session.username + '@' : '') + (tab().session.hostname || '') }}@if (tab().hopTarget) { → {{ tab().hopTarget }}}</span>
        <span class="grow"></span>
        <button class="root-btn" [class.on]="tab().root" [disabled]="rootBusy()" (click)="runAsMenu($event)"
          [title]="tab().root ? 'Browsing as ' + tab().runAsUser + '. Click to switch back or to another user.' : 'Browse as root or another user (sudo, su, …)'">
          {{ rootBusy() ? '…' : tab().root ? 'as ' + tab().runAsUser + ' ▾' : 'Run as ▾' }}</button>
        @if (shell()) {
          <button class="root-btn" [class.on]="follow()" (click)="terminalMenu($event)"
            [title]="shell()!.cwd ? 'Terminal folder: ' + shell()!.cwd + (follow() ? ' (following)' : '') : 'The terminal does not report its folder yet'">
            &gt;_ {{ follow() ? 'Following' : 'Terminal' }} ▾</button>
        }
        <select class="pill" [ngModel]="via()" (ngModelChange)="setVia($event)" [disabled]="!!tab().root"
          title="Protocol used by → and ← (browsing always uses SFTP). SCP helps with devices whose SFTP server is slow or limited.">
          <option value="sftp">SFTP</option><option value="scp">SCP</option></select>
      </div>
    }
    <div class="panes">
      @for (p of [local(), remote()]; track p.side) {
        <section class="pane" [class.focus]="focus() === p.side" (mousedown)="focus.set(p.side)">
          <header>
            <span class="label" [title]="p.side === 'remote' && tab().hopTarget ? 'Files on ' + tab().hopTarget + ', reached through the jump station ' + tab().session.hostname : ''">{{ p.side === 'local' ? 'This computer' : (tab().root ? tab().runAsUser + ' @ ' + hostOnly() : tab().hopTarget || tab().session.hostname || 'Server') }}@if (p.side === 'remote' && tab().hopTarget) {<span class="via-jump"> via {{ tab().session.hostname }}</span>}</span>
            @if (modern()) {
              @if (editing() !== p.side) {
                <div class="crumb" title="Click to type a path" (click)="editing.set(p.side)">
                  @for (c of crumbs(p); track $index) {
                    @if (!$first && crumbs(p)[$index - 1].label !== '/') { <span class="cs">/</span> }
                    <button type="button" class="cb" [class.last]="$last" [title]="c.path" (click)="$event.stopPropagation(); go(p, c.path)">{{ c.label }}</button>
                  }
                </div>
              } @else {
                <input class="path mono" [ngModel]="p.dir" appAutofocus (keydown.enter)="editing.set(''); go(p, $any($event.target).value)"
                  (keydown.escape)="editing.set('')" (blur)="editing.set('')" spellcheck="false" />
              }
              <button class="icon-btn" title="Up" (click)="up(p)"><app-icon name="up" /></button>
              <button class="icon-btn" title="Refresh" (click)="load(p)"><app-icon name="refresh" /></button>
              <button class="icon-btn" title="New folder" (click)="mkdir(p)"><app-icon name="folder-plus" /></button>
              <button class="icon-btn" title="New file" (click)="newFile(p)"><app-icon name="file-plus" /></button>
              <button class="icon-btn del" [disabled]="!p.selected.size" [title]="delTitle(p)" (click)="remove(p)"><app-icon name="trash" /></button>
              <button class="icon-btn bm" [class.on]="isBookmarked(p)" title="Bookmarks" (click)="bookmarkMenu($event, p)"><app-icon name="star" /></button>
            } @else {
              <button class="icon-btn" title="Up" (click)="up(p)"><app-icon name="up" /></button>
              <input class="path mono" [ngModel]="p.dir" (keydown.enter)="go(p, $any($event.target).value)" spellcheck="false" />
              <button class="icon-btn" title="Refresh" (click)="load(p)"><app-icon name="refresh" /></button>
              <button class="icon-btn" title="New folder" (click)="mkdir(p)"><app-icon name="folder-plus" /></button>
              <button class="icon-btn" title="New file" (click)="newFile(p)"><app-icon name="file-plus" /></button>
              <button class="icon-btn del" [disabled]="!p.selected.size" [title]="delTitle(p)" (click)="remove(p)"><app-icon name="trash" /></button>
              @if (p.side === 'remote') {
                <button class="root-btn" [class.on]="tab().root" [disabled]="rootBusy()" (click)="runAsMenu($event)"
                  [title]="tab().root ? 'Browsing as ' + tab().runAsUser + '. Click to switch back or to another user.' : 'Browse as root or another user (sudo, su, …)'">
                  {{ rootBusy() ? '…' : tab().root ? 'as ' + tab().runAsUser + ' ▾' : 'Run as ▾' }}</button>
              }
              @if (p.side === 'remote' && shell()) {
                <button class="root-btn" [class.on]="follow()" (click)="terminalMenu($event)"
                  [title]="shell()!.cwd ? 'Terminal folder: ' + shell()!.cwd + (follow() ? ' (following)' : '') : 'The terminal does not report its folder yet'">
                  &gt;_ {{ follow() ? 'Following' : 'Terminal' }} ▾</button>
              }
              <button class="icon-btn bm" [class.on]="isBookmarked(p)" title="Bookmarks" (click)="bookmarkMenu($event, p)">{{ isBookmarked(p) ? '★' : '☆' }}</button>
            }
          </header>
          <div class="bmbar">
            @for (b of bookmarkList(p); track b.name + b.path) {
              <button type="button" class="bmc" [class.on]="b.path === p.dir" [title]="b.path + ' - right-click to remove'"
                (click)="go(p, b.path)" (contextmenu)="bookmarkChipMenu($event, p, b)"><app-icon name="star" [size]="12" />{{ b.name }}</button>
            }
            @if (!isBookmarked(p)) { <button type="button" class="bmc add" title="Bookmark this folder" (click)="addBookmark(p)">+ Bookmark</button> }
          </div>
          <div class="list" tabindex="0" (keydown)="key($event, p)" [class.multi]="p.selected.size > 0">
            <div class="row head"><span class="n">@if (modern()) { <span class="ck" title="Select all / none" (click)="toggleAll(p)"><input type="checkbox" tabindex="-1" [checked]="p.entries.length > 0 && p.selected.size === p.entries.length" [indeterminate]="p.selected.size > 0 && p.selected.size < p.entries.length" /></span> }Name</span><span class="s">Size</span><span class="d">Modified</span><span class="m">Mode</span></div>
            @if (p.error) { <div class="err pad">{{ p.error }}</div> }
            @if (modern() && crumbs(p).length > 1) {
              <div class="row" title="Up one folder" (dblclick)="up(p)"><span class="n"><span class="fold"></span>..</span><span class="s"></span><span class="d"></span><span class="m"></span></div>
            }
            @for (e of p.entries; track e.name) {
              <div class="row" [class.sel]="p.selected.has(e.name)" (click)="select(p, e, $event)" (dblclick)="open(p, e)"
                   (contextmenu)="menu($event, p, e)">
                @if (modern()) {
                  <span class="n"><span class="ck" (click)="$event.stopPropagation(); toggle(p, e)" (dblclick)="$event.stopPropagation()" title="Select (also Cmd/Ctrl-click, Shift-click for a range)"><input type="checkbox" tabindex="-1" [checked]="p.selected.has(e.name)" /></span>@if (e.isDir) { <span class="fold" [class.dot]="e.name.startsWith('.')"></span> } @else { <app-file-icon [name]="e.name" [link]="e.isLink" [badge]="true" /> }{{ e.name }}</span>
                  <span class="s">{{ e.isDir ? '' : size(e.size) }}</span>
                  <span class="d" [title]="(e.modTime | date: 'yyyy-MM-dd HH:mm') + '  ' + e.mode">{{ when(e.modTime) }}</span>
                } @else {
                <span class="n">@if (e.isDir) { <app-session-icon kind="folder" /> } @else { <app-file-icon [name]="e.name" [link]="e.isLink" /> }{{ e.name }}</span>
                <span class="s">{{ e.isDir ? '' : size(e.size) }}</span>
                <span class="d">{{ e.modTime | date: 'yyyy-MM-dd HH:mm' }}</span>
                }
                <span class="m mono">{{ e.mode }}</span>
              </div>
            }
          </div>
          <footer>{{ p.entries.length }} items @if (p.selected.size) { · {{ p.selected.size }} selected <a class="clr" (click)="clearSel(p)">Clear</a> }</footer>
        </section>
        @if (p.side === 'local') {
          @if (modern()) {
            <div class="mid">
              <span class="ml">Copy</span>
              <button class="ar" [title]="'Upload the selected files to the server via ' + via().toUpperCase()" [disabled]="!local().selected.size" (click)="transfer('up')">→</button>
              <button class="ar" [title]="'Download the selected files to this computer via ' + via().toUpperCase()" [disabled]="!remote().selected.size" (click)="transfer('down')">←</button>
              <span class="ml">Sync</span>
              <button class="ar sy" title="Sync this computer → server: copy new and changed files of the left folder into the right folder (nothing is deleted)" (click)="sync('up')"><app-icon name="sync-right" [size]="16" /></button>
              <button class="ar sy" title="Sync server → this computer: copy new and changed files of the right folder into the left folder (nothing is deleted)" (click)="sync('down')"><app-icon name="sync-left" [size]="16" /></button>
              <span class="ml">Diff</span>
              <button class="ar sy" [disabled]="!canCompare()" [title]="canCompare() ? 'Compare ' + one(local()) + ' with ' + one(remote()) : 'Compare: select one file on each side'" (click)="compare()"><app-icon name="compare" [size]="16" /></button>
            </div>
          } @else {
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
      }
    </div>
    @if (modern()) {
      @if (current(); as t) {
        <div class="xbar" [class]="'xbar ' + t.state">
          <span class="xf" [title]="t.file">{{ dirGlyph(t.direction) }} {{ t.file }}</span>
          @if (t.state === 'running') {
            <span class="bar"><i [style.width.%]="t.total ? t.bytes * 100 / t.total : 0"></i></span>
            <span class="hint">{{ size(t.bytes) }} / {{ size(t.total) }}@if (rate(t) > 0) { · {{ size(rate(t)) }}/s@if (left(t); as l) { · {{ l }}}}@if (t.filesTotal > 1) { · file {{ t.filesDone + 1 }} of {{ t.filesTotal }}}</span>
          } @else {
            <span class="hint st">{{ t.state === 'done' ? '✓ Done' + (t.filesTotal > 1 ? ' - ' + t.filesTotal + ' files' : '') + ' · ' + size(t.total) + avgText(t) : t.state === 'error' ? '✗ ' + t.error : 'Cancelled' }}</span>
          }
          <span class="grow"></span>
          @if (others() > 0) { <button type="button" class="pill" (click)="showQueue.set(!showQueue())" title="Show all transfers">{{ others() }} more {{ showQueue() ? '▾' : '▴' }}</button> }
          @if (t.resumable && (t.state === 'error' || t.state === 'cancelled')) { <button class="sbtn" title="Resume: continue partial files, skip completed ones" (click)="resume(t.id)">Resume</button> }
          @if (t.state === 'running') { <button class="sbtn" (click)="cancel(t.id)">Cancel</button> }
          @else { <button class="sbtn" (click)="clearDone()">Clear</button> }
          <button class="icon-btn xclose" title="Close - transfers that are running go on in the background" (click)="closeTransfers()"><app-icon name="x" /></button>
        </div>
      }
      @if (showQueue() && shown().length > 1) {
      @if (shown().length) {
        <div class="queue">
          @for (t of shown(); track t.id) {
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
          <button class="clear" title="Close - transfers that are running go on in the background" (click)="closeTransfers()">Close</button>
        </div>
      }
      }
    } @else {
      @if (shown().length) {
        <div class="queue">
          @for (t of shown(); track t.id) {
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
          <button class="clear" title="Close - transfers that are running go on in the background" (click)="closeTransfers()">Close</button>
        </div>
      }
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
  readonly modern = () => this.ui.settings()?.layout !== 'classic';
  /** The pane whose path is being typed (modern layout shows clickable folders otherwise). */
  readonly editing = signal<'' | 'local' | 'remote'>('');
  /** The transfer the bar shows: the first running one, else the latest. */
  /** Transfers closed with ✕ (they go on; the bar shows again for a new one). */
  private readonly closed = signal<Set<string>>(new Set());
  readonly shown = computed(() => { const c = this.closed(); return this.tabs.transfers().filter(t => !c.has(t.id)); });
  /** ✕ on the transfer status: finished transfers are cleared, running ones keep going in the background. */
  closeTransfers() {
    this.clearDone();
    this.closed.update(c => new Set([...c, ...this.tabs.transfers().map(t => t.id)]));
    this.showQueue.set(false);
  }
  readonly current = computed(() => { const l = this.shown(); return l.find(t => t.state === 'running') ?? l[l.length - 1] ?? null; });
  readonly others = computed(() => Math.max(0, this.shown().length - 1));
  readonly showQueue = signal(false);
  private rates = new Map<string, { b: number; at: number; r: number; t0: number; b0: number; end?: number }>();
  /** Bytes per second of a running transfer (smoothed; the average since it
   *  started until two updates were seen). */
  rate(t: Transfer): number {
    const now = performance.now();
    let m = this.rates.get(t.id);
    if (!m) { m = { b: t.bytes, at: now, r: 0, t0: now, b0: t.bytes }; this.rates.set(t.id, m); return 0; }
    if (t.bytes !== m.b && now - m.at > 250) {
      const r = (t.bytes - m.b) / ((now - m.at) / 1000);
      m.r = m.r ? m.r * .6 + r * .4 : r; m.b = t.bytes; m.at = now;
    }
    if (m.r) return m.r;
    const dt = (now - m.t0) / 1000;
    return dt > .3 && t.bytes > m.b0 ? (t.bytes - m.b0) / dt : 0;
  }
  /** "· 11.4 MB/s" for a finished transfer: its average speed. */
  avgText(t: Transfer) {
    const m = this.rates.get(t.id);
    if (!m) return '';
    m.end ??= performance.now();
    const dt = (m.end - m.t0) / 1000;
    return dt > .2 && t.total ? ' · ' + this.size((t.total - m.b0) / dt) + '/s' : '';
  }
  /** One file selected on each side: they can be compared. */
  canCompare() {
    const one = (p: Pane) => p.selected.size === 1 && !p.entries.find(e => e.name === [...p.selected][0])?.isDir;
    return one(this.local()) && one(this.remote());
  }
  one(p: Pane) { return [...p.selected][0] ?? ''; }
  /** Opens Compare with the selected local and remote file (text, up to 5 MB). */
  async compare() {
    if (!this.canCompare()) return;
    const l = this.local(), r = this.remote();
    const ln = this.one(l), rn = this.one(r);
    try {
      const [a, b] = await Promise.all([this.backend.readTextFile(this.join(l, ln)), this.backend.sftpReadText(this.sid(), this.join(r, rn))]);
      const host = this.tab().hopTarget || this.hostOnly();
      this.ui.compareTexts.set([{ label: 'This computer: ' + ln, text: a }, { label: host + ': ' + rn, text: b }]);
      this.ui.open('compare');
    } catch (e) { this.ui.error(e); }
  }
  left(t: Transfer) {
    const r = this.rate(t);
    if (!r || !t.total) return '';
    const s = Math.max(0, Math.round((t.total - t.bytes) / r));
    return s < 60 ? s + ' s left' : s < 3600 ? Math.round(s / 60) + ' min left' : (s / 3600).toFixed(1) + ' h left';
  }
  /** "Today 09:41", "Yesterday 18:02", "12 Oct", "3 Mar 2025". */
  when(iso: string) {
    const d = new Date(iso);
    if (isNaN(d.getTime())) return '';
    const now = new Date();
    const hm = d.toTimeString().slice(0, 5);
    const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
    const diff = Math.round((day(now) - day(d)) / 86400000);
    if (diff === 0) return 'Today ' + hm;
    if (diff === 1) return 'Yesterday ' + hm;
    const m = d.toLocaleString('en-GB', { day: 'numeric', month: 'short' });
    return d.getFullYear() === now.getFullYear() ? m : m + ' ' + d.getFullYear();
  }
  /** The folders of the pane's path, each with the path to open it. */
  crumbs(p: Pane): { label: string; path: string }[] {
    const d = p.dir;
    if (!d) return [];
    const win = /^[a-zA-Z]:([\\/]|$)/.test(d) || (d.includes('\\') && !d.includes('/'));
    const parts = d.split(/[\\/]+/).filter(Boolean);
    const out: { label: string; path: string }[] = [];
    if (!win && d.startsWith('/')) out.push({ label: '/', path: '/' });
    parts.forEach((x, i) => out.push({ label: x, path: win ? parts.slice(0, i + 1).join('\\') + (i === 0 ? '\\' : '') : (d.startsWith('/') ? '/' : '') + parts.slice(0, i + 1).join('/') }));
    // long paths: the first and the last three
    return out.length > 6 ? [out[0], { label: '…', path: out[out.length - 4].path }, ...out.slice(-3)] : out;
  }

  private viaKey() { return 'termwright.sftpVia:' + (this.tab().session.path || this.tab().session.hostname) + (this.tab().hopTarget ? '>' + this.tab().hopTarget : ''); }
  /** Remembers SFTP/SCP per session on this computer. */
  setVia(v: 'sftp' | 'scp') {
    this.via.set(v);
    try { if (v === 'scp') localStorage.setItem(this.viaKey(), v); else localStorage.removeItem(this.viaKey()); } catch { /* ignore */ }
  }

  readonly rootBusy = signal(false);
  /** The terminal tab with the same login whose folder this tab can follow. */
  readonly shell = computed(() => this.tabs.shellOf(this.tab()));
  readonly follow = signal(this.ui.settings()?.sftpFollowTerminal !== false);
  /** Browse as the user the terminal runs as (root via sudo, others via "sudo su -"). */
  private readonly followUser = this.ui.settings()?.sftpFollowUser !== false;
  private triedUser = '';
  readonly cwdHintClosed = signal(this.loadFlag('termwright.cwdHintClosed'));

  constructor() {
    // Follow the terminal: open the folder the shell reports after each command.
    effect(() => {
      const dir = this.shell()?.cwd;
      if (!this.follow() || !dir) return;
      untracked(() => { if (this.remote().dir !== dir && !this.rootBusy()) void this.go(this.remote(), dir); });
    });
    // Follow the terminal's user: "sudo su -" in the terminal → this tab browses as root too.
    effect(() => {
      const u = this.shell()?.shellUser;
      if (!this.followUser || !u) return;
      untracked(() => {
        const login = this.loginName();
        const cur = this.tab().root ? this.tab().runAsUser : login;
        if (u === cur || u === this.triedUser || this.rootBusy() || (!login && u !== 'root')) return;
        this.triedUser = u;
        void this.switchUser(u === login ? '' : u === 'root' ? 'sudo' : 'sudo su - ' + u);
      });
    });
  }

  private loadFlag(k: string) { try { return localStorage.getItem(k) === '1'; } catch { return false; } }
  private saveFlag(k: string, v: boolean) { try { localStorage.setItem(k, v ? '1' : '0'); } catch { /* ignore */ } }

  terminalMenu(ev: MouseEvent) {
    const sh = this.shell();
    if (!sh) return;
    const dir = this.remote().dir;
    this.ui.showMenu(ev, [
      { label: sh.cwd ? 'Go to Terminal Folder (' + sh.cwd + ')' : 'Go to Terminal Folder (not reported yet)', disabled: !sh.cwd,
        action: () => this.go(this.remote(), sh.cwd!) },
      { label: (this.follow() ? '✓ ' : '') + 'Follow Terminal Folder', action: () => this.setFollow(!this.follow()) },
      { label: 'Open This Folder in the Terminal (cd)', action: () => this.cdInTerminal(dir) },
      { separator: true },
      { label: 'Enable Folder Detection in This Shell', action: () => this.enableCwd(false) },
      { label: 'Enable Folder Detection Permanently…', action: () => this.enableCwd(true) },
    ]);
  }

  setFollow(v: boolean) {
    this.follow.set(v);
    if (v && !this.shell()?.cwd) this.ui.toast('The terminal does not report its folder yet - enable folder detection first.', 'info', 6000);
  }

  closeCwdHint() { this.cwdHintClosed.set(true); this.saveFlag('termwright.cwdHintClosed', true); }

  /** cd in the terminal to a remote folder. */
  cdInTerminal(dir: string) {
    const sh = this.shell();
    if (!sh || sh.state !== 'connected') { this.ui.toast('The terminal tab is not connected.', 'error'); return; }
    void this.backend.sendInput(sh.id, ` cd '${dir.replace(/'/g, `'\\''`)}'\r`);
    this.tabs.activeId.set(sh.id);
  }

  async enableCwd(permanent: boolean) {
    const sh = this.shell();
    if (!sh || sh.state !== 'connected') { this.ui.toast('The terminal tab is not connected.', 'error'); return; }
    if (permanent && !await this.ui.confirm('Enable folder detection permanently',
      'Termwright types a command in the terminal that adds two lines to ~/.bashrc (or ~/.zshrc for zsh) on ' +
      (this.tab().hopTarget || this.tab().session.hostname) + ', so every new shell reports its folder. ' +
      'It is skipped if the lines are already there. Remove them from that file to turn it off.', 'Add')) return;
    await this.tabs.enableCwdReporting(sh.id, permanent).catch(e => this.ui.error(e));
    this.ui.toast(permanent ? 'Added to the shell start-up file - also active in this shell now.' : 'Folder detection is on for this shell.', 'success');
  }

  hostOnly() { const h = this.tab().hopTarget || this.tab().session.hostname; return h.slice(h.lastIndexOf('@') + 1); }
  /** Login user name of this tab ("" when the session has none). */
  loginName() { const l = this.loginUser(); return l.includes('@') ? l.slice(0, l.lastIndexOf('@')) : ''; }
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
      const cwd = this.follow() ? this.shell()?.cwd : '';
      await this.go(this.remote(), cwd || home);
      if (cwd && this.remote().error) await this.go(this.remote(), home);
    } catch (e) {
      const msg = errText(e);
      if (SUDO_ERR.test(msg)) {
        this.ui.toast(msg + (cmd === 'sudo' ? '\n\nThis user may not use sudo here. Try a Run As command that works for it (su - root, sudo su - user) or browse as the login user.' : ''), 'error', 12000, [
          { label: 'Run As Command...', run: () => void this.askCommand() },
          ...(cmd !== 'sudo' ? [{ label: 'root (sudo)', run: () => void this.switchUser('sudo') }] : []),
          ...(this.tab().root ? [{ label: 'Back to ' + this.loginUser(), run: () => void this.switchUser('') }] : []),
        ]);
      } else this.ui.error(e);
    }
    this.rootBusy.set(false);
  }

  async ngOnInit() {
    try { if (localStorage.getItem(this.viaKey()) === 'scp') this.via.set('scp'); } catch { /* ignore */ }
    const home = await this.backend.localHome().catch(() => '/');
    this.local.update(p => ({ ...p, dir: home }));
    const start = this.tab().startDir;
    this.remote.update(p => ({ ...p, dir: start || this.tab().home || '/' }));
    await Promise.all([this.load(this.local()), this.load(this.remote())]);
    if (start && this.remote().error) await this.go(this.remote(), this.tab().home || '/'); // not readable over SFTP
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

  /** The check box of a row: adds it to or takes it out of the selection. */
  toggle(p: Pane, e: FileEntry) {
    this.sig(p).update(x => { const sel = new Set(x.selected); if (sel.has(e.name)) sel.delete(e.name); else sel.add(e.name); return { ...x, selected: sel }; });
    this.lastClicked = e.name;
  }
  toggleAll(p: Pane) {
    this.sig(p).update(x => ({ ...x, selected: x.selected.size === x.entries.length ? new Set<string>() : new Set(x.entries.map(f => f.name)) }));
  }
  clearSel(p: Pane) { this.sig(p).update(x => ({ ...x, selected: new Set<string>() })); }

  /** New empty file in the pane's folder (never overwrites). */
  async newFile(p: Pane) {
    const name = (await this.ui.input('New File', 'File name', 'new.txt'))?.trim();
    if (!name) return;
    if (/[\\/]/.test(name)) { this.ui.toast('Type a name without / or \\', 'error'); return; }
    const cur = this.sig(p)();
    try {
      if (p.side === 'local') await this.backend.localCreateFile(this.join(cur, name));
      else await this.backend.sftpCreateFile(this.sid(), this.join(cur, name));
      await this.load(cur);
      this.sig(p).update(x => ({ ...x, selected: new Set([name]) }));
    } catch (e) { this.ui.error(e); }
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
    if (!sel.length) return;
    const what = sel.length === 1 ? `"${sel[0]}"` : `${sel.length} items`;
    if (p.side === 'local') {
      // on this computer: to the Trash / Recycle Bin, so it can be put back
      if (!(await this.ui.confirm('Move to Trash', `Move ${what} on this computer to the ${this.isWin ? 'Recycle Bin' : 'Trash'}?`, 'Move to Trash', true))) return;
      try { await this.backend.localTrash(sel.map(n => this.join(p, n))); } catch (e) { this.ui.error(e); }
      await this.load(p);
      return;
    }
    if (!(await this.ui.confirm('Delete', `Delete ${what} on the server? This cannot be undone; folders are deleted with their contents.`, 'Delete', true))) return;
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
          ...(this.shell() ? [{ label: 'Open in Terminal (cd)', action: () => this.cdInTerminal(e.isDir ? this.join(cur, e.name) : cur.dir) }] : []),
          { label: 'Rename...', action: () => this.rename(cur, e) },
          { label: 'Delete', action: () => this.remove(cur) },
          { separator: true },
          { label: 'New Folder...', action: () => this.mkdir(cur) },
          { label: 'Refresh', action: () => this.load(cur) },
        ]
      : [
          { label: this.via() === 'scp' ? 'Upload (SCP)' : 'Upload', action: () => this.transfer('up') },
          { label: 'Move to Trash', action: () => this.remove(cur) },
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

  bookmarkList(p: Pane) { return this.bookmarks(p); }
  bookmarkChipMenu(ev: MouseEvent, p: Pane, b: { name: string; path: string }) {
    ev.preventDefault();
    this.ui.showMenu(ev, [
      { label: 'Open', action: () => this.go(this.sig(p)(), b.path) },
      { label: `Remove Bookmark "${b.name}"`, action: () => this.saveBookmarks((this.ui.settings()?.bookmarks ?? []).filter(x => x !== b)) },
    ]);
  }
  readonly isWin = navigator.userAgent.includes('Windows');
  delTitle(p: Pane) {
    if (!p.selected.size) return 'Delete - select files or folders first';
    return p.side === 'local' ? `Move the selected item(s) to the ${this.isWin ? 'Recycle Bin' : 'Trash'} (Delete key)` : 'Delete the selected item(s) on the server (Delete key)';
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
