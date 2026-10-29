import { Injectable, computed, effect, inject, signal } from '@angular/core';
import type { Terminal } from '@xterm/xterm';
import type { FitAddon } from '@xterm/addon-fit';
import type { SearchAddon } from '@xterm/addon-search';
import type { SerializeAddon } from '@xterm/addon-serialize';
import { Backend, SftpMode } from './backend';
import { AlertEvent, DeviceFacts, RemoteInfo, Session, TabInfo, Transfer, isRemoteDesktop, unescape } from './models';
import { Ui } from './ui.service';
import { envOf } from './env';
import { HtmlKeyword, bufferHtml } from './buffer-html';
import { screenPng } from './screen-image';
import { commandOfLine } from './history.service';

export interface TabView {
  id: string;
  title: string;
  state: string;
  message: string;
  /** How far the connection got while connecting: route, tcp, hostkey, auth, shell. */
  step?: string;
  /** Has been connected at least once (the connecting and error cards only show before that). */
  wasConnected?: boolean;
  info: string;
  session: Session;
  logFile: string;
  cols: number;
  rows: number;
  locked: boolean;
  kind: 'terminal' | 'sftp' | 'rdp' | 'vnc' | 'player';
  /** Terminal tab: the session recording (.cast) being written. */
  castFile?: string;
  /** Player tab: the recording it plays. */
  playFile?: string;
  /** RDP / VNC tab: the open remote desktop. */
  remote?: RemoteInfo;
  parentId: string;
  home: string;
  recording: boolean;
  alert?: boolean;
  timestamps?: boolean;
  /** Terminal tab: follow new output (default); false keeps the view still. */
  autoScroll?: boolean;
  /** Terminal tab: show what the device sends as hex bytes. */
  hexView?: boolean;
  /** Terminal tab: the bar under the terminal (message box on serial tabs; timestamps, auto scroll, clear on all). */
  sendBar?: boolean;
  /** Terminal tab: user@host of its "ssh … host" remote command. SFTP tab: the host it browses through the jump station. */
  hopTarget?: string;
  sftpMode?: SftpMode;
  /** SFTP tab browsing as another user: root (sudo) or via a Run As command. */
  root?: boolean;
  runAsUser?: string;
  /** Terminal tab: folder the shell reports (OSC 7 / OSC 1337 CurrentDir / OSC 633 Cwd). */
  cwd?: string;
  /** Terminal tab: user the shell runs as (OSC 1337 RemoteHost=user@host), e.g. root after "sudo su -". */
  shellUser?: string;
  /** Terminal tab: host the shell reports (OSC 7 file://host/… or RemoteHost=user@host); '' after ssh / exit until it reports again. */
  shellHost?: string;
  /** Terminal tab (SSH2): the Files panel beside it is shown. */
  filesPanel?: boolean;
  /** SFTP tab: folder to open first (the terminal's folder), falls back to home. */
  startDir?: string;
}

export type Layout = 'tabs' | 'tile-v' | 'tile-h' | 'grid';
export type SendTarget = 'active' | 'all' | 'selected' | boolean;

export interface TermHandle { term: Terminal; fit: FitAddon; search: SearchAddon; serialize?: SerializeAddon; keywords?: () => HtmlKeyword[]; }

/** Owns the list of open terminal tabs and routes actions to them. */
@Injectable({ providedIn: 'root' })
export class Tabs {
  private backend = inject(Backend);
  private ui = inject(Ui);

