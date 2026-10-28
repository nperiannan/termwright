import { Injectable, inject, signal } from '@angular/core';
import { Backend, errText } from './backend';
import { MovedDevice, Session, SessionNode, Settings, Snippet } from './models';

export interface ToastAction { label: string; run: () => void; }
export interface Toast { id: number; level: 'info' | 'error' | 'success'; message: string; actions?: ToastAction[]; }
export interface MenuItem { label?: string; action?: () => void; separator?: boolean; disabled?: boolean; hint?: string; }
export type DialogName = 'sessionEditor' | 'quickConnect' | 'options' | 'import' | 'export' | 'about' | 'shortcuts' | 'passwords' | 'update' | 'keySetup' | 'palette' | 'commands' | 'workspaces' | 'folderDefaults' | 'compare' | 'discover' | 'bulkEdit' | 'backups' | 'checklists' | 'history' | 'collect' | 'consolePorts' | 'recordings' | 'moved' | 'welcome' | 'factTeach';

interface ConfirmReq { title: string; message: string; ok: string; danger: boolean; resolve: (v: boolean) => void; }
interface InputReq { title: string; label: string; value: string; secret?: boolean; resolve: (v: string | null) => void; }

/** Shared UI state: settings, session tree, dialogs, toasts and menus. */
@Injectable({ providedIn: 'root' })
export class Ui {
  private backend = inject(Backend);

  readonly settings = signal<Settings | null>(null);
  /** True while saved passwords wait for the master password. */
  readonly locked = signal(false);
  /** True while the UI uses the light theme (set by the app shell). */
  readonly lightUi = signal(false);
  /** Tab the Key Login Setup dialog works on ('' = generate a key only). */
  readonly keySetupTab = signal('');
  /** Folder whose defaults the Folder Defaults dialog edits. */
  readonly folderDefaultsFor = signal('');
  /** Sends a library command (set by the Command Library). */
  runSnippet?: (s: Snippet) => void;
  readonly tree = signal<SessionNode | null>(null);
  readonly selectedPath = signal<string>('');
  readonly selectedIsFolder = signal<boolean>(false);
  /** Sessions selected together in the Session Manager (Cmd/Ctrl- or Shift-click). */
  readonly selectedPaths = signal<Set<string>>(new Set());
  /** Sessions the Edit Sessions dialog changes. */
  readonly bulkPaths = signal<string[]>([]);
  /** Sessions the Configuration Backups window backs up right away (from the Session Manager); null = just open it. */
  readonly backupTargets = signal<string[] | null>(null);
  /** Sessions or folders the Change Checklists window starts with (from the Session Manager). */
  readonly checklistTargets = signal<string[] | null>(null);
  /** Sessions or folders Run and Collect starts with. */
  readonly collectTargets = signal<string[] | null>(null);
  /** Console server session the Console Server Ports dialog creates sessions for. */
  readonly consoleServerFor = signal('');
  /** Moved Devices dialog: a list from Discover Devices, or one session to find by its MAC address (from a tab). */
  /** Save Selection as Device Fact: what was highlighted and where. */
  readonly factTeach = signal<{ path: string; deviceType: string; line: string; start: number; length: number; command: string; output: string } | null>(null);
  /** Text Quick Open starts with (the start page's search box). */
  readonly paletteSeed = signal('');
  readonly movedInput = signal<{ items: MovedDevice[] } | { find: string; tab?: string } | null>(null);
  /** Text the Command Window should take (from Command History). */
  readonly cwInsert = signal<string | null>(null);
  /** Page Global Options opens on (set before opening it). */
  readonly optionsPage = signal('');
  readonly toasts = signal<Toast[]>([]);
  readonly confirmReq = signal<ConfirmReq | null>(null);
  readonly inputReq = signal<InputReq | null>(null);
  readonly menu = signal<{ x: number; y: number; items: MenuItem[] } | null>(null);
  readonly dialog = signal<DialogName | null>(null);
  /** Sources the Compare Outputs dialog starts with ('tab:<id>' or 'file'). */
  readonly compareWith = signal<[string, string] | null>(null);
  /** Session being edited and its original path ('' for new). */
  readonly editing = signal<{ session: Session; oldPath: string; tabId?: string; pw?: string } | null>(null);
  /** Modern layout: the session being edited in the side panel. */
  readonly panel = signal<{ session: Session; oldPath: string; tabId?: string } | null>(null);
  /** Page the Session Options dialog opens at ('' = Connection). */
  readonly editorPage = signal('');

