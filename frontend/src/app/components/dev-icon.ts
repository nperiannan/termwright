import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';

/** What a device is drawn as. */
export type DevRole = 'access' | 'core' | 'router' | 'fw' | 'ap' | 'server' | 'host' | 'cloud';

export const ROLE_COLOR: Record<DevRole, string> = { access: '#3b8cf6', core: '#2f6fe0', router: '#16a39a', fw: '#e0603a', ap: '#8b5cf6', server: '#64748b', host: '#475569', cloud: '#0ea5e9' };
export const ROLES: { k: DevRole; name: string }[] = [
  { k: 'access', name: 'Access switch' }, { k: 'core', name: 'Core switch' }, { k: 'router', name: 'Router' }, { k: 'fw', name: 'Firewall' },
  { k: 'ap', name: 'Wireless AP' }, { k: 'server', name: 'Server' }, { k: 'host', name: 'KVM host' }, { k: 'cloud', name: 'Cloud' },
];

/** A role from what is known of a device: its model, OS and image name. */
export function roleOf(model: string, os: string, image = ''): DevRole {
  const t = `${model} ${os} ${image}`.toLowerCase();
  if (/firewall|fortigate|pfsense|opnsense|\basa\b|palo|fw\b|srx/.test(t)) return 'fw';
  if (/router|vyos|csr|\bxrv|\bmx\b|routeros|mikrotik|frr|isr/.test(t)) return 'router';
  if (/\bap\d|\bap\b|access point|wireless|wifi/.test(t)) return 'ap';
  if (/linux|ubuntu|debian|centos|rocky|alpine|windows|server/.test(t)) return 'server';
  if (/\b(57\d\d|7[0-9]{3}|8[0-9]{3}|9[0-9]{3})|core|spine|chassis/.test(t)) return 'core';
  return 'access';
}

const G = {
  access: 'M4 8h12M13 5l3 3-3 3M20 16H8M11 13l-3 3 3 3',
  core: 'M4 6h12M13 3.5l3 2.5-3 2.5M20 12H8M11 9.5L8 12l3 2.5M4 18h12M13 15.5l3 2.5-3 2.5',
  router: 'M12 2.5v6M9.5 6L12 8.5 14.5 6M12 21.5v-6M9.5 18l2.5-2.5 2.5 2.5M2.5 12h6M6 9.5L8.5 12 6 14.5M21.5 12h-6M18 9.5L15.5 12l2.5 2.5',
  shield: 'M12 2.8l7.5 3v5.6c0 4.6-3.1 8.3-7.5 9.8-4.4-1.5-7.5-5.2-7.5-9.8V5.8zM8.6 12l2.4 2.4 4.6-4.8',
  cloud: 'M7 18.5h10.5a4 4 0 0 0 .3-8 6 6 0 0 0-11.5-1.3A4.7 4.7 0 0 0 7 18.5z',
};
const CS = 0.866;
type P3 = (x: number, y: number, z: number) => [number, number];
const f1 = (n: number) => n.toFixed(1);

