import { DEVICE_KINDS, DeviceKind, GLYPHS } from '../components/device-icon';
import type { TopoLag } from './models';

/** Drawing of the Network Map as SVG markup (strings, so the same picture is on screen and in the JPEG, PDF
 *  and HTML exports). Nothing here reads the DOM or Angular. */

export type Look = 'ant' | 'material' | 'constellation' | 'zones' | 'flow' | 'focus' | 'card' | 'tier';
/** What the cables are coloured by: their speed, their health (up / down), spanning tree, or LAG membership. */
export type View = 'speed' | 'health' | 'stp' | 'lag';
export const LOOKS: { id: Look; label: string; hint: string }[] = [
  { id: 'ant', label: 'Clean boxes', hint: 'Tidy boxes in tiers with right-angle cables' },
  { id: 'material', label: 'Soft cards', hint: 'Rounded tonal cards with curved cables' },
  { id: 'focus', label: 'Focus', hint: 'Round devices; cables stay quiet unless something needs a look. Click a device to focus on it' },
  { id: 'constellation', label: 'Constellation', hint: 'The core in the middle, one ring per level' },
  { id: 'zones', label: 'Zones', hint: 'Devices grouped by role, cables bundled between the groups' },
  { id: 'flow', label: 'Flow (default)', hint: 'Left to right from the core to the edge' },
  { id: 'card', label: 'Classic cards', hint: 'Cards with a switch front panel, any layout' },
  { id: 'tier', label: 'Classic tiers', hint: 'Classic cards in labelled tiers' },
];
/** Looks whose positions come from the links (nothing is stored or dragged). */
export const COMPUTED_LOOKS: Look[] = ['constellation', 'zones', 'flow'];
const SIZES: Record<Look, { w: number; h: number }> = {
  card: { w: 184, h: 56 }, tier: { w: 184, h: 56 }, ant: { w: 176, h: 54 }, material: { w: 176, h: 66 },
  constellation: { w: 78, h: 78 }, focus: { w: 74, h: 74 }, zones: { w: 164, h: 78 }, flow: { w: 216, h: 52 },
};
/** Size of a device box in each look. */
export const dims = (look: Look = 'ant') => SIZES[look] ?? SIZES.ant;
const orthLook = (l?: Look) => l === 'tier' || l === 'ant';

export interface Prog { state: 'queued' | 'running' | 'done' | 'error'; done: number; }
export interface MNode { id: string; name: string; sub: string; kind: string; ghost: boolean; x: number; y: number; flag?: string; role?: string; ip?: string; os?: string; notes?: number; group?: string; prog?: Prog; }
export interface Zone { x: number; y: number; w: number; h: number; label: string; right?: string; id?: string; ghost?: boolean; }
export interface MLink { key: string; a: string; aPort: string; b: string; bPort: string; state: 'up' | 'blocked' | 'down'; lagA?: TopoLag | null; lagB?: TopoLag | null; warn?: string; seen?: string[] | null; ends?: number; speed?: string; stpA?: string; stpB?: string; }
export interface MTheme { bg: string; dot: string; node: string; nodeLine: string; ink: string; mut: string; acc: string; ok: string; warn: string; bad: string; label: string; labelLine: string; ring: string; face: string; faceLine: string; port: string; zone: string; zoneLine: string; shadow: string; devBg: string; devLine: string; devInk: string; devCard: string; devCardInk: string; badgeBg: string; badgeLine: string; badgeInk: string; }

export const THEME_DARK: MTheme = { bg: '#1e1f22', dot: '#2b2c31', node: '#2a2b30', nodeLine: '#36373d', ink: '#dcdde1', mut: '#8b8e96', acc: '#4f9cf7', ok: '#34d399', warn: '#f59e0b', bad: '#f87171', label: '#222327', labelLine: '#36373d', ring: '#1e1f22', face: '#0f1115', faceLine: '#3a404c', port: '#4b5260', zone: '#191b20', zoneLine: '#2a2e36', shadow: 'rgba(0,0,0,.55)', devBg: '#2a1b4d', devLine: '#7c4dd8', devInk: '#c9b3ff', devCard: '#3b2a6b', devCardInk: '#efe6ff', badgeBg: '#2f3138', badgeLine: '#4a4d56', badgeInk: '#c5c8d0' };
export const THEME_LIGHT: MTheme = { bg: '#f4f5f7', dot: '#dcdfe4', node: '#ffffff', nodeLine: '#d5d8de', ink: '#1f2328', mut: '#656d76', acc: '#1f6fd6', ok: '#16a34a', warn: '#d97706', bad: '#dc2626', label: '#ffffff', labelLine: '#d5d8de', ring: '#f4f5f7', face: '#2b303a', faceLine: '#8a93a3', port: '#6b7385', zone: '#eceef2', zoneLine: '#d3d7de', shadow: 'rgba(20,30,50,.18)', devBg: '#efe6ff', devLine: '#8a5cf0', devInk: '#5b2fb8', devCard: '#e6dcff', devCardInk: '#2a1650', badgeBg: '#eef0f4', badgeLine: '#c9ced8', badgeInk: '#4a5160' };

export interface MOpts { theme: MTheme; showPorts: boolean; selLink: string; selNode: string; missing?: MLink[]; fresh?: Set<string>; look?: Look; zones?: Zone[]; view?: View; rings?: number[]; sweep?: boolean; }

const esc = (s: string) => s.replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]!));
const f = (n: number) => Math.round(n * 10) / 10;

/** Which device picture a node gets, from its OS or capabilities. */
export function kindOf(os: string | undefined, caps: string | undefined, ghost: boolean): string {
  const c = (caps ?? '').toLowerCase();
  if (os) return 'switch';
  if (/wlan|access point|wireless/.test(c)) return 'wireless';
  if (/router/.test(c) && !/bridge|switch/.test(c)) return 'router';
  if (/bridge|switch/.test(c)) return 'switch';
  if (/station|telephone|phone/.test(c)) return 'phone';
  return ghost ? 'unknown' : 'switch';
}

function defs(kinds: string[]): string {
  return '<defs>' + kinds.map(k => {
    const s = DEVICE_KINDS[k as DeviceKind] ?? DEVICE_KINDS.unknown;
    return `<linearGradient id="mg-${k}" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${s.from}"/><stop offset="1" stop-color="${s.to}"/></linearGradient>`;
  }).join('') + '</defs>';
}

