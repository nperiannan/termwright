import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { tr } from '../core/i18n';
import { EmuFlowResult, EmuHostUse, EmuPortResult, EmuStream, EmuStreamResult, EmuTester, EmuTrafficView, LabDevice } from '../core/models';
import { Ui } from '../core/ui.service';

/** A test port's row: where it is and its counters (since Clear counters). */
interface PortRow {
  t: EmuTester; host: string; sw: string; port: string; up: boolean; link: string; linkWhy: string;
  txr: number; rxr: number; txm: number; rxm: number; tx: number; rx: number; txb: number; rxb: number;
  exp: number; got: number; loss: number; lat: number; latMax: number; err: number; spark: { tx: string; rx: string } | null;
}
interface StreamRow { s: EmuStream; r: EmuStreamResult; tx: number; rx: number; exp: number; loss: number; ooo: number; dup: number; }
interface FlowRow { s: EmuStream; f: EmuFlowResult; }
type Tab = 'ports' | 'streams' | 'flows';
/** The counters at Clear counters: per test port and per stream. */
interface Base { at: string; started: string; ports: Record<string, EmuPortResult>; streams: Record<string, EmuStreamResult>; }

/** The optional columns of the ports table (key, label, shown at first). */
const PORT_COLS: { k: string; label: string; on: boolean }[] = [
  { k: 'link', label: 'Link', on: true }, { k: 'rate', label: 'Tx / Rx fps', on: true }, { k: 'mbps', label: 'Tx / Rx Mbit/s', on: true },
  { k: 'frames', label: 'Tx / Rx frames', on: true }, { k: 'bytes', label: 'Tx / Rx bytes', on: false }, { k: 'loss', label: 'Loss', on: true },
  { k: 'lat', label: 'Latency avg', on: true }, { k: 'latmax', label: 'Latency max', on: false }, { k: 'err', label: 'Errors', on: true }, { k: 'spark', label: 'Last 60 s', on: true },
];

/**
 * The Traffic page's Statistics: every test port's counters at once, grouped by the host it runs on (a virtual
 * chassis), live every second; a port's streams and a stream's flows below it. Freeze, Clear counters, Columns and
 * Export CSV, like a hardware tester's statistics views.
 */
