import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { Backend } from './backend';
import { Ui } from './ui.service';
import { Tabs } from './tabs.service';
import { SessionNode } from './models';
import { tr } from './i18n';
import { deviceKindOf } from '../components/device-icon';

/** A device as the team server knows it: address and port, or MAC address. */
export interface DeviceRef { host: string; port: number; protocol: string; mac?: string; name?: string; since?: string; }
export interface Reservation { id: string; device: DeviceRef; by: string; name: string; until: string; note: string; at: string; }
export interface InUse { device: DeviceRef; by: string; name: string; since: string; }
export interface LastUse { device: DeviceRef; name: string; s: number; e: number; }
export interface TeamStatus { rev: number; me: string; inUse: InUse[]; reserved: Reservation[]; last?: LastUse[];
  /** the other members with my name: me on another computer */ also?: string[];
  /** the devices I wait for */ watching?: DeviceRef[]; }
interface View { teamId: string; team: string; status: TeamStatus; }

/** What a device's status is for this person. */
export interface DeviceState {
  others: InUse[];          // other members who have it open
  mine: boolean;            // I have it open (in some team's view)
  reserved?: Reservation;   // reserved by someone
  reservedByMe: boolean;
  /** everyone with it open now, you included */
  open: InUse[];
  me: string;
  /** my other computers (members with my name) */
  also?: string[];
  /** I wait for it to be free (a notice comes then) */
  watching?: boolean;
  /** the last time someone had it open (when nobody has it open now) */
  last?: LastUse;
  teamId: string;
  team: string;
}

/** Kinds that take several logins at once: never "in use" or reserved. */
const SHARED_KINDS = new Set(['iot', 'phone', 'mobile', 'server', 'computer', 'printer', 'nas', 'tv', 'camera']);

/** A network device (switch, router, firewall, access point, …) reached by SSH or Telnet: it has a
 *  device type, and not one of the kinds that take several logins at once. */
export function isNetworkDevice(s: { protocol?: string; deviceType?: string }): boolean {
  if (s.protocol !== 'SSH2' && s.protocol !== 'Telnet') return false;
  if (!(s.deviceType ?? '').trim()) return false;
  return !SHARED_KINDS.has(deviceKindOf(s.deviceType));
}

/** A console session's device name: "core-sw1 console", "core-sw1-con", "core-sw1 (console)" → "core-sw1". */
export function consoleName(n: string): string {
  return n.trim().toLowerCase().replace(/[\s_-]*\(?\b(console|con|serial|cons)\b\)?$/, '').trim();
}

type SessionLike = { name?: string; hostname?: string; port?: number; protocol?: string; mac?: string; deviceType?: string };

