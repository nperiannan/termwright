/** Layouts of the Network Map that are worked out from the links alone (no positions are stored): the
 *  Constellation (rings), Flow (columns) and Zones (role panels) looks. Pure functions, no Angular or DOM. */

export interface LNode { id: string; name: string; ghost: boolean; }
export interface LEdge { a: string; b: string; }
export interface Placed { id: string; x: number; y: number; group?: string; }
export interface Panel { id: string; x: number; y: number; w: number; h: number; label: string; right: string; ghost?: boolean; }
export interface Laid { nodes: Placed[]; panels: Panel[]; levels: Map<string, number>; levelCount: number; err?: string; }

/** The level of every device: 0 = the best connected devices (the core), +1 for each hop away. Neighbours
 *  without a session ("ghosts") get level -1 and are placed apart. Devices with no link at all are put one level
 *  below the deepest one. */
export function levelsOf(nodes: LNode[], edges: LEdge[]): { lvl: Map<string, number>; count: number } {
  const real = nodes.filter(n => !n.ghost), ids = new Set(real.map(n => n.id));
  const adj = new Map<string, Set<string>>();
  for (const n of real) adj.set(n.id, new Set());
  for (const e of edges) if (ids.has(e.a) && ids.has(e.b) && e.a !== e.b) { adj.get(e.a)!.add(e.b); adj.get(e.b)!.add(e.a); }
  const deg = (id: string) => adj.get(id)!.size;
  const max = Math.max(0, ...real.map(n => deg(n.id)));
  const lvl = new Map<string, number>();
  let queue = real.filter(n => deg(n.id) >= Math.max(1, Math.ceil(max * .9))).map(n => n.id);
  if (!queue.length) queue = real.map(n => n.id).slice(0, 1);
  for (const id of queue) lvl.set(id, 0);
  while (queue.length) {
    const next: string[] = [];
    for (const id of queue) for (const m of adj.get(id) ?? []) if (!lvl.has(m)) { lvl.set(m, lvl.get(id)! + 1); next.push(m); }
    queue = next;
  }
  let deepest = Math.max(-1, ...lvl.values());
  for (const n of real) if (!lvl.has(n.id)) lvl.set(n.id, deepest + 1);
  deepest = Math.max(-1, ...lvl.values());
  for (const n of nodes) if (n.ghost) lvl.set(n.id, -1);
  return { lvl, count: deepest + 1 };
}

/** What a level is called, from how many levels there are. */
export const levelName = (i: number, count: number): string =>
  i < 0 ? 'NEIGHBOURS' : count === 1 ? 'DEVICES' : i === 0 ? 'CORE' : i === count - 1 ? 'ACCESS' : count === 3 ? 'DISTRIBUTION' : 'TIER ' + (i + 1);

interface Ctx { byLevel: Map<number, LNode[]>; nb: Map<string, string[]>; lvl: Map<string, number>; count: number; nodes: LNode[]; }

function context(nodes: LNode[], edges: LEdge[]): Ctx {
  const { lvl, count } = levelsOf(nodes, edges);
  const nb = new Map<string, string[]>();
  for (const n of nodes) nb.set(n.id, []);
  for (const e of edges) { nb.get(e.a)?.push(e.b); nb.get(e.b)?.push(e.a); }
  const byLevel = new Map<number, LNode[]>();
  for (const n of nodes) { const l = lvl.get(n.id)!; (byLevel.get(l) ?? byLevel.set(l, []).get(l)!).push(n); }
  // order inside a level by where its neighbours in the level above are (fewer crossings)
  const order = new Map<string, number>();
  for (let l = -1; l < count; l++) {
    const list = byLevel.get(l) ?? [];
    if (l <= 0) list.sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }));
    else list.sort((a, b) => {
      const bary = (n: LNode) => { const p = (nb.get(n.id) ?? []).filter(m => lvl.get(m) === l - 1).map(m => order.get(m) ?? 0); return p.length ? p.reduce((s, v) => s + v, 0) / p.length : 1e6; };
      return bary(a) - bary(b) || a.name.localeCompare(b.name, undefined, { numeric: true });
    });
    list.forEach((n, i) => order.set(n.id, i));
  }
  return { byLevel, nb, lvl, count, nodes };
}