@Component({
  selector: 'app-traffic-stats',
  imports: [FormsModule],
  template: `
    <section class="ts" [attr.aria-label]="tr('Statistics')">
      <div class="tsb">
        <span class="seg2"><a [class.on]="tab() === 'ports'" (click)="tab.set('ports')">{{ tr('Ports') }} <i>{{ ports().length }}</i></a><a [class.on]="tab() === 'streams'" (click)="tab.set('streams')">{{ tr('Streams') }} <i>{{ streams().length }}</i></a><a [class.on]="tab() === 'flows'" (click)="tab.set('flows')">{{ tr('Flows') }} <i>{{ flowsN() }}</i></a></span>
        <input class="q" type="search" [ngModel]="q()" (ngModelChange)="q.set($event)" [placeholder]="tr('Search ports, switches, streams')" [attr.aria-label]="tr('Search')" />
        <span class="sp"></span>
        <span class="live" [class.on]="!!traf()?.running && !frozen()" [title]="liveTip()"><i></i>{{ liveText() }}</span>
        <button type="button" class="sm" (click)="freeze()" [class.act]="!!frozen()" [title]="tr('Keep the numbers still to read them; the counting goes on')">{{ frozen() ? '▶ ' + tr('Resume') : '❚❚ ' + tr('Freeze') }}</button>
        <button type="button" class="sm" (click)="clear()" [disabled]="!ports().length" [title]="tr('Count from now on (the switches and test ports keep running; latency stays since the start of the run)')">{{ tr('Clear counters') }}</button>
        @if (base()) { <a class="lnk" (click)="base.set(null)" [title]="tr('Show the counters since the start of the run again')">{{ tr('since {0}', base()!.at) }} ✕</a> }
        <span class="cm"><button type="button" class="sm" (click)="colsOpen.set(!colsOpen()); $event.stopPropagation()" [attr.aria-expanded]="colsOpen()">{{ tr('Columns') }} ▾</button>
          @if (colsOpen()) { <div class="cmm" (click)="$event.stopPropagation()">@for (c of cols; track c.k) { <label><input type="checkbox" [checked]="shown().has(c.k)" (change)="toggleCol(c.k)" /> {{ tr(c.label) }}</label> }</div> }</span>
        <button type="button" class="sm" (click)="exportCsv()" [disabled]="!ports().length">{{ tr('Export CSV') }}</button>
      </div>

      @if (!ports().length) {
        <p class="tse muted">{{ tr('No test port yet: ＋ Test Port, or right-click a switch on the canvas → Add Test Port. Its counters show here, a row per port.') }}</p>
      } @else if (tab() === 'ports') {
        <div class="tw"><table class="st">
          <thead><tr>
            <th (click)="sortBy('port')">{{ tr('Test port') }}{{ arrow('port') }}</th><th (click)="sortBy('sw')">{{ tr('Switch port') }}{{ arrow('sw') }}</th>
            @if (on('link')) { <th>{{ tr('Link') }}</th> }
            @if (on('rate')) { <th class="n" (click)="sortBy('txr')">{{ tr('Tx fps') }}{{ arrow('txr') }}</th><th class="n" (click)="sortBy('rxr')">{{ tr('Rx fps') }}{{ arrow('rxr') }}</th> }
            @if (on('mbps')) { <th class="n" (click)="sortBy('txm')">{{ tr('Tx Mbit/s') }}{{ arrow('txm') }}</th><th class="n" (click)="sortBy('rxm')">{{ tr('Rx Mbit/s') }}{{ arrow('rxm') }}</th> }
            @if (on('frames')) { <th class="n" (click)="sortBy('tx')">{{ tr('Tx frames') }}{{ arrow('tx') }}</th><th class="n" (click)="sortBy('rx')">{{ tr('Rx frames') }}{{ arrow('rx') }}</th> }
            @if (on('bytes')) { <th class="n" (click)="sortBy('txb')">{{ tr('Tx bytes') }}{{ arrow('txb') }}</th><th class="n" (click)="sortBy('rxb')">{{ tr('Rx bytes') }}{{ arrow('rxb') }}</th> }
            @if (on('loss')) { <th class="n" (click)="sortBy('loss')" [title]="tr('Of its streams’ frames that were for this port, how many did not arrive')">{{ tr('Loss') }}{{ arrow('loss') }}</th> }
            @if (on('lat')) { <th class="n" (click)="sortBy('lat')">{{ tr('Latency avg') }}{{ arrow('lat') }}</th> }
            @if (on('latmax')) { <th class="n" (click)="sortBy('latMax')">{{ tr('Latency max') }}{{ arrow('latMax') }}</th> }
            @if (on('err')) { <th class="n" (click)="sortBy('err')" [title]="tr('Frames it could not send, and frames that arrived out of order or twice')">{{ tr('Errors') }}{{ arrow('err') }}</th> }
            @if (on('spark')) { <th>{{ tr('Last 60 s') }}</th> }
          </tr></thead>
          @for (g of groups(); track g.host) {
            <tbody>
              <tr class="grp"><td [attr.colspan]="ncols()"><a class="tg" (click)="fold(g.host)" [attr.aria-expanded]="!folded().has(g.host)">{{ folded().has(g.host) ? '▸' : '▾' }}</a>
                <i class="hd" [style.background]="hcol()(g.host)"></i><b>{{ hname()(g.host) || tr('this computer') }}</b>
                <span class="muted">{{ tr('virtual chassis') }} · {{ g.rows.length === 1 ? tr('1 test port') : tr('{0} test ports', g.rows.length) }}</span>
                <span class="gt">Tx {{ pps(g.txr) }} · Rx {{ pps(g.rxr) }}</span>
                @if (g.cpu) { <span class="gt muted" [title]="tr('The host’s load and its cores')">CPU {{ g.cpu }}</span> }</td></tr>
              @if (!folded().has(g.host)) { @for (r of g.rows; track r.t.id) {
                <tr [class.sel]="sel() === r.t.id" [class.down]="!r.up" (click)="pick(r.t.id)">
                  <td><b class="tp">⇄ {{ r.t.name }}</b></td><td>{{ r.sw }} · {{ r.port }}</td>
                  @if (on('link')) { <td [title]="r.linkWhy"><span class="lk" [class.up]="r.up">{{ r.link }}</span></td> }
                  @if (on('rate')) { <td class="n">{{ int(r.txr) }}</td><td class="n">{{ int(r.rxr) }}</td> }
                  @if (on('mbps')) { <td class="n">{{ mb(r.txm) }}</td><td class="n">{{ mb(r.rxm) }}</td> }
                  @if (on('frames')) { <td class="n" [title]="int(r.tx)">{{ big(r.tx) }}</td><td class="n" [title]="int(r.rx)">{{ big(r.rx) }}</td> }
                  @if (on('bytes')) { <td class="n">{{ bytes(r.txb) }}</td><td class="n">{{ bytes(r.rxb) }}</td> }
                  @if (on('loss')) { <td class="n" [class.bad]="r.loss > 0" [title]="r.exp ? tr('{0} of {1} frames', r.exp - r.got, r.exp) : ''">{{ r.exp ? pct(r.loss) : '-' }}</td> }
                  @if (on('lat')) { <td class="n">{{ ms(r.lat) }}</td> }
                  @if (on('latmax')) { <td class="n">{{ ms(r.latMax) }}</td> }
                  @if (on('err')) { <td class="n" [class.bad]="r.err > 0">{{ r.err }}</td> }
                  @if (on('spark')) { <td class="sk">@if (r.spark; as k) { <svg viewBox="0 0 90 22" width="90" height="22" aria-hidden="true"><polyline [attr.points]="k.tx" class="ktx" /><polyline [attr.points]="k.rx" class="krx" /></svg> } @else { <span class="muted">{{ r.up ? '' : tr('no link') }}</span> }</td> }
                </tr>
              } }
            </tbody>
          } @empty { <tbody><tr><td [attr.colspan]="ncols()" class="muted">{{ tr('Nothing matches the search.') }}</td></tr></tbody> }
        </table></div>
        @if (selRow(); as p) {
          <div class="sub"><b>⇄ {{ p.t.name }} · {{ p.sw }} · {{ p.port }}</b><span class="muted"> - {{ tr('its streams') }}</span><a class="x" (click)="sel.set('')" [attr.aria-label]="tr('Close')">✕</a></div>
          <div class="tw"><table class="st">
            <thead><tr><th>{{ tr('Stream') }}</th><th>{{ tr('From → To') }}</th><th>{{ tr('Frame') }}</th><th class="n">{{ tr('Tx fps') }}</th><th class="n">{{ tr('Rx fps') }}</th><th class="n">{{ tr('Loss') }}</th><th class="n">{{ tr('Latency min / avg / max') }}</th><th class="n">{{ tr('Out of order') }}</th></tr></thead>
            <tbody>@for (x of portStreams(); track x.s.id) {
              <tr [class.sel]="selS() === x.s.id" (click)="selS.set(selS() === x.s.id ? '' : x.s.id)" [title]="x.r.flows?.length ? tr('Its flows below') : ''">
                <td><b>{{ x.s.name }}</b>@if (x.r.flows?.length) { <span class="muted"> ▸</span> }</td><td>{{ nameOf(x.s.from) }} → {{ x.s.to ? nameOf(x.s.to) : toText(x.s) }}</td><td>{{ frame(x.s) }}</td>
                <td class="n">{{ int(x.r.txRate) }}</td><td class="n">{{ int(x.r.rxRate) }}</td><td class="n" [class.bad]="x.loss > 0">{{ x.exp ? pct(x.loss) : '-' }}</td>
                <td class="n">{{ lat3(x.r) }}</td><td class="n" [class.bad]="x.ooo > 0">{{ x.ooo }}</td></tr>
            } @empty { <tr><td colspan="8" class="muted">{{ tr('No stream sends from or to this test port.') }}</td></tr> }</tbody>
          </table></div>
        }
      } @else if (tab() === 'streams') {
        <div class="tw"><table class="st">
          <thead><tr><th>{{ tr('Stream') }}</th><th>{{ tr('From → To') }}</th><th>{{ tr('Frame') }}</th><th class="n">{{ tr('Tx fps') }}</th><th class="n">{{ tr('Rx fps') }}</th><th class="n">{{ tr('Tx frames') }}</th><th class="n">{{ tr('Rx frames') }}</th><th class="n">{{ tr('Loss') }}</th><th class="n">{{ tr('Latency min / avg / max') }}</th><th class="n">{{ tr('Jitter') }}</th><th class="n">{{ tr('Out of order') }}</th><th class="n">{{ tr('Duplicates') }}</th><th>{{ tr('Result') }}</th></tr></thead>
          <tbody>@for (x of streamRows(); track x.s.id) {
            <tr [class.sel]="selS() === x.s.id" (click)="selS.set(selS() === x.s.id ? '' : x.s.id)">
              <td><b>{{ x.s.name }}</b>@if (x.r.flows?.length) { <span class="muted"> ▸</span> }</td><td>{{ nameOf(x.s.from) }} → {{ x.s.to ? nameOf(x.s.to) : toText(x.s) }}</td><td>{{ frame(x.s) }}</td>
              <td class="n">{{ int(x.r.txRate) }}</td><td class="n">{{ int(x.r.rxRate) }}</td><td class="n" [title]="int(x.tx)">{{ big(x.tx) }}</td><td class="n" [title]="int(x.rx)">{{ big(x.rx) }}</td>
              <td class="n" [class.bad]="x.loss > 0">{{ x.exp ? pct(x.loss) : '-' }}</td><td class="n">{{ lat3(x.r) }}</td><td class="n">{{ x.r.jitter ? ms(x.r.jitter) : '-' }}</td>
              <td class="n" [class.bad]="x.ooo > 0">{{ x.ooo }}</td><td class="n">{{ x.dup }}</td>
              <td><span class="rs" [class.pass]="x.r.result === 'pass'" [class.fail]="x.r.result === 'fail'" [title]="x.r.why || ''">{{ x.r.result ? tr(x.r.result === 'pass' ? 'pass' : 'fail') : x.r.running ? tr('running') : '-' }}</span></td></tr>
          } @empty { <tr><td colspan="13" class="muted">{{ q() ? tr('Nothing matches the search.') : tr('No stream yet: ＋ Stream.') }}</td></tr> }</tbody>
        </table></div>
      } @else {
        <div class="tw"><table class="st">
          <thead><tr><th>{{ tr('Stream') }}</th><th>{{ tr('Flow') }}</th><th class="n">{{ tr('Tx frames') }}</th><th class="n">{{ tr('Rx frames') }}</th><th class="n">{{ tr('Loss') }}</th><th class="n">{{ tr('Latency avg') }}</th><th class="n">{{ tr('Latency max') }}</th></tr></thead>
          <tbody>@for (x of flowRows(); track x.s.id + ':' + x.f.flow) {
            <tr><td><b>{{ x.s.name }}</b></td><td class="mono">{{ x.f.name }}</td><td class="n">{{ int(x.f.tx) }}</td><td class="n">{{ int(x.f.rx) }}</td><td class="n" [class.bad]="x.f.loss > 0">{{ pct(x.f.loss) }}</td><td class="n">{{ ms(x.f.latAvg) }}</td><td class="n">{{ ms(x.f.latMax) }}</td></tr>
          } @empty { <tr><td colspan="7" class="muted">{{ tr('No flows: a stream has flows when some of its fields change from frame to frame (addresses, VLAN, ports). Its 32 most lossy flows show here.') }}</td></tr> }</tbody>
        </table></div>
      }
      @if (tab() !== 'flows' && flowsOf(); as fl) {
        <div class="sub"><b>{{ tr('Flows of {0}', fl.s.name) }}</b><span class="muted"> - {{ tr('{0} flows, {1} of them lost frames; the most lossy first', fl.r.flowsN || fl.r.flows!.length, fl.r.flowsLossy || 0) }}</span><a class="x" (click)="selS.set('')" [attr.aria-label]="tr('Close')">✕</a></div>
        <div class="tw"><table class="st">
          <thead><tr><th>{{ tr('Flow') }}</th><th class="n">{{ tr('Tx frames') }}</th><th class="n">{{ tr('Rx frames') }}</th><th class="n">{{ tr('Loss') }}</th><th class="n">{{ tr('Latency avg') }}</th><th class="n">{{ tr('Latency max') }}</th></tr></thead>
          <tbody>@for (f of fl.r.flows ?? []; track f.flow) { <tr><td class="mono">{{ f.name }}</td><td class="n">{{ int(f.tx) }}</td><td class="n">{{ int(f.rx) }}</td><td class="n" [class.bad]="f.loss > 0">{{ pct(f.loss) }}</td><td class="n">{{ ms(f.latAvg) }}</td><td class="n">{{ ms(f.latMax) }}</td></tr> }</tbody>
        </table></div>
      }
    </section>`,
  styles: [`
    :host { display: block; min-width: 0; }
    .ts { display: flex; flex-direction: column; gap: 10px; min-width: 0; }
    .tsb { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; .sp { flex: 1; } }
    .seg2 { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; a { padding: 5px 12px; cursor: pointer; font-size: 12px; display: inline-flex; gap: 6px; align-items: center; } a.on { background: var(--accent-soft); color: var(--accent); }
      i { font-style: normal; font-size: 10.5px; min-width: 18px; padding: 0 5px; border-radius: 9px; background: var(--line); color: var(--muted); text-align: center; } a.on i { background: var(--accent); color: #fff; } }
    .q { width: 240px; }
    .live { display: inline-flex; align-items: center; gap: 6px; font-size: 11.5px; color: var(--muted); i { width: 8px; height: 8px; border-radius: 50%; background: var(--muted); opacity: .6; } &.on i { background: var(--success, #1fa45a); opacity: 1; animation: lv 1.6s ease-in-out infinite; } }
    @keyframes lv { 50% { opacity: .35; } }
    button.act { border-color: var(--accent); color: var(--accent); }
    .lnk { font-size: 11.5px; color: var(--accent); cursor: pointer; white-space: nowrap; }
    .cm { position: relative; }
    .cmm { position: absolute; right: 0; top: calc(100% + 4px); z-index: 30; background: var(--surface); border: 1px solid var(--line); border-radius: 8px; box-shadow: 0 8px 24px rgba(0,0,0,.18); padding: 6px; display: flex; flex-direction: column; min-width: 170px;
      label { display: flex; align-items: center; gap: 8px; padding: 4px 6px; border-radius: 6px; font-size: 12px; white-space: nowrap; cursor: pointer; } label:hover { background: var(--accent-soft); } }
    .tse { margin: 0; border: 1px dashed var(--line); border-radius: 10px; padding: 18px 14px; text-align: center; line-height: 1.5; }
    .tw { border: 1px solid var(--line); border-radius: 10px; background: var(--surface); overflow: auto; }
    table.st { width: 100%; border-collapse: collapse; font-size: 12px; font-variant-numeric: tabular-nums;
      th { position: sticky; top: 0; z-index: 1; background: var(--surface); text-align: left; font-weight: 600; font-size: 11px; color: var(--muted); padding: 7px 10px; border-bottom: 1px solid var(--line); white-space: nowrap; cursor: pointer; user-select: none; }
      td { padding: 5px 10px; border-bottom: 1px solid var(--line); white-space: nowrap; }
      tbody tr:last-child td { border-bottom: 0; }
      tbody + tbody tr.grp td { border-top: 1px solid var(--line); }
      .n { text-align: right; }
      tr:not(.grp):hover td { background: color-mix(in srgb, var(--accent-soft) 50%, transparent); cursor: pointer; }
      tr.sel td { background: var(--accent-soft); }
      tr.down td { color: var(--muted); }
      tr.grp td { background: color-mix(in srgb, var(--line) 35%, var(--surface)); padding: 6px 10px; font-size: 12px;
        > * { vertical-align: middle; } .tg { cursor: pointer; display: inline-block; width: 14px; color: var(--muted); } .hd { display: inline-block; width: 9px; height: 9px; border-radius: 2px; margin: 0 6px 0 2px; } b { margin-right: 8px; } .gt { margin-left: 16px; font-variant-numeric: tabular-nums; } }
      .tp { color: var(--warning, #e39b17); font-weight: 600; }
      .bad { color: var(--danger, #d64545); font-weight: 600; }
      .lk { display: inline-block; padding: 0 7px; border-radius: 4px; font-size: 11px; background: var(--line); color: var(--muted); &.up { background: color-mix(in srgb, var(--success, #1fa45a) 16%, transparent); color: var(--success, #1fa45a); } }
      .sk { padding-top: 2px; padding-bottom: 2px; svg { display: block; } polyline { fill: none; stroke-width: 1.4; } .ktx { stroke: var(--s1); } .krx { stroke: var(--s2); stroke-dasharray: 3 2; } }
      .rs { font-size: 11px; &.pass { color: var(--success, #1fa45a); } &.fail { color: var(--danger, #d64545); font-weight: 600; } }
      .mono { font-family: var(--mono, monospace); font-size: 11.5px; } }
    .sub { display: flex; align-items: baseline; gap: 4px; font-size: 12.5px; margin-top: 4px; .x { margin-left: auto; cursor: pointer; color: var(--muted); } .x:hover { color: var(--danger); } }
    .muted { color: var(--muted); }
  `],
  host: { '(document:click)': 'colsOpen.set(false)' },
})
export class TrafficStats {
  private backend = inject(Backend);
  private ui = inject(Ui);
  readonly traf = input<EmuTrafficView | null>(null);
  readonly devs = input<LabDevice[]>([]);
  readonly hostUse = input<EmuHostUse[]>([]);
  /** The lab's host (a switch without its own). */
  readonly mainHost = input('');
  readonly hname = input<(h: string) => string>(h => h);
  readonly hcol = input<(h: string) => string>(() => 'var(--accent)');
  protected tr = tr;
  protected tab = signal<Tab>('ports');
  protected q = signal('');
  protected sel = signal('');
  protected selS = signal('');
  protected frozen = signal<EmuTrafficView | null>(null);
  protected base = signal<Base | null>(null);
  protected colsOpen = signal(false);
  protected cols = PORT_COLS;
  protected shown = signal(new Set(PORT_COLS.filter(c => c.on).map(c => c.k)));
  protected folded = signal(new Set<string>());
  protected sort = signal<{ k: string; dir: 1 | -1 } | null>(null);

