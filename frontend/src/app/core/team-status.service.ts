import { Injectable, effect, inject, signal } from '@angular/core';
import { Backend } from './backend';
import { Ui } from './ui.service';
import { Tabs } from './tabs.service';
import { deviceKindOf } from '../components/device-icon';

/** A device as the team server knows it: address and port, or MAC address. */
export interface DeviceRef { host: string; port: number; protocol: string; mac?: string; name?: string; since?: string; }
export interface Reservation { id: string; device: DeviceRef; by: string; name: string; until: string; note: string; at: string; }
export interface InUse { device: DeviceRef; by: string; name: string; since: string; }
export interface TeamStatus { rev: number; me: string; inUse: InUse[]; reserved: Reservation[]; }
interface View { teamId: string; team: string; status: TeamStatus; }

/** What a device's status is for this person. */
export interface DeviceState {
  others: InUse[];          // other members who have it open
  mine: boolean;            // I have it open (in some team's view)
  reserved?: Reservation;   // reserved by someone
  reservedByMe: boolean;
  teamId: string;
  team: string;
}

/** Kinds that take several logins at once: never "in use" or reserved. */
const SHARED_KINDS = new Set(['iot', 'phone', 'mobile', 'server', 'computer', 'printer', 'nas', 'tv', 'camera']);

/** A network device (switch, router, firewall, access point, …) reached by SSH or Telnet. */
export function isNetworkDevice(s: { protocol?: string; deviceType?: string }): boolean {
  if (s.protocol !== 'SSH2' && s.protocol !== 'Telnet') return false;
  return !SHARED_KINDS.has(deviceKindOf(s.deviceType));
}

/** "Natarajan Periannan" → "NP". */
function initials(n: string) { return n.split(/\s+/).filter(Boolean).slice(0, 2).map(w => w[0].toUpperCase()).join(''); }
function defPort(p: string) { return p === 'Telnet' ? 23 : 22; }
function normMac(m?: string) { return (m ?? '').toLowerCase().replace(/[^0-9a-f]/g, ''); }
export function sameDevice(a: { host?: string; hostname?: string; port?: number; protocol?: string; mac?: string }, b: DeviceRef) {
  const ha = (a.host ?? a.hostname ?? '').trim().toLowerCase(), pa = a.port || defPort(a.protocol ?? '');
  if (ha && ha === b.host.trim().toLowerCase() && pa === (b.port || defPort(b.protocol))) return true;
  const ma = normMac(a.mac), mb = normMac(b.mac);
  return !!ma && ma === mb;
}

/**
 * Who uses which device in the teams this Termwright joined: tells the team
 * servers which network devices have a connected tab (the backend keeps only
 * those in each team's inventory), and follows their status. Nothing happens
 * without a team.
 */
@Injectable({ providedIn: 'root' })
export class TeamStatusService {
  private backend = inject(Backend);
  private ui = inject(Ui);
  private tabs = inject(Tabs);
  readonly views = signal<Map<string, View>>(new Map());
  private lastSent = '';

  constructor() {
    this.backend.on('team:status', (v: View) => this.views.update(m => new Map(m).set(v.teamId, v)));
    this.backend.on('team:note', (n: { team: string; text: string }) => this.ui.toast(`${n.team}: ${n.text}`, 'info', 12000));
    effect(() => {
      if (!(this.ui.settings()?.teams ?? []).length) { this.views.set(new Map()); return; }
      void this.backend.teamStatuses().then(l => this.views.set(new Map((l ?? []).map(v => [v.teamId, v])))).catch(() => {});
    });
    // the open network devices, whenever the tabs change
    effect(() => {
      const teams = (this.ui.settings()?.teams ?? []).length;
      const list = this.tabs.tabs()
        .filter(t => t.kind === 'terminal' && t.state === 'connected' && t.session && isNetworkDevice(t.session) && t.session.hostname)
        .map(t => ({ host: t.session.hostname, port: t.session.port || defPort(t.session.protocol), protocol: t.session.protocol, mac: t.session.mac ?? '',
          name: t.session.name || t.session.hostname, since: '' } as DeviceRef));
      const key = JSON.stringify(list.map(d => d.host + ':' + d.port + '/' + d.mac).sort());
      if (!teams || key === this.lastSent) return;
      this.lastSent = key;
      // "since": when the device was first reported open by this window
      const now = new Date().toISOString();
      for (const d of list) d.since = this.since.get(d.host + ':' + d.port) ?? now;
      this.since = new Map(list.map(d => [d.host + ':' + d.port, d.since!]));
      void this.backend.teamPresence(list).catch(() => {});
    });
  }
  private since = new Map<string, string>();