/** Isometric drawings of network devices (Termwright's own, Apache-2.0): a shape per role. */
function draw(role: DevRole, c: string, lit: boolean): string {
  const LED = lit ? 'var(--ic-led)' : 'var(--ic-off)';
  const frame = (W: number, D: number, H: number, oy = 0): P3 => { const cx = 65 - (W - D) * CS / 2, cy = 56 + oy - ((W + D) * 0.5 - H) / 2; return (x, y, z) => [cx + (x - y) * CS, cy + (x + y) * 0.5 - z]; };
  const pts = (P: P3, a: [number, number, number][]) => a.map(p => P(...p).map(f1).join(',')).join(' ');
  const box = (P: P3, x: number, y: number, z: number, w: number, d: number, h: number, col: { top?: string; fr?: string; sd?: string } = {}) =>
    `<polygon points="${pts(P, [[x + w, y, z], [x + w, y + d, z], [x + w, y + d, z + h], [x + w, y, z + h]])}" fill="${col.sd ?? 'var(--ic-sd)'}" stroke="var(--ic-ed)" stroke-width=".8" stroke-linejoin="round"/>` +
    `<polygon points="${pts(P, [[x, y + d, z], [x + w, y + d, z], [x + w, y + d, z + h], [x, y + d, z + h]])}" fill="${col.fr ?? 'var(--ic-fr)'}" stroke="var(--ic-ed)" stroke-width=".8" stroke-linejoin="round"/>` +
    `<polygon points="${pts(P, [[x, y, z + h], [x + w, y, z + h], [x + w, y + d, z + h], [x, y + d, z + h]])}" fill="${col.top ?? 'var(--ic-top)'}" stroke="var(--ic-ed)" stroke-width=".8" stroke-linejoin="round"/>`;
  const fq = (P: P3, Y: number, x0: number, z0: number, x1: number, z1: number, f: string, cl = '') => `<polygon class="${cl}" points="${pts(P, [[x0, Y, z0], [x1, Y, z0], [x1, Y, z1], [x0, Y, z1]])}" ${f}/>`;
  const rq = (P: P3, X: number, y0: number, z0: number, y1: number, z1: number, f: string) => `<polygon points="${pts(P, [[X, y0, z0], [X, y1, z0], [X, y1, z1], [X, y0, z1]])}" ${f}/>`;
  const glyph = (P: P3, gx: number, gy: number, z: number, sz: number, g: string, col: string, sw = 2.2) => {
    const s = sz / 24, [ex, ey] = P(gx - sz / 2, gy - sz / 2, z);
    return `<g transform="matrix(${(s * CS).toFixed(3)} ${(s * 0.5).toFixed(3)} ${(-s * CS).toFixed(3)} ${(s * 0.5).toFixed(3)} ${f1(ex)} ${f1(ey)})" fill="none" stroke="${col}" stroke-width="${sw}" stroke-linecap="round" stroke-linejoin="round"><path d="${g}"/></g>`;
  };
  const shadow = (P: P3, W: number, D: number) => { const [x, y] = P(W / 2, D / 2, 0); return `<ellipse cx="${f1(x)}" cy="${f1(y + 3)}" rx="${f1((W + D) * 0.55)}" ry="${f1((W + D) * 0.2)}" fill="#000" opacity=".3" filter="url(#icsh)"/>`; };
  const on = (i: number) => (i * 7) % 5 !== 0;
  const led = (i: number) => `fill="${on(i) ? LED : 'var(--ic-off)'}"`;
  const ears = (P: P3, d: number, w: number, h: number) => fq(P, d, -5, 0, 0, h, 'fill="var(--ic-ear)" stroke="var(--ic-ed)" stroke-width=".6"') + fq(P, d, w, 0, w + 5, h, 'fill="var(--ic-ear)" stroke="var(--ic-ed)" stroke-width=".6"');
  switch (role) {
    case 'access': {
      const W = 72, D = 38, H = 11, P = frame(W, D, H); let s = shadow(P, W, D) + ears(P, D, W, H) + box(P, 0, 0, 0, W, D, H);
      s += rq(P, W, D * 0.1, H * 0.25, D * 0.9, H * 0.45, `fill="${c}"`) + fq(P, D, 3, H - 3.5, 6, H - 6, `fill="${c}"`);
      for (let r = 0; r < 2; r++) for (let i = 0; i < 12; i++) s += fq(P, D, 9 + i * 4.1, H - 2.5 - r * 4, 9 + i * 4.1 + 3, H - 5 - r * 4, led(i + r), 'pl');
      for (let i = 0; i < 4; i++) s += fq(P, D, 60 + (i % 2) * 4.6, H - 2.5 - (i >> 1) * 4, 60 + (i % 2) * 4.6 + 3.6, H - 5 - (i >> 1) * 4, 'fill="var(--ic-sfp)" stroke="var(--ic-ed)" stroke-width=".3"');
      return s + glyph(P, W * 0.45, D * 0.5, H, 17, G.access, c);
    }
    case 'core': {
      const W = 58, D = 44, H = 44, P = frame(W, D, H, 4); let s = shadow(P, W, D) + ears(P, D, W, H) + box(P, 0, 0, 0, W, D, H);
      s += fq(P, D, 2, 1.5, W - 2, 7, 'fill="var(--ic-sd)" stroke="var(--ic-ed)" stroke-width=".5"');
      for (let i = 0; i < 9; i++) s += fq(P, D, 5 + i * 5.6, 3, 6.2 + i * 5.6, 5.5, 'fill="var(--ic-ed)"');
      for (let i = 0; i < 8; i++) {
        const x0 = 3 + i * 6.6, sup = i === 3 || i === 4;
        s += fq(P, D, x0, 9, x0 + 5.6, H - 3, `fill="${sup ? 'var(--ic-top)' : 'var(--ic-sd)'}" stroke="var(--ic-ed)" stroke-width=".5"`);
        if (sup) s += fq(P, D, x0 + 1.5, H - 6, x0 + 4, H - 8.5, `fill="${c}"`);
        else for (let r = 0; r < 6; r++) s += fq(P, D, x0 + 1.6, H - 6 - r * 4.8, x0 + 4, H - 8 - r * 4.8, led(i + r), 'pl');
      }
      for (let i = 0; i < 6; i++) s += rq(P, W, 6, 10 + i * 5, D - 6, 11 + i * 5, 'fill="var(--ic-fr)"');
      s += rq(P, W, D * 0.1, H - 6, D * 0.9, H - 9, `fill="${c}"`);
      return s + glyph(P, W * 0.5, D * 0.5, H, 22, G.core, c);
    }
    case 'router': {
      const r = 22, h = 17, P = frame(2 * r, 2 * r, h, 2), [bx, by] = P(r, r, 0), [tx, ty] = P(r, r, h), rx = 1.2247 * r, ry = 0.7071 * r;
      let s = `<ellipse cx="${f1(bx)}" cy="${f1(by + 3)}" rx="${f1(rx * 1.05)}" ry="${f1(ry * 1.1)}" fill="#000" opacity=".3" filter="url(#icsh)"/>`;
      s += `<path d="M${f1(tx - rx)} ${f1(ty)}L${f1(bx - rx)} ${f1(by)}A${f1(rx)} ${f1(ry)} 0 0 0 ${f1(bx + rx)} ${f1(by)}L${f1(tx + rx)} ${f1(ty)}Z" fill="var(--ic-fr)" stroke="var(--ic-ed)" stroke-width=".8"/>`;
      s += `<path d="M${f1(bx - rx)} ${f1(by - h * 0.62)}A${f1(rx)} ${f1(ry)} 0 0 0 ${f1(bx + rx)} ${f1(by - h * 0.62)}" fill="none" stroke="${c}" stroke-width="2.4"/>`;
      for (let i = 0; i < 6; i++) { const th = (15 + i * 12) * Math.PI / 180, px = bx + (Math.cos(th) - Math.sin(th)) * r * CS, py = by + (Math.cos(th) + Math.sin(th)) * r * 0.5 - h * 0.3; s += `<rect class="pl" x="${f1(px - 2)}" y="${f1(py - 1.5)}" width="4" height="3" rx=".6" ${led(i + 1)}/>`; }
      s += `<ellipse cx="${f1(tx)}" cy="${f1(ty)}" rx="${f1(rx)}" ry="${f1(ry)}" fill="var(--ic-top)" stroke="var(--ic-ed)" stroke-width=".8"/>`;
      return s + glyph(P, r, r, h, 26, G.router, c, 2.3);
    }
    case 'fw': {
      const W = 60, D = 40, H = 18, P = frame(W, D, H); let s = shadow(P, W, D) + ears(P, D, W, H) + box(P, 0, 0, 0, W, D, H);
      for (let r = 0; r < 3; r++) for (let i = 0; i < 5; i++) { const x0 = 3 + (r % 2 ? 3 : 0) + i * 6.2; if (x0 + 5.4 > 36) continue; s += fq(P, D, x0, H - 2 - r * 5, x0 + 5.4, H - 6 - r * 5, `fill="${c}" opacity="${0.95 - r * 0.12}"`); }
      for (let r = 0; r < 2; r++) for (let i = 0; i < 4; i++) s += fq(P, D, 38 + i * 4.8, H - 4 - r * 6, 38 + i * 4.8 + 3.4, H - 7 - r * 6, led(i + r + 2), 'pl');
      s += rq(P, W, D * 0.1, H * 0.25, D * 0.9, H * 0.42, `fill="${c}"`);
      return s + glyph(P, W * 0.48, D * 0.5, H, 19, G.shield, c);
    }
    case 'ap': {
      const r = 22, P = frame(2 * r, 2 * r, 10, -2), [bx, by] = P(r, r, 0), rx = 1.2247 * r, ry = 0.7071 * r;
      let s = `<ellipse cx="${f1(bx)}" cy="${f1(by + 3)}" rx="${f1(rx)}" ry="${f1(ry)}" fill="#000" opacity=".3" filter="url(#icsh)"/>`;
      s += `<path d="M${f1(bx - rx)} ${f1(by)}A${f1(rx)} ${f1(ry * 0.95)} 0 0 1 ${f1(bx + rx)} ${f1(by)}A${f1(rx)} ${f1(ry)} 0 0 1 ${f1(bx - rx)} ${f1(by)}Z" fill="var(--ic-sd)" stroke="var(--ic-ed)" stroke-width=".8"/>`;
      s += `<path d="M${f1(bx - rx * 0.92)} ${f1(by - 2)}C${f1(bx - rx * 0.9)} ${f1(by - ry * 1.5)},${f1(bx + rx * 0.9)} ${f1(by - ry * 1.5)},${f1(bx + rx * 0.92)} ${f1(by - 2)}A${f1(rx * 0.92)} ${f1(ry * 0.9)} 0 0 1 ${f1(bx - rx * 0.92)} ${f1(by - 2)}Z" fill="var(--ic-top)" stroke="var(--ic-ed)" stroke-width=".8"/>`;
      s += `<ellipse class="pl" cx="${f1(bx)}" cy="${f1(by - ry * 0.72)}" rx="5" ry="2.6" fill="${lit ? c : 'var(--ic-off)'}"/>`;
      s += `<g transform="translate(${f1(bx - 14)} ${f1(by - ry * 1.9 - 14)}) scale(1.17)" fill="none" stroke="${c}" stroke-width="1.6" stroke-linecap="round"><path d="M8.6 14a4.8 4.8 0 0 1 6.8 0M5.7 11a9 9 0 0 1 12.6 0M2.8 8a13 13 0 0 1 18.4 0"/></g>`;
      return s;
    }
    case 'server': {
      const W = 54, D = 48, H = 21, P = frame(W, D, H); let s = shadow(P, W, D) + ears(P, D, W, H) + box(P, 0, 0, 0, W, D, H);
      for (let r = 0; r < 2; r++) for (let i = 0; i < 8; i++) { const x0 = 3 + i * 5.2; s += fq(P, D, x0, H - 2 - r * 9, x0 + 4.4, H - 9.5 - r * 9, 'fill="var(--ic-sd)" stroke="var(--ic-ed)" stroke-width=".4"') + fq(P, D, x0 + 1.3, H - 3.2 - r * 9, x0 + 3, H - 4.4 - r * 9, led(i + r), 'pl'); }
      s += fq(P, D, 46, H - 3, 51, H - 5, `fill="${c}"`) + fq(P, D, 46, H - 7, 51, H - 9, `fill="${LED}"`, 'pl');
      for (let i = 0; i < 5; i++) s += `<polyline points="${pts(P, [[8 + i * 8, 6, H], [8 + i * 8, D - 6, H]])}" stroke="var(--ic-ed)" stroke-width=".7"/>`;
      return s;
    }
    case 'host': {
      const W = 58, D = 46, H = 15, P = frame(W, D, H + 16, 4); let s = shadow(P, W, D) + ears(P, D, W, H) + box(P, 0, 0, 0, W, D, H);
      for (let i = 0; i < 8; i++) s += fq(P, D, 3 + i * 5.2, H - 2, 3 + i * 5.2 + 4.4, H - 7, 'fill="var(--ic-sd)" stroke="var(--ic-ed)" stroke-width=".4"');
      s += fq(P, D, 47, H - 3, 53, H - 5, `fill="${LED}"`, 'pl');
      for (const [col, x, y] of [['#3b8cf6', 8, 8], ['#8b5cf6', 30, 8], ['#16a39a', 8, 27], ['#e0603a', 30, 27]] as [string, number, number][]) s += box(P, x, y, H + 2, 13, 13, 11, { top: col, fr: col + 'cc', sd: col + '99' });
      return s;
    }
    case 'cloud': {
      const W = 66, D = 44, H = 4, P = frame(W, D, H + 18, 4); let s = shadow(P, W, D) + box(P, 0, 0, 0, W, D, H);
      for (let i = 0; i < 6; i++) s += fq(P, D, 6 + i * 9, H - 1, 10 + i * 9, H - 2.6, `fill="${c}"`);
      const [x, y] = P(W / 2, D / 2, H);
      return s + `<path d="M${f1(x)} ${f1(y)}V${f1(y - 14)}" stroke="${c}" stroke-dasharray="2 2"/><g transform="translate(${f1(x - 26)} ${f1(y - 60)}) scale(2.15)" fill="${c}" fill-opacity=".22" stroke="${c}" stroke-width="1.1" stroke-linejoin="round"><path d="${G.cloud}"/></g>`;
    }
  }
}