function icon(kind: string, x: number, y: number, size: number, ghost: boolean): string {
  const spec = DEVICE_KINDS[kind as DeviceKind] ?? DEVICE_KINDS.unknown, g = GLYPHS[kind as DeviceKind] ?? GLYPHS.unknown;
  const sc = size / 24;
  return `<g transform="translate(${f(x)} ${f(y)}) scale(${f(sc)})" opacity="${ghost ? .5 : 1}"><path d="M12 .4C3 .4.4 3 .4 12S3 23.6 12 23.6 23.6 21 23.6 12 21 .4 12 .4z" fill="url(#mg-${kind})"/>`
    + `<g transform="translate(3.6 3.6) scale(.7)"><path d="${g.body}" fill="#fff"/>`
    + (g.cut ? `<path d="${g.cut}" fill="${spec.to}" fill-opacity=".85"/>` : '')
    + (g.cutLine ? `<path d="${g.cutLine}" fill="none" stroke="${spec.to}" stroke-width="1.6" stroke-linecap="round"/>` : '')
    + (g.line ? `<path d="${g.line}" fill="none" stroke="#fff" stroke-width="1.9" stroke-linecap="round"/>` : '')
    + (g.leds ?? []).map(l => `<circle cx="${l[0]}" cy="${l[1]}" r="1.05" fill="#22c55e"/>`).join('')
    + '</g></g>';
}

type P = { x: number; y: number };

/** Where a line from inside a box, going in direction (dx,dy), leaves it. */
function exit(c: P, p: P, dx: number, dy: number, hw: number, hh: number): number {
  const tx = dx > 0 ? (c.x + hw - p.x) / dx : dx < 0 ? (c.x - hw - p.x) / dx : Infinity;
  const ty = dy > 0 ? (c.y + hh - p.y) / dy : dy < 0 ? (c.y - hh - p.y) / dy : Infinity;
  return Math.min(tx, ty);
}

function pill(x: number, y: number, t: string, th: MTheme, col?: string, line?: string): string {
  const w = t.length * 6.2 + 14;
  return `<rect x="${f(x - w / 2)}" y="${f(y - 9)}" width="${f(w)}" height="18" rx="9" fill="${th.label}" stroke="${line ?? th.labelLine}"/>`
    + `<text x="${f(x)}" y="${f(y + 4)}" font-size="10.5" font-weight="500" fill="${col ?? th.mut}" text-anchor="middle">${esc(t)}</text>`;
}

interface Track { links: MLink[]; lag: boolean; }

/** One pair of devices: its cables as tracks - a LAG (the cables with the same LAG names) is one track. */
function pairTracks(ls: MLink[]): Track[] {
  const tracks: Track[] = [], by = new Map<string, Track>();
  for (const l of ls) {
    if (!l.lagA && !l.lagB) { tracks.push({ links: [l], lag: false }); continue; }
    const k = (l.lagA?.name ?? '') + '|' + (l.lagB?.name ?? '');
    let t = by.get(k);
    if (!t) { t = { links: [], lag: true }; by.set(k, t); tracks.push(t); }
    t.links.push(l);
  }
  return tracks;
}

const trackState = (t: Track): 'up' | 'blocked' | 'down' => t.links.every(l => l.state === 'down') ? 'down' : t.links.some(l => l.state === 'blocked') ? 'blocked' : 'up';

/** A speed such as "100M", "1G", "2.5G", "40G" in Mbit/s (0 when unknown). */
export function mbps(s?: string): number {
  const m = /^\s*([\d.]+)\s*([MGT])?/i.exec(s ?? '');
  if (!m) return 0;
  const v = parseFloat(m[1]), u = (m[2] ?? 'M').toUpperCase();
  return Math.round(u === 'T' ? v * 1e6 : u === 'G' ? v * 1000 : v);
}

/** Colour and thickness of a cable by speed: 10 Mbps to 10 Gbps green, 25 to 100 Gbps blue, above 100 Gbps a thick blue line. */
function bySpeed(speed: string | undefined, th: MTheme, w: number): { c: string; w: number } {
  const v = mbps(speed);
  if (!v || v <= 10000) return { c: th.ok, w };
  if (v <= 100000) return { c: th.acc, w: w + .4 };
  return { c: th.acc, w: w + 3.2 };
}

/** Blocked by spanning tree: the cable still carries the control traffic, so it stays green but dashed; an amber dot sits on the port that blocks. */
function stroke(state: string, ghost: boolean, th: MTheme, w: number, speed?: string, view?: View | 'quiet'): string {
  if (state === 'down') return `stroke="${th.bad}" stroke-width="${w * .9}" stroke-dasharray="3 7" stroke-linecap="round"`;
  if (state === 'blocked') return `stroke="${th.ok}" stroke-width="${w}" stroke-dasharray="9 6" stroke-linecap="round"`;
  if (ghost) return `stroke="${th.mut}" stroke-width="2" stroke-dasharray="4 6" opacity=".75"`;
  if (view === 'quiet') { const m = mbps(speed); return `stroke="${th.mut}" stroke-opacity=".55" stroke-width="${m >= 40000 ? 3.6 : m >= 10000 ? 2.4 : 1.4}" stroke-linecap="round"`; }
  if (view === 'speed') { const k = bySpeed(speed, th, w); return `stroke="${k.c}" stroke-width="${k.w}" stroke-linecap="round"`; }
  return `stroke="${th.ok}" stroke-width="${w}" stroke-linecap="round"`;
}

const stpColor = (st: string | undefined, th: MTheme): string | null =>
  st === 'blocked' ? th.warn : st === 'forwarding' ? th.ok : st === 'learning' ? th.acc : st === 'disabled' ? th.mut : null;

/** The spanning-tree dot on the port at one end of a cable: amber for the port that blocks (in every view); the other states only in the STP view. */
function stpDot(p: P, st: string | undefined, o: MOpts): string {
  const c = stpColor(st, o.theme);
  if (!c || (st !== 'blocked' && o.view !== 'stp')) return '';
  return `<circle cx="${f(p.x)}" cy="${f(p.y)}" r="${st === 'blocked' ? 5.5 : 4.2}" fill="${c}" stroke="${o.theme.ring}" stroke-width="1.5"/>`;
}

const portList = (t: Track, end: 'a' | 'b') => t.links.map(l => end === 'a' ? l.aPort : l.bPort).join(',');

/** The links of the map: cables between each pair side by side, a LAG as a thick track with a ring where the
 *  switch has the LAG and the cables fanning into it (as in the Emulated Lab). */
