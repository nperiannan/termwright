import { Injectable, inject, signal } from '@angular/core';
import { Backend, errText } from './backend';
import { RemoteDesktopRun, Session } from './models';
import { RdProgress, applyLine, cleanLine, emptyProgress } from './remote-desktop-progress';
import { Tabs } from './tabs.service';
import { Ui } from './ui.service';

export const VNC_DISPLAY = 2;
export const VNC_PORT = 5900 + VNC_DISPLAY;
export const RDP_PORT = 3389;

/** What a finished or running setup keeps (never a password). */
export interface RdRun extends RdProgress {
  id: string;
  phase: 'running' | 'done' | 'failed' | 'cancelled';
  check: boolean;
  machine: string;
  log: string[];
  /** Sessions made when it ended without errors. */
  made: { rdp?: string; vnc?: string };
  /** Sessions made without a saved password: the desktop user or its VNC password was not set by this run. */
  unsaved?: ('RDP' | 'VNC')[];
  /** Services that run (a service that failed was removed again). */
  ready?: { rdp: boolean; vnc: boolean };
}

/** What is kept of a request once its passwords have been sent. */
export type RdRequest = Omit<RemoteDesktopRun, 'sudoPassword' | 'accountPassword' | 'vncPassword'>;

/** Name of the session made for one protocol next to the SSH session. */
export const remoteSessionName = (ssh: string, kind: 'RDP' | 'VNC') => `${ssh} (${kind})`;

/**
 * Set Up Remote Desktop: runs the setup script on the Linux machine of an SSH
 * tab in the background (nothing appears in the terminal), with the passwords
 * handed over on its standard input, and follows its output as one line of
 * progress. Only when it ends without errors are the RDP and VNC sessions made.
 */
@Injectable({ providedIn: 'root' })
export class RemoteDesktopSetup {
  private backend = inject(Backend);
  private ui = inject(Ui);
  private tabs = inject(Tabs);

  /** The run shown in the dialog (null: the form). */
  readonly run = signal<RdRun | null>(null);
  private unsubs: (() => void)[] = [];
  private req: RdRequest | null = null;
  private createSessions = true;
  /** Services the script removed again because they did not come up. */
  private removed = new Set<string>();

  /** Opens the dialog for a saved SSH session: connects it first when no terminal of it is open. */
  async openFor(path: string) {
    let t = this.tabs.tabs().find(x => x.kind === 'terminal' && x.session.path === path && x.state === 'connected');
    if (!t) {
      await this.tabs.openSession(path, 'terminal');
      this.ui.toast('Connecting - the dialog opens when the login is done…');
      t = await this.waitConnected(path);
      if (!t) { this.ui.toast('The SSH session did not connect, so the remote desktop cannot be set up.', 'error'); return; }
    }
    this.openForTab(t.id);
  }

  openForTab(tabId: string) {
    const t = this.tabs.get(tabId);
    if (!t || t.kind !== 'terminal' || t.session.protocol !== 'SSH2' || t.state !== 'connected') {
      this.ui.toast('Open and log in to the SSH session of the Linux machine first (right-click it → Set Up Remote Desktop…).', 'info', 7000);
      return;
    }
    this.ui.remoteDesktopTab.set(tabId);
    this.ui.open('remoteDesktop');
  }

  private async waitConnected(path: string) {
    for (let i = 0; i < 240; i++) { // 2 minutes, with time for a password prompt
      const t = this.tabs.tabs().find(x => x.kind === 'terminal' && x.session.path === path);
      if (t?.state === 'connected') return t;
      if (!t || t.state === 'error' || t.state === 'disconnected') return undefined;
      await new Promise(r => setTimeout(r, 500));
    }
    return undefined;
  }

  /** Back to the form (after a finished run). */
  reset() {
    if (this.run()?.phase === 'running') return;
    this.run.set(null);
  }

  /** Starts the script on the machine of the request's tab. The passwords in r are sent, then forgotten. */
  async start(r: RemoteDesktopRun, createSessions: boolean) {
    const tab = this.tabs.get(r.tabId);
    if (!tab) throw new Error('The SSH tab is closed.');
    const id = await this.backend.startRemoteDesktop(r);
    const { sudoPassword: _s, accountPassword: _a, vncPassword: _v, ...kept } = r;
    this.req = kept;
    this.createSessions = createSessions;
    this.removed.clear();
    this.unsubs.forEach(u => u());
    this.unsubs = [
      this.backend.on('rd:line:' + id, (line: string) => this.line(line)),
      this.backend.on('rd:done:' + id, (e: { code: number; error: string }) => void this.done(e)),
    ];
    this.run.set({ ...emptyProgress(), id, phase: 'running', check: r.check, machine: tab.title, log: [], made: {} });
  }

  async cancel() {
    const r = this.run();
    if (r?.phase === 'running') await this.backend.cancelRemoteDesktop(r.id).catch(() => {});
  }

  private line(raw: string) {
    const r = this.run();
    if (!r || r.phase !== 'running') return;
    const clean = cleanLine(raw);
    if (!clean.trim()) return;
    const gone = /^TWREMOVED (rdp|vnc)$/.exec(clean.trim());
    if (gone) { this.removed.add(gone[1]); return; }
    const p = applyLine(r, clean);
    this.run.set({ ...r, ...p, log: [...r.log.slice(-400), clean] });
  }

