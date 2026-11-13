import { Component, computed, input } from '@angular/core';
import { DecimalPipe } from '@angular/common';
import { tr } from '../core/i18n';
import { EmuStream, EmuStreamResult } from '../core/models';

/**
 * A stream's measurements on the Traffic page: its latency percentiles and histogram, the VLAN, priority and
 * DSCP its frames arrived with (a remarked frame stands out), and its flows (a stream with changing fields).
 */
@Component({
  selector: 'app-stream-detail',
  imports: [DecimalPipe],
  template: `
    <div class="sd">
      <div class="col">
        <div class="h"><b>{{ tr('Latency') }}</b><span class="u">ms · {{ stream().rtt ? tr('round trip') : tr('one way') }}</span></div>
        @if (res().latAvg >= 0) {
          <div class="ps">@for (p of pcts(); track p.k) { <div [class.hi]="p.hi"><span>{{ p.k }}</span><b>{{ p.v | number: '1.3-3' }}</b></div> }</div>
          @if (bars(); as bs) {
            <div class="hist" role="img" [attr.aria-label]="tr('Latency histogram')">
              @for (b of bs; track b.lo) { <div class="bar" [title]="b.tip"><i [style.height.%]="b.h"></i><span>{{ b.label }}</span></div> }
            </div>
          }
        } @else { <p class="em">{{ tr('Not measured: the test ports are on two hosts. Turn on Round trip to measure it there.') }}</p> }
      </div>
      <div class="col">
        <div class="h"><b>{{ tr('Arrived with') }}</b><span class="u">{{ tr('sent with VLAN {0} · PCP {1} · DSCP {2}', stream().vlan || '-', stream().vlan ? stream().prio || 0 : '-', stream().dscp || 0) }}</span></div>
        @if (marks().length) {
          <table><tr><th>VLAN</th><th>PCP</th><th>DSCP</th><th class="n">{{ tr('Frames') }}</th><th class="n">%</th></tr>
            @for (m of marks(); track $index) { <tr [class.chg]="m.chg" [title]="m.chg ? tr('Changed on the way') : ''"><td>{{ m.vlan || tr('untagged') }}</td><td>{{ m.vlan ? m.prio : '-' }}</td><td>{{ m.dscp < 0 ? '-' : m.dscp }}</td><td class="n">{{ m.n | number }}</td><td class="n">{{ m.pct | number: '1.0-1' }}</td></tr> }
          </table>
          @if (remarked()) { <p class="warn">⚠ {{ tr('Some frames arrived marked differently from how they were sent.') }}</p> }
        } @else { <p class="em">{{ tr('Nothing received yet.') }}</p> }
      </div>
      @if (res().flows?.length) {
        <div class="col wide">
          <div class="h"><b>{{ tr('Flows') }}</b><span class="u">{{ tr('{0} flows, {1} with loss', res().flowsN ?? 0, res().flowsLossy ?? 0) }}@if ((res().flowsN ?? 0) > res().flows!.length) { · {{ tr('the {0} with the most loss', res().flows!.length) }} }</span></div>
          <div class="fl"><table><tr><th>{{ tr('Flow') }}</th><th class="n">{{ tr('Sent') }}</th><th class="n">{{ tr('Received') }}</th><th class="n">{{ tr('Loss') }}</th><th class="n">{{ tr('Latency') }}</th></tr>
            @for (f of res().flows!; track f.flow) { <tr><td class="mono">{{ f.name || '#' + f.flow }}</td><td class="n">{{ f.tx | number }}</td><td class="n">{{ f.rx | number }}</td><td class="n" [class.bad]="f.loss > stream().maxLoss">{{ f.loss | number: '1.1-1' }} %</td><td class="n">{{ f.latAvg >= 0 ? (f.latAvg | number: '1.3-3') + ' ms' : '-' }}</td></tr> }
          </table></div>
        </div>
      }
    </div>`,
  styles: [`
    :host { display: block; }
    .sd { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 12px; padding: 12px; border-top: 1px solid var(--line); }
    .col { border: 1px solid var(--line); border-radius: 10px; background: var(--surface); padding: 10px 12px; min-width: 0; } .wide { grid-column: 1 / -1; }
    .h { display: flex; align-items: baseline; gap: 6px; font-size: 12.5px; margin-bottom: 8px; .u { color: var(--muted); font-size: 11.5px; } }
    .ps { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 6px; margin-bottom: 10px;
      div { border-radius: 8px; background: var(--surface-2); padding: 5px 7px; } span { display: block; color: var(--muted); font-size: 10.5px; } b { font-variant-numeric: tabular-nums; font-size: 12.5px; } .hi b { color: var(--s1); } }
    .hist { display: flex; align-items: flex-end; gap: 3px; height: 110px; padding-bottom: 16px; position: relative; border-bottom: 1px solid var(--line);
      .bar { flex: 1; min-width: 0; height: 100%; display: flex; flex-direction: column; justify-content: flex-end; position: relative; }
      i { display: block; background: var(--s1); border-radius: 3px 3px 0 0; min-height: 1px; }
      span { position: absolute; bottom: -15px; left: 50%; transform: translateX(-50%); font-size: 9.5px; color: var(--muted); white-space: nowrap; } }
    table { width: 100%; border-collapse: collapse; font-size: 12px; th { text-align: left; font-weight: 500; color: var(--muted); font-size: 11px; padding: 3px 8px; border-bottom: 1px solid var(--line); } td { padding: 3px 8px; border-bottom: 1px solid var(--line); } .n { text-align: right; font-variant-numeric: tabular-nums; } }
    tr.chg td { color: var(--warning, #e39b17); } td.bad { color: var(--danger); font-weight: 600; } .mono { font-family: var(--mono, monospace); }
    .fl { max-height: 240px; overflow: auto; }
    .em { color: var(--muted); font-size: 12px; margin: 8px 0; } .warn { color: var(--warning, #e39b17); font-size: 11.5px; margin: 6px 0 0; }`],
})
export class StreamDetail {
  readonly stream = input.required<EmuStream>();
  readonly res = input.required<EmuStreamResult>();
  protected tr = tr;

  protected pcts = computed(() => {
    const r = this.res();
    return [{ k: 'min', v: r.latMin ?? 0 }, { k: 'p50', v: r.p50 ?? r.latAvg }, { k: 'p95', v: r.p95 ?? r.latAvg, hi: true }, { k: 'p99', v: r.p99 ?? r.latMax, hi: true }, { k: 'max', v: r.latMax }, { k: tr('jitter'), v: r.jitter ?? 0 }]
      .map(p => ({ hi: false, ...p }));
  });

  protected bars = computed(() => {
    const h = this.res().hist ?? [];
    const top = Math.max(0, ...h.map(b => b.n));
    if (!top) return null;
    const ms = (v: number) => (v >= 1 ? String(+v.toPrecision(3)) : String(+v.toPrecision(2)));
    return h.map(b => ({ lo: b.lo, h: (b.n / top) * 100, label: ms(b.lo), tip: tr('{0}-{1} ms: {2} frames', ms(b.lo), ms(b.hi), b.n) }));
  });

  protected marks = computed(() => {
    const s = this.stream(), ms = this.res().marks ?? [], all = ms.reduce((a, m) => a + m.n, 0) || 1;
    return ms.map(m => ({ ...m, pct: (m.n / all) * 100, chg: m.vlan !== (s.vlan || 0) || (!!s.vlan && m.prio !== (s.prio || 0)) || (m.dscp >= 0 && m.dscp !== (s.dscp || 0)) }));
  });
  protected remarked = computed(() => this.marks().some(m => m.chg));
}
