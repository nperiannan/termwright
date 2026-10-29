import { Component, ElementRef, OnInit, computed, effect, inject, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from './core/backend';
import { Tabs, TabView } from './core/tabs.service';
import { connectString, findSessionByName, looksLikeConnect } from './core/connect-string';
import { SessionNode } from './core/models';
import { ago } from './core/env';
import { factList } from './core/facts';
import { SessionIcon } from './components/session-icon';
import { DeviceIcon, deviceKindOf } from './components/device-icon';
import { sessionAddr } from './components/session-tree';
import { SessionPanel } from './components/session-panel';
import { menuBar, shortcutOf } from './core/menu';
import { FilesPanel } from './components/files-panel';
import { WebOpenDialog } from './dialogs/web-open';
import { envInfo, envOf } from './core/env';
import { Ui } from './core/ui.service';
import { uiFontSize, uiFontStack } from './core/ui-fonts';
import { Icon } from './components/icon';
import { SessionTree } from './components/session-tree';
import { TerminalView } from './components/terminal-view';
import { SftpView } from './components/sftp-view';
import { CommandWindow } from './components/command-window';
import { ButtonBar } from './components/button-bar';
import { SessionEditor } from './dialogs/session-editor';
import { QuickConnect } from './dialogs/quick-connect';
import { PromptDialog } from './dialogs/prompt-dialog';
import { OptionsDialog } from './dialogs/options-dialog';
import { PasswordsDialog } from './dialogs/passwords-dialog';
import { UpdateDialog } from './dialogs/update-dialog';
import { DiscoverDialog } from './dialogs/discover-dialog';
import { BulkEdit } from './dialogs/bulk-edit';
import { RemoteView } from './components/remote-view';
import { Backups } from './dialogs/backups';
import { Checklists } from './dialogs/checklists';
import { Collect } from './dialogs/collect';
import { Recordings } from './dialogs/recordings';
import { Moved } from './dialogs/moved';
import { PlayerView } from './components/player-view';
import { ConsolePorts } from './dialogs/console-ports';
import { HistoryDialog } from './dialogs/history';
import { KeySetupDialog } from './dialogs/key-setup-dialog';
import { Palette } from './dialogs/palette';
import { SessionsMenu } from './components/sessions-menu';
import { Commands, sendSnippet } from './dialogs/commands';
import { FolderDefaults, Workspaces, saveWorkspace } from './dialogs/workspaces';
import { Compare } from './dialogs/compare';
import { ExportDialog, ImportDialog } from './dialogs/import-export';
import { About, Overlays } from './dialogs/overlays';
import { WELCOME_VERSION, Welcome } from './dialogs/welcome';
import { Unlock } from './dialogs/unlock';
import { FactTeach } from './dialogs/fact-teach';

@Component({
  selector: 'app-root',
  imports: [FormsModule, Icon, SessionsMenu, SessionIcon, DeviceIcon, SessionPanel, SessionTree, TerminalView, SftpView, FilesPanel, WebOpenDialog, CommandWindow, ButtonBar, SessionEditor, QuickConnect,
    PromptDialog, OptionsDialog, PasswordsDialog, UpdateDialog, KeySetupDialog, Palette, Commands, Workspaces, FolderDefaults, Compare, ImportDialog, ExportDialog, DiscoverDialog, BulkEdit, RemoteView, Backups, Checklists, Collect, ConsolePorts, Recordings, PlayerView, Moved, HistoryDialog, Overlays, About, Welcome, FactTeach, Unlock],
  templateUrl: './app.html',
  styleUrl: './app.scss',
})
export class App implements OnInit {
  protected ui = inject(Ui);
  protected tabs = inject(Tabs);
  private backend = inject(Backend);
  private cmdWin = viewChild(CommandWindow);

  readonly treeWidth = signal(this.loadNum('termwright.treeWidth', 260));
  readonly cmdHeight = signal(this.loadNum('termwright.cmdHeight', 110));
  readonly findOpen = signal(false);
  findText = '';
  readonly backendMissing = !this.backend.available;
  readonly settings = this.ui.settings;
  readonly active = this.tabs.active;
  readonly recent = computed(() => this.settings()?.recentSessions ?? []);
  /** Tab hover: name, environment, device type, the device's facts and the connection. */
  tabTip(t: TabView) {
    const e = this.tabEnv(t);
    const f = factList(this.ui.factsOf(t.session?.path));
    return [t.title + (e ? '  (' + e.label + ')' : ''),
      ...(t.session?.deviceType ? ['Type: ' + t.session.deviceType] : []),
      ...f.map(([k, v]) => k + ': ' + v),
      ...(t.info && t.info !== t.title ? [t.info] : [])].join('\n');
  }
  /** Status bar: "VOSS 8.10.0.0 · up 41 day(s)". */
  statusFacts(t: TabView) {
    const f = this.ui.factsOf(t.session?.path);
    const parts = [[t.session?.deviceType, f?.['Version']].filter(Boolean).join(' '), f?.['Uptime'] ? 'up ' + f['Uptime'] : ''].filter(Boolean);
    return parts.join(' · ');
  }
  tabEnv(t: TabView) { return envInfo(envOf(t.session?.path ?? '', t.session?.environment, this.settings()?.folderDefaults)); }
  readonly tiled = computed(() => this.tabs.layout() !== 'tabs');
  readonly gridCols = computed(() => {
    const n = this.tabs.tabs().length;
    const l = this.tabs.layout();
    return l === 'grid' ? Math.max(1, Math.ceil(Math.sqrt(n))) : n;
  });
  connectText = '';
  readonly isMac = navigator.platform.toLowerCase().includes('mac') || navigator.userAgent.includes('Mac OS');
  /** The shortcut of a menu action, for tooltips ("⌘N"). */
  scLabel(action: string) { return shortcutOf(action, this.isMac); }

  // ---- theme switch (top right) ----
  readonly systemLight = signal(false);
  readonly themes = [
    { v: 'light', icon: 'sun', name: 'Light' },
    { v: 'dark', icon: 'moon', name: 'Dark' },
    { v: 'system', icon: 'theme-system', name: 'System' },
  ] as const;
  readonly themeMode = computed(() => {
    const t = this.settings()?.theme;
    return t === 'light' || t === 'dark' ? t : 'system';
  });
  themeTip(v: 'light' | 'dark' | 'system') {
    const now = this.themeMode() === v;
    if (v === 'system') {
      const looks = this.systemLight() ? 'light' : 'dark';
      return now ? `Theme: follows the system (now ${looks})` : `Follow the system theme (now ${looks})`;
    }
    return now ? `Theme: ${v === 'light' ? 'Light' : 'Dark'}` : `Switch to the ${v === 'light' ? 'Light' : 'Dark'} theme`;
  }
  setTheme(v: 'light' | 'dark' | 'system') {
    if (this.themeMode() !== v) this.ui.patchSettings({ theme: v });
  }
  readonly menus = menuBar(this.isMac);

  // ---- layout: modern (tabs in the title bar) or classic (toolbar, tab strip, menus) ----
  readonly modern = computed(() => this.settings()?.layout !== 'classic');
  /** Title-bar session list, under its button. */
  readonly sessMenu = signal<{ top: number; right: number } | null>(null);
  openSessMenu(ev: MouseEvent) {
    if (this.sessMenu()) { this.sessMenu.set(null); return; }
    const r = (ev.currentTarget as HTMLElement).getBoundingClientRect();
    this.sessMenu.set({ top: Math.round(r.bottom + 6), right: Math.max(8, Math.round(window.innerWidth - r.right - 8)) });
  }
  /** macOS without a title bar (started in the modern layout): room for the window buttons. */
  readonly insetTitle = signal(false);
  readonly quickOpenKeys = this.isMac ? '⌘G' : 'Ctrl+Shift+G';
  readonly layoutIcon = computed(() => ({ tabs: 'tabs', 'tile-v': 'tile-v', 'tile-h': 'tile-h', grid: 'grid' } as Record<string, string>)[this.tabs.layout()] ?? 'tabs');

  // ---- start page (modern layout) ----
  homeText = '';
  private readonly byPath = computed(() => {
    const m = new Map<string, SessionNode>();
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => (c.isFolder ? walk(c) : m.set(c.path, c)));
    walk(this.ui.tree());
    return m;
  });
  readonly homeFavs = computed(() => (this.settings()?.favorites ?? []).map(p => this.byPath().get(p)).filter((n): n is SessionNode => !!n).slice(0, 8));
  readonly showAllRecent = signal(false);
  private readonly recentAll = computed(() => {
    const fav = new Set(this.settings()?.favorites ?? []);
    return (this.settings()?.recentSessions ?? []).filter(p => !fav.has(p)).map(p => this.byPath().get(p)).filter((n): n is SessionNode => !!n);
  });
  readonly homeRecent = computed(() => this.showAllRecent() ? this.recentAll() : this.recentAll().slice(0, 8));
  readonly recentMore = computed(() => this.recentAll().length > 8 || this.showAllRecent());
  /** Session path → state of its open tab (connected wins). */
  readonly liveState = computed(() => {
    const m = new Map<string, string>();
    for (const t of this.tabs.tabs()) {
      const p = t.session?.path;
      if (p && m.get(p) !== 'connected') m.set(p, t.state);
    }
    return m;
  });
  /** Card second line: the model when known, else user@host[:port]. */
  cardAddr(n: SessionNode) {
    const m = this.ui.factsOf(n.path)?.['Model'];
    if (m) return m + ' · ' + (n.hostname ?? ''); return sessionAddr({ ...n, deviceType: '', protocol: n.protocol === 'SSH2' ? 'SSH2' : n.protocol }).replace(/ · (Telnet|Raw|RDP|VNC|Serial|Console)$/, ''); }
  /** Card chip: "VOSS · SSH2", "Linux · 3 h ago". */
  cardMeta(n: SessionNode, recent: boolean) {
    const ver = this.ui.factsOf(n.path)?.['Version'] ?? '';
    const kind = [n.deviceType || ({ Serial: 'Serial', 'Local Shell': 'Local', Console: 'Console' } as Record<string, string>)[n.protocol ?? ''] || '', ver].filter(Boolean).join(' ');
    const tail = recent ? this.lastAgo(n) : (n.protocol === 'Serial' ? '' : n.protocol ?? '');
    return [kind, tail].filter(Boolean).join(' · ') || n.protocol || '';
  }
  devKind(n: SessionNode) { return n.protocol === 'WOL' ? null : deviceKindOf(n.deviceType); }
  addr(n: SessionNode) { return sessionAddr(n); }
  cardEnv(n: SessionNode) { return envInfo(envOf(n.path, n.environment, this.settings()?.folderDefaults)); }
  lastAgo(n: SessionNode) { return ago(this.settings()?.lastConnected?.[n.path]); }
  protoKind(n: SessionNode) {
    const m: Record<string, string> = { Serial: 'serial', Console: 'console', 'Local Shell': 'shell', Telnet: 'telnet', Raw: 'raw', RDP: 'rdp', VNC: 'vnc', WOL: 'wol' };
    return (m[n.protocol ?? ''] ?? (n.openAs === 'sftp' ? 'sftp' : 'ssh')) as never;
  }

  /** Right-click on a start page card. */
  cardMenu(ev: MouseEvent, n: SessionNode, recent: boolean) {
    const fav = (this.settings()?.favorites ?? []).includes(n.path);
    this.ui.showMenu(ev, [
      { label: 'Connect', action: () => void this.tabs.openSession(n.path) },
      ...(n.protocol === 'SSH2' && n.openAs !== 'sftp' ? [{ label: 'Connect SFTP Session', action: () => void this.tabs.openSession(n.path, 'sftp') }] : []),
      { label: 'Session Options...', action: () => void this.ui.editSession(n.path) },
      { separator: true },
      { label: fav ? 'Remove from Favourites' : 'Add to Favourites', action: () => this.ui.patchSettings({ favorites: fav ? (this.settings()?.favorites ?? []).filter(p => p !== n.path) : [...(this.settings()?.favorites ?? []), n.path] }) },
      ...(recent ? [{ label: 'Remove from Recent', action: () => this.ui.removeRecent(n.path) }] : []),
      ...(n.protocol === 'SSH2' ? [{ separator: true }, n.openAs === 'sftp'
        ? { label: 'Create SSH Session (terminal)', action: () => void this.ui.copySessionAs(n.path, '') }
        : { label: 'Create SFTP Session', action: () => void this.ui.copySessionAs(n.path, 'sftp') }] : []),
    ]);
  }

  /** Start page search box: connect to an address or a session, or search in Quick Open. */
  async homeGo() {
    const t = this.homeText.trim();
    if (!t) { this.action('quick-open'); return; }
    if (findSessionByName(this.ui, t) || looksLikeConnect(t, this.ui)) {
      if (await connectString(t, this.ui, this.tabs, this.backend)) this.homeText = '';
      return;
    }
    this.ui.paletteSeed.set(t);
    this.homeText = '';
    this.ui.open('palette');
  }

  /** Status bar (modern): "SSH2 · admin@10.1.1.1:22", "Serial · COM3 115200". */
  statusTarget(t: TabView) {
    const s = t.session;
    if (t.kind === 'sftp') return 'SFTP · ' + (t.info || s.hostname);
    if (s.protocol === 'Serial') return `Serial · ${s.comPort} ${s.baudRate || ''}`.trim();
    if (s.protocol === 'Local Shell') return 'Local shell';
    if (!s.hostname) return t.info;
    return `${s.protocol} · ${s.username ? s.username + '@' : ''}${s.hostname}${s.port ? ':' + s.port : ''}` + (s.jumpSession ? ' · via ' + s.jumpSession.split('/').pop() : '');
  }

  scriptNames() { return this.tabs.scripts().map(x => x.name).join(', '); }

  /** Title bar tabs: the mouse wheel scrolls them sideways when they do not fit. */
  tabsWheel(ev: WheelEvent) {
    const el = ev.currentTarget as HTMLElement;
    if (el.scrollWidth <= el.clientWidth) return;
    el.scrollLeft += Math.abs(ev.deltaX) > Math.abs(ev.deltaY) ? ev.deltaX : ev.deltaY;
    ev.preventDefault();
  }

  private ptabs = viewChild<ElementRef<HTMLElement>>('ptabs');
  private readonly followActive = effect(() => {
    const id = this.tabs.activeId();
    this.tabs.tabs().length;
    setTimeout(() => this.ptabs()?.nativeElement.querySelector<HTMLElement>(`[data-tab="${id}"]`)?.scrollIntoView({ block: 'nearest', inline: 'nearest' }), 0);
  });

  layoutMenu(ev: MouseEvent) {
    const r = (ev.currentTarget as HTMLElement).getBoundingClientRect();
    const l = this.tabs.layout();
    const it = (v: 'tabs' | 'tile-v' | 'tile-h' | 'grid', label: string) => ({ label: (l === v ? '✓ ' : '   ') + label, action: () => this.tabs.setLayout(v) });
    this.ui.showMenuAt(r.left, r.bottom + 4, [
      it('tabs', 'Tabbed'), it('tile-v', 'Side by Side'), it('tile-h', 'Stacked'), it('grid', 'Grid'),
    ]);
  }

  /** The "⋯" button: the menus of the classic menu bar, one level deep. */
  moreMenu(ev: MouseEvent) {
    const el = ev.currentTarget as HTMLElement;
    const r = el.getBoundingClientRect();
    const s = this.settings();
    this.ui.showMenuAt(r.right - 220, r.bottom + 4, [
      ...this.menus.map(m => ({ label: m.title + '  ›', action: () => setTimeout(() => {
        const items = m.items.map(i => (i.sep ? { separator: true } : { label: i.label, hint: i.keys, action: () => void this.action(i.action!) }));
        this.ui.showMenuAt(r.right - 260, r.bottom + 4, items);
      }, 0) })),
      { separator: true },
      { label: (s?.showSessionManager ? '✓ ' : '') + 'Session Manager', action: () => void this.action('toggle-session-manager') },
      { label: (s?.showCommandWindow ? '✓ ' : '') + 'Command Window', action: () => void this.action('toggle-command-window') },
      { label: (s?.showButtonBar ? '✓ ' : '') + 'Button Bar', action: () => void this.action('toggle-button-bar') },
      { label: (s?.compactRows ? '✓ ' : '') + 'Compact Session Manager', action: () => this.ui.patchSettings({ compactRows: !s?.compactRows }) },
      { label: (s?.iconStyle === 'glyph' ? '✓ ' : '') + 'Glyph Icons (no tiles)', action: () => void this.action('toggle-icon-style') },
      { separator: true },
      { label: 'Switch to the Classic Layout (toolbar and menus)', action: () => void this.action('toggle-layout') },
    ]);
  }

  openMenu(ev: MouseEvent, title: string) {
    const m = this.menus.find(x => x.title === title);
    const r = (ev.currentTarget as HTMLElement).getBoundingClientRect();
    if (!m) return;
    this.ui.showMenuAt(r.left, r.bottom + 2, m.items.map(i => (i.sep ? { separator: true } : { label: i.label, hint: i.keys, action: () => void this.action(i.action!) })));
  }

  constructor() {
    // UI theme: dark, light or follow the operating system.
    const media = window.matchMedia('(prefers-color-scheme: light)');
    this.systemLight.set(media.matches);
    media.addEventListener('change', e => this.systemLight.set(e.matches));
    effect(() => {
      const t = this.settings()?.theme ?? 'system';
      const light = t === 'light' || (t === 'system' && this.systemLight());
      document.documentElement.classList.toggle('light', light);
      document.documentElement.classList.toggle('modern', this.settings()?.layout !== 'classic');
      this.ui.lightUi.set(light);
      const root = document.documentElement.style;
      root.setProperty('--ui-font', uiFontStack(this.settings()?.uiFont));
      root.setProperty('--ui-scale', String(uiFontSize(this.settings()?.uiFontSize) / 13));
      const accent = this.settings()?.accentColor;
      if (accent) document.documentElement.style.setProperty('--accent', accent);
      else document.documentElement.style.removeProperty('--accent');
    });
    effect(() => {
      const t = this.active();
      document.title = t ? `${t.title} - Termwright` : 'Termwright';
    });
  }

  /** macOS: size the title bar so its buttons and tabs sit on the same line
   *  as the window buttons, wherever this macOS version draws them. */
  private async alignWithWindowButtons(tries = 10) {
    const b = await this.backend.windowButtons().catch(() => null);
    if (!b) { if (tries > 0) setTimeout(() => void this.alignWithWindowButtons(tries - 1), 300); return; }
    const root = document.documentElement.style;
    root.setProperty('--inset-h', Math.round(Math.min(64, Math.max(36, b.centerY * 2))) + 'px');
    root.setProperty('--inset-left', Math.round(Math.min(130, Math.max(70, b.right + 14))) + 'px');
  }

  async ngOnInit() {
    if (this.backendMissing) {
      this.ui.toast('Backend not available: run the app with "wails dev" or a built binary.', 'error', 15000);
      return;
    }
    const info = await this.backend.getAppInfo();
    this.insetTitle.set(!!info.insetTitle);
    if (info.insetTitle) void this.alignWithWindowButtons();
    if (info.initError) this.ui.toast('Startup error: ' + info.initError, 'error', 20000);
    await this.ui.loadSettings();
    if (info.locked) { this.ui.locked.set(true); this.ui.lockScreen.set(true); }
    else if (this.ui.settings() && this.ui.settings()!.welcomeSeen !== WELCOME_VERSION) this.ui.open('welcome');
    await this.ui.refreshTree();
    await this.tabs.restoreTabs();
    void this.startupUpdateCheck();
    this.backend.on('menu', (a: string) => void this.action(a));
    this.backend.on('device:find', (d: { path: string; tab: string }) => { this.ui.movedInput.set({ find: d.path, tab: d.tab }); this.ui.open('moved'); });
    this.ui.actionHandler = a => void this.action(a);
    this.ui.runSnippet = s => void sendSnippet(this.ui, this.tabs, s);
    this.backend.on('toast', (t: { level: 'info' | 'error'; message: string }) => this.ui.toast(t.message, t.level));
    this.backend.on('app:confirm-quit', async () => {
      const n = this.tabs.connectedCount();
      if (n === 0 || await this.ui.confirm('Quit Termwright', `${n} session(s) are still connected. Disconnect and quit?`, 'Quit', true)) {
        await this.backend.quit();
      }
    });
  }

  /** Quiet check at start-up (at most once a day): only a notice when something newer exists. */
  private async startupUpdateCheck() {
    const s = this.ui.settings();
    if (!s?.updateOnStart || s.updateChannel === 'off') return;
    try {
      const last = Number(localStorage.getItem('termwright.updateCheck')) || 0;
      if (Date.now() - last < 20 * 3600_000) return;
      localStorage.setItem('termwright.updateCheck', String(Date.now()));
    } catch { /* storage unavailable: check anyway */ }
    try {
      const st = await this.backend.checkForUpdate();
      if (st.stable?.newer) this.ui.toast(`Termwright ${st.stable.latest} is available - Help → Check for Updates…`, 'success', 15000);
      else if (s.updateChannel === 'nightly' && st.nightly?.newer) this.ui.toast('A new nightly build is available - Help → Check for Updates…', 'success', 15000);
    } catch { /* offline or no token: stay quiet at start-up */ }
  }

  private loadNum(k: string, d: number): number {
    try { return Number(localStorage.getItem(k)) || d; } catch { return d; }
  }

  private saveNum(k: string, v: number) {
    try { localStorage.setItem(k, String(v)); } catch { /* storage unavailable */ }
  }

  /** Runs a named action from the native menu, toolbar or shortcuts. */
  async action(a: string) {
    const s = this.settings();
    const t = this.active();
    const h = t ? this.tabs.handles.get(t.id) : undefined;
    switch (a) {
      case 'connect': this.ui.patchSettings({ showSessionManager: true }); break;
      case 'quick-connect': this.ui.open('quickConnect'); break;
      case 'local-shell': await this.tabs.openLocalShell(); break;
      case 'reconnect': await this.tabs.reconnect(); break;
      case 'disconnect': await this.tabs.disconnect(); break;
      case 'clone-tab': await this.tabs.clone(); break;
      case 'close-tab': await this.tabs.close(); break;
      case 'log-toggle': await this.tabs.toggleLog(); break;
      case 'save-scrollback': await this.tabs.saveScrollback(); break;
      case 'save-scrollback-html': await this.tabs.saveScrollback(undefined, 'html'); break;
      case 'import': this.ui.open('import'); break;
      case 'discover': this.ui.open('discover'); break;
      case 'backups': this.ui.backupTargets.set(null); this.ui.open('backups'); break;
      case 'checklists': this.ui.open('checklists'); break;
      case 'collect': this.ui.open('collect'); break;
      case 'history': this.ui.open('history'); break;
      case 'export': this.ui.open('export'); break;
      case 'copy': if (this.treeFocused()) this.treeKey('c'); else if (!this.editInput('copy')) await this.tabs.copy(); break;
      case 'cut': this.editInput('cut'); break;
      case 'paste': if (this.treeFocused()) this.treeKey('v'); else if (!(await this.pasteInput())) await this.tabs.paste(); break;
      case 'copy-paste': await this.tabs.copy(); await this.tabs.paste(); break;
      case 'select-all': if (!this.editInput('select-all')) h?.term.selectAll(); break;
      case 'find': this.findOpen.set(true); setTimeout(() => document.getElementById('findInput')?.focus(), 20); break;
      case 'clear-scrollback': h?.term.write('\x1b[3J'); break;
      case 'clear-screen': h?.term.clear(); h?.term.focus(); break;
      case 'toggle-autoscroll': {
        const t = this.tabs.active();
        if (t?.kind === 'terminal') { this.tabs.patch(t.id, { autoScroll: t.autoScroll === false }); if (t.autoScroll === false) h?.term.scrollToBottom(); }
        break;
      }
      case 'toggle-timestamps': { const t = this.tabs.active(); if (t?.kind === 'terminal') this.tabs.patch(t.id, { timestamps: !t.timestamps }); break; }
      case 'reset': h?.term.reset(); break;
      case 'toggle-session-manager': this.ui.patchSettings({ showSessionManager: !s?.showSessionManager }); break;
      case 'toggle-session-manager-side': this.ui.patchSettings({ sessionManagerRight: !s?.sessionManagerRight, showSessionManager: true }); break;
      case 'toggle-command-window':
        this.ui.patchSettings({ showCommandWindow: !s?.showCommandWindow });
        if (!s?.showCommandWindow) setTimeout(() => this.cmdWin()?.focus(), 50);
        break;
      case 'send-all':
        this.ui.patchSettings({ showCommandWindow: true });
        setTimeout(() => this.cmdWin()?.focus(), 50);
        break;
      case 'toggle-button-bar': this.ui.patchSettings({ showButtonBar: !s?.showButtonBar }); break;
      case 'wake': {
        const t = this.tabs.active();
        if (t?.session?.mac) void this.tabs.wake(t.session, t.id, t.state === 'connected' ? 'none' : 'reconnect');
        else this.ui.toast('Right-click a session with a MAC address → Wake Up, or add the MAC address in Session Options → Connection.', 'info', 7000);
        break;
      }
      case 'web-page': {
        const t = this.tabs.active();
        if (t?.kind === 'terminal' && t.session.protocol === 'SSH2') this.ui.openWeb(t.session, t.id, 'session');
        else this.ui.toast('Select an SSH2 terminal tab, or right-click a session → Open Web Page Through This Session.', 'info', 6000);
        break;
      }
      case 'files-panel': {
        const t = this.tabs.active();
        if (t?.kind === 'terminal' && t.session.protocol === 'SSH2') this.tabs.patch(t.id, { filesPanel: !t.filesPanel });
        else this.ui.toast('The Files panel is for SSH2 terminal tabs.', 'info');
        break;
      }
      case 'zoom-in': if (s) this.ui.patchSettings({ fontSize: Math.min(48, s.fontSize + 1) }); break;
      case 'zoom-out': if (s) this.ui.patchSettings({ fontSize: Math.max(6, s.fontSize - 1) }); break;
      case 'zoom-reset': this.ui.patchSettings({ fontSize: 14 }); break;
      case 'session-options':
        if (t?.session.path) await this.ui.editSession(t.session.path, t.id);
        else if (this.ui.selectedPath() && !this.ui.selectedIsFolder()) await this.ui.editSession(this.ui.selectedPath());
        else this.ui.toast('Select a saved session first.');
        break;
      case 'global-options': this.ui.open('options'); break;
      case 'next-tab': this.tabs.next(); break;
      case 'prev-tab': this.tabs.next(-1); break;
      case 'sftp-tab': await this.tabs.openSftp(); break;
      case 'run-script': await this.tabs.runScript(); break;
      case 'cancel-scripts': this.tabs.cancelScripts(); break;
      case 'record-toggle': await this.tabs.toggleRecording(); break;
      case 'cast-toggle': await this.tabs.toggleCast(); break;
      case 'cast-marker': await this.tabs.addMarker(); break;
      case 'recordings': this.ui.open('recordings'); break;
      case 'xmodem': await this.tabs.sendXmodem(); break;
      case 'send-break': await this.tabs.sendBreak(); break;
      case 'send-file': await this.tabs.sendTextFile(); break;
      case 'save-selection': await this.tabs.saveSelection(); break;
      case 'user-guide': this.backend.openURL('https://github.com/nperiannan/termwright/blob/main/docs/USER_GUIDE.md'); break;
      case 'xmodem-receive': await this.tabs.receiveXmodem(); break;
      case 'scp-upload': await this.tabs.scp('up'); break;
      case 'scp-download': await this.tabs.scp('down'); break;
      case 'scp-upload-folder': await this.tabs.scp('up', undefined, true); break;
      case 'ymodem-send': await this.tabs.sendYmodem(); break;
      case 'ymodem-receive': await this.tabs.receiveYmodem(); break;
      case 'passwords': this.ui.open('passwords'); break;
      case 'check-updates': this.ui.open('update'); break;
      case 'quick-open': this.ui.open('palette'); break;
      case 'command-library': this.ui.open('commands'); break;
      case 'workspaces': this.ui.open('workspaces'); break;
      case 'compare': this.ui.open('compare'); break;
      case 'save-workspace': await saveWorkspace(this.ui, this.tabs); break;
      case 'key-setup': {
        const t = this.tabs.get(this.tabs.activeId());
        this.ui.keySetupTab.set(t?.kind === 'terminal' && t.session.protocol === 'SSH2' && t.state === 'connected' ? t.id : '');
        this.ui.open('keySetup');
        break;
      }
      case 'layout-tabs': this.tabs.setLayout('tabs'); break;
      case 'layout-tile-v': this.tabs.setLayout('tile-v'); break;
      case 'layout-tile-h': this.tabs.setLayout('tile-h'); break;
      case 'layout-grid': this.tabs.setLayout('grid'); break;
      case 'lock': await this.tabs.lock(false); break;
      case 'lock-all': await this.tabs.lock(true); break;
      case 'toggle-window-menu': this.ui.patchSettings({ inWindowMenu: !s?.inWindowMenu }); break;
      case 'toggle-icon-style': this.ui.patchSettings({ iconStyle: s?.iconStyle === 'glyph' ? '' : 'glyph' }); break;
      case 'toggle-layout': {
        const toClassic = s?.layout !== 'classic';
        this.ui.patchSettings({ layout: toClassic ? 'classic' : '' });
        this.ui.toast((toClassic ? 'Classic layout' : 'Modern layout') + ' - View → Modern / Classic Layout switches back'
          + (this.isMac ? '. The window title bar follows when Termwright restarts.' : ''), 'info', 6000);
        break;
      }
      case 'toggle-connect-bar':
        // the modern layout has no toolbar: Quick Open does what the Connect Bar does
        if (this.modern()) { this.action('quick-open'); this.ui.toast('In the modern layout Quick Open (' + this.quickOpenKeys + ') is the Connect Bar: type an address, "ssh user@host", "voss 10.1.1.1" or a session name and press Enter.', 'info', 8000); }
        else this.ui.patchSettings({ showConnectBar: !s?.showConnectBar });
        break;
      case 'shortcuts': this.ui.open('shortcuts'); break;
      case 'about': this.ui.open('about'); break;
      case 'welcome': this.ui.open('welcome'); break;
      case 'new-session': await this.ui.newSession(this.ui.currentFolder()); break;
      case 'capture-screen': await this.tabs.captureScreen(); break;
      case 'teach-fact': this.tabs.teachFact(); break;
    }
  }

  /** Copy/cut/select-all inside a focused text field (not the terminal). */
  private focusedInput(): HTMLInputElement | HTMLTextAreaElement | null {
    const el = document.activeElement;
    if ((el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) && !el.classList.contains('xterm-helper-textarea')) return el;
    return null;
  }

  /** The Session Manager has the focus (not its filter box): Copy / Paste act on sessions. */
  private treeFocused() { const a = document.activeElement; return !!a?.closest('app-session-tree') && !a.closest('input, textarea, select'); }
  private treeKey(k: string) { document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key: k, metaKey: this.isMac, ctrlKey: !this.isMac, bubbles: true })); }

  private editInput(op: 'copy' | 'cut' | 'select-all'): boolean {
    const el = this.focusedInput();
    if (!el) return false;
    if (op === 'select-all') { el.select(); return true; }
    const [a, b] = [el.selectionStart ?? 0, el.selectionEnd ?? 0];
    const text = el.value.slice(a, b);
    if (text) void this.backend.clipboardSet(text);
    if (op === 'cut' && text) {
      el.setRangeText('', a, b, 'start');
      el.dispatchEvent(new Event('input', { bubbles: true }));
    }
    return true;
  }

  private async pasteInput(): Promise<boolean> {
    const el = this.focusedInput();
    if (!el) return false;
    const text = await this.backend.clipboardGet();
    el.setRangeText(text, el.selectionStart ?? 0, el.selectionEnd ?? 0, 'end');
    el.dispatchEvent(new Event('input', { bubbles: true }));
    return true;
  }

  tryUnlock(id: string, ev: Event) {
    const el = ev.target as HTMLInputElement;
    if (!this.tabs.unlock(id, el.value)) {
      el.value = '';
      this.ui.toast('Wrong password', 'error');
    }
  }

  /** Connect Bar: see connectString. */
  async connectBar() {
    const text = this.connectText.trim();
    if (!text) return;
    if (await connectString(text, this.ui, this.tabs, this.backend)) this.connectText = '';
  }

  find(back = false) {
    const h = this.tabs.handles.get(this.tabs.activeId());
    if (!h || !this.findText) return;
    const opts = { caseSensitive: false, decorations: { matchOverviewRuler: '#e5c07b', activeMatchColorOverviewRuler: '#ff9f43', matchBackground: '#5c4a1f', activeMatchBackground: '#a86f00' } };
    if (back) h.search.findPrevious(this.findText, opts); else h.search.findNext(this.findText, opts);
  }

  closeFind() {
    this.findOpen.set(false);
    const h = this.tabs.handles.get(this.tabs.activeId());
    h?.search.clearDecorations();
    h?.term.focus();
  }

  tabMenu(ev: MouseEvent, t: TabView) {
    this.tabs.activeId.set(t.id);
    this.ui.showMenu(ev, [
      { label: 'Reconnect', action: () => this.tabs.reconnect(t.id) },
      { label: 'Disconnect', action: () => this.tabs.disconnect(t.id), disabled: t.state !== 'connected' || t.kind === 'sftp' },
      { label: 'Clone Session', action: () => this.tabs.clone(t.id) },
      ...(t.kind === 'terminal' ? [{ label: this.tabs.selected().has(t.id) ? 'Remove from Send Group' : 'Add to Send Group',
        action: () => this.tabs.toggleSelected(t.id) }] : []),
      ...(t.kind === 'terminal' && t.session.protocol === 'SSH2' ? [
        { separator: true },
        ...(t.hopTarget ? [
          { label: `Open SFTP Tab on ${t.session.hostname} (same login)`, action: () => this.tabs.openSftp(t.id, 'jump'), disabled: t.state !== 'connected' },
          { label: `Open SFTP Tab on ${t.hopTarget} (through this login)`, action: () => this.tabs.openSftp(t.id, 'target'), disabled: t.state !== 'connected' },
        ] : [{ label: 'Open SFTP Tab (same login)', action: () => this.tabs.openSftp(t.id), disabled: t.state !== 'connected' }]),
        { label: 'Open Web Page Through This Session...', action: () => this.ui.openWeb(t.session, t.id, 'session') },
        { label: (t.filesPanel ? '✓ ' : '') + 'Files Panel', hint: shortcutOf('files-panel', this.isMac), action: () => this.tabs.patch(t.id, { filesPanel: !t.filesPanel }) },
        { label: 'Set Up Key Login...', action: () => { this.ui.keySetupTab.set(t.id); this.ui.open('keySetup'); }, disabled: t.state !== 'connected' },
        { label: 'Connect SFTP Session (new login)', action: () => this.tabs.openSftpSession(t.session.path).catch(e => this.ui.error(e)),
          disabled: !t.session.path },
      ] : []),
      ...(t.kind === 'terminal' && t.session.hostname && (t.session.protocol === 'Telnet' || t.session.protocol === 'SSH2' || t.session.protocol === 'Raw')
        ? [{ label: 'Open Web Interface...', action: () => this.ui.openWeb(t.session, t.id, 'device') }] : []),
      ...(t.kind === 'terminal' && t.session.mac ? [{ label: 'Wake Up (Wake-on-LAN)', action: () => void this.tabs.wake(t.session, t.id, t.state === 'connected' ? 'none' : 'reconnect') }] : []),
      { separator: true },
      { label: t.logFile ? 'Stop Logging' : 'Log Session...', action: () => this.tabs.toggleLog(t.id) },
      ...(t.kind === 'terminal' ? [
        { label: t.castFile ? 'Stop Recording' : 'Record Session', action: () => this.tabs.toggleCast(t.id), disabled: !t.castFile && t.state !== 'connected' },
        ...(t.castFile ? [{ label: 'Add Marker...', hint: this.isMac ? '⌘J' : 'Ctrl+Shift+J', action: () => this.tabs.addMarker(t.id) }] : []),
      ] : []),
      { label: 'Save Scrollback As...', action: () => this.tabs.saveScrollback(t.id) },
      ...(t.kind === 'terminal' ? [{ label: 'Save Scrollback As HTML...', action: () => this.tabs.saveScrollback(t.id, 'html') },
        { label: 'Save Screen As Image...', hint: shortcutOf('capture-screen', this.isMac), action: () => this.tabs.captureScreen(t.id) }] : []),
      ...(t.kind === 'terminal' ? [{ label: 'Compare With...', action: () => this.compareMenu(ev, t) }] : []),
      ...(t.kind === 'terminal' ? [
        { label: (t.timestamps ? '✓ ' : '') + 'Timestamps', action: () => this.tabs.patch(t.id, { timestamps: !t.timestamps }) },
        { label: (t.autoScroll !== false ? '✓ ' : '') + 'Auto Scroll', action: () => { this.tabs.patch(t.id, { autoScroll: t.autoScroll === false }); if (t.autoScroll === false) this.tabs.handles.get(t.id)?.term.scrollToBottom(); } },
        { label: (t.sendBar ? '✓ ' : '') + (t.session.protocol === 'Serial' ? 'Send Bar' : 'Terminal Bar'), action: () => this.tabs.patch(t.id, { sendBar: !t.sendBar }) },
        { label: 'Clear Screen', action: () => this.tabs.handles.get(t.id)?.term.clear() },
      ] : []),
      { label: 'Session Options...', action: () => this.ui.editSession(t.session.path, t.id), disabled: !t.session.path },
      ...(t.session.protocol === 'SSH2' && t.session.path ? [t.session.openAs === 'sftp'
        ? { label: 'Create SSH Session (terminal)', action: () => void this.ui.copySessionAs(t.session.path, '') }
        : { label: 'Create SFTP Session', action: () => void this.ui.copySessionAs(t.session.path, 'sftp') }] : []),
      { label: 'Rename Tab...', action: async () => {
        const n = await this.ui.input('Rename Tab', 'Tab title', t.title);
        if (n) this.tabs.patch(t.id, { title: n });
      } },
      { separator: true },
      { label: 'Close', action: () => this.tabs.close(t.id) },
      { label: 'Close Other Tabs', action: async () => {
        for (const o of this.tabs.tabs().filter(x => x.id !== t.id)) await this.tabs.close(o.id);
      } },
    ]);
  }

  /** Second menu: the other terminal tabs (or a file) to compare this tab's output with. */
  private compareMenu(ev: MouseEvent, t: TabView) {
    const open = (other: string) => { this.ui.compareWith.set(['tab:' + t.id, other]); this.ui.open('compare'); };
    const others = this.tabs.tabs().filter(o => o.kind === 'terminal' && o.id !== t.id);
    setTimeout(() => this.ui.showMenuAt(ev.clientX + 12, ev.clientY, [
      ...others.map(o => ({ label: o.title, hint: o.session.hostname, action: () => open('tab:' + o.id) })),
      ...(others.length ? [] : [{ label: 'No other terminal tabs open', disabled: true }]),
      { separator: true },
      { label: 'A File...', action: () => open('file') },
    ]), 0);
  }

  readonly activeXfer = computed(() => this.tabs.transfers().find(t => t.state === 'running') ?? null);

  pct(t: { bytes: number; total: number }) { return t.total > 0 ? Math.floor((t.bytes * 100) / t.total) : 0; }

  stateLabel(t: TabView | null): string {
    if (!t) return 'Ready';
    return t.message || t.state;
  }

  startDrag(ev: MouseEvent, kind: 'tree' | 'cmd') {
    ev.preventDefault();
    const startX = ev.clientX, startY = ev.clientY;
    const w0 = this.treeWidth(), h0 = this.cmdHeight();
    const move = (e: MouseEvent) => {
      const dx = this.settings()?.sessionManagerRight ? startX - e.clientX : e.clientX - startX; // dragging towards the work area widens it
      if (kind === 'tree') this.treeWidth.set(Math.max(160, Math.min(600, w0 + dx)));
      else this.cmdHeight.set(Math.max(60, Math.min(500, h0 - (e.clientY - startY))));
    };
    const up = () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
      this.saveNum('termwright.treeWidth', this.treeWidth());
      this.saveNum('termwright.cmdHeight', this.cmdHeight());
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
  }

  onMiddleClick(ev: MouseEvent, t: TabView) {
    if (ev.button === 1) { ev.preventDefault(); void this.tabs.close(t.id); }
  }
}
