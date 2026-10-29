import { Injectable, inject, signal } from '@angular/core';
import { Backend, errText } from './backend';
import { Session, SessionNode } from './models';
import { Ui } from './ui.service';

/** Where a Wake-on-LAN device stands: up (green), down (orange), or being woken. */
export interface WolState {
  state: 'up' | 'down' | 'waking' | 'unknown';
  ip?: string;
  name?: string; // NetBIOS name
  message?: string;
  at?: number; // last check
}

/**
 * Wake-on-LAN sessions: waking a device (the backend sends the magic packet,
 * then pings it or searches its network for the MAC address and reports
 * "wol:status"), and pinging every device with an IP address every few
 * minutes (the session's setting) for the green / orange dot.
 */
@Injectable({ providedIn: 'root' })
export class Wol {
  private backend = inject(Backend);
  private ui = inject(Ui);
  readonly states = signal<Record<string, WolState>>({});
  private due: Record<string, number> = {};

  constructor() {
    this.backend.on('wol:status', (st: { path: string; state: string; message: string; ip?: string; name?: string; scan?: number; scans?: number }) => {
      const final = ['up', 'down', 'notfound', 'error', 'sent'].includes(st.state);
      const state: WolState['state'] = st.state === 'up' ? 'up' : st.state === 'down' || st.state === 'notfound' ? 'down' : final ? 'unknown' : 'waking';
      this.set(st.path, { state, ip: st.ip, name: st.name, message: st.message, at: Date.now() });
      if (final) this.ui.toast(st.message, st.state === 'up' ? 'success' : st.state === 'sent' ? 'info' : 'error', st.state === 'up' ? 8000 : 15000);
      if (st.state === 'up' && st.ip) void this.ui.refreshTree(); // an address found by the MAC search is saved
    });
    setInterval(() => void this.tick(), 30_000);
    setTimeout(() => void this.tick(), 4000);
  }

  get(path: string): WolState | undefined { return this.states()[path]; }
  private set(path: string, st: WolState) { this.states.update(m => ({ ...m, [path]: { ...m[path], ...st } })); }

  /** Wakes a Wake-on-LAN session's device and watches it. */
  async wake(s: Session) {
    try {
      const r = await this.backend.wakeDevice(s);
      this.set(s.path || 'mac:' + s.mac, { state: 'waking', message: 'Magic packet sent' });
      this.ui.toast(`Magic packet sent to ${s.name || s.mac}${r.via ? ' from ' + r.via : ''} (${r.sent.join(', ')})` +
        (s.hostname ? ` - waiting for ${s.hostname} to answer ping…` : s.wolNetwork ? ` - searching ${s.wolNetwork} for it…` : '.'), 'info', 8000);
    } catch (e) { this.ui.toast('Wake-on-LAN: ' + errText(e), 'error', 10000); }
  }

  /** Pings the Wake-on-LAN sessions whose time has come. */
  private async tick() {
    const nodes: SessionNode[] = [];
    const walk = (n: SessionNode | null) => n?.children?.forEach(c => (c.isFolder ? walk(c) : c.protocol === 'WOL' && c.hostname && (c.wolPing ?? 120) > 0 && nodes.push(c)));
    walk(this.ui.tree());
    const now = Date.now();
    const go = nodes.filter(n => (this.due[n.path] ?? 0) <= now && this.get(n.path)?.state !== 'waking');
    if (!go.length) return;
    go.forEach(n => (this.due[n.path] = now + (n.wolPing ?? 120) * 1000));
    const r = await this.backend.pingHosts(go.map(n => ({ key: n.path, host: n.hostname!, port: 0 }))).catch(() => null);
    if (!r) return;
    for (const n of go) {
      const p = r[n.path];
      if (p) this.set(n.path, { state: p.up ? 'up' : 'down', ip: n.hostname, name: p.name || this.get(n.path)?.name, at: Date.now() });
    }
  }

  /** Checks one session now (after editing it). */
  checkNow(path: string) { this.due[path] = 0; void this.tick(); }
}