function linksMarkup(nodes: Map<string, MNode>, links: MLink[], o: MOpts): string {
  const th = o.theme, D = dims(o.look), pairs = new Map<string, MLink[]>();
  for (const l of links) {
    const [x, y] = l.a < l.b ? [l.a, l.b] : [l.b, l.a];
    const k = x + '\u0001' + y;
    (pairs.get(k) ?? pairs.set(k, []).get(k)!).push(l.a < l.b ? l : { ...l, a: l.b, b: l.a, aPort: l.bPort, bPort: l.aPort, lagA: l.lagB, lagB: l.lagA, stpA: l.stpB, stpB: l.stpA });
  }
  // right-angle cables (Tiers look): where each cable leaves the top / bottom edge of a device, spread apart
  const slotOf = new Map<string, number>();
  if (orthLook(o.look)) {
    const by = new Map<string, { k: string; ox: number }[]>();
    const add = (nk: string, e: { k: string; ox: number }) => { const a = by.get(nk); if (a) a.push(e); else by.set(nk, [e]); };
    for (const [k, ls] of pairs) {
      const A = nodes.get(ls[0].a), B = nodes.get(ls[0].b);
      if (!A || !B || Math.abs(B.y - A.y) <= D.h) continue;
      const [top, bot] = A.y < B.y ? [A, B] : [B, A];
      add(top.id + '|b', { k, ox: bot.x }); add(bot.id + '|t', { k, ox: top.x });
    }
    for (const [nk, arr] of by) { arr.sort((p, q) => p.ox - q.ox); arr.forEach((e, i) => slotOf.set(nk + '#' + e.k, (i - (arr.length - 1) / 2) * 30)); }
  }
  let out = '', pi = 0;
  for (const [pk, ls] of pairs) {
    const pidx = pi++;
    const A = nodes.get(ls[0].a), B = nodes.get(ls[0].b);
    if (!A || !B) continue;
    const dx0 = B.x - A.x, dy0 = B.y - A.y, L0 = Math.hypot(dx0, dy0);
    if (L0 < 1) continue;
    const u = { x: dx0 / L0, y: dy0 / L0 }, n = { x: -u.y, y: u.x };
    const tracks = pairTracks(ls), m = tracks.length, ghost = A.ghost || B.ghost;
    tracks.forEach((t, j) => {
      const off = (j - (m - 1) / 2) * (m > 1 ? 44 : 0), stag = (j - (m - 1) / 2) * 34; // tracks apart, mid labels staggered along the line
      const pa = { x: A.x + n.x * off, y: A.y + n.y * off }, pb = { x: B.x + n.x * off, y: B.y + n.y * off };
      const ta = exit(A, pa, u.x, u.y, D.w / 2 + 1, D.h / 2 + 1), tb = exit(B, pb, -u.x, -u.y, D.w / 2 + 1, D.h / 2 + 1);
      const S = { x: pa.x + u.x * ta, y: pa.y + u.y * ta }, E = { x: pb.x - u.x * tb, y: pb.y - u.y * tb };
      const len = Math.hypot(E.x - S.x, E.y - S.y);
      if (len < 24) return;
      const state = trackState(t), sel = t.links.some(l => l.key === o.selLink), keys = t.links.map(l => l.key).join(' ');
      const near = !!o.selNode && (A.id === o.selNode || B.id === o.selNode), vw: View | 'quiet' | undefined = o.look === 'focus' && !near ? 'quiet' : o.view;
      const dim = o.look === 'focus' && !!o.selNode && !near, sp = o.showPorts && (o.look !== 'focus' || near);
      const warn = t.links.find(l => l.warn)?.warn;
      const at = (p: P, d: number, side = 0): P => ({ x: p.x + u.x * d + n.x * side, y: p.y + u.y * d + n.y * side });
      let g = `<g class="mlink${sel ? ' sel' : ''}" data-link="${esc(keys)}"${(o.view === 'lag' && !t.lag) || dim ? ' opacity=".28"' : ''}>`, hit = '';
      const labels: string[] = [];
      const orth = orthLook(o.look) && Math.abs(B.y - A.y) > D.h;
      if (!t.lag && orth) {
        const l = t.links[0], aTop = A.y < B.y, top = aTop ? A : B, bot = aTop ? B : A;
        const tp = aTop ? l.aPort : l.bPort, bp = aTop ? l.bPort : l.aPort, jo = (j - (m - 1) / 2) * 14;
        const x1 = top.x + (slotOf.get(top.id + '|b#' + pk) ?? 0) + jo, x2 = bot.x + (slotOf.get(bot.id + '|t#' + pk) ?? 0) + jo;
        const y1 = top.y + D.h / 2, y2 = bot.y - D.h / 2, ym = y1 + 40 + (pidx % 5) * 14 + j * 10, r = 12;
        let d = `M${f(x1)} ${f(y1)}V${f(ym - r)}`;
        if (Math.abs(x2 - x1) > 2) { const sx = x2 > x1 ? 1 : -1; d += `Q${f(x1)} ${f(ym)} ${f(x1 + sx * r)} ${f(ym)}H${f(x2 - sx * r)}Q${f(x2)} ${f(ym)} ${f(x2)} ${f(ym + r)}`; }
        d += `V${f(y2)}`;
        hit = d;
        if (sel) g += `<path d="${d}" fill="none" stroke="${th.acc}" stroke-width="10" opacity=".28" stroke-linecap="round" stroke-linejoin="round"/>`;
        g += `<path d="${d}" fill="none" ${stroke(state, ghost, th, l.speed === '1G' ? 2.8 : 3.6, l.speed, vw)} stroke-linejoin="round"/>`;
        g += stpDot({ x: x1, y: y1 + 11 }, aTop ? l.stpA : l.stpB, o) + stpDot({ x: x2, y: y2 - 11 }, aTop ? l.stpB : l.stpA, o);
        if (sp || sel) { labels.push(pill(x1 + 2, y1 + 15, tp, th)); labels.push(pill(x2 + 2, y2 - 15, bp, th)); }
        const mx = (x1 + x2) / 2, my = Math.abs(x2 - x1) > 2 ? ym : (y1 + y2) / 2;
        const txt = state === 'blocked' ? 'STP blocked' : state === 'down' ? 'port down' : l.speed ?? '';
        if (txt) labels.push(pill(mx, my, txt, th, state === 'blocked' ? th.warn : state === 'down' ? th.bad : undefined));
      } else if (!t.lag) {
        const l = t.links[0];
        let s0 = S, e0 = E, d = `M${f(S.x)} ${f(S.y)}L${f(E.x)} ${f(E.y)}`, pa = at(S, Math.min(len * .22, 44) + 8), pb = at(E, -(Math.min(len * .22, 44) + 8));
        if (o.look === 'flow') { // columns: the cable leaves the right side of the left device and enters the left side of the right one
          const [L, R] = A.x <= B.x ? [A, B] : [B, A];
          if (Math.abs(A.x - B.x) < 4) { // same column: an arc on the right
            s0 = { x: A.x + D.w / 2, y: A.y }; e0 = { x: B.x + D.w / 2, y: B.y };
            d = `M${f(s0.x)} ${f(s0.y)}C${f(s0.x + 70)} ${f(s0.y)} ${f(e0.x + 70)} ${f(e0.y)} ${f(e0.x)} ${f(e0.y)}`; pa = { x: s0.x + 28, y: s0.y - 11 }; pb = { x: e0.x + 28, y: e0.y + 11 };
          } else {
            s0 = { x: L.x + D.w / 2, y: L.y }; e0 = { x: R.x - D.w / 2, y: R.y };
            const mx = (s0.x + e0.x) / 2; d = `M${f(s0.x)} ${f(s0.y)}C${f(mx)} ${f(s0.y)} ${f(mx)} ${f(e0.y)} ${f(e0.x)} ${f(e0.y)}`;
            pa = { x: s0.x + 30, y: s0.y - 11 }; pb = { x: e0.x - 30, y: e0.y - 11 };
            if (L !== A) [pa, pb] = [pb, pa];
          }
        } else if (o.look === 'material' && len > 40) { // soft curves
          const horiz = Math.abs(E.x - S.x) > Math.abs(E.y - S.y) * 2.5, my = (S.y + E.y) / 2, mx = (S.x + E.x) / 2;
          d = horiz ? `M${f(S.x)} ${f(S.y)}C${f(mx)} ${f(S.y)} ${f(mx)} ${f(E.y)} ${f(E.x)} ${f(E.y)}` : `M${f(S.x)} ${f(S.y)}C${f(S.x)} ${f(my)} ${f(E.x)} ${f(my)} ${f(E.x)} ${f(E.y)}`;
          const sg = Math.sign(E.y - S.y) || 1, sx = Math.sign(E.x - S.x) || 1;
          pa = horiz ? { x: S.x + sx * 30, y: S.y - 11 } : { x: S.x + 2, y: S.y + sg * 16 }; pb = horiz ? { x: E.x - sx * 30, y: E.y - 11 } : { x: E.x + 2, y: E.y - sg * 16 };
        }
        hit = d;
        if (sel) g += `<path d="${d}" fill="none" stroke="${th.acc}" stroke-width="9" opacity=".28" stroke-linecap="round"/>`;
        if (state === 'up' && !ghost) g += `<path d="${d}" fill="none" stroke="${th.ok}" stroke-width="8" opacity=".14" stroke-linecap="round"/>`;
        g += `<path d="${d}" fill="none" ${stroke(state, ghost, th, 3.4, l.speed, vw)}/>`;
        g += stpDot(at(S, 11), l.stpA, o) + stpDot(at(E, -11), l.stpB, o);
        if (state === 'up' && l.speed) { const p = at(S, len / 2 + stag); labels.push(pill(p.x, p.y, l.speed, th)); }
        if (sp || sel) {
          if (l.aPort) labels.push(pill(pa.x, pa.y, l.aPort, th));
          if (l.bPort) labels.push(pill(pb.x, pb.y, l.bPort, th));
        }
        if (state === 'blocked') { const p = at(S, len / 2 + stag); labels.push(pill(p.x, p.y - 12 * Math.sign(n.y || 1), 'STP blocked', th, th.warn)); }
        if (state === 'down') { const p = at(S, len / 2 + stag); labels.push(pill(p.x, p.y - 12 * Math.sign(n.y || 1), 'port down', th, th.bad)); }
      } else {
        const first = t.links[0], lagA = first.lagA, lagB = first.lagB;
        const fan = Math.min(len * .3, 64), s0 = lagA ? at(S, fan) : at(S, fan * .5), e0 = lagB ? at(E, -fan) : at(E, -fan * .5);
        const trunkCol = warn ? th.warn : null;
        const d = `M${f(s0.x)} ${f(s0.y)}L${f(e0.x)} ${f(e0.y)}`;
        hit = `M${f(S.x)} ${f(S.y)}L${f(E.x)} ${f(E.y)}`;
        if (sel) g += `<path d="${d}" fill="none" stroke="${th.acc}" stroke-width="13" opacity=".28" stroke-linecap="round"/>`;
        g += `<path d="${d}" fill="none" ${stroke(state, false, th, 7, first.speed, vw)}${trunkCol && state === 'up' ? ` style="stroke:${trunkCol}"` : o.view === 'lag' && state === 'up' ? ` style="stroke:${th.acc}"` : ''}/>`;
        const side = (end: 'a' | 'b') => {
          const o0 = end === 'a' ? S : E, dir = end === 'a' ? 1 : -1, ring = at(o0, dir * fan), lag = end === 'a' ? lagA : lagB;
          const k = t.links.length;
          if (lag) {
            t.links.forEach((l, i) => { const st = at(o0, 0, (i - (k - 1) / 2) * 6); g += `<path d="M${f(st.x)} ${f(st.y)}L${f(ring.x)} ${f(ring.y)}" fill="none" ${stroke(l.state, false, th, 2)}/>`; });
            g += `<circle cx="${f(ring.x)}" cy="${f(ring.y)}" r="9" fill="${th.ring}" stroke="${warn ? th.warn : th.ink}" stroke-width="2"/>`;
            labels.push(pill(ring.x + n.x * 21, ring.y + n.y * 21, (lag.mode === 'static' ? 'static ' : 'LACP ') + portList(t, end), th, th.ink, warn ? th.warn : th.acc));
          } else { // no LAG on this switch: the cables go on to their own ports
            const from = end === 'a' ? s0 : e0;
            t.links.forEach((l, i) => {
              const so = (i - (k - 1) / 2) * 12, mid = at(o0, dir * fan * .8, so);
              g += `<path d="M${f(o0.x)} ${f(o0.y)}L${f(mid.x)} ${f(mid.y)}L${f(from.x)} ${f(from.y)}" fill="none" ${stroke(l.state, false, th, 2)}/>`;
              if (sp) labels.push(pill(mid.x, mid.y + (so >= 0 ? 14 : -14), end === 'a' ? l.aPort : l.bPort, th));
            });
          }
        };
        side('a'); side('b');
        const mid = at(S, len / 2 + stag), tt = 'LAG ×' + t.links.length + (warn ? ' ⚠' : '');
        labels.push(pill(mid.x - n.x * 15, mid.y - n.y * 15, tt, th, warn ? th.warn : th.ink));
      }
      g += `<path class="hit" d="${hit}" fill="none" stroke="transparent" stroke-width="16"/>` + labels.join('') + '</g>';
      out += g;
    });
  }
  // links of the plan that are not there: a red dashed line between the two devices
  for (const l of o.missing ?? []) {
    const A = nodes.get(l.a), B = nodes.get(l.b);
    if (!A || !B) continue;
    out += `<g class="mlink missing" data-link="${esc(l.key)}"><path d="M${f(A.x)} ${f(A.y)}L${f(B.x)} ${f(B.y)}" stroke="${th.bad}" stroke-width="2" stroke-dasharray="2 6" opacity=".7"/>`
      + pill((A.x + B.x) / 2, (A.y + B.y) / 2 - 12, 'missing: ' + l.aPort + ' ↔ ' + l.bPort, th, th.bad) + '</g>';
  }
  return out;
}