// ---- the other styles: classic diagram symbols, classic 3-D (diagram blue), line icons, front panels ----
const W = '#ffffff';
/* eslint-disable */
// ---------- Style 1: classic diagram symbols (2D, the shapes network diagrams use) ----------
const P2 = { // 24-unit paths, white on the shape
 l2:'M5 8.5h10M12.5 6l2.5 2.5-2.5 2.5M19 15.5H9M11.5 13L9 15.5l2.5 2.5',
 l3:'M5 7h9M11.5 4.5L14 7l-2.5 2.5M19 12h-9M12.5 9.5L10 12l2.5 2.5M5 17h9M11.5 14.5L14 17l-2.5 2.5',
 rt:'M12 3v6M9.5 6.5L12 9l2.5-2.5M12 21v-6M9.5 17.5L12 15l2.5 2.5M3 12h6M6.5 9.5L9 12l-2.5 2.5M21 12h-6M17.5 9.5L15 12l2.5 2.5',
 ap:'M12 18h.01M8.6 14.6a4.8 4.8 0 0 1 6.8 0M5.7 11.6a9 9 0 0 1 12.6 0M2.8 8.6a13 13 0 0 1 18.4 0',
};
function classic(k: string, c: string, o: { mono?: number } = {}): string {
 const fill=o.mono?'#3d6fb6':c, st=`stroke="${W}" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"`;
 const g=(p: string,x: number,y: number,s: number)=>`<g transform="translate(${x} ${y}) scale(${s})" ${st}><path d="${p}"/></g>`;
 let s='';
 switch(k){
  case 'access': s=`<rect x="20" y="28" width="80" height="42" rx="8" fill="${fill}"/><rect x="20" y="28" width="80" height="42" rx="8" fill="url(#gl)"/>`+g(P2.l2,40,25,2); break;
  case 'core': s=`<rect x="28" y="16" width="64" height="64" rx="10" fill="${fill}"/><rect x="28" y="16" width="64" height="64" rx="10" fill="url(#gl)"/><circle cx="60" cy="48" r="23" fill="none" stroke="${W}" stroke-opacity=".35" stroke-width="1.5"/>`+g(P2.l3,36,24,2); break;
  case 'router': s=`<circle cx="60" cy="48" r="32" fill="${fill}"/><circle cx="60" cy="48" r="32" fill="url(#gl)"/>`+g(P2.rt,36,24,2); break;
  case 'fw': s=`<rect x="20" y="22" width="80" height="52" rx="5" fill="${o.mono?fill:'#c4472a'}"/>`; for(let r=0;r<4;r++)for(let i=0;i<5;i++){const x=22+(r%2?8:0)+i*16;if(x+14>98)continue;s+=`<rect x="${x}" y="${24+r*12.5}" width="14" height="10" rx="1.5" fill="${o.mono?'#5e8dd0':'#e0603a'}"/>`} break;
  case 'ap': s=`<rect x="30" y="20" width="60" height="56" rx="28" fill="${fill}"/><rect x="30" y="20" width="60" height="56" rx="28" fill="url(#gl)"/>`+g(P2.ap,38,22,1.85); break;
  case 'server': s=`<rect x="40" y="10" width="40" height="76" rx="5" fill="${fill}"/>`; for(let i=0;i<4;i++)s+=`<rect x="46" y="${18+i*12}" width="28" height="7" rx="2" fill="none" stroke="${W}" stroke-width="1.5"/><circle cx="70" cy="${21.5+i*12}" r="1.4" fill="${W}"/>`; s+=`<circle cx="60" cy="74" r="3" fill="none" stroke="${W}" stroke-width="1.5"/>`; break;
  case 'host': s=`<rect x="24" y="14" width="72" height="68" rx="7" fill="${fill}"/>`; ([[32,22],[62,22],[32,50],[62,50]] as [number,number][]).forEach(([x,y],i)=>s+=`<rect x="${x}" y="${y}" width="26" height="24" rx="4" fill="${W}" fill-opacity="${i?.85:1}"/><text x="${x+13}" y="${y+16}" font-size="10" font-weight="700" text-anchor="middle" fill="${fill}" font-family="Inter,sans-serif">VM</text>`); break;
  case 'cloud': s=`<path transform="translate(14 0) scale(3.9)" d="${G.cloud}" fill="${o.mono?fill:'#7cc3ef'}"/>`; break;
 }
 return `<svg width="120" height="96" viewBox="0 0 120 96"><defs><linearGradient id="gl" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#fff" stop-opacity=".18"/><stop offset="1" stop-color="#000" stop-opacity=".12"/></linearGradient></defs>${s}</svg>`;
}
// ---------- Style 2: classic 3-D (the blue boxes and cylinders of network diagrams) ----------
function blue3d(k: string, c: string): string {
 const B={top:'#5c9be6',fr:'#2a6cc0',sd:'#1d528f',ed:'#174178'};
 const P=(W2: number,D: number,H: number,oy=0)=>{const cx=60-(W2-D)*CS/2,cy=52+oy-((W2+D)*.5-H)/2;return (x: number,y: number,z: number): [number, number]=>[cx+(x-y)*CS,cy+(x+y)*.5-z]};
 const pts=(f: P3,a: [number,number,number][])=>a.map(p=>f(...p).map(n=>n.toFixed(1)).join(',')).join(' ');
 const bx=(f: P3,w: number,d: number,h: number,col: {top: string; fr: string; sd: string; ed: string}=B,z0=0)=>`<polygon points="${pts(f,[[w,0,z0],[w,d,z0],[w,d,z0+h],[w,0,z0+h]])}" fill="${col.sd}" stroke="${col.ed}" stroke-width=".7"/><polygon points="${pts(f,[[0,d,z0],[w,d,z0],[w,d,z0+h],[0,d,z0+h]])}" fill="${col.fr}" stroke="${col.ed}" stroke-width=".7"/><polygon points="${pts(f,[[0,0,z0+h],[w,0,z0+h],[w,d,z0+h],[0,d,z0+h]])}" fill="${col.top}" stroke="${col.ed}" stroke-width=".7"/>`;
 const top=(f: P3,w: number,d: number,h: number,g: string,sz: number,sw=2.3)=>{const s=sz/24,[ex,ey]=f(w/2-sz/2,d/2-sz/2,h);return `<g transform="matrix(${(s*CS).toFixed(3)} ${(s*.5).toFixed(3)} ${(-s*CS).toFixed(3)} ${(s*.5).toFixed(3)} ${ex.toFixed(1)} ${ey.toFixed(1)})" fill="none" stroke="#fff" stroke-width="${sw}" stroke-linecap="round" stroke-linejoin="round"><path d="${g}"/></g>`};
 let s='';
 switch(k){
  case 'access':{const f=P(64,40,12);s=bx(f,64,40,12)+top(f,64,40,12,P2.l2,30);break}
  case 'core':{const f=P(52,46,34,4);s=bx(f,52,46,34)+top(f,52,46,34,P2.l3,32);break}
  case 'router':{const r=22,h=18,f=P(2*r,2*r,h,2),[bx2,by]=f(r,r,0),[tx,ty]=f(r,r,h),rx=1.2247*r,ry=.7071*r;
   s=`<path d="M${tx-rx} ${ty}L${bx2-rx} ${by}A${rx} ${ry} 0 0 0 ${bx2+rx} ${by}L${tx+rx} ${ty}Z" fill="${B.fr}" stroke="${B.ed}" stroke-width=".7"/><ellipse cx="${tx}" cy="${ty}" rx="${rx}" ry="${ry}" fill="${B.top}" stroke="${B.ed}" stroke-width=".7"/>`+top(f,2*r,2*r,h,P2.rt,30);break}
  case 'fw':{const f=P(58,30,30),R={top:'#e99a7e',fr:'#c4472a',sd:'#9a3520',ed:'#7a2a19'};s=bx(f,58,30,30,R);for(let r=0;r<4;r++)for(let i=0;i<5;i++){const x0=1+(r%2?6:0)+i*12;if(x0+10>57)continue;s+=`<polygon points="${pts(f,[[x0,30,29-r*7.2],[x0+10,30,29-r*7.2],[x0+10,30,23.5-r*7.2],[x0,30,23.5-r*7.2]])}" fill="#e0603a" stroke="#7a2a19" stroke-width=".5"/>`}break}
  case 'ap':{const r=22,f=P(2*r,2*r,8),[x,y]=f(r,r,8),rx=1.2247*r,ry=.7071*r;s=`<path d="M${x-rx} ${y}v7a${rx} ${ry} 0 0 0 ${2*rx} 0v-7" fill="${B.fr}" stroke="${B.ed}"/><ellipse cx="${x}" cy="${y}" rx="${rx}" ry="${ry}" fill="${B.top}" stroke="${B.ed}"/>`+top(f,2*r,2*r,8,P2.ap,28,2.4);break}
  case 'server':{const f=P(26,40,52,6);s=bx(f,26,40,52);for(let i=0;i<5;i++)s+=`<polygon points="${pts(f,[[3,40,47-i*8],[23,40,47-i*8],[23,40,43-i*8],[3,40,43-i*8]])}" fill="none" stroke="#9cc3ff" stroke-width=".8"/>`;break}
  case 'host':{const f=P(56,44,16,10);s=bx(f,56,44,16);([[6,6,'#16a39a'],[30,6,'#8b5cf6'],[6,24,'#e0603a'],[30,24,'#f2b52e']] as [number,number,string][]).forEach(([x,y,cc])=>{const C={top:cc,fr:cc+'cc',sd:cc+'99',ed:'#174178'};s+=`<g>${bx((a: number,b2: number,z: number)=>f(a+x,b2+y,z),14,14,12,C,18)}</g>`});break}
  case 'cloud':{s=`<path transform="translate(16 2) scale(3.7)" d="${G.cloud}" fill="#d9e9fb" stroke="#2a6cc0" stroke-width=".55"/>`;break}
 }
 return `<svg width="120" height="96" viewBox="0 0 120 96">${s}</svg>`;
}
// ---------- Style 3: line icons (outline, one colour per role) ----------
function line(k: string, c: string, o: { ink?: string } = {}): string {
 const col=o.ink||c,st=`fill="none" stroke="${col}" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"`;
 const g=(p: string,x: number,y: number,s: number,w=1.6)=>`<g transform="translate(${x} ${y}) scale(${s})" fill="none" stroke="${col}" stroke-width="${w}" stroke-linecap="round" stroke-linejoin="round"><path d="${p}"/></g>`;
 let s='';
 switch(k){
  case 'access': s=`<rect x="18" y="32" width="84" height="32" rx="6" ${st}/>`; for(let i=0;i<8;i++)s+=`<rect x="${26+i*9}" y="${44}" width="5" height="5" rx="1" fill="${col}"/>`; s+=g(P2.l2,44,4,1.3,1.8); break;
  case 'core': s=`<rect x="30" y="16" width="60" height="66" rx="8" ${st}/><path d="M30 30h60M30 68h60" ${st}/>`+g(P2.l3,42,31,1.5,1.6); break;
  case 'router': s=`<circle cx="60" cy="48" r="32" ${st}/>`+g(P2.rt,38,26,1.85,1.4); break;
  case 'fw': s=`<rect x="22" y="22" width="76" height="52" rx="4" ${st}/><path d="M22 35h76M22 48h76M22 61h76M48 22v13M74 22v13M36 35v13M60 35v13M86 35v13M48 48v13M74 48v13M36 61v13M60 61v13M86 61v13" ${st} stroke-width="1.6"/>`; break;
  case 'ap': s=`<circle cx="60" cy="58" r="22" ${st}/><circle cx="60" cy="58" r="4" fill="${col}"/>`+g(P2.ap,44,8,1.35,1.6); break;
  case 'server': s=`<rect x="40" y="12" width="40" height="74" rx="5" ${st}/><path d="M48 26h24M48 38h24M48 50h24" ${st}/><circle cx="60" cy="72" r="3" ${st}/>`; break;
  case 'host': s=`<rect x="24" y="16" width="72" height="64" rx="7" ${st}/><rect x="34" y="26" width="22" height="18" rx="3" ${st}/><rect x="64" y="26" width="22" height="18" rx="3" ${st}/><rect x="34" y="52" width="22" height="18" rx="3" ${st}/><rect x="64" y="52" width="22" height="18" rx="3" ${st}/>`; break;
  case 'cloud': s=g(G.cloud,14,0,3.9,.55); break;
 }
 return `<svg width="120" height="96" viewBox="0 0 120 96">${s}</svg>`;
}
// ---------- Style 4: front panel (rack hardware, front view) ----------
function front(k: string, c: string): string {
 const body='var(--ic-fr)',ln='var(--ic-ed)',led='var(--ic-led)';let s='';
 const unit=(y: number,h: number)=>`<rect x="8" y="${y}" width="104" height="${h}" rx="3" fill="${body}" stroke="${ln}"/><rect x="2" y="${y}" width="7" height="${h}" rx="1.5" fill="var(--ic-ear)"/><rect x="111" y="${y}" width="7" height="${h}" rx="1.5" fill="var(--ic-ear)"/>`;
 switch(k){
  case 'access': s=unit(36,22)+`<rect x="12" y="40" width="5" height="3" fill="${c}"/>`; for(let r=0;r<2;r++)for(let i=0;i<12;i++)s+=`<rect x="${22+i*6.2}" y="${40+r*8}" width="4.6" height="5.5" rx=".8" fill="${(i*7+r)%5?led:'var(--ic-off)'}"/>`; for(let i=0;i<4;i++)s+=`<rect x="${98+(i%2)*6}" y="${40+(i>>1)*8}" width="4.6" height="5.5" fill="#b8c2d0"/>`; break;
  case 'core': s=unit(14,68); for(let i=0;i<8;i++){const x=13+i*12.4,sup=i===3||i===4;s+=`<rect x="${x}" y="18" width="10.5" height="54" rx="1.5" fill="${sup?'var(--ic-top)':'var(--ic-sd)'}" stroke="${ln}"/>`;if(sup)s+=`<rect x="${x+2}" y="22" width="6.5" height="3" fill="${c}"/>`;else for(let r=0;r<7;r++)s+=`<rect x="${x+3}" y="${23+r*6.6}" width="4.5" height="4" fill="${(i+r)%4?led:'var(--ic-off)'}"/>`} s+=`<rect x="13" y="74" width="94" height="5" fill="#1b1f27"/>`; break;
  case 'router': s=unit(34,26)+`<circle cx="24" cy="47" r="8" fill="none" stroke="${c}" stroke-width="2"/><path d="M24 41v12M18 47h12" stroke="${c}" stroke-width="1.6"/>`; for(let i=0;i<6;i++)s+=`<rect x="${42+i*9}" y="42" width="7" height="9" rx="1" fill="#1b1f27" stroke="${ln}"/><rect x="${44+i*9}" y="44" width="3" height="2" fill="${i%3?led:'var(--ic-off)'}"/>`; break;
  case 'fw': s=unit(32,30)+`<rect x="8" y="32" width="104" height="5" fill="${c}"/>`; for(let r=0;r<3;r++)for(let i=0;i<5;i++){const x=12+(r%2?5:0)+i*10;if(x+8>62)continue;s+=`<rect x="${x}" y="${40+r*6.5}" width="8.5" height="5" fill="${c}" opacity=".85"/>`} for(let i=0;i<8;i++)s+=`<rect x="${68+(i%4)*10}" y="${42+(i>>2)*9}" width="7" height="6" fill="${(i*3)%5?led:'var(--ic-off)'}"/>`; break;
  case 'ap': s=`<circle cx="60" cy="48" r="32" fill="#e9edf3" stroke="#c6cdd8" stroke-width="2"/><circle cx="60" cy="48" r="20" fill="none" stroke="#d3d9e2"/><circle cx="60" cy="48" r="4" fill="${c}"/>`; break;
  case 'server': s=unit(26,44); for(let r=0;r<2;r++)for(let i=0;i<6;i++)s+=`<rect x="${14+i*13}" y="${31+r*18}" width="11" height="15" rx="1.5" fill="#1b1f27" stroke="${ln}"/><rect x="${18+i*13}" y="${33+r*18}" width="3" height="2" fill="${led}"/>`; s+=`<rect x="96" y="32" width="10" height="4" fill="${c}"/>`; break;
  case 'host': s=unit(26,44)+`<rect x="14" y="31" width="60" height="34" rx="3" fill="#1b1f27" stroke="${ln}"/>`; ([['#3b8cf6',18],['#8b5cf6',32],['#16a39a',46],['#e0603a',60]] as [string,number][]).forEach(([cc,x])=>s+=`<rect x="${x}" y="40" width="11" height="16" rx="2" fill="${cc}"/>`); s+=`<rect x="80" y="34" width="26" height="4" fill="${led}"/>`; break;
  case 'cloud': s=`<path transform="translate(16 2) scale(3.7)" d="${G.cloud}" fill="${c}" fill-opacity=".22" stroke="${c}" stroke-width=".5"/>`; break;
 }
 return `<svg width="120" height="96" viewBox="0 0 120 96">${s}</svg>`;
}

