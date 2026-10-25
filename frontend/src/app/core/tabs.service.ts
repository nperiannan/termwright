import { Injectable, computed, effect, inject, signal } from '@angular/core';
import type { Terminal } from '@xterm/xterm';
import type { FitAddon } from '@xterm/addon-fit';
import type { SearchAddon } from '@xterm/addon-search';
import { Backend, SftpMode } from './backend';
import { AlertEvent, Session, TabInfo, Transfer, unescape } from './models';
import { Ui } from './ui.service';

export interface TabView {
  id: string;
  title: string;
  state: string;
  message: string;
  info: string;
  session: Session;
  logFile: string;
  cols: number;
  rows: number;
  locked: boolean;
  kind: 'terminal' | 'sftp';
  parentId: string;
  home: string;
  recording: boolean;
  alert?: boolean;
  timestamps?: boolean;
  /** Terminal tab: user@host of its "ssh … host" remote command. SFTP tab: the host it browses through the jump station. */
  hopTarget?: string;
  sftpMode?: SftpMode;
  /** SFTP tab browsing as another user: root (sudo) or via a Run As command. */
  root?: boolean;
  runAsUser?: string;
}

export type Layout = 'tabs' | 'tile-v' | 'tile-h' | 'grid';
export type SendTarget = 'active' | 'all' | 'selected' | boolean;

export interface TermHandle { term: Terminal; fit: FitAddon; search: SearchAddon; }

/** Owns the list of open terminal tabs and routes actions to them. */
@Injectable({ providedIn: 'root' })
export class Tabs {
  private backend = inject(Backend);
  private ui = inject(Ui);

  readonly tabs = signal<TabView[]>([]);
  readonly activeId = signal<string>('');
  readonly active = computed(() => this.tabs().find(t => t.id === this.activeId()) ?? null);
  readonly handles = new Map<string, TermHandle>();
  readonly layout = signal<Layout>(this.loadLayout());
  private lockPw = new Map<string, string>();
  readonly transfers = signal<Transfer[]>([]);
  readonly scripts = signal<{ runId: string; tabId: string; name: string }[]>([]);

  private saveTimer?: ReturnType<typeof setTimeout>;

  /** Remembers saved sessions that are open so they can be reopened next start. */
  private rememberTabs() {
    clearTimeout(this.saveTimer);
    this.saveTimer = setTimeout(() => {
      const s = this.ui.settings();
      if (!s?.restoreTabs) return;
      const paths = this.tabs().filter(t => t.kind === 'terminal' && t.session.path).map(t => t.session.path);
      if (JSON.stringify(paths) !== JSON.stringify(s.openTabs ?? [])) this.ui.patchSettings({ openTabs: paths });
    }, 1000);
  }

  async restoreTabs() {
    const s = this.ui.settings();
    if (!s?.restoreTabs) return;
    for (const p of s.openTabs ?? []) await this.openSession(p);
  }

