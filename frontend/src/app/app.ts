import { Component, OnInit, computed, effect, inject, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from './core/backend';
import { Tabs, TabView } from './core/tabs.service';
import { SessionNode, findDeviceType } from './core/models';
import { menuBar } from './core/menu';
import { envInfo, envOf } from './core/env';
import { Ui } from './core/ui.service';
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
import { KeySetupDialog } from './dialogs/key-setup-dialog';
import { Palette } from './dialogs/palette';
import { Commands, sendSnippet } from './dialogs/commands';
import { FolderDefaults, Workspaces, saveWorkspace } from './dialogs/workspaces';
import { Compare } from './dialogs/compare';
import { ExportDialog, ImportDialog } from './dialogs/import-export';
import { About, Overlays } from './dialogs/overlays';

@Component({
  selector: 'app-root',
  imports: [FormsModule, Icon, SessionTree, TerminalView, SftpView, CommandWindow, ButtonBar, SessionEditor, QuickConnect,
    PromptDialog, OptionsDialog, PasswordsDialog, UpdateDialog, KeySetupDialog, Palette, Commands, Workspaces, FolderDefaults, Compare, ImportDialog, ExportDialog, DiscoverDialog, BulkEdit, Overlays, About],
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
  tabEnv(t: TabView) { return envInfo(envOf(t.session?.path ?? '', t.session?.environment, this.settings()?.folderDefaults)); }
  readonly tiled = computed(() => this.tabs.layout() !== 'tabs');
  readonly gridCols = computed(() => {
    const n = this.tabs.tabs().length;
    const l = this.tabs.layout();
    return l === 'grid' ? Math.max(1, Math.ceil(Math.sqrt(n))) : n;
  });
  connectText = '';
  readonly isMac = navigator.platform.toLowerCase().includes('mac') || navigator.userAgent.includes('Mac OS');
  readonly menus = menuBar(this.isMac);

  openMenu(ev: MouseEvent, title: string) {
    const m = this.menus.find(x => x.title === title);
    const r = (ev.currentTarget as HTMLElement).getBoundingClientRect();
    if (!m) return;
    this.ui.showMenuAt(r.left, r.bottom + 2, m.items.map(i => (i.sep ? { separator: true } : { label: i.label, hint: i.keys, action: () => void this.action(i.action!) })));
  }

  constructor() {
    // UI theme: dark, light or follow the operating system.
    const media = window.matchMedia('(prefers-color-scheme: light)');
    const systemLight = signal(media.matches);
    media.addEventListener('change', e => systemLight.set(e.matches));
    effect(() => {
      const t = this.settings()?.theme ?? 'dark';
      const light = t === 'light' || (t === 'system' && systemLight());
      document.documentElement.classList.toggle('light', light);
      this.ui.lightUi.set(light);
      const accent = this.settings()?.accentColor;
      if (accent) document.documentElement.style.setProperty('--accent', accent);
      else document.documentElement.style.removeProperty('--accent');
    });
    effect(() => {
      const t = this.active();
      document.title = t ? `${t.title} - Termwright` : 'Termwright';
    });
  }

  async ngOnInit() {
    if (this.backendMissing) {
      this.ui.toast('Backend not available: run the app with "wails dev" or a built binary.', 'error', 15000);
      return;
    }
    const info = await this.backend.getAppInfo();
    if (info.initError) this.ui.toast('Startup error: ' + info.initError, 'error', 20000);
    await this.ui.loadSettings();
    if (info.locked) { this.ui.locked.set(true); this.ui.open('passwords'); }
    await this.ui.refreshTree();
    await this.tabs.restoreTabs();
    void this.startupUpdateCheck();
    this.backend.on('menu', (a: string) => void this.action(a));
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
      case 'import': this.ui.open('import'); break;
      case 'discover': this.ui.open('discover'); break;
      case 'export': this.ui.open('export'); break;
      case 'copy': if (!this.editInput('copy')) await this.tabs.copy(); break;
      case 'cut': this.editInput('cut'); break;
      case 'paste': if (!(await this.pasteInput())) await this.tabs.paste(); break;
      case 'copy-paste': await this.tabs.copy(); await this.tabs.paste(); break;
      case 'select-all': if (!this.editInput('select-all')) h?.term.selectAll(); break;
      case 'find': this.findOpen.set(true); setTimeout(() => document.getElementById('findInput')?.focus(), 20); break;
      case 'clear-scrollback': h?.term.clear(); break;
      case 'clear-screen': h?.term.write('\x1b[H\x1b[2J'); break;
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
      case 'toggle-connect-bar': this.ui.patchSettings({ showConnectBar: !s?.showConnectBar }); break;
      case 'shortcuts': this.ui.open('shortcuts'); break;
      case 'about': this.ui.open('about'); break;
    }
  }

  /** Copy/cut/select-all inside a focused text field (not the terminal). */
  private focusedInput(): HTMLInputElement | HTMLTextAreaElement | null {
    const el = document.activeElement;
    if ((el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) && !el.classList.contains('xterm-helper-textarea')) return el;
    return null;
  }

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

  /** Connect Bar: "ssh [-p port] [-l user] user@host[:port]", "telnet host [port]",
   *  "serial PORT [baud]", "raw host port", a saved session name, or a bare host (SSH2). */
  async connectBar() {
    const text = this.connectText.trim();
    if (!text) return;
    const words = text.split(/\s+/);
    const cmd = words[0].toLowerCase();
    const saved = this.findSession(text);
    if (saved && !['ssh', 'ssh2', 'telnet', 'serial', 'raw'].includes(cmd)) {
      this.connectText = '';
      await this.tabs.openSession(saved);
      return;
    }
    try {
      const s = await this.backend.newSessionDefaults();
      Object.assign(s, { name: '', logonActions: [], triggers: [], portForwards: [], description: [] });
      let args = ['ssh', 'ssh2', 'telnet', 'serial', 'raw'].includes(cmd) ? words.slice(1) : words;
      // A device type name anywhere in the line: "voss 10.1.1.1", "telnet exos 10.0.0.5 2001"
      const dev = args.map(w => findDeviceType(this.ui.settings()?.deviceTypes, w)).find(Boolean);
      if (dev) {
        args = args.filter(w => !findDeviceType([dev], w));
        s.deviceType = dev.name;
        if (dev.username) s.username = dev.username;
      }
      const explicit = ['ssh', 'ssh2', 'telnet', 'serial', 'raw'].includes(cmd);
      const proto = cmd === 'telnet' ? 'Telnet' : cmd === 'serial' ? 'Serial' : cmd === 'raw' ? 'Raw' : !explicit && dev?.protocol ? dev.protocol : 'SSH2';
      s.protocol = proto;
      s.port = proto === 'Telnet' ? 23 : 22;
      if (dev?.port && !explicit) s.port = dev.port;
      const pos: string[] = [];
      for (let i = 0; i < args.length; i++) {
        if (args[i] === '-p' && args[i + 1]) s.port = Number(args[++i]);
        else if (args[i] === '-l' && args[i + 1]) s.username = args[++i];
        else pos.push(args[i]);
      }
      if (proto === 'Serial') {
        if (!pos[0]) throw new Error('usage: serial PORT [baud]');
        s.comPort = pos[0];
        s.baudRate = Number(pos[1]) || 9600;
      } else {
        let host = pos[0] ?? '';
        const at = host.lastIndexOf('@');
        if (at > 0) { s.username = host.slice(0, at); host = host.slice(at + 1); }
        const m = /^([^:\[\]]+):(\d+)$/.exec(host) ?? /^\[(.+)\]:(\d+)$/.exec(host);
        if (m) { host = m[1]; s.port = Number(m[2]); }
        if (pos[1] && /^\d+$/.test(pos[1])) s.port = Number(pos[1]);
        if (!host) throw new Error('no host given');
        s.hostname = host;
      }
      this.connectText = '';
      await this.tabs.openAdHoc(s);
    } catch (e) { this.ui.error(e); }
  }

  private findSession(q: string): string | null {
    const ql = q.toLowerCase();
    let found: string | null = null;
    const walk = (n: SessionNode) => (n.children ?? []).forEach(c => {
      if (found) return;
      if (c.isFolder) walk(c);
      else if (c.path.toLowerCase() === ql || c.name.toLowerCase() === ql) found = c.path;
    });
    const root = this.ui.tree();
    if (root) walk(root);
    return found;
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
        { label: 'Set Up Key Login...', action: () => { this.ui.keySetupTab.set(t.id); this.ui.open('keySetup'); }, disabled: t.state !== 'connected' },
        { label: 'Connect SFTP Session (new login)', action: () => this.tabs.openSftpSession(t.session.path).catch(e => this.ui.error(e)),
          disabled: !t.session.path },
      ] : []),
      { separator: true },
      { label: t.logFile ? 'Stop Logging' : 'Log Session...', action: () => this.tabs.toggleLog(t.id) },
      { label: 'Save Scrollback As...', action: () => this.tabs.saveScrollback(t.id) },
      ...(t.kind === 'terminal' ? [{ label: 'Compare With...', action: () => this.compareMenu(ev, t) }] : []),
      ...(t.kind === 'terminal' ? [{ label: t.timestamps ? 'Hide Timestamps' : 'Show Timestamps', action: () => this.tabs.patch(t.id, { timestamps: !t.timestamps }) }] : []),
      { label: 'Session Options...', action: () => this.ui.editSession(t.session.path, t.id), disabled: !t.session.path },
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