/** The front of a switch: two rows of ports and an uplink pair. */
function panel(x: number, y: number, w: number, h: number, th: MTheme, rows = 2, cols = 6): string {
  let s = `<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="3" fill="${th.face}" stroke="${th.faceLine}" stroke-width=".8"/>`;
  for (let r = 0; r < rows; r++) for (let c = 0; c < cols; c++) s += `<rect x="${f(x + 4 + c * ((w - 8) / cols))}" y="${f(y + 4 + r * ((h - 8) / rows))}" width="${f((w - 8) / cols - 1.6)}" height="${f((h - 8) / rows - 2)}" rx=".8" fill="${th.port}"/>`;
  return s;
}

const isDev = (os?: string) => os === 'exos' || os === 'voss';

/** The small OS tag inside a device box: EXOS and VOSS in purple, any other OS in grey. */
function badge(os: string | undefined, x: number, y: number, th: MTheme, anchor: 'start' | 'end' = 'end'): string {
  if (!os) return '';
  const t = os.toUpperCase(), w = t.length * 6.2 + 11, dev = isDev(os), x0 = anchor === 'end' ? x - w : x;
  return `<rect x="${f(x0)}" y="${f(y)}" width="${f(w)}" height="15" rx="4" fill="${dev ? th.devBg : th.badgeBg}" stroke="${dev ? th.devLine : th.badgeLine}"/>`
    + `<text x="${f(x0 + w / 2)}" y="${f(y + 10.8)}" font-size="9.5" font-weight="700" text-anchor="middle" fill="${dev ? th.devInk : th.badgeInk}">${esc(t)}</text>`;
}