// ---------- Style 0: boxes (the first look: a rounded box with a small faceplate) ----------
function box(k: string, lit: boolean): string {
 let s=`<rect x="-3" y="19" width="126" height="60" rx="13" fill="var(--surface, #fff)" stroke="var(--line, #c9d0da)" stroke-width="2"/>`;
 const fx=18.5, fy=32, fw=83, fh=34, on=lit?'#8fd18f':'var(--ic-off)';
 const plate=(c: string)=>`<rect x="${fx}" y="${fy}" width="${fw}" height="${fh}" rx="6.5" fill="${c}"/>`;
 const lights=(y: number,col=on,x0=fx+6.5)=>{let r='';for(let x=x0;x+4<=fx+fw-6.5;x+=6.5)r+=`<rect class="pl" x="${x.toFixed(1)}" y="${y}" width="4" height="6.5" fill="${col}"/>`;return r};
 switch(k){
  case 'core': s+=plate('#323b4c')+lights(fy+8)+lights(fy+19); break;
  case 'router': s+=plate('#2c4a4a')+`<circle cx="${fx+12}" cy="${fy+fh/2}" r="7.5" fill="none" stroke="#7fd6cb" stroke-width="2"/><path d="M${fx+12} ${fy+11}v12M${fx+6} ${fy+17}h12" stroke="#7fd6cb" stroke-width="1.6"/>`+lights(fy+14,on,fx+26); break;
  case 'fw': s+=plate('#b9531a')+`<path d="M${fx} ${fy+12}h${fw}M${fx} ${fy+23}h${fw}" stroke="#7a2a19" stroke-width="1.4"/>`+lights(fy+3.5,'#ffd2b8')+lights(fy+15)+lights(fy+26,'#ffd2b8'); break;
  case 'ap': s+=plate('#5a4fc6')+`<circle cx="60" cy="${fy+fh/2}" r="9" fill="none" stroke="#c9c2ff" stroke-width="2"/><circle cx="60" cy="${fy+fh/2}" r="3" fill="${lit?'#c9c2ff':'var(--ic-off)'}"/>`; break;
  case 'server': case 'host': s+=plate('#d0661c')+`<text x="${fx+fw/2}" y="${fy+fh/2+6}" text-anchor="middle" fill="#fff" font-family="monospace" font-weight="700" font-size="17">&gt;_</text>`; break;
  case 'cloud': s+=`<path transform="translate(26 18) scale(2.85)" d="${G.cloud}" fill="#d9e9fb" stroke="#2a6cc0" stroke-width=".7"/>`; break;
  default: s+=plate('#323b4c')+lights(fy+14);
 }
 return s;
}

