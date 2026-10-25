import { Injectable, inject, signal } from '@angular/core';
import { Backend, errText } from './backend';
import { Session, SessionNode, Settings, Snippet } from './models';

export interface Toast { id: number; level: 'info' | 'error' | 'success'; message: string; }
export interface MenuItem { label?: string; action?: () => void; separator?: boolean; disabled?: boolean; hint?: string; }
export type DialogName = 'sessionEditor' | 'quickConnect' | 'options' | 'import' | 'export' | 'about' | 'shortcuts' | 'passwords' | 'update' | 'keySetup' | 'palette' | 'commands' | 'workspaces' | 'folderDefaults' | 'compare';

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
  readonly toasts = signal<Toast[]>([]);
  readonly confirmReq = signal<ConfirmReq | null>(null);
  readonly inputReq = signal<InputReq | null>(null);
  readonly menu = signal<{ x: number; y: number; items: MenuItem[] } | null>(null);
  readonly dialog = signal<DialogName | null>(null);
  /** Sources the Compare Outputs dialog starts with ('tab:<id>' or 'file'). */
  readonly compareWith = signal<[string, string] | null>(null);
  /** Session being edited and its original path ('' for new). */
  readonly editing = signal<{ session: Session; oldPath: string; tabId?: string } | null>(null);

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
    try { this.tree.set(await this.backend.getSessionTree()); } catch (e) { this.error(e); }
  }

  toast(message: string, level: Toast['level'] = 'info', ms = 4000) {
    const t: Toast = { id: ++this.toastSeq, level, message };
    this.toasts.update(l => [...l, t]);
    setTimeout(() => this.toasts.update(l => l.filter(x => x.id !== t.id)), ms);
  }

  error(e: unknown) { this.toast(errText(e), 'error', 7000); }

  confirm(title: string, message: string, ok = 'OK', danger = false): Promise<boolean> {
    return new Promise(resolve => this.confirmReq.set({ title, message, ok, danger, resolve }));
  }

  input(title: string, label: string, value = '', secret = false): Promise<string | null> {
    return new Promise(resolve => this.inputReq.set({ title, label, value, secret, resolve }));
  }

  /** Opens a menu at a fixed position (used by the in-window menu bar). */
  showMenuAt(x: number, y: number, items: MenuItem[]) {
    const h = items.reduce((a, i) => a + (i.separator ? 9 : 28), 12);
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

  async newSession(folder = '') {
    try {
      const s = await this.backend.newSessionDefaults();
      s.folder = folder;
      s.name = '';
      this.editing.set({ session: s, oldPath: '' });
      this.dialog.set('sessionEditor');
    } catch (e) { this.error(e); }
  }

  async editSession(path: string, tabId?: string) {
    try {
      const s = await this.backend.getSession(path);
      this.editing.set({ session: s, oldPath: path, tabId });
      this.dialog.set('sessionEditor');
    } catch (e) { this.error(e); }
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