/** Four small bars: the steps of reading a device (connect, identify, neighbours, ports); the next one blinks. */
function stepBars(x: number, y: number, w: number, done: number, th: MTheme): string {
  const bw = (w - 9) / 4;
  return [0, 1, 2, 3].map(i => {
    const fill = done > i ? th.ok : done === i ? th.acc : th.zoneLine;
    return `<rect x="${f(x + i * (bw + 3))}" y="${f(y)}" width="${f(bw)}" height="3" rx="1.5" fill="${fill}">${done === i ? '<animate attributeName="opacity" values="1;.3;1" dur="1.1s" repeatCount="indefinite"/>' : ''}</rect>`;
  }).join('');
}

/** A pulsing outline while a device is being read. */
const pulse = (w: number, h: number, rx: number, th: MTheme) =>
  `<rect x="-2" y="-2" width="${w + 4}" height="${h + 4}" rx="${rx + 2}" fill="none" stroke="${th.acc}" stroke-width="2"><animate attributeName="stroke-opacity" values="1;.2;1" dur="1.4s" repeatCount="indefinite"/></rect>`;

const dotOf = (nd: MNode, th: MTheme) => nd.ghost ? th.mut : nd.prog?.state === 'error' || nd.flag === 'err' ? th.bad : nd.flag === 'warn' ? th.warn : th.ok;
const warnTxt = (nd: MNode) => nd.notes ? `  ⚠ ${nd.notes}` : '';

function nodeAnt(nd: MNode, o: MOpts, D: { w: number; h: number }, sel: boolean): string {
  const th = o.theme, run = nd.prog?.state === 'running', q = nd.prog?.state === 'queued';
  let s = `<rect width="${D.w}" height="${D.h}" rx="9" fill="${th.node}" stroke="${sel || run ? th.acc : th.nodeLine}" stroke-width="${sel ? 1.8 : 1}"${nd.ghost ? ' stroke-dasharray="4 3"' : ''} style="filter:drop-shadow(0 2px 3px ${th.shadow})"/>`;
  s += `<circle cx="15" cy="19" r="3.6" fill="${dotOf(nd, th)}"/>`;
  s += `<text x="26" y="22.5" font-size="12.5" font-weight="700" fill="${nd.ghost ? th.mut : th.ink}"${nd.ghost ? ' font-style="italic"' : ''}>${esc(clip(nd.name, nd.ghost ? 20 : 15))}</text>`;
  if (!nd.ghost) s += badge(nd.os, D.w - 9, 8, th);
  s += `<text x="26" y="40" font-size="10.5" fill="${th.mut}">${esc(clip(nd.ghost ? tr0('neighbour · not managed') : (nd.ip || nd.sub) + warnTxt(nd), 30))}</text>`;
  if (run || q) s += (run ? pulse(D.w, D.h, 9, th) : '') + stepBars(10, D.h - 7, D.w - 20, nd.prog!.done, th);
  return s;
}

function nodeMaterial(nd: MNode, o: MOpts, D: { w: number; h: number }, sel: boolean): string {
  const th = o.theme, run = nd.prog?.state === 'running', dev = isDev(nd.os) && !nd.ghost;
  const ink = dev ? th.devCardInk : th.ink, mut = dev ? th.devCardInk : th.mut;
  let s = `<rect width="${D.w}" height="${D.h}" rx="16" fill="${dev ? th.devCard : th.node}" stroke="${sel ? th.acc : dev ? th.devLine : th.nodeLine}" stroke-opacity="${sel ? 1 : dev ? .55 : 1}" stroke-width="${sel ? 2 : 1}"${nd.ghost ? ' stroke-dasharray="4 3"' : ''} style="filter:drop-shadow(0 2px 4px ${th.shadow})"/>`;
  s += `<text x="14" y="24" font-size="13.5" font-weight="700" fill="${nd.ghost ? th.mut : ink}"${nd.ghost ? ' font-style="italic"' : ''}>${esc(clip(nd.name, 17))}</text>`;
  s += nd.prog?.state === 'done' ? `<circle cx="${D.w - 18}" cy="19" r="9" fill="${th.ok}"/><path d="M${D.w - 22.5} 19l3 3 5.5-6" fill="none" stroke="#0b1b12" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>` : `<circle cx="${D.w - 16}" cy="19" r="4" fill="${dotOf(nd, th)}"/>`;
  s += `<text x="14" y="41" font-size="10.5" font-weight="600" fill="${mut}" opacity=".85">${esc(nd.ghost ? tr0('neighbour · not managed') : (nd.os ?? '').toUpperCase())}</text>`;
  if (!nd.ghost) s += `<text x="14" y="55" font-size="10.5" fill="${mut}" opacity=".7">${esc(clip((nd.ip || '') + warnTxt(nd), 28))}</text>`;
  if (run) s += pulse(D.w, D.h, 16, th) + `<rect x="14" y="${D.h - 8}" width="${D.w - 28}" height="3" rx="1.5" fill="${th.zoneLine}"/><rect x="14" y="${D.h - 8}" width="${(D.w - 28) * .4}" height="3" rx="1.5" fill="${th.acc}"><animate attributeName="x" values="14;${D.w - 14 - (D.w - 28) * .4};14" dur="1.5s" repeatCount="indefinite"/></rect>`;
  return s;
}