/** The look of the device drawings. */
export type IconStyle = 'box' | 'iso' | 'classic' | 'blue' | 'line' | 'front';
export const ICON_STYLES: { k: IconStyle; name: string }[] = [
  { k: 'box', name: 'Boxes' }, { k: 'iso', name: 'Isometric hardware' }, { k: 'blue', name: 'Classic 3-D (diagram blue)' },
  { k: 'front', name: 'Front panels' },
];
const strip = (svg: string) => svg.replace(/^<svg[^>]*>/, '').replace(/<\/svg>$/, '');

const cache = new Map<string, string>();

/** The drawing of a device (the inside of its svg; viewBox '0 0 130 98' for iso, else '-5 0 130 98'). */
export function iconSvg(look: IconStyle, role: DevRole, c: string, lit: boolean): string {
  return look === 'box' ? box(role, lit) : look === 'classic' ? strip(classic(role, c)) : look === 'blue' ? strip(blue3d(role, c)) : look === 'line' ? strip(line(role, c)) : look === 'front' ? strip(front(role, c))
    : '<defs><filter id="icsh" x="-50%" y="-50%" width="200%" height="200%"><feGaussianBlur stdDeviation="4"/></filter></defs>' + draw(role, c, lit);
}

const reach = new Map<string, number[] | null>();
/** How far the drawing reaches from (cx, cy) (drawing units), every 5 degrees from the x axis
 *  (clockwise, y down): where a cable meets it. Measured once per look and role; null when the
 *  browser cannot tell. */