/** Flow: one column per level, left to right; a device sits level with the middle of its neighbours below it. */
export function flowLayout(nodes: LNode[], edges: LEdge[], w: number, h: number, colGap = 150): Laid {
  const c = context(nodes, edges), pitch = h + 26, pos = new Map<string, number>();
  const deepest = c.count - 1;
  for (let l = deepest; l >= 0; l--) {
    const list = c.byLevel.get(l) ?? [];
    list.forEach((n, i) => {
      const kids = (c.nb.get(n.id) ?? []).filter(m => c.lvl.get(m) === l + 1 && pos.has(m)).map(m => pos.get(m)!);
      pos.set(n.id, kids.length ? kids.reduce((s, v) => s + v, 0) / kids.length : i * pitch);
    });
    const sorted = [...list].sort((a, b) => pos.get(a.id)! - pos.get(b.id)!);
    for (let i = 1; i < sorted.length; i++) { const lo = pos.get(sorted[i - 1].id)! + pitch; if (pos.get(sorted[i].id)! < lo) pos.set(sorted[i].id, lo); }
  }
  const ghosts = c.byLevel.get(-1) ?? [];
  for (const g of ghosts) { const m = (c.nb.get(g.id) ?? []).find(x => pos.has(x)); pos.set(g.id, m ? pos.get(m)! : 0); }
  const gs = [...ghosts].sort((a, b) => pos.get(a.id)! - pos.get(b.id)!);
  for (let i = 1; i < gs.length; i++) { const lo = pos.get(gs[i - 1].id)! + pitch; if (pos.get(gs[i].id)! < lo) pos.set(gs[i].id, lo); }
  const out: Placed[] = [], colOf = (l: number) => (l < 0 ? c.count : l) * (w + colGap);
  const all = nodes.map(n => ({ id: n.id, x: colOf(c.lvl.get(n.id)!), y: pos.get(n.id) ?? 0 }));
  const mid = all.length ? (Math.min(...all.map(p => p.y)) + Math.max(...all.map(p => p.y))) / 2 : 0;
  for (const p of all) out.push({ id: p.id, x: p.x, y: p.y - mid });
  const panels: Panel[] = [];
  for (let l = -1; l < c.count; l++) {
    const list = c.byLevel.get(l) ?? [];
    if (!list.length) continue;
    const ys = list.map(n => out.find(p => p.id === n.id)!.y);
    panels.push({ id: 'col' + l, x: colOf(l) - w / 2, y: Math.min(...ys) - h / 2 - 34, w, h: 0, label: levelName(l, c.count), right: String(list.length) });
  }
  const top = panels.length ? Math.min(...panels.map(p => p.y)) : 0;
  for (const p of panels) p.y = top; // the column headings share one line
  return { nodes: out, panels, levels: c.lvl, levelCount: c.count };
}

/** Constellation: the core in the middle, each level on its own ring, a device at the angle of its neighbours
 *  in the ring inside; neighbours without a session on an outer ring. */
export function radialLayout(nodes: LNode[], edges: LEdge[], size: number): Laid {
  const c = context(nodes, edges), ang = new Map<string, number>(), rad = new Map<string, number>();
  let r = 0;
  for (let l = 0; l < c.count; l++) {
    const list = c.byLevel.get(l) ?? [];
    if (!list.length) continue;
    if (l === 0) {
      if (list.length === 1) { ang.set(list[0].id, 0); rad.set(list[0].id, 0); r = 0; }
      else { r = Math.max(size * .95, (list.length * size * 1.4) / (2 * Math.PI)); list.forEach((n, i) => { ang.set(n.id, -Math.PI / 2 + (i / list.length) * 2 * Math.PI); rad.set(n.id, r); }); }
      continue;
    }
    r = Math.max(r + size * 2.5, (list.length * size * 1.5) / (2 * Math.PI));
    const want = list.map((n, i) => {
      const ps = (c.nb.get(n.id) ?? []).filter(m => c.lvl.get(m) === l - 1 && ang.has(m)).map(m => ang.get(m)!);
      if (!ps.length) return { n, a: (i / list.length) * 2 * Math.PI - Math.PI / 2 };
      const x = ps.reduce((s, a) => s + Math.cos(a), 0), y = ps.reduce((s, a) => s + Math.sin(a), 0);
      return { n, a: Math.atan2(y, x) };
    }).sort((p, q) => p.a - q.a);
    // evenly round the ring, in the order of the neighbours inside, turned to sit as close to them as it can
    const step = (2 * Math.PI) / want.length;
    let sx = 0, sy = 0;
    want.forEach((e, i) => { sx += Math.sin(e.a - i * step); sy += Math.cos(e.a - i * step); });
    const base = sx || sy ? Math.atan2(sx, sy) : -Math.PI / 2;
    want.forEach((e, i) => { e.a = base + i * step; });
    for (const e of want) { ang.set(e.n.id, e.a); rad.set(e.n.id, r); }
  }
  const out: Placed[] = [];
  for (const n of nodes) {
    const l = c.lvl.get(n.id)!;
    if (l >= 0) { const a = ang.get(n.id) ?? 0, q = rad.get(n.id) ?? 0; out.push({ id: n.id, x: Math.cos(a) * q, y: Math.sin(a) * q }); continue; }
    const m = (c.nb.get(n.id) ?? []).find(x => ang.has(x));
    const a = m ? ang.get(m)! : 0, q = (m ? rad.get(m)! : r) + size * 1.8;
    out.push({ id: n.id, x: Math.cos(a) * q, y: Math.sin(a) * q });
  }
  // a ghost on top of another one: nudge along the ring
  const seen: Placed[] = [];
  for (const p of out.filter(p => c.lvl.get(p.id)! < 0)) { for (let k = 0; k < 400 && seen.some(s => Math.hypot(s.x - p.x, s.y - p.y) < size * 1.2); k++) { p.x += size * .6; p.y += size * .3; } seen.push(p); }
  return { nodes: out, panels: [], levels: c.lvl, levelCount: c.count };
}