/** Focus (mock 1): a round device with a switch face, its name and address below; status dot at the top right. */
function nodeFocus(nd: MNode, o: MOpts, sel: boolean): string {
  const th = o.theme, p = nd.prog, run = p?.state === 'running', near = !o.selNode || sel;
  const op = o.selNode && !sel ? ' opacity=".55"' : '';
  let s = `<g${op}>`;
  if (sel) s += `<circle r="42" fill="none" stroke="${th.acc}" stroke-width="2"/>`;
  if (run) s += `<circle r="42" fill="none" stroke="${th.acc}" stroke-width="2" stroke-dasharray="5 6"/>`;
  s += `<circle r="34" fill="${th.node}" stroke="${nd.ghost ? th.mut : th.nodeLine}" stroke-width="1.4"${nd.ghost ? ' stroke-dasharray="4 4"' : ''}/>`;
  s += `<rect x="-18" y="-9" width="36" height="18" rx="4" fill="none" stroke="${nd.ghost ? th.mut : th.ink}" stroke-width="2"/>`;
  for (let i = 0; i < 5; i++) s += `<rect x="${-12 + i * 5}" y="-4" width="3" height="8" rx="1" fill="${nd.ghost ? th.mut : th.ink}"/>`;
  s += `<circle cx="25" cy="-25" r="5" fill="${dotOf(nd, th)}" stroke="${th.bg}" stroke-width="2"/>`;
  s += `<text y="56" font-size="14" font-weight="700" text-anchor="middle" fill="${nd.ghost ? th.mut : near ? th.ink : th.mut}"${nd.ghost ? ' font-style="italic"' : ''}>${esc(clip(nd.name, 20))}</text>`;
  if (!nd.ghost) s += `<text y="72" font-size="11" text-anchor="middle" fill="${th.mut}">${esc((nd.ip || '') + warnTxt(nd))}</text>`;
  return s + '</g>';
}

/** Constellation: a round device with its name below; the ring around it is the reading progress. */
function nodeRound(nd: MNode, o: MOpts, sel: boolean): string {
  const th = o.theme, p = nd.prog, dev = isDev(nd.os) && !nd.ghost;
  if (nd.ghost) return `<circle r="17" fill="none" stroke="${sel ? th.acc : th.mut}" stroke-width="1.5" stroke-dasharray="3 3"/><text y="33" font-size="11" font-style="italic" text-anchor="middle" fill="${th.mut}">${esc(clip(nd.name, 18))}</text>`;
  let s = `<circle r="33" fill="none" stroke="${p ? th.zoneLine : th.nodeLine}" stroke-width="${p ? 3 : 1.4}"/>`;
  if (p && p.state !== 'queued') s += `<circle r="33" fill="none" stroke="${p.state === 'error' ? th.bad : th.ok}" stroke-width="3" stroke-linecap="round" stroke-dasharray="${f(207 * Math.min(4, p.state === 'done' ? 4 : p.done) / 4)} 208" transform="rotate(-90)"/>`;
  if (p?.state === 'running') s += `<circle r="40" fill="none" stroke="${th.acc}" stroke-width="1.5" stroke-dasharray="4 8"/>`;
  s += `<circle r="26" fill="${dev ? th.devCard : th.node}" stroke="${sel ? th.acc : dev ? th.devLine : th.nodeLine}" stroke-width="${sel ? 2.2 : 1.4}" style="filter:drop-shadow(0 2px 4px ${th.shadow})"/>`;
  s += `<text y="3.5" font-size="9.5" font-weight="800" text-anchor="middle" fill="${dev ? th.devCardInk : th.ink}">${esc((nd.os ?? 'SW').toUpperCase())}</text>`;
  s += `<circle cx="22" cy="-22" r="4.6" fill="${dotOf(nd, th)}" stroke="${th.ring}" stroke-width="1.5"/>`;
  s += `<text y="52" font-size="12" font-weight="700" text-anchor="middle" fill="${th.ink}">${esc(clip(nd.name, 18))}</text>`;
  s += `<text y="66" font-size="10" text-anchor="middle" fill="${th.mut}">${esc((nd.ip || '') + warnTxt(nd))}</text>`;
  return s;
}

function nodeTile(nd: MNode, o: MOpts, D: { w: number; h: number }, sel: boolean): string {
  const th = o.theme, run = nd.prog?.state === 'running', q = nd.prog?.state === 'queued';
  let s = `<rect width="${D.w}" height="${D.h}" rx="11" fill="${th.node}" stroke="${sel || run ? th.acc : th.nodeLine}" stroke-width="${sel ? 1.8 : 1}"${nd.ghost ? ' stroke-dasharray="4 3"' : ''}/>`;
  s += `<circle cx="16" cy="19" r="4" fill="${dotOf(nd, th)}"/>`;
  s += `<text x="28" y="23" font-size="12.5" font-weight="700" fill="${nd.ghost ? th.mut : th.ink}"${nd.ghost ? ' font-style="italic"' : ''}>${esc(clip(nd.name, 17))}</text>`;
  s += `<text x="14" y="${nd.ghost ? 42 : 40}" font-size="10.5" fill="${th.mut}">${esc(clip(nd.ghost ? tr0('not managed') : (nd.ip || nd.sub) + warnTxt(nd), 28))}</text>`;
  if (!nd.ghost) s += badge(nd.os, 14, 49, th, 'start');
  if (run || q) s += (run ? pulse(D.w, D.h, 11, th) : '') + stepBars(12, D.h - 8, D.w - 24, nd.prog!.done, th);
  return s;
}

