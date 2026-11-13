import { Component, computed, input, signal } from '@angular/core';
import { tr } from '../core/i18n';

/** One line of a chart: its name, its colour (a CSS colour or variable) and its points [seconds, value]. */
export interface ChartSeries { name: string; color: string; pts: [number, number][]; }

const W = 560, H = 150, L = 46, R = 10, T = 10, B = 22;

/**
 * A small line chart over time for the Traffic page: one axis, a recessive grid, 2 px lines, a legend
 * (names, so a line is never told by its colour alone) and a crosshair with the values at the pointer.
 */
@Component({
  selector: 'app-tchart',
  template: `
    <div class="tc">
      <div class="th"><b>{{ title() }}</b><span class="u">{{ unit() }}</span></div>
      @if (series().length > 1) { <div class="lg">@for (s of series(); track s.name) { <span><i [style.background]="s.color"></i>{{ s.name }}</span> }</div> }
      @if (box(); as b) {
        <div class="plot">
        @for (g of b.grid; track g.y) { <span class="yl" [style.top.%]="(g.y / H) * 100" [style.width.%]="((L - 6) / W) * 100">{{ g.label }}</span> }
        @for (x of b.xt; track x.x) { <span class="xl" [style.left.%]="(x.x / W) * 100" [class.l]="$first" [class.r]="$last">{{ x.label }}</span> }
        <svg [attr.viewBox]="'0 0 ' + W + ' ' + H" preserveAspectRatio="none" role="img" [attr.aria-label]="title() + ' ' + unit()" (pointermove)="move($event)" (pointerleave)="hov.set(-1)">
          @for (g of b.grid; track g.y) { <line class="gl" [attr.x1]="L" [attr.x2]="W - R" [attr.y1]="g.y" [attr.y2]="g.y" /> }
          @for (s of b.lines; track s.name) { <polyline class="ln" [attr.points]="s.d" [style.stroke]="s.color" /> }
          @if (hov() >= 0 && b.hover; as hv) {
            <line class="ch" [attr.x1]="hv.x" [attr.x2]="hv.x" [attr.y1]="T" [attr.y2]="H - B" />
            @for (p of hv.pts; track p.name) { <circle [attr.cx]="hv.x" [attr.cy]="p.y" r="4" [style.fill]="p.color" class="dot" /> }
          }
        </svg>
        </div>
        @if (hov() >= 0 && b.hover; as hv) {
          <div class="tip" [style.left.%]="hv.x / W * 100" [class.left]="hv.x > W * 0.6"><div class="tt">{{ hv.t }}</div>@for (p of hv.pts; track p.name) { <div><i [style.background]="p.color"></i>{{ p.name }}<b>{{ p.v }}</b></div> }</div>
        }
      } @else { <div class="em">{{ empty() || tr('Nothing yet: start a stream') }}</div> }
    </div>`,
  styles: [`
    :host { display: block; min-width: 0; }
    .tc { position: relative; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); padding: 10px 12px 6px; }
    .th { display: flex; align-items: baseline; gap: 6px; font-size: 12.5px; margin-bottom: 2px; .u { color: var(--muted); font-size: 11.5px; } }
    .lg { display: flex; gap: 4px 12px; flex-wrap: wrap; font-size: 11.5px; color: var(--muted); margin-bottom: 2px; span { display: inline-flex; align-items: center; gap: 5px; } i { width: 12px; height: 2px; border-radius: 1px; display: inline-block; } }
    .plot { position: relative; }
    .yl { position: absolute; left: 0; transform: translateY(-50%); text-align: right; font-size: 10px; color: var(--muted); font-variant-numeric: tabular-nums; line-height: 1; }
    .xl { position: absolute; bottom: 2px; transform: translateX(-50%); font-size: 10px; color: var(--muted); line-height: 1; &.l { transform: none; } &.r { transform: translateX(-100%); } }
    svg { width: 100%; height: 150px; display: block; overflow: visible; cursor: crosshair; }
    .gl { stroke: var(--line); stroke-width: 1; vector-effect: non-scaling-stroke; }
    .ln { fill: none; stroke-width: 2; stroke-linejoin: round; stroke-linecap: round; vector-effect: non-scaling-stroke; }
    .ch { stroke: var(--muted); stroke-width: 1; stroke-dasharray: 3 3; vector-effect: non-scaling-stroke; }
    .dot { stroke: var(--surface); stroke-width: 2; vector-effect: non-scaling-stroke; }
    .tip { position: absolute; top: 34px; transform: translateX(12px); background: var(--surface); border: 1px solid var(--line); border-radius: 8px; box-shadow: 0 8px 24px rgba(0,0,0,.25); padding: 6px 9px; font-size: 11.5px; pointer-events: none; white-space: nowrap; z-index: 2;
      &.left { transform: translateX(calc(-100% - 12px)); } .tt { color: var(--muted); margin-bottom: 2px; } div { display: flex; align-items: center; gap: 6px; } i { width: 8px; height: 8px; border-radius: 50%; } b { margin-left: auto; padding-left: 10px; font-variant-numeric: tabular-nums; } }
    .em { height: 150px; display: grid; place-items: center; color: var(--muted); font-size: 12px; }`],
})
export class TrafficChart {
  readonly title = input.required<string>();
  readonly unit = input('');
  readonly series = input.required<ChartSeries[]>();
  /** How a value is written (axis and tooltip). */
  readonly fmt = input<(v: number) => string>((v: number) => String(Math.round(v)));
  readonly empty = input('');
  protected readonly W = W; protected readonly H = H; protected readonly L = L; protected readonly R = R; protected readonly T = T; protected readonly B = B;
  protected tr = tr;
  protected hov = signal(-1);