/** "Natarajan Periannan" → "NP". */
function initials(n: string) { return n.split(/\s+/).filter(Boolean).slice(0, 2).map(w => w[0].toUpperCase()).join(''); }
/** 4 min, 2 h 10 min, 3 d 4 h */
export function span(sec: number) {
  const m = Math.max(1, Math.round(Math.max(0, sec) / 60));
  if (m < 60) return m + ' min';
  if (m < 1440) return Math.floor(m / 60) + ' h' + (m % 60 ? ' ' + (m % 60) + ' min' : '');
  const h = Math.round(m / 60); return Math.floor(h / 24) + ' d' + (h % 24 ? ' ' + (h % 24) + ' h' : '');
}
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
    this.backend.on('team:sso', (n: { teamId: string; team: string }) => this.ui.toast(tr('{0} asks you to sign in again with your company account.', n.team), 'info', 20000,
      [{ label: tr('Sign In Again…'), run: () => void this.backend.teamSSOAgain(n.teamId).then(() => this.ui.toast(tr('Signed in to {0}', n.team), 'success')).catch(e => this.ui.error(e)) }]));
    this.backend.on('team:note', (n: { team: string; text: string }) => this.ui.toast(`${n.team}: ${n.text}`, 'info', 12000));
    effect(() => {
      if (!(this.ui.settings()?.teams ?? []).length) { this.views.set(new Map()); return; }
      void this.backend.teamStatuses().then(l => this.views.set(new Map((l ?? []).map(v => [v.teamId, v])))).catch(() => {});
      // tells owners and co-owners once a run when a team server is older than this Termwright (teams joined later too)
      const fresh = (this.ui.settings()?.teams ?? []).map(t => t.id).filter(id => !this.versionsChecked.has(id));
      if (fresh.length) {
        fresh.forEach(id => this.versionsChecked.add(id));
        setTimeout(() => void this.backend.teams().then(ts => {
          for (const t of ts ?? []) {
            if (!fresh.includes(t.id) || !t.reachable || !t.update || t.role === 'member' || this.snoozed(t.id, t.update)) continue;
            this.ui.toast(tr('The team server of {0} runs {1}: version {2} is available.', t.name, t.version || '?', t.update), 'info', 15000, [
              { label: 'Update Server…', run: () => { this.ui.teamsUpdate.set(t.id); this.ui.open('teams'); } },
              { label: 'Later', run: () => this.snooze(t.id, t.update!) }]);
          }
        }).catch(() => {}), 8000);
      }
    });
    // the open network devices, whenever the tabs change
    effect(() => {
      const teams = (this.ui.settings()?.teams ?? []).length;
      // connected SSH and Telnet tabs, and consoles of team devices; Termwright keeps those of each team's
      // inventory whose device type is a network device
      const seen = new Set<string>();
      const list = this.tabs.tabs()
        .filter(t => t.kind === 'terminal' && t.state === 'connected' && t.session)
        .map(t => this.target(t.session))
        .filter((d): d is SessionLike => !!d && !!d.hostname)
        .map(d => ({ host: d.hostname!, port: d.port || defPort(d.protocol ?? ''), protocol: d.protocol ?? 'SSH2', mac: d.mac ?? '', name: d.name || d.hostname!, since: '' } as DeviceRef))
        .filter(d => { const k = d.host + ':' + d.port; if (seen.has(k)) return false; seen.add(k); return true; });
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

  /** The team devices of the Session Manager (team folders), by name: a serial or console server
   *  session named after one is its console. */
  private readonly teamByName = computed(() => {
    const m = new Map<string, SessionLike>();
    const walk = (n: SessionNode) => (n.children ?? []).forEach(c => {
      if (c.isFolder) walk(c);
      else if (c.team && c.hostname && (c.protocol === 'SSH2' || c.protocol === 'Telnet')) m.set(consoleName(c.name), c);
    });
    const r = this.ui.tree();
    if (r) walk(r);
    return m;
  });

  /** The device whose team status a session shows: itself (SSH, Telnet), or for a console session
   *  (Serial, console server port) the team device it is named after. */
  target(s: SessionLike | null | undefined): SessionLike | null {
    if (!s) return null;
    if (s.protocol === 'Serial' || s.protocol === 'Console') return s.name ? this.teamByName().get(consoleName(s.name)) ?? null : null;
    if ((s.protocol === 'SSH2' || s.protocol === 'Telnet') && s.hostname) return s;
    return null;
  }
  private versionsChecked = new Set<string>();
  private static readonly LATER = 'termwright.teamUpdateLater';
  /** "Later" on an update notice: not again for this version for a week */
  private snooze(id: string, version: string) {
    try {
      const m = JSON.parse(localStorage.getItem(TeamStatusService.LATER) || '{}');
      m[id] = { version, until: Date.now() + 7 * 864e5 };
      localStorage.setItem(TeamStatusService.LATER, JSON.stringify(m));
    } catch { /* no storage: it asks again next time */ }
  }
  private snoozed(id: string, version: string): boolean {
    try {
      const x = JSON.parse(localStorage.getItem(TeamStatusService.LATER) || '{}')[id];
      return !!x && x.version === version && x.until > Date.now();
    } catch { return false; }
  }

  /** The status of a session's device ('' when no team has it or it is not a network device). */
  stateOf(x: SessionLike | null | undefined): DeviceState | null {
    const s = this.target(x);
    if (!s || !s.hostname || !this.views().size) return null;
    for (const v of this.views().values()) {
      const isMe = (id: string) => id === v.status.me || (v.status.also ?? []).includes(id);
      const others = v.status.inUse.filter(u => !isMe(u.by) && sameDevice(s, u.device));
      const mine = v.status.inUse.some(u => isMe(u.by) && sameDevice(s, u.device));
      const reserved = v.status.reserved.find(r => sameDevice(s, r.device));
      const last = (v.status.last ?? []).find(l => sameDevice(s, l.device));
      if (others.length || mine || reserved || last) {
        const open = v.status.inUse.filter(u => sameDevice(s, u.device));
        const watching = (v.status.watching ?? []).some(w => sameDevice(s, w));
        return { others, mine, reserved, reservedByMe: !!reserved && isMe(reserved.by), open, me: v.status.me, also: v.status.also ?? [], last, teamId: v.teamId, team: v.team, watching };
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

  /** The lines of the team card: reservation, who has it open, when it was last used. */
  details(st: DeviceState): { head: string; rows: [string, string][]; note?: string; foot: string } {
    const now = Date.now() / 1000, t = (sec: number) => new Date(sec * 1000).toLocaleString([], { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
    const rows: [string, string][] = [];
    let head = 'Free', note: string | undefined;
    if (st.reserved) {
      const r = st.reserved, from = Date.parse(r.at) / 1000, to = Date.parse(r.until) / 1000;
      head = st.reservedByMe ? 'Reserved by you' : 'Reserved by ' + r.name;
      rows.push(['From', t(from)], ['Until', t(to) + (to > now ? ' · ' + span(to - now) + ' left' : '')], ['Length', span(to - from)]);
      note = r.note || undefined;
    }
    if (st.open.length) {
      if (!st.reserved) head = 'Open now';
      for (const u of st.open) rows.push(['Open by', (u.by === st.me ? 'you' : st.also?.includes(u.by) ? 'you (another computer)' : u.name) + (u.since ? ' · since ' + t(Date.parse(u.since) / 1000) + ' (' + span(now - Date.parse(u.since) / 1000) + ')' : '')]);
    } else if (st.last) {
      rows.push(['Last used', st.last.name + ' · ' + t(st.last.s) + ' for ' + span(st.last.e - st.last.s)]);
      rows.push([st.reserved ? 'Not open for' : 'Free for', span(now - st.last.e)]);
    }
    if (st.watching) rows.push(['Waiting', 'you get a notice when it is free']);
    const foot = st.reservedByMe ? 'Right-click → Change Reservation… or Release Reservation' : st.reserved ? 'Connecting asks first; ' + st.reserved.name + ' gets a notice' : 'Right-click → Reserve… to keep it for a while';
    return { head, rows, note, foot };
  }

  /** Before connecting: asks when someone else reserved the device; tells them when you go ahead. */
  async allowConnect(x: SessionLike): Promise<boolean> {
    const st = this.stateOf(x), s = this.target(x);
    if (!st || !s) return true;
    const label = x.name || s.name || s.hostname;
    if (st.reserved && !st.reservedByMe) {
      const r = st.reserved;
      const ok = await this.ui.confirm(tr('{0} is reserved by {1}', label ?? '', r.name),
        tr('Until {0}', new Date(r.until).toLocaleString()) + (r.note ? '\n“' + r.note + '”' : '') + '\n\n' + tr('{0} gets a notice that you connected.', r.name)
          + (st.others.length ? '\n' + tr('Open now by {0}.', st.others.map(o => o.name).join(', ')) : ''),
        'Connect Anyway');
      if (!ok) return false;
      void this.backend.teamConnected({ host: s.hostname!, port: s.port || defPort(s.protocol ?? ''), protocol: s.protocol ?? 'SSH2', mac: s.mac ?? '' }).catch(() => {});
      return true;
    }
    if (st.others.length) this.ui.toast(tr('{0} is open by {1} too.', label ?? '', st.others.map(o => o.name).join(', ')), 'info', 7000);
    return true;
  }
}