  constructor() {
    effect(() => { this.tabs(); this.rememberTabs(); });
    // scripts read the screen exactly as rendered (crt.Screen.Get, CurrentRow, …)
    this.backend.on('screen:request', (r: { id: string; tabId: string }) => {
      const term = this.handles.get(r.tabId)?.term;
      if (!term) { void this.backend.screenReply(r.id, null); return; }
      const b = term.buffer.active;
      const lines: string[] = [];
      for (let y = 0; y < term.rows; y++) lines.push(b.getLine(b.baseY + y)?.translateToString(false) ?? ''.padEnd(term.cols));
      void this.backend.screenReply(r.id, { lines, row: b.cursorY + 1, col: b.cursorX + 1, rows: term.rows, cols: term.cols });
    });
    this.backend.on('xfer:progress', (tr: Transfer) => {
      this.transfers.update(l => {
        const i = l.findIndex(x => x.id === tr.id);
        return i < 0 ? [tr, ...l].slice(0, 50) : l.map(x => (x.id === tr.id ? tr : x));
      });
      if (tr.state === 'error') this.ui.toast(`Transfer failed: ${tr.error}`, 'error');
      if (tr.state === 'done' && !['up', 'down'].includes(tr.direction)) {
        const what = tr.direction.startsWith('zmodem') ? 'ZMODEM' : tr.direction.startsWith('ymodem') ? 'YMODEM'
          : tr.direction.startsWith('scp') ? 'SCP' : 'XMODEM';
        this.ui.toast(`${what} transfer complete: ${tr.file}`, 'success', 8000);
      }
    });
    this.backend.on('tab:opened', (ti: TabInfo) => this.add(ti));
    this.backend.on('tab:activate', (id: string) => { if (this.get(id)) this.activeId.set(id); });
    this.backend.on('alert', (a: AlertEvent) => this.onAlert(a));
    effect(() => { const id = this.activeId(); if (this.get(id)?.alert) this.patch(id, { alert: false }); });
    this.backend.on('zmodem:send-request', async (tabId: string) => {
      this.activeId.set(tabId);
      const files = await this.backend.chooseOpenFiles('ZMODEM: Choose Files to Send').catch(() => null);
      await this.backend.zmodemSend(tabId, files ?? []).catch(e => this.ui.error(e));
    });
    this.backend.on('sftp:closed', (id: string) => {
      const t = this.get(id);
      if (t && !t.parentId) this.ui.toast(`${t.title}: connection closed by the server`, 'error');
      if (t) void this.close(id);
    });
    this.backend.on('script:status', (st: { runId: string; tabId: string; name: string; state: string; message: string }) => {
      if (st.state === 'running') this.scripts.update(l => [...l, st]);
      else {
        this.scripts.update(l => l.filter(x => x.runId !== st.runId));
        if (st.state === 'error') this.ui.toast(`Script ${st.name}: ${st.message}`, 'error', 10000);
        else if (st.state === 'done') this.ui.toast(`Script ${st.name} finished`, 'success');
      }
    });
    this.backend.on('script:log', (l: { message: string }) => this.ui.toast(l.message, 'info', 6000));
  }

  async openSftp(parentId = this.activeId(), mode: SftpMode = '') {
    let p = this.get(parentId);
    if (p?.kind === 'sftp') p = this.get(p.parentId);
    if (!p || p.state !== 'connected' || p.session.protocol !== 'SSH2') {
      this.ui.toast('Select a connected SSH2 tab first.', 'error');
      return;
    }
    try {
      const r = await this.backend.sftpOpen(p.id, mode);
      const tv: TabView = {
        id: r.id, title: 'SFTP: ' + (r.target || p.title), state: 'connected', message: '',
        info: r.target ? `SFTP to ${r.target} through ${p.session.hostname}` : 'SFTP over ' + p.info,
        session: p.session, logFile: '', cols: 0, rows: 0, locked: false, kind: 'sftp', parentId: p.id, home: r.home, recording: false,
        hopTarget: r.target, sftpMode: mode,
      };
      this.tabs.update(l => [...l, tv]);
      this.activeId.set(tv.id);
    } catch (e) { this.ui.error(e); }
  }

  async runScript(id = this.activeId()) {
    const t = this.get(id);
    if (!t || t.kind !== 'terminal') { this.ui.toast('Select a terminal tab first.', 'error'); return; }
    const f = await this.backend.chooseOpenFile('Run Script', [{ displayName: 'Scripts (Python, VBScript, JavaScript)', pattern: '*.py;*.vbs;*.js' }]).catch(() => '');
    if (f) await this.backend.runScriptFile(id, f).catch(e => this.ui.error(e));
  }

  async runScriptPath(path: string, id = this.activeId()) {
    if (id) await this.backend.runScriptFile(id, path).catch(e => this.ui.error(e));
  }

  cancelScripts() {
    for (const s of this.scripts()) void this.backend.cancelScript(s.runId);
  }

