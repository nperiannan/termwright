import { Component, input, output } from '@angular/core';
import { tr } from '../core/i18n';
import { EmuTester, EmuTrafficView } from '../core/models';
import { ChartSeries, TrafficChart } from './traffic-chart';

/** The Traffic page's views. */
export type TrafficView = 'streams' | 'tests' | 'stats';

/** A tile at the top of the Traffic page. */
export interface TrafficKpi { label: string; value: string; sub: string; warn: boolean; good: boolean; }

/**
 * The Traffic page next to Canvas: its buttons, the tiles, the test ports as cards and the live charts, with its own
 * styles. The streams table and its editors (or the saved tests) come in as content, shared with the bottom pane.
 */
@Component({
  selector: 'app-traffic-page',
  imports: [TrafficChart],
  template: `
    <section class="tpg" [attr.aria-label]="tr('Traffic')">
      <header class="tph">
        <div class="tpt"><h2>{{ tr('Traffic generator') }}</h2><span class="muted small">{{ tr('Test ports send and count frames on switch ports: streams, RFC 2544 tests and their statistics') }}</span></div>
        <span class="seg2 tv"><a [class.on]="view() === 'streams'" (click)="viewChange.emit('streams')">{{ tr('Streams') }}</a><a [class.on]="view() === 'tests'" (click)="viewChange.emit('tests')">{{ tr('Tests') }}</a><a [class.on]="view() === 'stats'" (click)="viewChange.emit('stats')" [title]="tr('Every test port’s counters at once, its streams and their flows')">{{ tr('Statistics') }}</a></span>
        <span class="sp"></span>
        @if (view() === 'tests') { <button type="button" (click)="newTest.emit()">＋ {{ tr('Test') }}</button> }
        <button type="button" (click)="radius.emit()" [title]="tr('The lab\\'s RADIUS server: users, MAC logins and the logins it answered, for 802.1X on the switches')">{{ tr('RADIUS…') }}</button>
        <button type="button" (click)="library.emit()" [title]="tr('Ready tests: unicast, IMIX, QoS classes, MAC table fill, round trip, RFC 2544 and 2889 - and yours')">{{ tr('Library…') }}</button>
        <button type="button" (click)="newTester.emit()">＋ {{ tr('Test Port') }}</button>
        @if (view() !== 'tests') { <button type="button" (click)="newStream.emit()" [disabled]="(traf()?.testers?.length ?? 0) < 1">＋ {{ tr('Stream') }}</button>
          @if (traf()?.running) { <button type="button" (click)="stop.emit()">■ {{ tr('Stop') }}</button> } @else { <button type="button" class="primary" (click)="start.emit()" [disabled]="!traf()?.streams?.length">▶ {{ tr('Start All') }}</button> }
          <button type="button" (click)="report.emit()" [disabled]="!traf()?.results?.length">{{ tr('Report…') }}</button> }
      </header>
      <div class="kpis">@for (k of kpis(); track k.label) { <div class="kpi" [class.warn]="k.warn" [class.good]="k.good"><span>{{ k.label }}</span><b>{{ k.value }}</b><small>{{ k.sub }}</small></div> }</div>
      <div class="tpb" [class.tests]="view() !== 'streams'">
        @if (view() === 'streams') {
          <aside class="tpl">
            <div class="sec2">{{ tr('Test ports') }}<span class="muted">{{ cards().length }}</span></div>
            @for (c of cards(); track c.t.id) { @let t = c.t;
              <div class="tpc" [class.sel]="selected() === t.id" (click)="editTester.emit(t)" [title]="tr('Its settings')"><div class="r1"><b>⇄ {{ t.name }}</b><a class="x" (click)="removeTester.emit(t); $event.stopPropagation()" [title]="tr('Remove')" [attr.aria-label]="tr('Remove {0}?', t.name)">✕</a></div>
                <div class="small">{{ c.where }}</div>
                <div class="mono small">{{ t.dhcp ? (t.lease || 'DHCP…') : (t.ip || '-') }}@if (t.ip6 || t.slaac) { · {{ t.slaac ? (t.lease6 || 'SLAAC…') : t.ip6 }} }</div>
                <div class="mono small muted">{{ t.mac }}@if (t.vlan) { · VLAN {{ t.vlan }} }@if (t.speed) { · {{ t.speed >= 1000 ? t.speed / 1000 + 'G' : t.speed + 'M' }} }</div>
                @if (t.lag) { <div class="small" [class.up]="t.lacpUp?.startsWith('up')" [class.dn]="!t.lacpUp?.startsWith('up')" [title]="t.lacpUp || ''">LAG {{ t.lag }} · {{ t.lacpUp?.startsWith('up') ? tr('up') : tr('not up') }}</div> }
                @if (t.neighbor) { <div class="small muted" [title]="tr('What the switch says on LLDP')">⇆ {{ t.neighbor }}</div> }
                @if (t.clients?.n) { @let cs = t.clientState;
                  <div class="small" [class.up]="cs && cs.failed === 0 && (t.clients?.dot1x ? cs.auth : cs.bound) === cs.n" [class.dn]="!!cs?.failed" [title]="tr('Clients behind this test port')">{{ tr('{0} clients', t.clients!.n) }}@if (cs) { · @if (t.clients?.dot1x) { {{ tr('{0} in', cs.auth) }} }@else if (t.clients?.dhcp) { {{ tr('{0} with an address', cs.bound) }} }@if (cs.failed) { · {{ tr('{0} refused', cs.failed) }} } }</div> }
                @if (t.stp?.mode) { <div class="small" [class.up]="t.stpState?.rootIsUs" [title]="tr('Spanning tree')">{{ t.stp!.mode.toUpperCase() }} · {{ t.stpState?.rootIsUs ? tr('root') : t.stpState?.got ? tr('not root') : tr('no BPDU yet') }}</div> }</div>
            } @empty { <p class="muted small tpe">{{ tr('No test port yet: ＋ Test Port, or right-click a switch on the canvas → Add Test Port.') }}</p> }
          </aside>
        }
        <div class="tpm">
          @if (view() === 'streams') {
            <div class="tcs">
              <app-tchart [title]="tr('Throughput')" unit="pps" [series]="throughput()" [fmt]="fmtPps()" />
              <app-tchart [title]="tr('Loss so far')" unit="%" [series]="loss()" [fmt]="fmtPct()" />
              <app-tchart [title]="tr('Latency')" unit="ms" [series]="latency()" [fmt]="fmtMs()" [empty]="tr('No latency yet: it is measured when both test ports are on one host, or with Round trip')" />
            </div>
          }
          <ng-content />
        </div>
      </div>
    </section>`,
  styles: [`
    :host { display: contents; }
    .tpg {
      position: absolute; inset: 0; z-index: 25; background: var(--bg); overflow: auto; padding: 16px 20px 20px; display: flex; flex-direction: column; gap: 14px; }
    .tph { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; h2 { margin: 0; font-size: 17px; } .tpt { display: flex; flex-direction: column; gap: 2px; } .sp { flex: 1; } }
    .kpis { display: grid; grid-template-columns: repeat(7, minmax(120px, 1fr)); gap: 10px; }
    .kpi { border: 1px solid var(--line); border-radius: 10px; background: var(--surface); padding: 9px 12px; display: flex; flex-direction: column; gap: 2px; min-width: 0;
      span { font-size: 11px; color: var(--muted); text-transform: uppercase; letter-spacing: .04em; } b { font-size: 19px; font-weight: 600; font-variant-numeric: tabular-nums; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; } small { font-size: 11px; color: var(--muted); }
      &.warn { border-color: var(--warning, #e39b17); b::before { content: '⚠ '; font-size: 14px; color: var(--warning, #e39b17); } } &.good { b::before { content: '✓ '; font-size: 14px; color: var(--success, #1fa45a); } } }
    .tpb { display: grid; grid-template-columns: 250px minmax(0, 1fr); gap: 14px; flex: 1; min-height: 0; }
    .tpl { display: flex; flex-direction: column; gap: 8px; padding-top: 11px; .tpe { margin: 0; border: 1px dashed var(--line); border-radius: 10px; padding: 16px 14px; text-align: center; line-height: 1.5; } .sec2 { display: flex; justify-content: space-between; font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); } }
    .tpc { border: 1px solid var(--line); border-radius: 10px; background: var(--surface); padding: 8px 10px; display: flex; flex-direction: column; gap: 2px; cursor: pointer; &:hover { border-color: var(--muted); } &.sel { border-color: var(--accent); background: var(--accent-soft); } .r1 { display: flex; align-items: center; b { flex: 1; color: var(--warning, #e39b17); font-size: 12.5px; } } .x { cursor: pointer; color: var(--muted); } .x:hover { color: var(--danger); } }
    .tpm { display: flex; flex-direction: column; gap: 12px; min-width: 0; }
    .tcs { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
    .tpb.tests { grid-template-columns: minmax(0, 1fr); }
    .up { color: var(--success, #1fa45a); } .dn { color: var(--warning, #e39b17); }
    .seg2 { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; a { padding: 5px 12px; cursor: pointer; font-size: 12px; } a.on { background: var(--accent-soft); color: var(--accent); } }
    .muted { color: var(--muted); } .small { font-size: 11.5px; } .mono { font-family: var(--mono, monospace); }
    @media (max-width: 1300px) { .kpis { grid-template-columns: repeat(4, minmax(120px, 1fr)); } .tcs { grid-template-columns: 1fr; } }
  `],
})
export class TrafficPage {
  readonly traf = input<EmuTrafficView | null>(null);
  readonly view = input<TrafficView>('streams');
  readonly kpis = input<TrafficKpi[]>([]);
  readonly cards = input<{ t: EmuTester; where: string }[]>([]);
  readonly selected = input('');
  readonly throughput = input<ChartSeries[]>([]);
  readonly loss = input<ChartSeries[]>([]);
  readonly latency = input<ChartSeries[]>([]);
  readonly fmtPps = input<(v: number) => string>(v => String(v));
  readonly fmtPct = input<(v: number) => string>(v => String(v));
  readonly fmtMs = input<(v: number) => string>(v => String(v));
  readonly viewChange = output<TrafficView>();
  readonly newTest = output<void>();
  readonly newTester = output<void>();
  readonly newStream = output<void>();
  readonly start = output<void>();
  readonly stop = output<void>();
  readonly report = output<void>();
  readonly library = output<void>();
  readonly radius = output<void>();
  readonly editTester = output<EmuTester>();
  readonly removeTester = output<EmuTester>();
  protected tr = tr;
}