function nodePill(nd: MNode, o: MOpts, D: { w: number; h: number }, sel: boolean): string {
  const th = o.theme, run = nd.prog?.state === 'running', q = nd.prog?.state === 'queued';
  let s = `<rect width="${D.w}" height="${D.h}" rx="13" fill="${th.node}" stroke="${sel || run ? th.acc : th.nodeLine}" stroke-width="${sel ? 1.8 : 1}"${nd.ghost ? ' stroke-dasharray="4 3"' : ''} style="filter:drop-shadow(0 2px 4px ${th.shadow})"/>`;
  s += run ? `<circle cx="22" cy="24" r="7" fill="none" stroke="${th.zoneLine}" stroke-width="2.4"/><path d="M22 17a7 7 0 0 1 7 7" fill="none" stroke="${th.acc}" stroke-width="2.4" stroke-linecap="round"><animateTransform attributeName="transform" type="rotate" from="0 22 24" to="360 22 24" dur="0.9s" repeatCount="indefinite"/></path>` : `<circle cx="22" cy="24" r="7" fill="${dotOf(nd, th)}" fill-opacity=".18"/><circle cx="22" cy="24" r="3.6" fill="${dotOf(nd, th)}"/>`;
  s += `<text x="40" y="22" font-size="12.5" font-weight="700" fill="${nd.ghost ? th.mut : th.ink}"${nd.ghost ? ' font-style="italic"' : ''}>${esc(clip(nd.name, 16))}</text>`;
  s += `<text x="40" y="37" font-size="10.5" fill="${th.mut}">${esc(clip(nd.ghost ? tr0('not managed') : (nd.ip || nd.sub) + warnTxt(nd), 24))}</text>`;
  if (!nd.ghost) s += badge(nd.os, D.w - 10, 9, th);
  if (run || q) s += stepBars(40, D.h - 7, D.w - 54, nd.prog!.done, th);
  return s;
}

function nodeClassic(nd: MNode, o: MOpts, D: { w: number; h: number }, sel: boolean): string {
  const th = o.theme, sw = nd.kind === 'switch' || nd.kind === 'unknown';
  let s = `<rect width="${D.w}" height="${D.h}" rx="9" fill="${th.node}" stroke="${sel ? th.acc : th.nodeLine}" stroke-width="${sel ? 1.8 : 1}"${nd.ghost ? ' stroke-dasharray="4 3"' : ''} style="filter:drop-shadow(0 3px 4px ${th.shadow})"/>`;
  s += sw ? panel(9, 13, 38, 30, th, 2, 5) : icon(nd.kind, 10, 11, 34, nd.ghost);
  s += `<circle cx="${D.w - 13}" cy="13" r="3.4" fill="${dotOf(nd, th)}"/>`;
  s += `<text x="56" y="24" font-size="12.5" font-weight="700" fill="${nd.ghost ? th.mut : th.ink}"${nd.ghost ? ' font-style="italic"' : ''}>${esc(clip(nd.name, 17))}</text>`;
  const sub = nd.ghost ? tr0('neighbour') : [nd.sub, nd.ip].filter(Boolean).join(' · ');
  s += `<text x="56" y="40" font-size="10" fill="${th.mut}">${esc(clip(sub, 24))}</text>`;
  if (nd.prog?.state === 'running') s += pulse(D.w, D.h, 9, th) + stepBars(10, D.h - 6, D.w - 20, nd.prog.done, th);
  return s;
}

function nodesMarkup(nodes: MNode[], o: MOpts): string {
  const D = dims(o.look), round = o.look === 'constellation' || o.look === 'focus';
  return nodes.map(nd => {
    const sel = nd.id === o.selNode, x = nd.x - D.w / 2, y = nd.y - D.h / 2;
    const at = round ? `translate(${f(nd.x)} ${f(nd.y)})` : `translate(${f(x)} ${f(y)})`;
    let s = `<g class="mnode${sel ? ' sel' : ''}${nd.ghost ? ' ghost' : ''}" data-node="${esc(nd.id)}" transform="${at}"${nd.ghost ? ' opacity=".82"' : ''}>`;
    if (round) s += `<circle r="40" fill="transparent"/>`; // a bigger hit area
    s += o.look === 'ant' ? nodeAnt(nd, o, D, sel) : o.look === 'material' ? nodeMaterial(nd, o, D, sel) : o.look === 'focus' ? nodeFocus(nd, o, sel) : round ? nodeRound(nd, o, sel)
      : o.look === 'zones' ? nodeTile(nd, o, D, sel) : o.look === 'flow' ? nodePill(nd, o, D, sel) : nodeClassic(nd, o, D, sel);
    if (o.fresh?.has(nd.id)) s += round ? `<circle r="45" fill="none" stroke="${o.theme.acc}" stroke-width="1.5" stroke-dasharray="2 3"/>` : `<rect x="-3" y="-3" width="${D.w + 6}" height="${D.h + 6}" rx="11" fill="none" stroke="${o.theme.acc}" stroke-width="1.5" stroke-dasharray="2 3"/>`;
    return s + '</g>';
  }).join('');
}

const tr0 = (s: string) => s;
const clip = (s: string, n: number) => s.length > n ? s.slice(0, n - 1) + '…' : s;

/** Tier guides: a thin line and a small label at the left of each row (nothing is filled behind the devices);
 *  panels (a zone with a height) are rounded frames with their title and count. */
function zonesMarkup(zones: Zone[], o: MOpts): string {
  const th = o.theme;
  return zones.map(z => z.h > 0
    ? `<rect x="${f(z.x)}" y="${f(z.y)}" width="${f(z.w)}" height="${f(z.h)}" rx="16" fill="${th.zone}" stroke="${th.zoneLine}"${z.ghost ? ' stroke-dasharray="5 4"' : ''}/>`
      + `<text x="${f(z.x + 16)}" y="${f(z.y + 25)}" font-size="11" font-weight="800" letter-spacing="1.4" fill="${th.mut}">${esc(z.label)}</text>`
      + `<text x="${f(z.x + z.w - 16)}" y="${f(z.y + 25)}" font-size="11" text-anchor="end" fill="${th.mut}">${esc(z.right ?? '')}</text>`
    : `<line x1="${f(z.x)}" y1="${f(z.y)}" x2="${f(z.x + z.w)}" y2="${f(z.y)}" stroke="${th.zoneLine}" stroke-dasharray="2 6"/>`
      + `<text x="${f(z.x + 4)}" y="${f(z.y - 7)}" font-size="10.5" font-weight="800" letter-spacing="1.6" fill="${th.mut}">${esc(z.label)}${z.right ? '  ·  ' + esc(z.right) : ''}</text>`).join('');
}

/** Constellation backdrop: a thin ring per level, and while the devices are being read a sweep turning round the core. */
function ringsMarkup(o: MOpts): string {
  const th = o.theme, R = o.rings ?? [];
  let s = R.map(r => `<circle r="${f(r)}" fill="none" stroke="${th.zoneLine}" stroke-dasharray="2 7"/>`).join('');
  return s;
}