  private toastSeq = 0;
  /** Set by the root component: runs a named action (menus, key map, buttons). */
  actionHandler: (action: string) => void = () => {};

  async loadSettings() {
    try { this.settings.set(await this.backend.getSettings()); } catch (e) { this.error(e); }
  }

  async saveSettings(s: Settings) {
    try {
      await this.backend.saveSettings(s);
      this.settings.set({ ...s });
      await this.refreshTree();
    } catch (e) { this.error(e); }
  }

  patchSettings(p: Partial<Settings>) {
    const s = this.settings();
    if (s) void this.saveSettings({ ...s, ...p });
  }

  async refreshTree() {
    try {
      this.tree.set(await this.backend.getSessionTree());
      // favourites and recent sessions follow renamed, moved and deleted sessions in the backend
      if (this.settings()) this.settings.set(await this.backend.getSettings());
    } catch (e) { this.error(e); }
  }

  toast(message: string, level: Toast['level'] = 'info', ms = 4000, actions?: ToastAction[]) {
    const t: Toast = { id: ++this.toastSeq, level, message, actions };
    this.toasts.update(l => [...l, t]);
    setTimeout(() => this.dismissToast(t.id), actions?.length ? Math.max(ms, 12000) : ms);
  }

  dismissToast(id: number) { this.toasts.update(l => l.filter(x => x.id !== id)); }

  /** Shows an error; actions are buttons that fix it (e.g. Edit Session). */
  error(e: unknown, actions?: ToastAction[]) { this.toast(errText(e), 'error', 7000, actions); }

  openOptions(page = '') { this.optionsPage.set(page); this.open('options'); }

  confirm(title: string, message: string, ok = 'OK', danger = false): Promise<boolean> {
    return new Promise(resolve => this.confirmReq.set({ title, message, ok, danger, resolve }));
  }

  input(title: string, label: string, value = '', secret = false): Promise<string | null> {
    return new Promise(resolve => this.inputReq.set({ title, label, value, secret, resolve }));
  }

  /** Opens a menu at a fixed position (used by the in-window menu bar). */
  /** Height a menu of these items takes (as limited by the stylesheet). */
  menuHeight(items: MenuItem[]) { return Math.min(items.reduce((a, i) => a + (i.separator ? 9 : 32), 12), window.innerHeight * 0.7); }

  showMenuAt(x: number, y: number, items: MenuItem[]) {
    const h = Math.min(items.reduce((a, i) => a + (i.separator ? 9 : 32), 12), window.innerHeight * 0.7);
    this.menu.set({ x: Math.min(x, window.innerWidth - 260), y: Math.max(0, Math.min(y, window.innerHeight - h)), items });
  }

  showMenu(ev: MouseEvent, items: MenuItem[]) {
    ev.preventDefault();
    ev.stopPropagation();
    const x = Math.min(ev.clientX, window.innerWidth - 220);
    const y = Math.min(ev.clientY, window.innerHeight - items.length * 28 - 12);
    this.menu.set({ x, y: Math.max(0, y), items });
  }

  open(d: DialogName) { this.dialog.set(d); }
  close() { this.dialog.set(null); this.editing.set(null); }

  /** In the modern layout sessions are edited in a side panel (with a link to the full dialog). */
  private usePanel(full: boolean) { return !full && this.settings()?.layout !== 'classic'; }

