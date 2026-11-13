import { Component, computed, input, output } from '@angular/core';
import { DecimalPipe } from '@angular/common';
import { LabDevice, LabLink } from '../core/models';
import { TableView, TvBar, TvHead } from '../core/table-view';
import { tr } from '../core/i18n';

/** What a row of the Overview does: show the device on the canvas, or one of its actions. */
export type OverviewAct = 'select' | 'console' | 'screen' | 'restart' | 'start' | 'stop';

/**
 * Overview: every device of the lab in one table - its state, kind, host, management address, CPU and memory
 * now, cables, checkpoints and what needs attention - searchable, sortable, with its actions.
 */
@Component({
  selector: 'app-lab-overview',
  imports: [DecimalPipe, TvBar, TvHead],
  template: `
    <section class="ovp" [attr.aria-label]="tr('Overview')">
      <header><div><h2>{{ tr('Overview') }}</h2><span class="muted small">{{ tr('Every device of the lab: its state, where it runs and what it uses') }}</span></div><span class="sp"></span>
        <div class="sum">@for (s of sums(); track s.k) { <span [class]="s.c"><b>{{ s.n }}</b> {{ tr(s.k) }}</span> }</div></header>
      <app-tv-bar [tv]="tv" />
      <div class="tw"><table>
        <tr><th tvh [tv]="tv" key="name" filter="text">{{ tr('Device') }}</th><th tvh [tv]="tv" key="state">{{ tr('State') }}</th><th tvh [tv]="tv" key="kind">{{ tr('Kind') }}</th><th tvh [tv]="tv" key="host">{{ tr('Host') }}</th>
          <th tvh [tv]="tv" key="mgmt" filter="text">{{ tr('Management') }}</th><th tvh [tv]="tv" key="cpu" filter="none" class="n">CPU</th><th tvh [tv]="tv" key="mem" filter="none" class="n">{{ tr('Memory') }}</th>
          <th tvh [tv]="tv" key="cables" filter="none" class="n">{{ tr('Cables') }}</th><th tvh [tv]="tv" key="ck" filter="none" class="n">{{ tr('Checkpoints') }}</th><th tvh [tv]="tv" key="att">{{ tr('Attention') }}</th><th></th></tr>
        @for (d of tv.rows(); track d.id) {
          <tr (click)="act.emit({ what: 'select', dev: d })" [class.off]="!d.running">
            <td><b>{{ d.hostname }}</b></td>
            <td><span class="st" [class]="'st ' + stOf(d)"><i></i>{{ tr(d.stateText || (d.running ? 'running' : 'stopped')) }}</span></td>
            <td class="small">{{ kindOf(d) }}</td><td class="small">{{ hostName(d) }}</td><td class="mono small">{{ (d.mgmtIp || '').split('/')[0] || '-' }}</td>
            <td class="n mono small">@if (d.running) { {{ d.cpu ?? 0 | number: '1.0-0' }} % } @else { - }</td>
            <td class="n mono small">@if (d.running && d.memUsed) { {{ gb(d.memUsed) }} / }{{ d.ram ? gb(d.ram) : '-' }}</td>
            <td class="n mono small">{{ cables(d.id) }}</td><td class="n mono small">{{ d.checkpoints?.length || '-' }}</td>
            <td class="small att">@if (d.error) { <span class="er" [title]="d.error">⚠ {{ d.error }}</span> } @else if (d.restart) { <span class="wr" [title]="d.restart">↻ {{ tr('restart needed') }}</span> } @else if (d.personaError) { <span class="wr">{{ d.personaError }}</span> }</td>
            <td class="acts" (click)="$event.stopPropagation()">
              @if (d.running) { <a (click)="act.emit({ what: 'console', dev: d })" [title]="tr('Console')">⌨</a><a (click)="act.emit({ what: 'screen', dev: d })" [title]="tr('Screen…')">▣</a><a (click)="act.emit({ what: 'restart', dev: d })" [title]="tr('Restart')">⟳</a><a (click)="act.emit({ what: 'stop', dev: d })" [title]="tr('Stop')">■</a> }
              @else { <a (click)="act.emit({ what: 'start', dev: d })" [title]="tr('Start')">▶</a> }</td>
          </tr>
        } @empty { <tr><td colspan="11" class="muted">{{ devs().length ? tr('No device matches the search or the filters.') : tr('No devices yet.') }}</td></tr> }
      </table></div>
    </section>`,
  styles: [`
    :host { display: contents; }
    .ovp { position: absolute; inset: 0; z-index: 25; background: var(--bg); overflow: auto; padding: 16px 20px 20px; display: flex; flex-direction: column; gap: 10px; }
    header { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; h2 { margin: 0; font-size: 17px; } > div:first-child { display: flex; flex-direction: column; gap: 2px; } }
    .sp { flex: 1; } .muted { color: var(--muted); } .small { font-size: 11.5px; } .mono { font-family: var(--mono, monospace); }
    .sum { display: flex; gap: 8px; span { border: 1px solid var(--line); border-radius: 9px; padding: 3px 10px; font-size: 12px; background: var(--surface); } b { font-variant-numeric: tabular-nums; } .ok b { color: var(--success, #1fa45a); } .w b { color: var(--warning, #e39b17); } .e b { color: var(--danger); } }
    .tw { border: 1px solid var(--line); border-radius: 10px; background: var(--surface); overflow: auto; }
    table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
    th { position: sticky; top: 0; background: var(--surface-2); text-align: left; font-weight: 500; padding: 6px 10px; color: var(--muted); font-size: 11px; text-transform: uppercase; letter-spacing: .03em; white-space: nowrap; }
    td { padding: 6px 10px; border-top: 1px solid var(--line); white-space: nowrap; } tr { cursor: pointer; } tr:hover td { background: color-mix(in srgb, var(--accent) 6%, transparent); } tr.off td { color: var(--muted); }
    .n { text-align: right; font-variant-numeric: tabular-nums; }
    .st { display: inline-flex; align-items: center; gap: 6px; font-size: 11.5px; i { width: 7px; height: 7px; border-radius: 50%; background: var(--muted); } &.run i { background: var(--success, #1fa45a); } &.boot i { background: var(--warning, #e39b17); } &.err i { background: var(--danger); } }
    .att { max-width: 280px; overflow: hidden; text-overflow: ellipsis; .er { color: var(--danger); } .wr { color: var(--warning, #e39b17); } }
    .acts { text-align: right; a { cursor: pointer; color: var(--muted); margin-left: 10px; font-size: 13px; } a:hover { color: var(--text); } }`],
})
export class LabOverview {
  readonly devs = input<LabDevice[]>([]);
  readonly links = input<LabLink[]>([]);
  readonly hostNames = input<Record<string, string>>({});
  readonly act = output<{ what: OverviewAct; dev: LabDevice }>();
  protected tr = tr;
  protected tv = new TableView<LabDevice>(() => this.devs(), [
    { key: 'name', label: 'Device', get: d => d.hostname },
    { key: 'state', label: 'State', get: d => tr(d.stateText || (d.running ? 'running' : 'stopped')) },
    { key: 'kind', label: 'Kind', get: d => this.kindOf(d) },
    { key: 'host', label: 'Host', get: d => this.hostName(d) },
    { key: 'mgmt', label: 'Management', get: d => (d.mgmtIp || '').split('/')[0] },
    { key: 'cpu', label: 'CPU', get: d => (d.running ? d.cpu ?? 0 : null), kind: 'num' },
    { key: 'mem', label: 'Memory', get: d => d.ram ?? 0, kind: 'num' },
    { key: 'cables', label: 'Cables', get: d => this.cableN(d.id), kind: 'num' },
    { key: 'ck', label: 'Checkpoints', get: d => d.checkpoints?.length ?? 0, kind: 'num' },
    { key: 'att', label: 'Attention', get: d => (d.error ? tr('problem') : d.restart ? tr('restart needed') : '') },
  ]);
  protected sums = computed(() => {
    const ds = this.devs(), run = ds.filter(d => d.running).length, err = ds.filter(d => !!d.error).length, rs = ds.filter(d => !!d.restart).length;
    return [{ k: 'running', n: run, c: 'ok' }, { k: 'stopped', n: ds.length - run, c: '' }, ...(rs ? [{ k: 'restart needed', n: rs, c: 'w' }] : []), ...(err ? [{ k: 'with a problem', n: err, c: 'e' }] : [])];
  });
  protected stOf(d: LabDevice) { return d.error ? 'err' : d.running && (d.state === 'running' || !d.state) ? 'run' : d.running ? 'boot' : ''; }
  protected kindOf(d: LabDevice) { return [d.computer ? 'PC' : d.model, d.version].filter(Boolean).join(' · '); }
  protected hostName(d: LabDevice) { const h = d.host || ''; return this.hostNames()[h] ?? (h.split('/').pop() || '-'); }
  private cableN(id: string) { return this.links().filter(k => k.a === id || k.b === id).length; }
  protected cables(id: string) { return this.cableN(id) || '-'; }
  protected gb(mb: number) { return mb >= 1024 ? (Math.round((mb / 1024) * 10) / 10) + ' GB' : mb + ' MB'; }
}