  readonly tabs = signal<TabView[]>([]);
  readonly activeId = signal<string>('');
  readonly active = computed(() => this.tabs().find(t => t.id === this.activeId()) ?? null);
  /** Device type of the active terminal tab ('' = none). */
  readonly activeDeviceType = computed(() => this.active()?.session?.deviceType ?? '');
  readonly handles = new Map<string, TermHandle>();
  /** Terminal output held back while folder reporting is turned on quietly (until the first OSC 7). */
  readonly quiet = new Map<string, { held: string }>();
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
    // Through a jump station the terminal shows the target (unless its shell
    // reports the jump station): open SFTP on the host the terminal is on, so
    // the tab can follow its folder; the jump station if that fails.
    const auto = !mode && !!p.hopTarget;
    // A host chosen in Session Options (SFTP opens on) wins.
    const pref = p.session.sftpOn || (p.session.sftpToTarget ? 'target' : '');
    if (auto) mode = pref || (this.onJump(p) ? 'jump' : 'target');
    try {
      let r: Awaited<ReturnType<Backend['sftpOpen']>>;
      try { r = await this.backend.sftpOpen(p.id, mode); }
      catch (e) {
        if (!auto || pref || mode !== 'target') throw e;
        this.ui.toast(`SFTP on ${p.hopTarget} did not start (${e instanceof Error ? e.message : e}) - opened on ${p.session.hostname}, where the terminal's folder cannot be followed.`, 'error', 10000);
        mode = 'jump';
        r = await this.backend.sftpOpen(p.id, mode);
      }
      const cwd = p.cwd && (!p.hopTarget || r.target) ? p.cwd : '';
      const tv: TabView = {
        id: r.id, title: 'SFTP: ' + (r.target || p.title), state: 'connected', message: '',
        info: r.target ? `SFTP to ${r.target} through ${p.session.hostname}` : 'SFTP over ' + p.info,
        session: p.session, logFile: '', cols: 0, rows: 0, locked: false, kind: 'sftp', parentId: p.id, home: r.home, recording: false,
        hopTarget: r.target, sftpMode: mode, startDir: cwd,
      };
      this.tabs.update(l => [...l, tv]);
      this.activeId.set(tv.id);
      void this.autoCwd(tv);
    } catch (e) { this.ui.error(e); }
  }

  /**
   * Working directory detection (Global Options → General, on by default):
   * when an SFTP tab opens from a terminal whose shell does not report its
   * folder yet, turn reporting on in that shell - only for bash or zsh login
   * shells (checked over a separate channel, through the jump station when
   * there is one) and only while the terminal sits at a shell prompt.
   */
  private async autoCwd(sftp: TabView) {
    const sh = this.shellOf(sftp);
    if (sh) await this.detectCwd(sh, sftp.id);
  }

  /** The terminal's cursor sits after a shell prompt ($ # % >), nothing typed after it. */
  atPrompt(id: string) {
    const term = this.handles.get(id)?.term;
    if (!term || term.buffer.active.type !== 'normal') return false;
    const b = term.buffer.active;
    const line = b.getLine(b.baseY + b.cursorY)?.translateToString(true) ?? '';
    return /[$#%>]\s*$/.test(line.trimEnd() + ' ') && line.trimEnd().length === line.slice(0, b.cursorX).trimEnd().length;
  }

  /** Turns folder reporting on in a terminal's shell when it is bash or zsh at a prompt (checked over the SFTP session sftpId). */
  async detectCwd(sh: TabView, sftpId: string) {
    if (this.ui.settings()?.sftpCwdAuto === false || sh.state !== 'connected') return;
    const sftp = { id: sftpId };
    // Also when a folder is already known: after "sudo su -" the new shell
    // does not report yet, so the line is typed again (it is harmless twice).
    const name = await this.backend.sftpLoginShell(sftp.id).catch(() => '');
    if (!/^(bash|zsh)$/.test(name)) return;
    const term = this.handles.get(sh.id)?.term;
    if (!term || term.buffer.active.type !== 'normal') return;
    const b = term.buffer.active;
    const line = b.getLine(b.baseY + b.cursorY)?.translateToString(true) ?? '';
    if (!/[$#%>]\s*$/.test(line.trimEnd() + ' ') || line.trimEnd().length !== line.slice(0, b.cursorX).trimEnd().length) return;
    await this.enableCwdReporting(sh.id, false, true);
  }

  /**
   * The terminal tab whose shell runs on the host an SFTP tab browses (same
   * login), or undefined. With a jump station, only the target tab's shell
   * qualifies (the terminal shows the target, not the jump station).
   */
  shellOf(sftp: TabView): TabView | undefined {
    const p = this.linkedTerm(sftp);
    if (!p) return undefined;
    // A session through a jump station: the terminal shows the target, unless
    // its shell reports the jump station's host (after "exit" from the target).
    if (p.hopTarget && !!sftp.hopTarget === this.onJump(p)) return undefined;
    return p;
  }

  /**
   * The terminal tab an SFTP tab belongs to: the tab it was opened from, or -
   * for an SFTP session with its own login - an open terminal tab of the same
   * session (same address and user).
   */
  linkedTerm(sftp: TabView): TabView | undefined {
    if (sftp.kind !== 'sftp') return undefined;
    const p = this.get(sftp.parentId);
    if (p) return p.kind === 'terminal' ? p : undefined;
    const s = sftp.session;
    const same = (t: TabView) => t.kind === 'terminal' && t.session.protocol === 'SSH2' &&
      (s.path ? t.session.path === s.path : t.session.hostname === s.hostname && (t.session.username ?? '') === (s.username ?? '') && (t.session.port || 22) === (s.port || 22));
    const all = this.tabs().filter(same);
    return all.find(t => t.id === this.activeId()) ?? all.find(t => t.state === 'connected') ?? all[0];
  }

  /** The terminal of a jump-station session reports the jump station's host. */
  onJump(p: TabView) {
    const short = (h: string) => h.slice(h.lastIndexOf('@') + 1).split('.')[0].toLowerCase();
    return !!p.shellHost && !!p.session.hostname && short(p.shellHost) === short(p.session.hostname);
  }

  /**
   * After a command that starts a new shell as another user - "sudo su -",
   * "sudo -i", "su - oracle", "sudo su - oracle", "bash" - the new shell does
   * not report its folder and user yet, so an SFTP tab that follows this
   * terminal would stay where it was (also on a host behind a jump station).
   * Once the new shell's prompt shows, reporting is turned on in it again
   * (silently); the SFTP tab then follows the folder and switches user.
   */
  afterCommand(id: string, cmd: string) {
    const c = cmd.trim();
    // ssh to another host or exit / logout: the host is not known until the
    // shell reports again.
    const leave = /^(exit|logout)\b/.test(c);
    if ((leave || /^ssh\s/.test(c)) && this.get(id)?.shellHost) this.patch(id, { shellHost: '' });
    if (this.ui.settings()?.sftpCwdAuto === false) return;
    // Leaving the target of a jump-station session with an SFTP tab on the
    // jump station open: turn reporting on in the shell it returns to, so that
    // tab can follow it there.
    const t0 = this.get(id);
    const jumpTab = !!t0?.hopTarget && this.tabs().some(t => t.kind === 'sftp' && this.linkedTerm(t)?.id === id && !t.hopTarget);
    if (!(leave && (jumpTab || (!!t0?.hopTarget && !!t0.filesPanel))) && !/^(sudo\s+(-\S+\s+)*(su\b|-i\b|-s\b|bash\b|zsh\b|-iu\s|-u\s+\S+\s+(-i|-s|bash|zsh)\b)|su\b|exec\s+(bash|zsh)\b|(bash|zsh)(\s+-l)?$)/.test(c)) return;
    if (!t0?.filesPanel && !this.tabs().some(t => t.kind === 'sftp' && this.linkedTerm(t)?.id === id)) return;
    let tries = 0;
    const tick = () => {
      const term = this.handles.get(id)?.term;
      const t = this.get(id);
      if (!term || !t || t.state !== 'connected') return;
      const b = term.buffer.active;
      const line = b.getLine(b.baseY + b.cursorY)?.translateToString(true) ?? '';
      const atPrompt = /[$#%>]\s*$/.test(line.trimEnd() + ' ') && !/password|passphrase|verification|code/i.test(line) && b.type === 'normal';
      if (atPrompt && tries > 0) { void this.enableCwdReporting(id, false, true); return; }
      if (++tries < 90) setTimeout(tick, 500); // up to 45 s, e.g. while sudo asks for a password
    };
    setTimeout(tick, 600);
  }

  /** Types the line that makes bash or zsh report its folder (OSC 7) after every prompt. */
  enableCwdReporting(termId: string, permanent: boolean, silent = false) {
    const line = `_tw_cwd(){ printf '\\033]7;file://%s%s\\007\\033]1337;RemoteHost=%s@%s\\007' "\${HOSTNAME:-\$HOST}" "\$PWD" "\${USER:-\$(id -un)}" "\${HOSTNAME:-\$HOST}"; }; ` +
      `if [ -n "\$ZSH_VERSION" ]; then typeset -U precmd_functions; precmd_functions+=(_tw_cwd); ` +
      `else case ";\$PROMPT_COMMAND;" in *_tw_cwd*) ;; *) PROMPT_COMMAND="_tw_cwd\${PROMPT_COMMAND:+;\$PROMPT_COMMAND}";; esac; fi`;
    let cmd = ' ' + line + '; _tw_cwd\r'; // leading space: kept out of the shell history
    if (permanent) {
      const q = line.replace(/'/g, `'\\''`);
      cmd = ` f=~/.bashrc; [ -n "\$ZSH_VERSION" ] && f=~/.zshrc; grep -q _tw_cwd "\$f" 2>/dev/null || ` +
        `printf '%s\\n' '# Termwright: report the current folder (SFTP follows the terminal)' '${q}' >> "\$f"; echo "folder reporting added to \$f";` + cmd;
    }
    if (silent) {
      // hide the typed line: hold the output until the shell reports its folder
      this.quiet.set(termId, { held: '' });
      setTimeout(() => {
        const q = this.quiet.get(termId);
        if (!q) return;
        this.quiet.delete(termId);
        this.handles.get(termId)?.term.write(q.held);
      }, 4000);
    }
    return this.backend.sendInput(termId, cmd);
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
      timestamps: !!ti.session.timestamps, autoScroll: !ti.session.noAutoScroll, sendBar: ti.session.protocol === 'Serial' || !this.ui.settings()?.hideTerminalBar,
      filesPanel: ti.session.protocol === 'SSH2' && !!ti.session.filesPanel,
    };
    this.backend.sftpHopTarget(ti.session).then(h => { if (h) this.patch(ti.id, { hopTarget: h }); }).catch(() => {});
    this.unsubs.set(ti.id, [
      this.backend.on('term:step:' + ti.id, (step: string) => this.patch(ti.id, { step })),
      this.backend.on('term:status:' + ti.id, (st: { state: string; message: string; info: string }) => {
        const before = this.get(ti.id)?.state;
        if (st.state === 'connected' && before !== 'connected') this.factsAfterConnect(ti.id);
        this.patch(ti.id, { state: st.state, message: st.message, info: st.info,
          ...(st.state === 'connected' ? { wasConnected: true, step: '' } : {}) });
        const h = this.handles.get(ti.id);
        if (!h) return;
        if (st.state === 'disconnected' || st.state === 'error') {
          const color = st.state === 'error' ? '31' : '90';
          h.term.write(`\r\n\x1b[${color}m[${st.message}]\x1b[0m\r\n\x1b[90mPress Enter to reconnect.\x1b[0m\r\n`);
        }
      }),
      this.backend.on('term:log:' + ti.id, (path: string) => this.patch(ti.id, { logFile: path })),
      this.backend.on('term:rec:' + ti.id, (path: string) => this.patch(ti.id, { castFile: path })),
    ]);
    this.tabs.update(l => [...l, tv]);
    this.activeId.set(ti.id);
    if (ti.session.path) setTimeout(() => void this.ui.loadSettings(), 300); // recent sessions and last-connected times
  }

  patch(id: string, p: Partial<TabView>) {
    this.tabs.update(l => l.map(t => (t.id === id ? { ...t, ...p } : t)));
  }

  get(id: string) { return this.tabs().find(t => t.id === id) ?? null; }

  /** Opens a saved session as configured (terminal, or SFTP-only when "Connect as" is SFTP). */
  async openSession(path: string, as: 'auto' | 'terminal' | 'sftp' = 'auto', mode: SftpMode = '') {
    try {
      const node = this.ui.findNode(path);
      if (isRemoteDesktop(node?.protocol)) { await this.openRemote(path, null); return; }
      if (as !== 'terminal') {
        const s = await this.backend.getSession(path);
        if (as === 'sftp' || (s.protocol === 'SSH2' && s.openAs === 'sftp')) {
          if (s.protocol !== 'SSH2') { this.ui.toast('SFTP needs an SSH2 session.', 'error'); return; }
          await this.openSftpSession(path, mode);
          return;
        }
      }
      this.add(await this.backend.openTab(path));
    } catch (e) {
      this.ui.error(e, [
        { label: 'Try Again', run: () => void this.openSession(path, as, mode) },
        { label: 'Edit Session', run: () => void this.ui.editSession(path) },
      ]);
    }
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
    void this.autoCwd(tv); // when a terminal tab of this session is open
  }

  async openAdHoc(s: Session) {
    if (isRemoteDesktop(s.protocol)) { await this.openRemote('', s).catch(e => this.ui.error(e)); return; }
    try { this.add(await this.backend.openAdHoc(s)); } catch (e) { this.ui.error(e); }
  }

  /** RDP (opened in the computer's Remote Desktop client) or VNC (built-in viewer) tab. */
  async openRemote(path: string, s: Session | null) {
    const name = s?.name || s?.hostname || path.split('/').pop();
    this.ui.toast(`Opening ${name}...`);
    const r = await this.backend.openRemote(path, s);
    const tv: TabView = {
      id: r.id, title: r.title, state: r.kind === 'rdp' ? 'connected' : 'connecting', message: '', info: [r.kind.toUpperCase(), r.addr, r.via].filter(Boolean).join(' '),
      session: r.session, logFile: '', cols: 0, rows: 0, locked: false, kind: r.kind, parentId: '', home: '', recording: false, remote: r,
    };
    this.unsubs.set(r.id, [this.backend.on('remote:closed:' + r.id, (msg: string) => this.patch(r.id, { state: 'disconnected', message: msg }))]);
    this.tabs.update(l => [...l, tv]);
    this.activeId.set(tv.id);
    if (path) setTimeout(() => void this.ui.loadSettings(), 300);
  }

  private playerSeq = 0;
  /** Player tab for a session recording (.cast). */
  async openPlayer(path: string, title = '') {
    const open = this.tabs().find(t => t.kind === 'player' && t.playFile === path);
    if (open) { this.activeId.set(open.id); return; }
    const s = await this.backend.newSessionDefaults();
    const name = title || path.split(/[\\/]/).slice(-2).join(' ').replace(/\.cast$/i, '');
    const tv: TabView = {
      id: 'player-' + ++this.playerSeq, title: '▶ ' + name, state: 'connected', message: '', info: 'Recording ' + path,
      session: { ...s, name, path: '' }, logFile: '', cols: 0, rows: 0, locked: false, kind: 'player', parentId: '', home: '', recording: false, playFile: path,
    };
    this.tabs.update(l => [...l, tv]);
    this.activeId.set(tv.id);
  }

  /** Starts or stops the session recording of a terminal tab. */
  async toggleCast(id = this.activeId()) {
    const t = this.get(id);
    if (!t || t.kind !== 'terminal') { this.ui.toast('Select a terminal tab first.', 'error'); return; }
    try {
      if (t.castFile) { await this.backend.stopSessionRecording(id); this.patch(id, { castFile: '' }); this.ui.toast('Recording saved', 'success', 5000, [{ label: 'Play', run: () => void this.openPlayer(t.castFile!, t.title) }]); }
      else {
        if (t.state !== 'connected') { this.ui.toast('Connect the tab first - recordings start with the connection.', 'error'); return; }
        const p = await this.backend.startSessionRecording(id);
        this.patch(id, { castFile: p });
      }
    } catch (e) { this.ui.error(e); }
  }

  /** Adds a marker with a note to the active tab's recording. */
  async addMarker(id = this.activeId()) {
    const t = this.get(id);
    if (!t || t.kind !== 'terminal' || !t.castFile) { this.ui.toast('Markers go into a recording: start recording the tab first.', 'error'); return; }
    const note = await this.ui.input('Add Marker', 'Note for this point of the recording', '');
    if (note === null) return;
    try { await this.backend.addRecordingMarker(id, note.trim() || 'Marker'); this.ui.toast('Marker added'); } catch (e) { this.ui.error(e); }
    setTimeout(() => this.handles.get(id)?.term.focus());
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
    const rt = this.get(id);
    if (rt?.kind === 'rdp') { await this.backend.reopenRemote(id).catch(e => this.ui.error(e)); this.patch(id, { state: 'connected', message: '' }); return; }
    if (rt?.kind === 'vnc') { this.patch(id, { state: 'connecting', message: '', remote: { ...rt.remote! } }); return; }
    const t = this.get(id);
    if (!t) return;
    if (t.kind === 'sftp') {
      await this.close(id);
      if (!t.parentId && t.session.path) await this.openSftpSession(t.session.path, t.sftpMode).catch(e => this.ui.error(e));
      else if (t.parentId) await this.openSftp(t.parentId, t.sftpMode);
      return;
    }
    this.patch(id, { step: '' });
    if (t.state === 'connected' || t.state === 'connecting') await this.backend.disconnectTab(id);
    await this.backend.refreshTabSession(id).catch(() => {});
    this.handles.get(id)?.term.write('\r\n');
    this.start(id);
  }

  async disconnect(id = this.activeId()) {
    if (id) await this.backend.disconnectTab(id).catch(e => this.ui.error(e));
  }

  async clone(id = this.activeId()) {
    const ct = this.get(id);
    if (ct?.remote) { await this.openRemote(ct.session.path, ct.session.path ? null : ct.session).catch(e => this.ui.error(e)); return; }
    const t = this.get(id);
    if (!t) return;
    if (t.session.path) await this.openSession(t.session.path, t.kind === 'sftp' ? 'sftp' : 'terminal');
    else await this.openAdHoc(t.session);
  }

  async close(id = this.activeId()) {
    const t = this.get(id);
    if (!t) return;
    const s = this.ui.settings();
    if (t.kind === 'rdp' || t.kind === 'vnc') {
      await this.backend.closeRemote(id).catch(() => {});
    } else if (t.kind === 'sftp') {
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

  /** True for tabs of production sessions (own setting or folder default). */
  isProduction(t: TabView) {
    return envOf(t.session?.path ?? '', t.session?.environment, this.ui.settings()?.folderDefaults) === 'production';
  }

  private prodOk = { key: '', until: 0 };

  /** Asks before text goes to several tabs when one of them is a production session (remembered for a minute). */
  async confirmProduction(list: TabView[], what: string): Promise<boolean> {
    const prod = list.filter(t => this.isProduction(t));
    if (!prod.length) return true;
    const key = list.map(t => t.id).sort().join(',');
    if (this.prodOk.key === key && Date.now() < this.prodOk.until) return true;
    const ok = await this.ui.confirm('Production sessions', `${what}\n\nProduction: ${prod.map(t => t.title).join(', ')}`, 'Send', true);
    if (ok) this.prodOk = { key, until: Date.now() + 60000 };
    return ok;
  }

  /** Sends text to the target tabs. Supports a per-line delay. */
  async send(text: string, target: SendTarget = 'active', raw = false) {
    const data = raw ? text : unescape(text);
    const list = this.targets(target);
    if (list.length > 1 && !(await this.confirmProduction(list, `Send to ${list.length} tabs?\n\n${data.replace(/\r/g, '\n').trim().slice(0, 400)}`))) return;
    const targets = list.map(t => t.id);
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
    const tv = this.get(id);
    if (lines.length > 1 && tv && this.isProduction(tv) && !this.ui.settings()?.confirmPaste) {
      const preview = lines.slice(0, 8).join('\n') + (lines.length > 8 ? `\n… (${lines.length - 8} more)` : '');
      if (!(await this.ui.confirm('Paste into production?', `Paste ${lines.length} lines into the production session "${tv.title}"?\n\n${preview}`, 'Paste', true))) { h.term.focus(); return; }
    } else if (lines.length > 1 && this.ui.settings()?.confirmPaste) {
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
  /** Saves the scrollback as plain text, or as an HTML page with the colors as shown. */
  /** After connecting: read the device facts in the background (version and
   *  uptime each time, model / serial / MAC once) when the device type has
   *  fact rules. */
  private factsAfterConnect(id: string) {
    const t = this.get(id);
    const p = t?.session?.path;
    if (!p || t.kind !== 'terminal' || (t.session.protocol !== 'SSH2' && t.session.protocol !== 'Telnet')) return;
    setTimeout(() => void this.ui.refreshFacts(p), 1500);
  }

  /** Saves the visible screen of a terminal tab as a PNG image. */
  /** Save Selection as Device Fact: the selected value, its line and the
   *  command above it in the scrollback go to the fact dialog. */
  teachFact(id = this.activeId()) {
    const t = this.get(id);
    const term = this.handles.get(id)?.term;
    if (!t || t.kind !== 'terminal') { this.ui.toast('Open a terminal tab first.', 'info'); return; }
    if (!t.session.path) { this.ui.toast('Device facts are kept for saved sessions - save this session first.', 'info'); return; }
    if (!term?.hasSelection()) { this.ui.toast('Select the value first (for example the version in the output of show sys-info), then choose Save Selection as Device Fact.', 'info'); return; }
    const pos = term?.getSelectionPosition();
    if (!term || !pos) return;
    const sel = term.getSelection().split('\n')[0].replace(/\s+$/, '');
    if (!sel.trim()) return;
    const buf = term.buffer.active;
    let row = -1, col = -1, line = '';
    for (const y of [pos.start.y, pos.start.y - 1, pos.start.y + 1]) {
      const l = buf.getLine(y)?.translateToString(true) ?? '';
      const i = l.indexOf(sel, Math.max(0, pos.start.x - 2));
      const j = i >= 0 ? i : l.indexOf(sel);
      if (j >= 0) { row = y; col = j; line = l; break; }
    }
    if (row < 0) { this.ui.toast('Select the value on one line of the output.', 'info'); return; }
    let cmdRow = -1, command = '';
    for (let y = row - 1; y >= Math.max(0, row - 400); y--) {
      const c = commandOfLine(buf.getLine(y)?.translateToString(true) ?? '');
      if (c) { cmdRow = y; command = c; break; }
    }
    const out: string[] = [];
    for (let y = cmdRow + 1; y <= Math.min(buf.length - 1, row + 40); y++) out.push(buf.getLine(y)?.translateToString(true) ?? '');
    this.ui.factTeach.set({ path: t.session.path, deviceType: t.session.deviceType, line, start: col, length: sel.length, command, output: out.join('\n') });
    this.ui.open('factTeach');
  }

  async captureScreen(id = this.activeId()) {
    const t = this.get(id);
    const h = this.handles.get(id);
    if (!t || !h || t.kind !== 'terminal') { this.ui.toast('Open a terminal tab first.', 'info'); return; }
    try {
      const png = screenPng(h.term, h.keywords?.() ?? []);
      const d = new Date(), z = (n: number) => String(n).padStart(2, '0');
      const name = `${(t.title || 'screen').replace(/[\\/:*?"<>|]+/g, '_')}-${d.getFullYear()}${z(d.getMonth() + 1)}${z(d.getDate())}-${z(d.getHours())}${z(d.getMinutes())}${z(d.getSeconds())}.png`;
      const path = await this.backend.chooseSaveFile('Save Screen As Image', name, [{ displayName: 'PNG image', pattern: '*.png' }]);
      if (!path) return;
      await this.backend.saveBase64File(path, png);
      this.ui.toast('Screen saved: ' + path, 'success');
    } catch (e) { this.ui.error(e); }
  }

  async saveScrollback(id = this.activeId(), format: 'text' | 'html' = 'text') {
    const h = this.handles.get(id);
    const t = this.get(id);
    if (!h || !t) return;
    const base = (t.title || 'scrollback').replace(/[\\/:*?"<>|]/g, '_');
    try {
      if (format === 'html') {
        const path = await this.backend.chooseSaveFile('Save Scrollback As HTML', base + '.html', [{ displayName: 'HTML', pattern: '*.html;*.htm' }]);
        if (!path) return;
        const icon = await fetch('icon.svg').then(r => r.ok ? r.text() : '').catch(() => '');
        // model, OS version, serial and MAC: from the output on screen, else read from the device in the background
        const buf = h.term.buffer.active;
        const text: string[] = [];
        for (let i = 0; i < buf.length; i++) text.push(buf.getLine(i)?.translateToString(true) ?? '');
        const remote = ['SSH2', 'Telnet'].includes(t.session.protocol ?? '') && !!t.session.path;
        if (remote) this.ui.toast('Reading the model, OS version and serial number…', 'info');
        const facts = await this.backend.deviceFacts(t.session.path ?? '', t.session.deviceType ?? '', text.join('\n'), remote).catch(() => null);
        await this.backend.saveTextFile(path, scrollbackHtml(h, t, icon, facts));
        this.ui.toast(`Saved ${path}`, 'success');
        return;
      }
      const buf = h.term.buffer.active;
      const lines: string[] = [];
      for (let i = 0; i < buf.length; i++) lines.push(buf.getLine(i)?.translateToString(true) ?? '');
      while (lines.length && lines[lines.length - 1] === '') lines.pop();
      const path = await this.backend.chooseSaveFile('Save Scrollback As', base + '.txt', [{ displayName: 'Text', pattern: '*.txt;*.log' }]);
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

/** UTF-8 text as base64 (for data: URLs). */
function toBase64(text: string): string {
  let bin = '';
  for (const b of new TextEncoder().encode(text)) bin += String.fromCharCode(b);
  return btoa(bin);
}

const esc = (v: string) => v.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]!));

/** A self-contained HTML page of a tab's scrollback: a Termwright header with
 *  the logo, the device (name, address, protocol, device type, user, folder)
 *  and when it was saved, then the output with its colors and highlighting. */
export function scrollbackHtml(h: TermHandle, t: TabView, iconSvg = '', facts: DeviceFacts | null = null): string {
  const term = h.term;
  const bg = String(term.options.theme?.background ?? '#1e1f22');
  const fg = String(term.options.theme?.foreground ?? '#d4d4d4');
  const font = String(term.options.fontFamily ?? 'monospace');
  const { html: body, lines } = bufferHtml(term, h.keywords?.() ?? []);
  const s = t.session;
  const proto = s.protocol ?? '';
  const address = proto === 'Serial' ? `${s.comPort || '-'} at ${s.baudRate || 9600} baud`
    : proto === 'Local Shell' ? 'this computer'
    : `${s.hostname || '-'}${s.port ? ':' + s.port : ''}${t.hopTarget ? ' → ' + t.hopTarget : ''}`;
  const now = new Date();
  const date = now.toLocaleDateString(undefined, { weekday: 'short', year: 'numeric', month: 'short', day: 'numeric' });
  const time = now.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit', timeZoneName: 'short' });
  const rows: [string, string][] = [
    ['Session', s.path || t.title], ['Address', address],
    ['Model', facts?.model ?? ''], ['OS version', facts?.version ?? ''],
    ['Serial number', facts?.serial ?? ''], ['MAC address', facts?.mac || (s as { mac?: string }).mac || ''],
    ['Device type', s.deviceType || ''], ['Protocol', proto],
    ['User', t.shellUser || s.username || ''], ['Connection', t.info || ''],
    ['Saved', `${date}, ${time}`], ['Lines', String(lines)],
  ];
  const logo = iconSvg ? `<img class="logo" alt="" src="data:image/svg+xml;base64,${toBase64(iconSvg)}">` : '';
  return `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="generator" content="Termwright">
<title>${esc(t.title)} - ${esc(date)} ${esc(time)} - Termwright</title>
<style>
  body { margin: 0; background: ${esc(bg)}; color: ${esc(fg)}; }
  header { padding: 14px 20px 12px; background: #f6f7f9; color: #1f2328; border-bottom: 1px solid #d0d7de;
    font: 13px/1.45 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; }
  .brand { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
  .brand .logo { width: 34px; height: 34px; }
  .brand .name { font-size: 19px; font-weight: 600; letter-spacing: .06em; }
  .brand .what { margin-left: auto; color: #59636e; font-size: 12px; }
  dl { display: grid; grid-template-columns: max-content 1fr max-content 1fr; gap: 3px 14px; margin: 0; }
  dt { color: #59636e; } dd { margin: 0; font-weight: 500; word-break: break-word; }
  dd.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12.5px; }
  main { padding: 10px 20px 16px; overflow-x: auto; }
  main pre { margin: 0; font-family: ${esc(font)}; font-size: 13px; line-height: 1.35; }
  @media (max-width: 700px) { dl { grid-template-columns: max-content 1fr; } }
  @media print { main { white-space: pre-wrap; } }
</style></head>
<body>
<header>
  <div class="brand">${logo}<span class="name">Termwright</span><span class="what">Terminal output</span></div>
  <dl>${rows.filter(([, v]) => v).map(([k, v]) => `<dt>${esc(k)}</dt><dd${['Address', 'Connection', 'Serial number', 'MAC address'].includes(k) ? ' class="mono"' : ''}>${esc(v)}</dd>`).join('')}</dl>
</header>
<main>${body}</main>
</body></html>
`;
}