  /** The status of a session's device ('' when no team has it or it is not a network device). */
  stateOf(s: { hostname?: string; port?: number; protocol?: string; mac?: string; deviceType?: string } | null | undefined): DeviceState | null {
    if (!s || !s.hostname || !isNetworkDevice(s) || !this.views().size) return null;
    for (const v of this.views().values()) {
      const others = v.status.inUse.filter(u => u.by !== v.status.me && sameDevice(s, u.device));
      const mine = v.status.inUse.some(u => u.by === v.status.me && sameDevice(s, u.device));
      const reserved = v.status.reserved.find(r => sameDevice(s, r.device));
      if (others.length || mine || reserved) {
        return { others, mine, reserved, reservedByMe: !!reserved && reserved.by === v.status.me, teamId: v.teamId, team: v.team };
      }
    }
    return null;
  }

  /** Short label for a badge. */
  label(st: DeviceState | null): { text: string; short: string; tiny: string; cls: string; title: string } | null {
    if (!st) return null;
    if (st.reserved) {
      const until = new Date(st.reserved.until);
      const t = until.toDateString() === new Date().toDateString() ? until.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : until.toLocaleDateString([], { day: 'numeric', month: 'short' });
      const who = st.reservedByMe ? 'you' : st.reserved.name.split(' ')[0];
      return { text: '🔒 ' + who + ' · ' + t, short: '🔒 ' + who, tiny: st.reservedByMe ? '🔒 you' : '🔒 ' + initials(st.reserved.name), cls: st.reservedByMe ? 'me' : 'res',
        title: `Reserved by ${st.reservedByMe ? 'you' : st.reserved.name} until ${until.toLocaleString()}${st.reserved.note ? ' - “' + st.reserved.note + '”' : ''}` + this.useText(st) + ` (team ${st.team})` };
    }
    if (st.others.length) {
      const who = '● ' + st.others.map(o => o.name.split(' ')[0]).join(', ');
      return { text: who, short: who, tiny: '● ' + st.others.map(o => initials(o.name)).join(' '), cls: 'use', title: this.useText(st).replace(/^ · /, '') + ` (team ${st.team})` };
    }
    return null;
  }
  private useText(st: DeviceState) {
    return st.others.length ? ' · open by ' + st.others.map(o => o.name + (o.since ? ' since ' + new Date(o.since).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '')).join(', ') : '';
  }

  /** Before connecting: asks when someone else reserved the device; tells them when you go ahead. */
  async allowConnect(s: { hostname?: string; port?: number; protocol?: string; mac?: string; deviceType?: string; name?: string }): Promise<boolean> {
    const st = this.stateOf(s);
    if (!st) return true;
    if (st.reserved && !st.reservedByMe) {
      const r = st.reserved;
      const ok = await this.ui.confirm(`${s.name || s.hostname} is reserved by ${r.name}`,
        `Until ${new Date(r.until).toLocaleString()}${r.note ? '\n“' + r.note + '”' : ''}\n\n${r.name} gets a notice that you connected.` + (st.others.length ? `\nOpen now by ${st.others.map(o => o.name).join(', ')}.` : ''),
        'Connect Anyway');
      if (!ok) return false;
      void this.backend.teamConnected({ host: s.hostname!, port: s.port || defPort(s.protocol ?? ''), protocol: s.protocol ?? 'SSH2', mac: s.mac ?? '' }).catch(() => {});
      return true;
    }
    if (st.others.length) this.ui.toast(`${s.name || s.hostname} is open by ${st.others.map(o => o.name).join(', ')} too.`, 'info', 7000);
    return true;
  }
}