  async toggleRecording(id = this.activeId()) {
    const t = this.get(id);
    if (!t || t.kind !== 'terminal') return;
    try {
      if (!t.recording) {
        await this.backend.startRecording(id);
        this.patch(id, { recording: true });
        this.ui.toast('Recording commands. Stop recording to save the script.');
        return;
      }
      const src = await this.backend.stopRecording(id);
      this.patch(id, { recording: false });
      const path = await this.backend.chooseSaveFile('Save Recorded Script', (t.title || 'script').replace(/[\\/:*?"<>|@ ]/g, '_') + '.js',
        [{ displayName: 'JavaScript', pattern: '*.js' }]);
      if (path) {
        await this.backend.saveTextFile(path, src);
        this.ui.toast('Script saved to ' + path, 'success');
      }
    } catch (e) { this.ui.error(e); }
  }

  async receiveXmodem(id = this.activeId()) {
    const t = this.get(id);
    if (!t || t.kind !== 'terminal' || t.state !== 'connected') { this.ui.toast('Select a connected tab first.', 'error'); return; }
    const out = await this.backend.chooseSaveFile('Save XMODEM Download As', 'download.bin').catch(() => '');
    if (!out) return;
    try {
      await this.backend.receiveXmodem(id, out);
      this.ui.toast('Ready to receive - start the XMODEM send on the device now.');
    } catch (e) { this.ui.error(e); }
  }

  async sendYmodem(id = this.activeId()) {
    const t = this.get(id);
    if (!t || t.kind !== 'terminal' || t.state !== 'connected') { this.ui.toast('Select a connected tab first.', 'error'); return; }
    const files = await this.backend.chooseOpenFiles('Send Files with YMODEM').catch(() => null);
    if (!files?.length) return;
    try {
      await this.backend.sendYmodem(id, files);
      this.ui.toast('Waiting for the receiver - start "rb" (or the device\'s YMODEM receive) now.');
    } catch (e) { this.ui.error(e); }
  }

  async receiveYmodem(id = this.activeId()) {
    const t = this.get(id);
    if (!t || t.kind !== 'terminal' || t.state !== 'connected') { this.ui.toast('Select a connected tab first.', 'error'); return; }
    try {
      await this.backend.receiveYmodem(id);
      this.ui.toast('Ready to receive into the Downloads folder - start "sb <files>" on the device now.');
    } catch (e) { this.ui.error(e); }
  }

  async scp(dir: 'up' | 'down', id = this.activeId(), folder = false) {
    const t = this.get(id);
    if (!t || t.kind !== 'terminal' || t.state !== 'connected' || t.session.protocol !== 'SSH2') {
      this.ui.toast('Select a connected SSH2 tab first.', 'error'); return;
    }
    try {
      if (dir === 'up') {
        const f = folder ? await this.backend.chooseDirectory('SCP Upload: Choose Folder')
          : await this.backend.chooseOpenFile('SCP Upload: Choose File');
        if (!f) return;
        const dest = await this.ui.input('SCP Upload', 'Remote destination (folder or file path)', '.');
        if (dest === null) return;
        await this.backend.scpTransfer(id, 'up', f, dest || '.');
      } else {
        const src = await this.ui.input('SCP Download', 'Remote file or folder path (folders are copied recursively)');
        if (!src) return;
        const dir2 = await this.backend.chooseDirectory('Save Into Folder');
        if (!dir2) return;
        await this.backend.scpTransfer(id, 'down', src, dir2);
      }
    } catch (e) { this.ui.error(e); }
  }

  /** Sends a text file line by line (uses the paste line delay). */
  async sendTextFile(id = this.activeId()) {
    const t = this.get(id);
    if (!t || t.kind !== 'terminal' || t.state !== 'connected') { this.ui.toast('Select a connected tab first.', 'error'); return; }
    const f = await this.backend.chooseOpenFile('Send Text File').catch(() => '');
    if (!f) return;
    try {
      const text = await this.backend.readTextFile(f);
      await this.send(text.replace(/\r?\n/g, '\r'), false, true);
      this.ui.toast('Sent ' + f, 'success');
    } catch (e) { this.ui.error(e); }
  }

  async saveSelection(id = this.activeId()) {
    const sel = this.handles.get(id)?.term.getSelection();
    if (!sel) { this.ui.toast('Select some text in the terminal first.'); return; }
    try {
      const p = await this.backend.chooseSaveFile('Save Selection As', 'selection.txt', [{ displayName: 'Text', pattern: '*.txt;*.log' }]);
      if (p) { await this.backend.saveTextFile(p, sel); this.ui.toast('Saved to ' + p, 'success'); }
    } catch (e) { this.ui.error(e); }
  }

  async sendBreak(id = this.activeId()) {
    if (id) await this.backend.sendBreak(id).then(() => this.ui.toast('Break sent'), e => this.ui.error(e));
  }

  async sendXmodem(id = this.activeId()) {
    const t = this.get(id);
    if (!t || t.kind !== 'terminal' || t.state !== 'connected') { this.ui.toast('Select a connected tab first.', 'error'); return; }
    const f = await this.backend.chooseOpenFile('Send File with XMODEM').catch(() => '');
    if (!f) return;
    const oneK = await this.ui.confirm('XMODEM Block Size', 'Use 1K blocks (XMODEM-1K)? Choose Cancel for classic 128-byte blocks, which every receiver supports.', 'Use 1K');
    try {
      await this.backend.sendXmodem(id, f, oneK ? 1024 : 128);
      this.ui.toast('Waiting for the receiver to start (e.g. run the boot loader\'s XMODEM receive command)...');
    } catch (e) { this.ui.error(e); }
  }

  private loadLayout(): Layout {
    try { return (localStorage.getItem('termwright.layout') as Layout) || 'tabs'; } catch { return 'tabs'; }
  }

  setLayout(l: Layout) {
    this.layout.set(l);
    try { localStorage.setItem('termwright.layout', l); } catch { /* storage unavailable */ }
  }

  /** Locks tabs so they ignore keyboard input until unlocked with the password. */
  async lock(all = false) {
    const ids = all ? this.tabs().map(t => t.id) : [this.activeId()].filter(Boolean);
    if (!ids.length) return;
    const pw = await this.ui.input(all ? 'Lock All Sessions' : 'Lock Session', 'Unlock password (may be empty)');
    if (pw === null) return;
    for (const id of ids) {
      this.lockPw.set(id, pw);
      this.patch(id, { locked: true });
    }
  }

  unlock(id: string, pw: string): boolean {
    if ((this.lockPw.get(id) ?? '') !== pw) return false;
    this.lockPw.delete(id);
    this.patch(id, { locked: false });
    this.handles.get(id)?.term.focus();
    return true;
  }
  private unsubs = new Map<string, (() => void)[]>();
  /** Current remote folder of each SFTP tab (target folder for server-to-server copies). */
  readonly sftpDirs = new Map<string, string>();

  private add(ti: TabInfo) {
    const tv: TabView = {
      id: ti.id, title: ti.title, state: ti.state, message: '', info: ti.info,
      session: ti.session, logFile: ti.logFile, cols: 0, rows: 0, locked: false,
      kind: 'terminal', parentId: '', home: '', recording: false,
    };
    this.backend.sftpHopTarget(ti.session).then(h => { if (h) this.patch(ti.id, { hopTarget: h }); }).catch(() => {});
    this.unsubs.set(ti.id, [
      this.backend.on('term:status:' + ti.id, (st: { state: string; message: string; info: string }) => {
        this.patch(ti.id, { state: st.state, message: st.message, info: st.info });
        const h = this.handles.get(ti.id);
        if (!h) return;
        if (st.state === 'disconnected' || st.state === 'error') {
          const color = st.state === 'error' ? '31' : '90';
          h.term.write(`\r\n\x1b[${color}m[${st.message}]\x1b[0m\r\n\x1b[90mPress Enter to reconnect.\x1b[0m\r\n`);
        }
      }),
      this.backend.on('term:log:' + ti.id, (path: string) => this.patch(ti.id, { logFile: path })),
    ]);
    this.tabs.update(l => [...l, tv]);
    this.activeId.set(ti.id);
  }

  patch(id: string, p: Partial<TabView>) {
    this.tabs.update(l => l.map(t => (t.id === id ? { ...t, ...p } : t)));
  }

  get(id: string) { return this.tabs().find(t => t.id === id) ?? null; }

  /** Opens a saved session as configured (terminal, or SFTP-only when "Connect as" is SFTP). */
  async openSession(path: string, as: 'auto' | 'terminal' | 'sftp' = 'auto', mode: SftpMode = '') {
    try {
      if (as !== 'terminal') {
        const s = await this.backend.getSession(path);
        if (as === 'sftp' || (s.protocol === 'SSH2' && s.openAs === 'sftp')) {
          if (s.protocol !== 'SSH2') { this.ui.toast('SFTP needs an SSH2 session.', 'error'); return; }
          await this.openSftpSession(path, mode);
          return;
        }
      }
      this.add(await this.backend.openTab(path));
    } catch (e) { this.ui.error(e); }
  }

  /** Standalone SFTP tab: its own SSH login that only starts SFTP (no terminal). */
  async openSftpSession(path: string, mode: SftpMode = '') {
    const s = await this.backend.getSession(path);
    this.ui.toast('Connecting SFTP to ' + (s.hostname || s.name) + '...');
    const r = await this.backend.sftpConnect(path, mode);
    const tv: TabView = {
      id: r.id, title: 'SFTP: ' + (r.target || s.name), state: 'connected', message: '', info: r.info,
      session: s, logFile: '', cols: 0, rows: 0, locked: false, kind: 'sftp', parentId: '', home: r.home, recording: false,
      hopTarget: r.target, sftpMode: mode,
    };
    this.tabs.update(l => [...l, tv]);
    this.activeId.set(tv.id);
  }

  async openAdHoc(s: Session) {
    try { this.add(await this.backend.openAdHoc(s)); } catch (e) { this.ui.error(e); }
  }

  async openLocalShell() {
    try {
      const s = await this.backend.newSessionDefaults();
      s.protocol = 'Local Shell';
      s.autoReconnect = false;
      s.triggers = [];
      s.portForwards = [];
      s.name = 'Local Shell';
      s.logonActions = [];
      s.logOnConnect = false;
      await this.openAdHoc(s);
    } catch (e) { this.ui.error(e); }
  }

  /** Called by the terminal view once xterm is ready. */
  start(id: string) {
    const h = this.handles.get(id);
    const cols = h?.term.cols ?? 80, rows = h?.term.rows ?? 24;
    this.backend.startTab(id, cols, rows).catch(e => this.ui.error(e));
  }

  async reconnect(id = this.activeId()) {
    const t = this.get(id);
    if (!t) return;
    if (t.kind === 'sftp') {
      await this.close(id);
      if (!t.parentId && t.session.path) await this.openSftpSession(t.session.path, t.sftpMode).catch(e => this.ui.error(e));
      else if (t.parentId) await this.openSftp(t.parentId, t.sftpMode);
      return;
    }
    if (t.state === 'connected' || t.state === 'connecting') await this.backend.disconnectTab(id);
    await this.backend.refreshTabSession(id).catch(() => {});
    this.handles.get(id)?.term.write('\r\n');
    this.start(id);
  }

  async disconnect(id = this.activeId()) {
    if (id) await this.backend.disconnectTab(id).catch(e => this.ui.error(e));
  }

  async clone(id = this.activeId()) {
    const t = this.get(id);
    if (!t) return;
    if (t.session.path) await this.openSession(t.session.path, t.kind === 'sftp' ? 'sftp' : 'terminal');
    else await this.openAdHoc(t.session);
  }

  async close(id = this.activeId()) {
    const t = this.get(id);
    if (!t) return;
    const s = this.ui.settings();
    if (t.kind === 'sftp') {
      await this.backend.sftpClose(id).catch(() => {});
    } else if (t.state === 'connected' && s?.confirmClose) {
      const ok = await this.ui.confirm('Close Tab', `Disconnect from "${t.title}"?`, 'Disconnect');
      if (!ok) return;
    }
    if (t.kind === 'terminal') {
      for (const c of this.tabs().filter(x => x.parentId === id)) await this.close(c.id);
      await this.backend.closeTab(id).catch(() => {});
    }
    this.unsubs.get(id)?.forEach(u => u());
    this.unsubs.delete(id);
    const list = this.tabs();
    const idx = list.findIndex(x => x.id === id);
    this.tabs.set(list.filter(x => x.id !== id));
    if (this.activeId() === id) {
      const next = this.tabs()[Math.min(idx, this.tabs().length - 1)];
      this.activeId.set(next?.id ?? '');
    }
  }

  next(step = 1) {
    const l = this.tabs();
    if (!l.length) return;
    const i = l.findIndex(t => t.id === this.activeId());
    this.activeId.set(l[(i + step + l.length) % l.length].id);
  }

  connectedCount() { return this.tabs().filter(t => t.kind === 'terminal' && t.state === 'connected').length; }

  /** Tabs ticked for "Send to selected tabs" (Command Window, command library). */
  readonly selected = signal<Set<string>>(new Set());

  toggleSelected(id: string) {
    this.selected.update(s => { const n = new Set(s); if (n.has(id)) n.delete(id); else n.add(id); return n; });
  }

  /** Tabs a send goes to: the active tab, every connected tab or the selected ones. */
  targets(target: SendTarget): TabView[] {
    const live = this.tabs().filter(t => t.kind === 'terminal' && t.state === 'connected' && !t.locked);
    if (target === 'all' || target === true) return live;
    if (target === 'selected') return live.filter(t => this.selected().has(t.id));
    return live.filter(t => t.id === this.activeId());
  }

  /** Sends text to the target tabs. Supports a per-line delay. */
  async send(text: string, target: SendTarget = 'active', raw = false) {
    const data = raw ? text : unescape(text);
    const targets = this.targets(target).map(t => t.id);
    const delay = this.ui.settings()?.pasteLineDelayMs ?? 0;
    for (const id of targets.filter(Boolean)) {
      if (delay > 0 && data.includes('\r')) {
        const parts = data.split(/(?<=\r)/);
        for (const p of parts) {
          await this.backend.sendInput(id, p);
          await new Promise(r => setTimeout(r, delay));
        }
      } else {
        await this.backend.sendInput(id, data);
      }
    }
  }

  async copy() {
    const h = this.handles.get(this.activeId());
    const sel = h?.term.getSelection();
    if (sel) await this.backend.clipboardSet(sel);
  }

  async paste(id = this.activeId(), given?: string) {
    const h = this.handles.get(id);
    if (!h) return;
    const text = given ?? await this.backend.clipboardGet();
    if (!text) return;
    const lines = text.replace(/\r?\n$/, '').split(/\r?\n/);
    if (lines.length > 1 && this.ui.settings()?.confirmPaste) {
      const t = this.get(id);
      const preview = lines.slice(0, 8).join('\n') + (lines.length > 8 ? `\n… (${lines.length - 8} more)` : '');
      const ok = await this.ui.confirm('Paste ' + lines.length + ' lines?',
        `Paste ${lines.length} lines into "${t?.title ?? ''}"?\n\n${preview}`, 'Paste');
      if (!ok) { h.term.focus(); return; }
    }
    const delay = this.ui.settings()?.pasteLineDelayMs ?? 0;
    if (delay > 0 && /\r|\n/.test(text)) {
      const prev = this.activeId();
      this.activeId.set(id);
      await this.send(text.replace(/\r?\n/g, '\r'), 'active', true);
      this.activeId.set(prev);
    } else {
      h.term.paste(text);
    }
    h.term.focus();
  }

  async toggleLog(id = this.activeId()) {
    const t = this.get(id);
    if (!t) return;
    try {
      if (t.logFile) {
        await this.backend.stopLog(id);
        this.ui.toast('Logging stopped');
        return;
      }
      const name = (t.session.name || t.title || 'session').replace(/[\\/:*?"<>|]/g, '_') + '.log';
      const path = await this.backend.chooseSaveFile('Log Session As', name, [{ displayName: 'Log files', pattern: '*.log;*.txt' }]);
      if (!path) return;
      const p = await this.backend.startLog(id, path, t.session.logAppend, t.session.logRaw);
      this.ui.toast('Logging to ' + p, 'success');
    } catch (e) { this.ui.error(e); }
  }

  /** Writes the full scrollback buffer of the active tab to a text file. */
  async saveScrollback(id = this.activeId()) {
    const h = this.handles.get(id);
    const t = this.get(id);
    if (!h || !t) return;
    const buf = h.term.buffer.active;
    const lines: string[] = [];
    for (let i = 0; i < buf.length; i++) lines.push(buf.getLine(i)?.translateToString(true) ?? '');
    while (lines.length && lines[lines.length - 1] === '') lines.pop();
    try {
      const name = (t.title || 'scrollback').replace(/[\\/:*?"<>|]/g, '_') + '.txt';
      const path = await this.backend.chooseSaveFile('Save Scrollback As', name, [{ displayName: 'Text', pattern: '*.txt;*.log' }]);
      if (!path) return;
      await this.backend.saveTextFile(path, lines.join('\n') + '\n');
      this.ui.toast(`Saved ${lines.length} lines to ${path}`, 'success');
    } catch (e) { this.ui.error(e); }
  }

  private beepCtx?: AudioContext;

  /** Output alert: notice, tab badge, optional sound and OS notification. */
  private onAlert(a: AlertEvent) {
    const visible = this.activeId() === a.tabId && document.hasFocus();
    this.ui.toast(`${a.title}: ${a.line}`, 'error', 10000);
    if (this.activeId() !== a.tabId) this.patch(a.tabId, { alert: true });
    if (a.sound) {
      try {
        this.beepCtx ??= new AudioContext();
        const o = this.beepCtx.createOscillator(), g = this.beepCtx.createGain();
        o.frequency.value = 880; g.gain.value = 0.08;
        o.connect(g).connect(this.beepCtx.destination);
        o.start(); o.stop(this.beepCtx.currentTime + 0.25);
      } catch { /* audio unavailable */ }
    }
    if (a.desktop && !visible) void this.backend.notify('Termwright - ' + a.title, a.line).catch(() => {});
  }
}