  constructor() {
    // a new run starts its counters at 0: Clear counters no longer applies
    effect(() => { const st = this.traf()?.started ?? ''; untracked(() => { const b = this.base(); if (b && b.started !== st) this.base.set(null); }); });
    try { const c = localStorage.getItem('tw.stats.cols'); if (c) this.shown.set(new Set(JSON.parse(c) as string[])); } catch { /* the defaults */ }
  }

  /** What the tables show: the frozen numbers or the live ones. */
  protected view = computed(() => this.frozen() ?? this.traf());
  protected on(k: string) { return this.shown().has(k); }
  protected ncols = computed(() => { const s = this.shown(); return 2 + (s.has('link') ? 1 : 0) + ['rate', 'mbps', 'frames', 'bytes'].filter(k => s.has(k)).length * 2 + ['loss', 'lat', 'latmax', 'err', 'spark'].filter(k => s.has(k)).length; });
  protected toggleCol(k: string) {
    const s = new Set(this.shown()); if (s.has(k)) s.delete(k); else s.add(k); this.shown.set(s);
    try { localStorage.setItem('tw.stats.cols', JSON.stringify([...s])); } catch { /* not kept */ }
  }
  protected fold(h: string) { const s = new Set(this.folded()); if (s.has(h)) s.delete(h); else s.add(h); this.folded.set(s); }
  protected pick(id: string) { this.sel.set(this.sel() === id ? '' : id); this.selS.set(''); }
  protected sortBy(k: string) { const s = this.sort(); this.sort.set(s?.k === k ? (s.dir === 1 ? { k, dir: -1 } : null) : { k, dir: k === 'port' || k === 'sw' ? 1 : -1 }); }
  protected arrow(k: string) { const s = this.sort(); return s?.k === k ? (s.dir === 1 ? ' ▲' : ' ▼') : ''; }
  protected freeze() { this.frozen.set(this.frozen() ? null : this.traf()); }
  protected clear() {
    const v = this.traf(); if (!v) return;
    const ports: Record<string, EmuPortResult> = {}, streams: Record<string, EmuStreamResult> = {};
    for (const p of v.ports ?? []) ports[p.id] = p;
    for (const r of v.results ?? []) streams[r.id] = r;
    this.base.set({ at: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }), started: v.started ?? '', ports, streams });
    this.frozen.set(null);
  }
  protected liveText = computed(() => (this.frozen() ? tr('frozen') : this.traf()?.running ? tr('live · every second') : this.traf()?.started ? tr('the last run') : tr('no run yet')));
  protected liveTip = computed(() => (this.traf()?.started ? tr('The run started at {0}', new Date(this.traf()!.started!).toLocaleTimeString()) : ''));

  private dev(t: EmuTester) { return this.devs().find(d => d.id === t.vm); }
  protected nameOf(id: string) { return this.view()?.testers.find(t => t.id === id)?.name ?? id; }
  protected toText(s: EmuStream) { return s.kind === 'icmp' ? s.dstIp || tr('an address') : s.dstIp && /^(22[4-9]|23\d)\./.test(s.dstIp) ? s.dstIp : tr('every other port'); }
  protected frame(s: EmuStream) { return s.kind === 'pcap' ? 'pcap' : s.sizeMode === 'imix' ? 'IMIX' : s.sizeMode === 'inc' || s.sizeMode === 'random' ? `${s.size}-${s.sizeMax || s.size}` : String(s.size); }

  /** Each test port's row (counters since Clear counters). */
  protected ports = computed<PortRow[]>(() => {
    const v = this.view(), b = this.base(), ps = new Map((v?.ports ?? []).map(p => [p.id, p]));
    return (v?.testers ?? []).map(t => {
      const p = ps.get(t.id), o = b?.ports[t.id], d = this.dev(t), up = !!d && (d.running || d.state === 'running');
      const lagUp = !t.lag || (t.lacpUp ?? '').startsWith('up');
      const dl = (k: 'tx' | 'rx' | 'txBytes' | 'rxBytes' | 'expected' | 'got' | 'errors') => Math.max(0, (p?.[k] ?? 0) - (o?.[k] ?? 0));
      const exp = dl('expected'), got = Math.min(exp, dl('got'));
      const speed = t.speed ? (t.speed >= 1000 ? t.speed / 1000 + 'G' : t.speed + 'M') : '';
      return {
        t, host: (d?.host || this.mainHost()), sw: d?.hostname ?? '?', port: t.portName || String(t.port), up: up && lagUp,
        link: !up ? tr('down') : !lagUp ? tr('LACP') : [tr('up'), speed].filter(Boolean).join(' '), linkWhy: !d ? tr('The switch is not in the lab') : !up ? tr('The switch is not running') : !lagUp ? t.lacpUp || '' : '',
        txr: p?.txRate ?? 0, rxr: p?.rxRate ?? 0, txm: p?.txMbps ?? 0, rxm: p?.rxMbps ?? 0, tx: dl('tx'), rx: dl('rx'), txb: dl('txBytes'), rxb: dl('rxBytes'),
        exp, got, loss: exp ? ((exp - got) * 100) / exp : 0, lat: p?.latAvg ?? -1, latMax: p?.latMax ?? -1, err: dl('errors'), spark: this.spark(t.id),
      };
    });
  });
  private match(words: string[], hay: string) { const h = hay.toLowerCase(); return words.every(w => h.includes(w)); }
  private words = computed(() => this.q().toLowerCase().split(/\s+/).filter(Boolean));
  /** The ports by host (a virtual chassis each), searched and sorted. */
  protected groups = computed(() => {
    const w = this.words(), s = this.sort(), out = new Map<string, PortRow[]>();
    const streamsOf = (id: string) => (this.view()?.streams ?? []).filter(x => x.from === id || x.to === id).map(x => x.name).join(' ');
    for (const r of this.ports()) {
      if (w.length && !this.match(w, [r.t.name, r.sw, r.port, this.hname()(r.host), r.t.ip ?? '', r.t.mac, streamsOf(r.t.id)].join(' '))) continue;
      if (!out.has(r.host)) out.set(r.host, []);
      out.get(r.host)!.push(r);
    }
    const key = (r: PortRow): string | number => (s?.k === 'port' ? r.t.name : s?.k === 'sw' ? r.sw + ' ' + r.port.padStart(6, '0') : ((r as unknown as Record<string, number>)[s?.k ?? ''] ?? 0));
    return [...out.entries()].map(([host, rows]) => {
      if (s) rows = [...rows].sort((a, b) => { const x = key(a), y = key(b); return (typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y), undefined, { numeric: true })) * s.dir; });
      const u = this.hostUse().find(h => h.host === host);
      return { host, rows, txr: rows.reduce((a, r) => a + r.txr, 0), rxr: rows.reduce((a, r) => a + r.rxr, 0), cpu: u?.cpus ? `${u.load.toFixed(1)} / ${tr('{0} cores', u.cpus)}` : '' };
    }).sort((a, b) => (a.host === this.mainHost() ? -1 : b.host === this.mainHost() ? 1 : this.hname()(a.host).localeCompare(this.hname()(b.host))));
  });
  protected selRow = computed(() => this.ports().find(r => r.t.id === this.sel()) ?? null);

  /** Every stream's row (counters since Clear counters). */
  protected streams = computed<StreamRow[]>(() => {
    const v = this.view(), b = this.base(), rs = new Map((v?.results ?? []).map(r => [r.id, r]));
    return (v?.streams ?? []).map(s => {
      const r = rs.get(s.id) ?? ({ id: s.id, tx: 0, rx: 0, expected: 0, other: 0, loss: 0, latAvg: -1, latMax: -1, txRate: 0, rxRate: 0, running: false, result: '' } as EmuStreamResult), o = b?.streams[s.id];
      const d = (k: 'tx' | 'rx' | 'expected' | 'ooo' | 'dup') => Math.max(0, (r[k] ?? 0) - (o?.[k] ?? 0));
      const exp = d('expected'), rx = Math.min(exp || Infinity, d('rx'));
      return { s, r, tx: d('tx'), rx: d('rx'), exp, loss: exp ? ((exp - rx) * 100) / exp : 0, ooo: d('ooo'), dup: d('dup') };
    });
  });
  protected streamRows = computed(() => { const w = this.words(); return w.length ? this.streams().filter(x => this.match(w, [x.s.name, this.nameOf(x.s.from), x.s.to ? this.nameOf(x.s.to) : '', x.s.kind, x.s.dstIp ?? ''].join(' '))) : this.streams(); });
  /** The selected port's streams: what it sends and what is for it. */
  protected portStreams = computed(() => {
    const id = this.sel(); if (!id) return [];
    return this.streams().filter(x => x.s.from === id || x.s.to === id || (!x.s.to && x.s.kind !== 'icmp' && x.s.from !== id));
  });
  protected flowRows = computed<FlowRow[]>(() => {
    const w = this.words(), out: FlowRow[] = [];
    for (const x of this.streams()) for (const f of x.r.flows ?? []) if (!w.length || this.match(w, x.s.name + ' ' + f.name)) out.push({ s: x.s, f });
    return out;
  });
  protected flowsN = computed(() => this.streams().reduce((a, x) => a + (x.r.flowsN || x.r.flows?.length || 0), 0));
  protected flowsOf = computed(() => { const x = this.streams().find(y => y.s.id === this.selS()); return x?.r.flows?.length ? x : null; });

  private spark(id: string) {
    const h = this.view()?.portHistory?.[id]; if (!h || h.length < 2) return null;
    const mx = Math.max(1, ...h.map(p => Math.max(p[1], p[2]))), n = h.length - 1;
    const line = (k: 1 | 2) => h.map((p, i) => `${(i * 88 / n + 1).toFixed(1)},${(21 - p[k] * 19 / mx).toFixed(1)}`).join(' ');
    return { tx: line(1), rx: line(2) };
  }

  // numbers: whole numbers grouped by thin spaces, big counts as k / M / G, rates in pps / kpps / Mpps
  protected int(v: number) { return Math.round(v || 0).toLocaleString('en-US').replace(/,/g, ' '); }
  protected big(v: number) { return v < 100000 ? this.int(v) : v < 1e6 ? (v / 1e3).toFixed(1) + ' k' : v < 1e9 ? (v / 1e6).toFixed(1) + ' M' : (v / 1e9).toFixed(2) + ' G'; }
  protected bytes(v: number) { return v < 1024 ? v + ' B' : v < 1048576 ? (v / 1024).toFixed(1) + ' KB' : v < 1073741824 ? (v / 1048576).toFixed(1) + ' MB' : (v / 1073741824).toFixed(2) + ' GB'; }
  protected mb(v: number) { return v ? (v < 10 ? v.toFixed(2) : v.toFixed(1)) : '0'; }
  protected pct(v: number) { return v.toFixed(2) + ' %'; }
  protected ms(v: number) { return v >= 0 ? (v < 10 ? v.toFixed(2) : v.toFixed(1)) + ' ms' : '-'; }
  protected pps(v: number) { return v >= 1e6 ? (v / 1e6).toFixed(2) + ' Mpps' : v >= 1e3 ? (v / 1e3).toFixed(1) + ' kpps' : Math.round(v) + ' pps'; }
  protected lat3(r: EmuStreamResult) { return r.latAvg >= 0 ? `${(r.latMin ?? r.latAvg).toFixed(2)} / ${r.latAvg.toFixed(2)} / ${r.latMax.toFixed(2)} ms` : '-'; }

  /** The table on show as CSV (all its rows, the columns shown). */
  protected async exportCsv() {
    const esc = (x: string | number) => { const s = String(x); return /[",\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s; };
    let rows: (string | number)[][] = [], name = 'port-statistics.csv';
    if (this.tab() === 'ports') {
      const on = (k: string) => this.on(k);
      rows.push(['Host', 'Test port', 'Switch', 'Switch port', ...(on('link') ? ['Link'] : []), ...(on('rate') ? ['Tx fps', 'Rx fps'] : []), ...(on('mbps') ? ['Tx Mbit/s', 'Rx Mbit/s'] : []), 'Tx frames', 'Rx frames', 'Tx bytes', 'Rx bytes', 'Expected', 'Received', 'Loss %', 'Latency avg ms', 'Latency max ms', 'Errors']);
      for (const g of this.groups()) for (const r of g.rows) rows.push([this.hname()(g.host), r.t.name, r.sw, r.port, ...(on('link') ? [r.link] : []), ...(on('rate') ? [Math.round(r.txr), Math.round(r.rxr)] : []), ...(on('mbps') ? [r.txm, r.rxm] : []), r.tx, r.rx, r.txb, r.rxb, r.exp, r.got, r.exp ? r.loss.toFixed(3) : '', r.lat >= 0 ? r.lat : '', r.latMax >= 0 ? r.latMax : '', r.err]);
    } else if (this.tab() === 'streams') {
      name = 'stream-statistics.csv';
      rows.push(['Stream', 'From', 'To', 'Frame', 'Tx fps', 'Rx fps', 'Tx frames', 'Rx frames', 'Expected', 'Loss %', 'Latency min ms', 'Latency avg ms', 'Latency max ms', 'Jitter ms', 'Out of order', 'Duplicates', 'Result']);
      for (const x of this.streamRows()) rows.push([x.s.name, this.nameOf(x.s.from), x.s.to ? this.nameOf(x.s.to) : this.toText(x.s), this.frame(x.s), Math.round(x.r.txRate), Math.round(x.r.rxRate), x.tx, x.rx, x.exp, x.exp ? x.loss.toFixed(3) : '', x.r.latAvg >= 0 ? x.r.latMin ?? x.r.latAvg : '', x.r.latAvg >= 0 ? x.r.latAvg : '', x.r.latAvg >= 0 ? x.r.latMax : '', x.r.jitter ?? '', x.ooo, x.dup, x.r.result]);
    } else {
      name = 'flow-statistics.csv';
      rows.push(['Stream', 'Flow', 'Tx frames', 'Rx frames', 'Loss %', 'Latency avg ms', 'Latency max ms']);
      for (const x of this.flowRows()) rows.push([x.s.name, x.f.name, x.f.tx, x.f.rx, x.f.loss.toFixed(3), x.f.latAvg, x.f.latMax]);
    }
    rows = rows.map(r => r.map(c => (typeof c === 'number' ? c : esc(c))));
    try { const p = await this.backend.saveTextAs(tr('Export the statistics'), name, rows.map(r => r.join(',')).join('\n') + '\n'); if (p) this.ui.toast(tr('Saved'), 'success'); } catch (e) { this.ui.error(e); }
  }
}