  private async done(e: { code: number; error: string }) {
    this.unsubs.forEach(u => u());
    this.unsubs = [];
    const r = this.run();
    if (!r) return;
    const req = this.req;
    const name = r.machine;
    if (e.error === 'cancelled') {
      this.run.set({ ...r, phase: 'cancelled', step: 'Cancelled', detail: '', error: '' });
      this.ui.toast(`Remote desktop setup on ${name} was cancelled.`, 'info');
      return;
    }
    // exit status 3: one service did not come up and was removed again, the other works
    const partial = !e.error && e.code === 3 && !r.check && this.removed.size > 0;
    if (!partial && (e.error || e.code !== 0)) {
      const error = r.error || e.error || 'The setup ended with errors. Open the details to see what happened.';
      this.run.set({ ...r, phase: 'failed', step: 'Failed', detail: '', error });
      this.ui.toast(`Remote desktop setup on ${name} failed: ${error}`, 'error', 9000);
      return;
    }
    if (r.check) {
      this.run.set({ ...r, phase: 'done', progress: 100 });
      this.ui.toast(`Check finished on ${name}: nothing was changed.`, 'info', 6000);
      return;
    }
    const made: RdRun['made'] = {};
    this.unsaved = [];
    const tab = this.tabs.get(req?.tabId ?? '');
    if (this.createSessions && req && tab) {
      try {
        if (req.rdp && !this.removed.has('rdp')) made.rdp = await this.ensureSession(r.id, tab.session, 'RDP', req);
        if (req.vnc && !this.removed.has('vnc')) made.vnc = await this.ensureSession(r.id, tab.session, 'VNC', req);
        await this.ui.refreshTree();
        void this.backend.forgetRemoteDesktop(r.id).catch(() => {}); // the passwords are stored in the sessions now
      } catch (err) {
        const error = `The remote desktop is ready, but its sessions could not be created: ${errText(err)}`;
        this.run.set({ ...r, phase: 'failed', step: 'Failed', detail: '', error });
        this.ui.toast(error, 'error', 9000);
        return;
      }
    }
    const gone = [...this.removed].map(k => k.toUpperCase()).join(' and ');
    const step = partial ? `Ready, except ${gone}: it did not work and was removed again` : 'Remote desktop is ready';
    this.run.set({ ...r, phase: 'done', progress: 100, step, detail: '', error: '', made, unsaved: this.unsaved, ready: { rdp: !!req?.rdp && !this.removed.has('rdp'), vnc: !!req?.vnc && !this.removed.has('vnc') } });
    const actions = [
      ...(made.rdp ? [{ label: 'Open RDP', run: () => void this.tabs.openSession(made.rdp!, 'auto') }] : []),
      ...(made.vnc ? [{ label: 'Open VNC', run: () => void this.tabs.openSession(made.vnc!, 'auto') }] : []),
    ];
    this.ui.toast(partial ? `Remote desktop is partly ready on ${name}: ${gone} failed and was removed again.` : `Remote desktop is ready on ${name}.` + (actions.length ? ' The new sessions are next to it.' : ''), partial ? 'info' : 'success', 15000, actions);
  }

  private unsaved: ('RDP' | 'VNC')[] = [];

  private async hasPassword(path: string): Promise<boolean> {
    try { return (await this.backend.sessionPasswordState(path)) !== ''; } catch { return false; }
  }

  /** Opens one of the new sessions. */
  open(path: string) { void this.tabs.openSession(path, 'auto'); }

  /** Creates "<ssh> (RDP|VNC)" in the folder of the SSH session; an existing one that already does the job is reused. */
  private async ensureSession(runId: string, ssh: Session, kind: 'RDP' | 'VNC', o: RdRequest): Promise<string> {
    const path = await this.makeSession(ssh, kind, o);
    // the user name is in the session; the password too when this run set it (the OS keychain)
    const stored = await this.backend.remoteDesktopSessionPassword(runId, path, kind);
    if (!stored && !(await this.hasPassword(path))) this.unsaved.push(kind);
    return path;
  }

  private async makeSession(ssh: Session, kind: 'RDP' | 'VNC', o: RdRequest): Promise<string> {
    const viaSsh = !(kind === 'RDP' ? o.rdpNetwork : o.vncNetwork);
    const port = kind === 'RDP' ? RDP_PORT : VNC_PORT;
    const host = viaSsh ? '127.0.0.1' : ssh.hostname;
    const user = kind === 'RDP' ? o.rdpUser : o.vncUser;
    const base = remoteSessionName(ssh.name, kind);
    const tree = this.ui.tree();
    for (let n = 1; n < 50; n++) {
      const name = n === 1 ? base : `${base} ${n}`;
      const path = ssh.folder ? `${ssh.folder}/${name}` : name;
      const have = findNode(tree?.children, path);
      if (have) {
        if (have.protocol === kind && (have.port ?? 0) === port && (have.hostname ?? '') === host && (!viaSsh || have.jump === ssh.path)
          && (have.username ?? '') === user) return path;
        continue;
      }
      const s = await this.backend.newSessionDefaults();
      Object.assign(s, {
        name, folder: ssh.folder, protocol: kind, hostname: host, port, username: user,
        jumpSession: viaSsh ? ssh.path : '', description: [`${kind} desktop of ${ssh.name} (made by Set Up Remote Desktop)`],
      } as Partial<Session>);
      return (await this.backend.saveSession(s, '')).path;
    }
    throw new Error(`too many sessions named "${base}"`);
  }
}

function findNode(list: { path: string; isFolder: boolean; protocol?: string; port?: number; hostname?: string; jump?: string; username?: string; children?: any[] }[] | undefined, path: string): any {
  for (const c of list ?? []) {
    if (c.path === path) return c;
    if (c.isFolder) { const x = findNode(c.children, path); if (x) return x; }
  }
  return undefined;
}