/** Zones: a panel per role, devices as tiles in it; the access level is split by the device it hangs from. */
export function zonesLayout(nodes: LNode[], edges: LEdge[], tw: number, th: number): Laid {
  const c = context(nodes, edges), gap = 14, padX = 16, padTop = 40, padBot = 16, panelGap = 56, perRow = 4;
  interface Grp { id: string; label: string; right: string; list: LNode[]; ghost?: boolean; level: number; }
  const groups: Grp[] = [];
  const nameOf = new Map(nodes.map(n => [n.id, n.name]));
  for (let l = 0; l < c.count; l++) {
    const list = c.byLevel.get(l) ?? [];
    if (!list.length) continue;
    if (l === c.count - 1 && c.count > 1 && list.length > perRow) {
      const by = new Map<string, LNode[]>();
      for (const n of list) {
        const p = (c.nb.get(n.id) ?? []).filter(m => c.lvl.get(m) === l - 1).sort((a, b) => (nameOf.get(a) ?? '').localeCompare(nameOf.get(b) ?? '', undefined, { numeric: true }))[0] ?? '';
        (by.get(p) ?? by.set(p, []).get(p)!).push(n);
      }
      for (const [p, ns] of [...by].sort((a, b) => (nameOf.get(a[0]) ?? '').localeCompare(nameOf.get(b[0]) ?? '', undefined, { numeric: true }))) groups.push({ id: `g${l}-${p}`, label: levelName(l, c.count) + (p ? ' · ' + (nameOf.get(p) ?? '') : ''), right: String(ns.length), list: ns, level: l });
    } else groups.push({ id: 'g' + l, label: levelName(l, c.count), right: String(list.length), list, level: l });
  }
  const gh = c.byLevel.get(-1) ?? [];
  if (gh.length) groups.push({ id: 'g-1', label: levelName(-1, c.count), right: String(gh.length), list: gh, ghost: true, level: -1 });
  const size = (g: Grp) => { const cols = Math.min(perRow, g.list.length), rows = Math.ceil(g.list.length / perRow); return { w: padX * 2 + cols * tw + (cols - 1) * gap, h: padTop + padBot + rows * th + (rows - 1) * gap }; };
  // rows of panels: one per level; the access panels of one level (and the neighbours) side by side, wrapped at a width
  const maxW = Math.max(1500, tw * perRow * 3);
  const lines: Grp[][] = [];
  for (const g of groups.filter(g => !g.ghost)) {
    const last = lines[lines.length - 1];
    if (last && last[0].level === g.level && last.reduce((s, q) => s + size(q).w + panelGap, 0) + size(g).w <= maxW) last.push(g); else lines.push([g]);
  }
  const ghostG = groups.find(g => g.ghost);
  const panels: Panel[] = [], out: Placed[] = [];
  let y = 0;
  const widths = lines.map(l => l.reduce((s, g) => s + size(g).w, 0) + (l.length - 1) * panelGap);
  const total = Math.max(0, ...widths);
  lines.forEach((line, li) => {
    let x = -widths[li] / 2, rowH = 0;
    for (const g of line) {
      const s = size(g);
      panels.push({ id: g.id, x, y, w: s.w, h: s.h, label: g.label, right: g.right });
      g.list.forEach((n, i) => out.push({ id: n.id, x: x + padX + (i % perRow) * (tw + gap) + tw / 2, y: y + padTop + Math.floor(i / perRow) * (th + gap) + th / 2, group: g.id }));
      x += s.w + panelGap; rowH = Math.max(rowH, s.h);
    }
    y += rowH + panelGap + 20;
  });
  if (ghostG) {
    const s = size(ghostG), x = total / 2 + panelGap;
    panels.push({ id: ghostG.id, x, y: 0, w: s.w, h: s.h, label: ghostG.label, right: ghostG.right, ghost: true });
    ghostG.list.forEach((n, i) => out.push({ id: n.id, x: x + padX + (i % perRow) * (tw + gap) + tw / 2, y: padTop + Math.floor(i / perRow) * (th + gap) + th / 2, group: ghostG.id }));
  }
  return { nodes: out, panels, levels: c.lvl, levelCount: c.count };
}