/** Zones: the cables between two panels are drawn as one bundle with a count badge. */
function bundlesMarkup(nodes: MNode[], links: MLink[], o: MOpts): string {
  const th = o.theme, by = new Map(nodes.map(n => [n.id, n])), pan = new Map((o.zones ?? []).filter(z => z.h > 0 && z.id).map(z => [z.id!, z]));
  const bund = new Map<string, MLink[]>();
  for (const l of links) {
    const ga = by.get(l.a)?.group, gb = by.get(l.b)?.group;
    if (!ga || !gb || ga === gb) continue;
    const k = ga < gb ? ga + '\u0001' + gb : gb + '\u0001' + ga;
    (bund.get(k) ?? bund.set(k, []).get(k)!).push(l);
  }
  let out = '';
  for (const [k, ls] of bund) {
    const [ga, gb] = k.split('\u0001'), A = pan.get(ga), B = pan.get(gb);
    if (!A || !B) continue;
    const lag = new Set(ls.filter(l => l.lagA || l.lagB).map(l => (l.lagA?.name ?? '') + '|' + (l.lagB?.name ?? '') + '|' + l.a + l.b)).size;
    const blocked = ls.filter(l => l.state === 'blocked').length, down = ls.filter(l => l.state === 'down').length, up = ls.length - down;
    const top = Math.max(...ls.map(l => mbps(l.speed)));
    let d: string, mx: number, my: number;
    const ac = A.x + A.w / 2, bc = B.x + B.w / 2;
    if (Math.abs(A.y - B.y) < 40) { // side by side
      const [L, R] = A.x < B.x ? [A, B] : [B, A];
      const y1 = L.y + 34, y2 = R.y + 34;
      d = `M${f(L.x + L.w)} ${f(y1)}H${f((L.x + L.w + R.x) / 2)}V${f(y2)}H${f(R.x)}`; mx = (L.x + L.w + R.x) / 2; my = (y1 + y2) / 2;
    } else {
      const [U, W] = A.y < B.y ? [A, B] : [B, A];
      const cl = (v: number, P: Zone) => Math.min(P.x + P.w - 30, Math.max(P.x + 30, v));
      const x1 = cl(W.x + W.w / 2, U), x2 = cl(U.x + U.w / 2, W), y1 = U.y + U.h, y2 = W.y, ym = y1 + (y2 - y1) / 2;
      d = `M${f(x1)} ${f(y1)}V${f(ym)}H${f(x2)}V${f(y2)}`; mx = (x1 + x2) / 2; my = ym;
    }
    void ac; void bc;
    const allDown = down === ls.length, keys = ls.map(l => l.key).join(' '), sel = o.selLink.split(' ').some(x => ls.some(l => l.key === x));
    const w = o.view === 'speed' ? (top > 100000 ? 6 : 3.4) : 3.4;
    const col = allDown ? th.bad : o.view === 'speed' ? (top > 10000 ? th.acc : th.ok) : o.view === 'lag' && lag ? th.acc : th.ok;
    const label = `${ls.length} ${tr0(ls.length === 1 ? 'link' : 'links')}${lag ? ' · ' + lag + ' LAG' : ''}${top && o.view === 'speed' ? ' · ' + (top >= 1000 ? top / 1000 + 'G' : top + 'M') : ''}${blocked ? ' · ' + blocked + ' ' + tr0('blocked') : ''}${down ? ' · ' + down + ' ' + tr0('down') : ''}`;
    const lw = label.length * 6.1 + 22;
    out += `<g class="mlink${sel ? ' sel' : ''}" data-link="${esc(keys)}"${o.view === 'lag' && !lag ? ' opacity=".35"' : ''}>`
      + (sel ? `<path d="${d}" fill="none" stroke="${th.acc}" stroke-width="11" opacity=".28" stroke-linejoin="round"/>` : '')
      + `<path d="${d}" fill="none" stroke="${col}" stroke-width="${w}" stroke-linejoin="round" stroke-linecap="round"${allDown ? ' stroke-dasharray="3 7"' : up === 0 ? '' : blocked === ls.length ? ' stroke-dasharray="9 6"' : ''}/>`
      + `<path class="hit" d="${d}" fill="none" stroke="transparent" stroke-width="18"/>`
      + `<rect x="${f(mx - lw / 2)}" y="${f(my - 11)}" width="${f(lw)}" height="22" rx="11" fill="${th.label}" stroke="${th.labelLine}"/>`
      + (blocked ? `<circle cx="${f(mx - lw / 2 + 11)}" cy="${f(my)}" r="4.5" fill="${th.warn}"/>` : '')
      + `<text x="${f(mx + (blocked ? 6 : 0))}" y="${f(my + 4)}" font-size="11" font-weight="600" text-anchor="middle" fill="${th.ink}">${esc(label)}</text></g>`;
  }
  return out;
}

/** The picture's content (defs and shapes) for the given positions; no outer <svg>. */
export function mapMarkup(nodes: MNode[], links: MLink[], o: MOpts): string {
  const by = new Map(nodes.map(n => [n.id, n]));
  const rings = o.look === 'constellation' ? ringsMarkup(o) : '';
  const lk = o.look === 'zones' ? bundlesMarkup(nodes, links, o) : linksMarkup(by, links, o);
  return defs([...new Set(nodes.map(n => n.kind))]) + rings + zonesMarkup(o.zones ?? [], o) + lk + nodesMarkup(nodes, o);
}

/** The box around all the nodes (and zones), with a margin. */
export function mapBounds(nodes: { x: number; y: number }[], margin = 60, look: Look = 'ant', zones: Zone[] = []): { x: number; y: number; w: number; h: number } {
  if (!nodes.length) return { x: 0, y: 0, w: 800, h: 500 };
  const D = dims(look);
  let x0 = Math.min(...nodes.map(n => n.x)) - D.w / 2 - margin, y0 = Math.min(...nodes.map(n => n.y)) - D.h / 2 - margin;
  let x1 = Math.max(...nodes.map(n => n.x)) + D.w / 2 + margin, y1 = Math.max(...nodes.map(n => n.y)) + D.h / 2 + margin;
  for (const z of zones) { x0 = Math.min(x0, z.x - 20); y0 = Math.min(y0, z.y - 20); x1 = Math.max(x1, z.x + z.w + 20); y1 = Math.max(y1, z.y + z.h + 20); }
  return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 };
}

/** A whole standalone SVG document of the map (for the exports). */
export function mapDocument(nodes: MNode[], links: MLink[], o: MOpts, title: string): { svg: string; w: number; h: number } {
  const b = mapBounds(nodes, 60, o.look, o.zones), head = 38;
  const w = Math.round(b.w), h = Math.round(b.h + head), th = o.theme;
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="${f(b.x)} ${f(b.y - head)} ${w} ${h}" font-family="Inter,'Segoe UI',-apple-system,Helvetica,Arial,sans-serif">`
    + `<rect x="${f(b.x)}" y="${f(b.y - head)}" width="${w}" height="${h}" fill="${th.bg}"/>`
    + `<text x="${f(b.x + 18)}" y="${f(b.y - head + 26)}" font-size="15" font-weight="700" fill="${th.ink}">${esc(title)}</text>`
    + mapMarkup(nodes, links, o) + '</svg>';
  return { svg, w, h };
}