export function iconReach(look: IconStyle, role: DevRole, cx: number, cy: number): number[] | null {
  const key = look + role + cx + ',' + cy;
  if (reach.has(key)) return reach.get(key)!;
  let out: number[] | null = null;
  const NS = 'http://www.w3.org/2000/svg', vx = look === 'iso' ? 0 : -5;
  const svg = document.createElementNS(NS, 'svg') as SVGSVGElement;
  svg.setAttribute('width', '130'); svg.setAttribute('height', '98');
  svg.style.cssText = 'position:absolute;left:-9999px;top:0;visibility:hidden;pointer-events:none';
  svg.innerHTML = `<g transform="translate(${-vx} 0)">${iconSvg(look, role, ROLE_COLOR[role], true)}</g>`;
  document.body.appendChild(svg);
  try {
    const els = [...svg.querySelectorAll<SVGGeometryElement>('path,rect,circle,ellipse,polygon,polyline,line')]
      .filter(e => !e.closest('defs,[filter]') && typeof e.isPointInFill === 'function');
    const parts = els.map(e => { const st = getComputedStyle(e); return { e, m: e.getCTM()?.inverse(), fill: st.fill !== 'none', stroke: st.stroke !== 'none' && parseFloat(st.strokeWidth) > 0 }; }).filter(p => p.m);
    if (parts.length) {
      const pt = svg.createSVGPoint();
      const hit = (x: number, y: number) => parts.some(p => { pt.x = x - vx; pt.y = y; const q = pt.matrixTransform(p.m!); return (p.fill && p.e.isPointInFill(q)) || (p.stroke && p.e.isPointInStroke(q)); });
      out = [];
      for (let a = 0; a < 72; a++) {
        const t = a * Math.PI / 36, ux = Math.cos(t), uy = Math.sin(t);
        let r = 80;
        while (r > 0 && !hit(cx + ux * r, cy + uy * r)) r -= 1.5;
        if (r > 0) while (hit(cx + ux * (r + .5), cy + uy * (r + .5))) r += .5;
        out.push(r);
      }
      if (!out.some(r => r > 0)) out = null;
    }
  } catch { out = null; }
  svg.remove();
  reach.set(key, out);
  return out;
}