  /** The scales, the lines, the grid and the hovered time. */
  protected box = computed(() => {
    const ss = this.series().filter(s => s.pts.length > 1);
    if (!ss.length) return null;
    const ts = ss.flatMap(s => s.pts.map(p => p[0])), t0 = Math.min(...ts), t1 = Math.max(...ts, t0 + 1);
    const vmax = Math.max(...ss.flatMap(s => s.pts.map(p => p[1])), 0);
    const step = nice(vmax / 3 || 1), ymax = step * 3;
    const x = (t: number) => L + ((t - t0) / (t1 - t0)) * (W - L - R), y = (v: number) => H - B - (v / ymax) * (H - B - T);
    const f = this.fmt();
    const grid = [0, 1, 2, 3].map(k => ({ y: y(k * step), label: f(k * step) }));
    const xt = [0, 0.5, 1].map(k => ({ x: L + k * (W - L - R), label: secs(t0 + k * (t1 - t0)) }));
    const lines = ss.map(s => ({ name: s.name, color: s.color, d: s.pts.map(p => `${x(p[0]).toFixed(1)},${y(p[1]).toFixed(1)}`).join(' ') }));
    let hover: { x: number; t: string; pts: { name: string; color: string; y: number; v: string }[] } | null = null;
    const h = this.hov();
    if (h >= 0) {
      const t = t0 + ((h - L) / (W - L - R)) * (t1 - t0);
      const pts = ss.map(s => { const p = s.pts.reduce((a, b) => (Math.abs(b[0] - t) < Math.abs(a[0] - t) ? b : a)); return { name: s.name, color: s.color, y: y(p[1]), v: f(p[1]), at: p[0] }; });
      const at = pts[0]?.at ?? t;
      hover = { x: x(at), t: secs(at), pts };
    }
    return { grid, xt, lines, hover };
  });

  protected move(ev: PointerEvent) {
    const r = (ev.currentTarget as SVGElement).getBoundingClientRect();
    const px = ((ev.clientX - r.left) / r.width) * W;
    this.hov.set(px < L || px > W - R ? -1 : px);
  }
}

/** A round step for the axis: 1, 2 or 5 times a power of ten. */
function nice(v: number) {
  const p = Math.pow(10, Math.floor(Math.log10(v))), m = v / p;
  return (m <= 1 ? 1 : m <= 2 ? 2 : m <= 5 ? 5 : 10) * p;
}

/** Seconds since the start as the axis says it: 45 s, 2:05. */
function secs(t: number) {
  const s = Math.max(0, Math.round(t));
  return s < 60 ? s + ' s' : Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0');
}
