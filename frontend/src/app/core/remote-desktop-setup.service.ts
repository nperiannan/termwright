import { Injectable, inject } from '@angular/core';
import { Backend, errText } from './backend';
import { Session } from './models';
import { Tabs } from './tabs.service';
import { Ui } from './ui.service';

export interface RemoteDesktopOptions {
  /** Terminal tab of the SSH session the script runs on. */
  tabId: string;
  user: string;
  rdp: boolean;
  vnc: boolean;
  /** 'ssh': both services listen on the machine's own address only; 'network': on all interfaces. */
  access: 'ssh' | 'network';
  /** Network to open in the firewall (access = 'network'), e.g. 10.1.2.0/24. */
  cidr: string;
  /** Create the RDP / VNC sessions when the script ends without errors. */
  createSessions: boolean;
  /** Only report what is running, change nothing. */
  check: boolean;
  /** Reconfigure and restart services that already run (--force); connected users are disconnected. */
  force?: boolean;
}

export const VNC_DISPLAY = 2;
export const VNC_PORT = 5900 + VNC_DISPLAY;
export const RDP_PORT = 3389;

const SH = (s: string) => `'${s.replace(/'/g, `'\\''`)}'`;

/** The command typed into the terminal. It ends by telling Termwright (an OSC 1337 line) how the script ended. */
export function remoteDesktopCommand(remote: string, o: Pick<RemoteDesktopOptions, 'user' | 'rdp' | 'vnc' | 'access' | 'cidr' | 'check' | 'force'>, token: string): string {
  const args = ['--user', SH(o.user), '--vnc-display', String(VNC_DISPLAY)];
  if (!o.rdp) args.push('--no-rdp');
  if (!o.vnc) args.push('--no-vnc');
  if (o.access === 'network') {
    if (o.rdp) args.push('--rdp-network');
    if (o.vnc) args.push('--vnc-network');
    if (o.cidr) args.push('--allow-cidr', SH(o.cidr));
  }
  if (o.check) args.push('--check');
  else if (o.force) args.push('--force');
  const run = o.check ? 'bash' : 'sudo bash';
  // leading space: kept out of the shell history
  return ` ${run} ${SH(remote)} ${args.join(' ')}; _twrc=$?; rm -f ${SH(remote)}; ` +
    `printf '\\033]1337;TwRemoteDesktop=%s;${token}\\007' "$_twrc"\r`;
}

/** Name of the session made for one protocol next to the SSH session. */
export const remoteSessionName = (ssh: string, kind: 'RDP' | 'VNC') => `${ssh} (${kind})`;

/**
 * Set Up Remote Desktop: copies the script to the Linux machine of an SSH
 * terminal tab, runs it there with sudo in that tab (so the user sees
 * everything and types the passwords there), and - only when it ends without
 * errors - creates the RDP and VNC sessions that reach the new desktop.
 */
@Injectable({ providedIn: 'root' })
export class RemoteDesktopSetup {
  private backend = inject(Backend);
  private ui = inject(Ui);
  private tabs = inject(Tabs);

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

  /** Uploads the script, types the command in the tab and follows it to the end. */
  async run(o: RemoteDesktopOptions) {
    const t = this.tabs.get(o.tabId);
    if (!t) throw new Error('The SSH tab is closed.');
    const remote = await this.backend.uploadRemoteDesktopScript(o.tabId);
    const token = Math.random().toString(36).slice(2, 10);
    this.tabs.patch(o.tabId, { rdDone: '' });
    this.tabs.activeId.set(o.tabId);
    await this.backend.sendInput(o.tabId, remoteDesktopCommand(remote, o, token));
    void this.follow(o, token);
  }

  private async follow(o: RemoteDesktopOptions, token: string) {
    const name = this.tabs.get(o.tabId)?.title ?? '';
    for (let i = 0; i < 4 * 3600 * 2; i++) { // up to 4 hours: packages download, passwords are typed
      await new Promise(r => setTimeout(r, 500));
      const t = this.tabs.get(o.tabId);
      if (!t || t.state === 'disconnected' || t.state === 'error') return; // tab closed or connection lost
      const m = /^(\d+);(\w+)$/.exec(t.rdDone ?? '');
      if (!m || m[2] !== token) continue;
      const rc = +m[1];
      if (o.check) { this.ui.toast(`Check finished on ${name}: nothing was changed.`, 'info', 6000); return; }
      if (rc !== 0) { this.ui.toast(`Remote desktop setup on ${name} ended with errors - see the terminal tab.`, 'error', 9000); return; }
      await this.finish(o, t.session);
      return;
    }
  }

  private async finish(o: RemoteDesktopOptions, ssh: Session) {
    const made: { rdp?: string; vnc?: string } = {};
    if (o.createSessions) {
      try {
        if (o.rdp) made.rdp = await this.ensureSession(ssh, 'RDP', o);
        if (o.vnc) made.vnc = await this.ensureSession(ssh, 'VNC', o);
        await this.ui.refreshTree();
      } catch (e) {
        this.ui.toast(`The remote desktop is ready on ${ssh.name}, but its sessions could not be created: ${errText(e)}`, 'error', 9000);
        return;
      }
    }
    const actions = [
      ...(made.rdp ? [{ label: 'Open RDP', run: () => void this.tabs.openSession(made.rdp!, 'auto') }] : []),
      ...(made.vnc ? [{ label: 'Open VNC', run: () => void this.tabs.openSession(made.vnc!, 'auto') }] : []),
    ];
    this.ui.toast(`Remote desktop is ready on ${ssh.name}.` + (actions.length ? ' The new sessions are next to it.' : ''), 'success', 15000, actions);
  }

  /** Creates "<ssh> (RDP|VNC)" in the folder of the SSH session; an existing one that already does the job is reused. */
  private async ensureSession(ssh: Session, kind: 'RDP' | 'VNC', o: RemoteDesktopOptions): Promise<string> {
    const viaSsh = o.access === 'ssh';
    const port = kind === 'RDP' ? RDP_PORT : VNC_PORT;
    const host = viaSsh ? '127.0.0.1' : ssh.hostname;
    const base = remoteSessionName(ssh.name, kind);
    const tree = this.ui.tree();
    for (let n = 1; n < 50; n++) {
      const name = n === 1 ? base : `${base} ${n}`;
      const path = ssh.folder ? `${ssh.folder}/${name}` : name;
      const have = findNode(tree?.children, path);
      if (have) {
        if (have.protocol === kind && (have.port ?? 0) === port && (have.hostname ?? '') === host && (!viaSsh || have.jump === ssh.path)) return path;
        continue;
      }
      const s = await this.backend.newSessionDefaults();
      Object.assign(s, {
        name, folder: ssh.folder, protocol: kind, hostname: host, port, username: o.user,
        jumpSession: viaSsh ? ssh.path : '', description: [`${kind} desktop of ${ssh.name} (made by Set Up Remote Desktop)`],
      } as Partial<Session>);
      return (await this.backend.saveSession(s, '')).path;
    }
    throw new Error(`too many sessions named "${base}"`);
  }
}

function findNode(list: { path: string; isFolder: boolean; protocol?: string; port?: number; hostname?: string; jump?: string; children?: any[] }[] | undefined, path: string): any {
  for (const c of list ?? []) {
    if (c.path === path) return c;
    if (c.isFolder) { const x = findNode(c.children, path); if (x) return x; }
  }
  return undefined;
}