@Component({
  selector: 'app-dev-icon',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<svg [attr.width]="size()" [attr.height]="size() * 98 / 130" [attr.viewBox]="look() === 'iso' ? '0 0 130 98' : '-5 0 130 98'" [class]="'st-' + state()" aria-hidden="true" [innerHTML]="body()"></svg>`,
  styles: [`
    :host { display: inline-block; line-height: 0;
      --ic-top: #3a4252; --ic-fr: #262c37; --ic-sd: #1c2129; --ic-ed: #4b5466; --ic-off: #4f5968; --ic-ear: #3d4554; --ic-sfp: #b8c2d0; --ic-led: #2fbf71; }
    :host-context(.light) { --ic-top: #eef1f5; --ic-fr: #d3d9e2; --ic-sd: #bcc5d1; --ic-ed: #98a3b3; --ic-off: #aab3c0; --ic-ear: #c6cdd8; --ic-led: #22a35e; }
    svg { overflow: visible; }
    svg.st-off { opacity: .5; filter: grayscale(1); }
    svg.st-boot :is(.pl) { animation: icblink 1.1s ease-in-out infinite; }
    @keyframes icblink { 50% { opacity: .25; } }
    @media (prefers-reduced-motion: reduce) { svg.st-boot .pl { animation: none; } }
  `],
})
export class DevIcon {
  private san = inject(DomSanitizer);
  readonly role = input<DevRole>('access');
  /** run: ports lit; boot: they blink; off: grey; idle: unlit */
  readonly state = input<'run' | 'boot' | 'off' | 'idle'>('run');
  readonly size = input(104);
  readonly color = input('');
  readonly look = input<IconStyle>('box');
  readonly body = computed(() => {
    const role = this.role(), c = this.color() || ROLE_COLOR[role], lit = this.state() === 'run' || this.state() === 'boot', look = this.look(), key = look + role + c + lit;
    let s = cache.get(key);
    if (!s) { s = iconSvg(look, role, c, lit); cache.set(key, s); }
    return this.san.bypassSecurityTrustHtml(s);
  });
}