  async newSession(folder = '', full = false) {
    try {
      const s = await this.backend.newSessionDefaults();
      s.folder = folder;
      s.name = '';
      if (this.usePanel(full)) { this.panel.set({ session: s, oldPath: '' }); return; }
      this.editing.set({ session: s, oldPath: '' });
      this.dialog.set('sessionEditor');
    } catch (e) { this.error(e); }
  }

  async editSession(path: string, tabId?: string, full = false) {
    try {
      const s = await this.backend.getSession(path);
      if (this.usePanel(full)) { this.panel.set({ session: s, oldPath: path, tabId }); return; }
      this.editing.set({ session: s, oldPath: path, tabId });
      this.dialog.set('sessionEditor');
    } catch (e) { this.error(e); }
  }

  /** Empties the Recent list (and when each session was last connected). */
  async clearRecent() {
    if (!(this.settings()?.recentSessions ?? []).length) return;
    if (await this.confirm('Clear Recent', 'Remove all sessions from the Recent list? The sessions themselves are kept.', 'Clear')) {
      try { this.settings.set(await this.backend.clearRecent('')); } catch (e) { this.error(e); }
    }
  }

  /** Removes every session from Favourites. */
  async clearFavourites() {
    if (!(this.settings()?.favorites ?? []).length) return;
    if (await this.confirm('Clear Favourites', 'Remove all sessions from Favourites? The sessions themselves are kept.', 'Clear')) {
      this.patchSettings({ favorites: [] });
    }
  }

  async removeRecent(path: string) {
    try { this.settings.set(await this.backend.clearRecent(path)); } catch (e) { this.error(e); }
  }

  /** Facts read for a session (model, version, MAC, uptime, …; "_at" = when). */
  factsOf(path: string | undefined): Record<string, string> | null {
    return (path && this.settings()?.deviceFacts?.[path]) || null;
  }

  /** Reads a session's device facts in the background (all = every rule, asked from a menu). */
  async refreshFacts(path: string, all = false) {
    try {
      const f = await this.backend.refreshFacts(path, all);
      if (f) this.setFactsLocal(path, f);
      if (all) this.toast(f && Object.keys(f).some(k => k !== '_at') ? 'Device facts updated' : 'No device facts found - add fact rules to the device type', f ? 'success' : 'info');
    } catch (e) { if (all) this.error(e); }
  }

  /** Keeps the window's copy of the facts current (the backend stores them). */
  setFactsLocal(path: string, f: Record<string, string>) {
    const s = this.settings();
    if (s) this.settings.set({ ...s, deviceFacts: { ...(s.deviceFacts ?? {}), [path]: f } });
  }

  /** Saves a copy of an SSH2 session that opens as SFTP (sftp) or as a terminal. */
  async copySessionAs(path: string, openAs: '' | 'sftp') {
    try {
      const c = await this.backend.copySessionAs(path, openAs);
      await this.refreshTree();
      this.selectedPath.set(c.path);
      this.selectedIsFolder.set(false);
      this.toast(`Created ${c.path} - ${openAs === 'sftp' ? 'opens the SFTP file browser' : 'opens a terminal'}`, 'success');
    } catch (e) { this.error(e); }
  }

  /** The full Session Options dialog for a session (from the side panel), at a page. */
  openSessionDialog(session: Session, oldPath: string, tabId?: string, page = '', pw = '') {
    this.panel.set(null);
    this.editing.set({ session, oldPath, tabId, pw });
    this.editorPage.set(page);
    this.dialog.set('sessionEditor');
  }

  /** A session or folder node of the tree by path. */
  findNode(path: string): SessionNode | null {
    let found: SessionNode | null = null;
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => { if (c.path === path) found = c; else if (c.isFolder && !found) walk(c); });
    walk(this.tree());
    return found;
  }

  /** Folder that new items should go into, based on the tree selection. */
  currentFolder(): string {
    const p = this.selectedPath();
    if (!p) return '';
    if (this.selectedIsFolder()) return p;
    const i = p.lastIndexOf('/');
    return i < 0 ? '' : p.slice(0, i);
  }
}
