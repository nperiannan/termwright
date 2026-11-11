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

const cache = new Map<string, string>();

@Component({
  selector: 'app-dev-icon',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<svg [attr.width]="size()" [attr.height]="size() * 98 / 130" viewBox="0 0 130 98" [class]="'st-' + state()" aria-hidden="true" [innerHTML]="body()"></svg>`,
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
  readonly body = computed(() => {
    const role = this.role(), c = this.color() || ROLE_COLOR[role], lit = this.state() === 'run' || this.state() === 'boot', key = role + c + lit;
    let s = cache.get(key);
    if (!s) { s = '<defs><filter id="icsh" x="-50%" y="-50%" width="200%" height="200%"><feGaussianBlur stdDeviation="4"/></filter></defs>' + draw(role, c, lit); cache.set(key, s); }
    return this.san.bypassSecurityTrustHtml(s);
  });
}
