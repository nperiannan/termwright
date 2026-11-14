import { Component, computed, effect, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { LabService } from '../core/lab.service';
import { Ui } from '../core/ui.service';
import { EmuRoutingView, LabDevice } from '../core/models';
import { tr } from '../core/i18n';

type App = 'ip' | 'ping' | 'traceroute' | 'iperf' | 'web' | 'dns' | 'capture' | 'routes';

/** A Linux computer of the emulated lab as a PC on a desk: its Desktop apps, its configuration and its ports. */
@Component({
  selector: 'app-pc-window',
  standalone: true,
  imports: [FormsModule],
  template: `
    <div class="back" (click)="closed.emit()">
      <div class="win" role="dialog" [attr.aria-label]="dev().hostname" (click)="$event.stopPropagation()">
        <div class="hd"><b>{{ dev().hostname }}</b><span class="muted">{{ dev().model }} {{ dev().version }} · <span [class.ok]="dev().running">{{ tr(dev().stateText || (dev().running ? 'running' : 'stopped')) }}</span></span><span class="sp"></span>
          <button type="button" class="sm x" (click)="closed.emit()" [attr.aria-label]="tr('Close')">✕</button></div>
        <div class="tabs">@for (t of tabs; track t.k) { <a [class.on]="tab() === t.k" (click)="tab.set(t.k)">{{ tr(t.name) }}</a> }</div>
        @if (tab() === 'desktop') {
          <div class="desk">
            <div class="apps">@for (a of apps; track a.k) {
              <a class="app" [class.on]="app() === a.k" (click)="pick(a.k)" [title]="tr(a.tip)"><svg viewBox="0 0 30 26" [attr.stroke]="a.c"><path [attr.d]="a.d"/></svg>{{ tr(a.name) }}</a> }
              <a class="app" (click)="console.emit()" [title]="tr('Its console in a tab: the shell of the computer')"><svg viewBox="0 0 30 26" stroke="#3ecf8e"><path d="M3 4h24v18H3zM8 10l4 3-4 3M14 17h7"/></svg>{{ tr('Command Prompt') }}</a></div>
            <div class="pane">
              @if (!dev().running) { <p class="warn">{{ tr('It is not running: start it, then its apps work. Its IP configuration can be set now; it is typed when it starts.') }}</p> }
              @switch (app()) {
                @case ('ip') {
                  <div class="ph"><b>{{ tr('IP Configuration') }}</b><span class="muted">{{ tr('typed on its console; kept for each start') }}</span></div>
                  <div class="f"><span>{{ tr('Interface') }}</span><select [ngModel]="port()" (ngModelChange)="port.set(+$event)">@for (p of ports(); track p) { <option [ngValue]="p">{{ tr('Port {0}', p) }}@if (linkOf(p)) { → {{ linkOf(p) }} }</option> }</select></div>
                  <div class="sec">IPv4</div>
                  <div class="f"><span></span><span class="r">@for (m of v4modes; track m) { <label><input type="radio" name="v4" [value]="m" [(ngModel)]="v4" /> {{ tr(m === 'dhcp' ? 'DHCP' : m === 'static' ? 'Static' : 'Off') }}</label> }</span>
                    @if (v4 === 'static') { <span>{{ tr('IPv4 Address') }}</span><input class="mono" [(ngModel)]="addr4" placeholder="10.10.0.11" />
                      <span>{{ tr('Subnet Mask') }}</span><input class="mono" [(ngModel)]="mask" placeholder="255.255.255.0" /> }
                    @if (v4 !== 'off') { <span>{{ tr('Default Gateway') }}</span><input class="mono" [(ngModel)]="gw" [placeholder]="tr('the management one')" />
                      <span>{{ tr('DNS Server') }}</span><input class="mono" [(ngModel)]="dns" [placeholder]="tr('as it is')" /> }</div>
                  <div class="sec">IPv6</div>
                  <div class="f"><span></span><span class="r">@for (m of v6modes; track m) { <label><input type="radio" name="v6" [value]="m" [(ngModel)]="v6" /> {{ tr(m === 'auto' ? 'Automatic' : m === 'static' ? 'Static' : 'Off') }}</label> }</span>
                    @if (v6 === 'static') { <span>{{ tr('IPv6 Address') }}</span><input class="mono" [(ngModel)]="addr6" placeholder="2001:db8:10::11/64" />
                      <span>{{ tr('IPv6 Gateway') }}</span><input class="mono" [(ngModel)]="gw6" /> }
                    @if (dev().net?.linkLocal?.[port()]; as ll) { <span>{{ tr('Link-local') }}</span><span class="mono muted">{{ ll }}</span> }</div>
                  <div class="act"><button type="button" class="primary sm" (click)="apply()" [disabled]="busy()">{{ busy() ? tr('Typing…') : tr('Apply') }}</button>
                    @if (dev().running && dev().net; as n) { <span [class.ok]="!!n.addrs?.[port()]" class="rb">{{ n.addrs?.[port()] ? '✓ ' + tr('it has {0}', n.addrs![port()]) : tr('no address on this port yet') }}@if (n.gw) { · {{ tr('gateway {0}', n.gw) }} }</span> }
                    @if (dev().running) { <a class="lnk" (click)="run('net', '')">↻ {{ tr('Read Again') }}</a> }</div>
                  @if (msg()) { <p class="err">{{ msg() }}</p> }
                }
                @case ('routes') {
                  <div class="ph"><b>{{ tr('Route Generator') }}</b><span class="muted">{{ tr('OSPF or BGP with the switch, advertising many routes (FRR)') }}</span></div>
                  <div class="f"><span>{{ tr('Protocol') }}</span><span class="r">@for (pr of ['ospf', 'bgp']; track pr) { <label><input type="radio" name="rproto" [value]="pr" [(ngModel)]="rt.proto" /> {{ pr.toUpperCase() }}</label> }</span>
                    <span>{{ tr('Interface') }}</span><select [(ngModel)]="rt.port">@for (p of ports(); track p) { <option [ngValue]="p">{{ tr('Port {0}', p) }}@if (dev().addrs?.[p]; as a) { · {{ a }} }@if (linkOf(p)) { → {{ linkOf(p) }} }</option> }</select>
                    <span>{{ tr('Routes') }}</span><input type="number" min="1" max="10000" [(ngModel)]="rt.routes" [title]="tr('Prefixes it advertises (1-10000)')" />
                    <span>{{ tr('First prefix') }}</span><input class="mono" [(ngModel)]="rt.prefix" placeholder="100.64.0.0/24" [title]="tr('The others follow it: 100.64.1.0/24, 100.64.2.0/24 …')" />
                    @if (rt.proto === 'ospf') { <span>{{ tr('Area') }}</span><input class="mono" [(ngModel)]="rt.area" placeholder="0.0.0.0" /> }
                    @else { <span>{{ tr('Neighbour') }}</span><input class="mono" [(ngModel)]="rt.peer" placeholder="10.20.0.1" [title]="tr('The switch’s address on that port')" />
                      <span>{{ tr('Its AS / the switch’s') }}</span><span class="two"><input type="number" [(ngModel)]="rt.as" placeholder="65001" /><input type="number" [(ngModel)]="rt.peerAs" placeholder="65000" /></span> }</div>
                  <div class="act"><button type="button" class="primary sm" (click)="setRoutes()" [disabled]="busy() || !dev().running">{{ busy() ? tr('Installing FRR…') : dev().routing ? tr('Apply') : tr('Start') }}</button>
                    @if (dev().routing) { <button type="button" class="sm" (click)="readRoutes()" [disabled]="busy()">↻ {{ tr('Read Again') }}</button><button type="button" class="sm danger-ghost" (click)="stopRoutes()" [disabled]="busy()">{{ tr('Stop') }}</button> }</div>
                  @if (rv(); as v) {
                    <div class="rst" [class.ok]="v.up"><b>{{ v.up ? '✓ ' : '' }}{{ v.proto.toUpperCase() }}: {{ v.state }}</b><span>{{ tr('{0} prefixes from {1}', v.prefixes || v.routes, v.prefix) }}</span></div>
                    <div class="go"><span class="muted">{{ tr('Traffic to every route: an ICMP stream from') }}</span><select [(ngModel)]="rtFrom"><option value="">{{ tr('a test port') }}</option>@for (t of testers(); track t.id) { <option [value]="t.id">{{ t.name }}</option> }</select>
                      <button type="button" class="sm" (click)="routeStream()" [disabled]="!rtFrom || busy()">＋ {{ tr('Stream') }}</button></div>
                  }
                  @if (msg()) { <p class="err">{{ msg() }}</p> }
                  <p class="muted xs">{{ tr('Its port needs a fixed address and the computer internet (to install FRR). The switch needs OSPF on that network, or the BGP neighbour, and a route back to the test port. The computer answers ping on every address of the routes.') }}</p>
                }
                @case ('web') {
                  <div class="ph"><b>{{ tr('Web Browser') }}</b><span class="muted">{{ tr('the page as this computer gets it') }}</span></div>
                  <div class="dsk"><svg viewBox="0 0 30 26" stroke="#7fb2ff"><path d="M3 4h24v16H3zM11 24h8M15 20v4"/></svg>
                    @if (dev().desktop) { <div><b>{{ tr('Firefox on its screen') }}</b><span class="muted">{{ dev().restart ? tr('restart it once for its mouse') : tr('click and type on its screen') }}</span></div><span class="sp"></span>
                      <button type="button" class="primary sm" (click)="screen.emit()" [disabled]="!dev().running">{{ tr('Open Screen…') }}</button><button type="button" class="sm danger-ghost" (click)="setDesktop(false)" [disabled]="busy()">{{ tr('Turn Off') }}</button> }
                    @else { <div><b>{{ tr('A real browser on its screen') }}</b><span class="muted">{{ tr('Firefox with a desktop: switch web interfaces as the computer sees them (needs internet and 1 GB)') }}</span></div><span class="sp"></span>
                      <input class="mono home" [(ngModel)]="home" [placeholder]="tr('first page')" /><button type="button" class="sm" (click)="setDesktop(true)" [disabled]="busy() || !dev().running">{{ busy() ? tr('Installing…') : tr('Set Up') }}</button> }
                  </div>
                  <div class="go"><input class="mono" [(ngModel)]="target" placeholder="http://10.10.0.12" (keydown.enter)="run('web', target)" /><button type="button" class="primary sm" (click)="run('web', target)" [disabled]="busy() || !dev().running">{{ tr('Go') }}</button>
                    <a class="lnk" (click)="src.set(!src())">{{ src() ? tr('Page') : tr('Source') }}</a></div>
                  @if (out()) { <div class="page" [class.mono]="src()">{{ src() ? out() : pageText() }}</div> }
                }
                @case ('iperf') {
                  <div class="ph"><b>iperf3</b><span class="muted">{{ tr('throughput to another computer for 5 s') }}</span></div>
                  <div class="go"><button type="button" class="sm" (click)="run('iperf-server', '')" [disabled]="busy() || !dev().running">{{ tr('Be the Server') }}</button><span class="muted">{{ tr('or test to a server:') }}</span>
                    <input class="mono" [(ngModel)]="target" placeholder="10.10.0.12" (keydown.enter)="run('iperf', target)" /><button type="button" class="primary sm" (click)="run('iperf', target)" [disabled]="busy() || !dev().running">{{ tr('Run') }}</button></div>
                  @if (out()) { <pre class="term">{{ out() }}</pre> }
                }
                @case ('capture') {
                  <div class="ph"><b>{{ tr('Capture') }}</b><span class="muted">{{ tr('its packets on a port, in the Captures tab') }}</span></div>
                  <div class="caps">@for (p of ports(); track p) { <button type="button" class="sm" (click)="capture.emit(p)" [disabled]="!linkOf(p)">● {{ tr('Port {0}', p) }}@if (linkOf(p)) { → {{ linkOf(p) }} } @else { · {{ tr('no cable') }} }</button> }</div>
                }
                @default {
                  <div class="ph"><b>{{ tr(appName()) }}</b></div>
                  <div class="go"><input class="mono" [(ngModel)]="target" [placeholder]="app() === 'dns' ? 'example.com' : '10.10.0.12'" (keydown.enter)="run(cmdOf(), target)" />
                    @if (app() === 'ping') { <label class="v6"><input type="checkbox" [(ngModel)]="six" /> IPv6</label> }
                    <button type="button" class="primary sm" (click)="run(cmdOf(), target)" [disabled]="busy() || !dev().running">{{ busy() ? tr('Running…') : tr('Run') }}</button></div>
                  @if (out()) { <pre class="term">{{ out() }}</pre> }
                }
              }
            </div>
          </div>
        }
        @if (tab() === 'config') {
          <div class="cfg"><div class="kv">
            <span>{{ tr('Name') }}</span><b>{{ dev().hostname }}</b>
            <span>{{ tr('Image') }}</span><span>{{ dev().model }} {{ dev().version }}</span>
            <span>{{ tr('Management') }}</span><span class="mono">{{ dev().ssh || dev().mgmtIp || '-' }}</span>
            <span>{{ tr('Console') }}</span><span class="mono">telnet :{{ dev().console }}</span>
            <span>{{ tr('Resources') }}</span><span>{{ dev().cpus }} vCPU · {{ dev().ram }} MB</span>
            <span>{{ tr('Gateway') }}</span><span class="mono">{{ dev().net?.gw || dev().dataGw || '-' }}@if (dev().net?.gw6 || dev().dataGw6) { · {{ dev().net?.gw6 || dev().dataGw6 }} }</span>
            <span>{{ tr('DNS Server') }}</span><span class="mono">{{ dev().net?.dns || dev().dns || '-' }}</span></div>
            <p class="muted">{{ tr('Name, resources and CPU are in its details (Configure…). The image logs in as root, names it and installs iperf3 on its first start.') }}</p></div>
        }
        @if (tab() === 'ports') {
          <div class="cfg"><table><tr><th>{{ tr('Port') }}</th><th>{{ tr('Cable') }}</th><th>IPv4</th><th>IPv6</th><th>{{ tr('It has') }}</th></tr>
            @for (p of ports(); track p) { <tr (click)="port.set(p); pick('ip'); tab.set('desktop')"><td>{{ p }}</td><td>{{ linkOf(p) || '-' }}</td><td class="mono">{{ dev().addrs?.[p] || '-' }}</td><td class="mono">{{ dev().addrs6?.[p] || '-' }}</td><td class="mono">{{ dev().net?.addrs?.[p] || '-' }}</td></tr> }</table>
            <p class="muted">{{ tr('A click on a port opens its IP Configuration.') }}</p></div>
        }
      </div>
    </div>
  `,
  styles: [`
    :host { position: absolute; width: 0; height: 0; }
    .back { position: fixed; inset: 0; background: rgba(0, 0, 0, .45); z-index: 900; display: flex; align-items: center; justify-content: center; }
    .win { width: min(980px, calc(100vw - 32px)); height: min(640px, calc(100vh - 48px)); background: var(--bg); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 20px 60px rgba(0, 0, 0, .5); display: grid; grid-template-rows: auto auto minmax(0, 1fr); overflow: hidden; }
    .hd { display: flex; gap: 10px; align-items: baseline; padding: 10px 14px; border-bottom: 1px solid var(--line); } .sp { flex: 1; }
    .tabs { display: flex; gap: 2px; padding: 6px 10px 0; border-bottom: 1px solid var(--line); a { padding: 6px 14px; border-radius: 6px 6px 0 0; cursor: pointer; color: var(--muted); } a.on { background: var(--surface); color: var(--text); } }
    .desk { display: grid; grid-template-columns: 236px minmax(0, 1fr); min-height: 0; }
    .apps { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; padding: 12px; border-right: 1px solid var(--line); align-content: start; overflow: auto; }
    .app { display: flex; flex-direction: column; align-items: center; gap: 4px; padding: 10px 4px; border: 1px solid var(--line); border-radius: 9px; background: var(--surface); cursor: pointer; font-size: 11.5px; text-align: center;
      svg { width: 30px; height: 26px; fill: none; stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; } }
    .app.on { outline: 2px solid var(--accent); }
    .pane { padding: 14px 18px; overflow: auto; min-height: 0; }
    .ph { display: flex; justify-content: space-between; align-items: baseline; margin-bottom: 10px; gap: 10px; }
    .f { display: grid; grid-template-columns: 130px minmax(0, 340px); gap: 7px 10px; align-items: center; font-size: 12.5px; }
    .r { display: flex; gap: 14px; label { display: flex; gap: 5px; align-items: center; } }
    .sec { margin: 14px 0 6px; font-weight: 600; font-size: 12px; }
    .act { display: flex; gap: 10px; align-items: center; margin-top: 14px; flex-wrap: wrap; } .rb { font-size: 12px; }
    .go { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; input.mono { flex: 1; min-width: 160px; } }
    .two { display: grid; grid-template-columns: 1fr 1fr; gap: 6px; } .xs { font-size: 11px; margin: 10px 0 0; line-height: 1.45; }
    .rst { display: flex; flex-direction: column; gap: 2px; margin: 10px 0 8px; padding: 8px 10px; border-radius: 9px; border: 1px solid var(--line); background: var(--surface-2); font-size: 12.5px; span { color: var(--muted); font-size: 11.5px; }
      &.ok { border-color: color-mix(in srgb, var(--success, #1fa45a) 55%, transparent); b { color: var(--success, #1fa45a); } } }
    .dsk { display: flex; align-items: center; gap: 10px; padding: 10px 12px; margin-bottom: 10px; border-radius: 10px; border: 1px solid color-mix(in srgb, var(--accent) 40%, var(--line)); background: color-mix(in srgb, var(--accent) 7%, transparent);
      svg { width: 30px; height: 26px; fill: none; stroke-width: 1.8; flex: none; } button { white-space: nowrap; flex: none; } div { display: flex; flex-direction: column; gap: 1px; font-size: 12.5px; span { font-size: 11.5px; } } .sp { flex: 1; } input.home { width: 170px; height: 28px; } }
    .term { background: #05080d; color: #b9f6ca; border-radius: 8px; padding: 10px; font: 12px/1.5 var(--mono, monospace); white-space: pre-wrap; margin-top: 12px; max-height: 360px; overflow: auto; }
    .page { margin-top: 12px; border: 1px solid var(--line); border-radius: 8px; padding: 12px; background: var(--surface); white-space: pre-wrap; max-height: 380px; overflow: auto; font-size: 12.5px; }
    .caps { display: flex; flex-direction: column; gap: 6px; align-items: flex-start; }
    .cfg { padding: 14px 18px; overflow: auto; table { width: 100%; border-collapse: collapse; font-size: 12.5px; } td, th { text-align: left; padding: 6px; border-bottom: 1px solid var(--line); } th { color: var(--muted); font-weight: 500; } tr:has(td) { cursor: pointer; } }
    .kv { display: grid; grid-template-columns: 110px 1fr; gap: 6px 10px; font-size: 12.5px; > span:nth-child(odd) { color: var(--muted); } }
    .muted { color: var(--muted); font-size: 12px; } .ok { color: var(--ok, #2fa46a); } .err, .warn { color: var(--danger); font-size: 12px; } .warn { color: var(--warn, #c98a12); margin-top: 0; }
    .lnk { cursor: pointer; color: var(--accent); font-size: 12px; } .x { margin-left: 6px; } .v6 { display: flex; gap: 4px; align-items: center; font-size: 12px; }
  `],
})
export class PcWindow {
  readonly dev = input.required<LabDevice>();
  /** Port → what its cable goes to ("leaf-1 5"). */
  readonly links = input<Record<number, string>>({});
  readonly closed = output<void>();
  readonly console = output<void>();
  readonly capture = output<number>();
  readonly screen = output<void>();
  readonly testers = input<{ id: string; name: string }[]>([]);
  private backend = inject(Backend);
  private lab = inject(LabService);
  private ui = inject(Ui);
  protected tr = tr;
  protected readonly tabs = [{ k: 'desktop', name: 'Desktop' }, { k: 'config', name: 'Config' }, { k: 'ports', name: 'Ports' }];
  protected readonly apps: { k: App; name: string; tip: string; c: string; d: string }[] = [
    { k: 'ip', name: 'IP Configuration', tip: 'Addresses, gateway and DNS of a port', c: '#7fb2ff', d: 'M3 4h24v16H3zM8 10h6M8 14h10M20 10h2' },
    { k: 'ping', name: 'Ping', tip: 'Four pings to an address', c: '#f2b52e', d: 'M5 13a4 4 0 1 0 8 0a4 4 0 1 0-8 0M17 13a4 4 0 1 0 8 0a4 4 0 1 0-8 0M13 13h4' },
    { k: 'traceroute', name: 'Traceroute', tip: 'The routers on the way to an address', c: '#f2b52e', d: 'M4 20l6-6 5 4 10-12' },
    { k: 'iperf', name: 'iperf3', tip: 'Throughput between two computers', c: '#c084fc', d: 'M4 18a11 11 0 0 1 22 0M15 18l5-7' },
    { k: 'web', name: 'Web Browser', tip: 'A web page as this computer gets it', c: '#7fb2ff', d: 'M6 13a9 9 0 1 0 18 0a9 9 0 1 0-18 0M6 13h18M15 4c4 4 4 14 0 18M15 4c-4 4-4 14 0 18' },
    { k: 'dns', name: 'DNS Lookup', tip: 'A name through its DNS server', c: '#9aa8bd', d: 'M7 12a6 6 0 1 0 12 0a6 6 0 1 0-12 0M18 17l6 6' },
    { k: 'capture', name: 'Capture', tip: 'Its packets on a port', c: '#e0603a', d: 'M3 13h5l3-7 4 14 3-7h9' },
    { k: 'routes', name: 'Route Generator', tip: 'OSPF or BGP with the switch, advertising many routes (FRR)', c: '#3ecf8e', d: 'M5 20h4v-4H5zM13 8h4V4h-4zM21 20h4v-4h-4zM7 16l7-8M23 16l-7-8' },
  ];
  protected rt: { proto: 'ospf' | 'bgp'; port: number; routes: number; prefix: string; area: string; peer: string; as: number | null; peerAs: number | null } = { proto: 'ospf', port: 1, routes: 1000, prefix: '100.64.0.0/24', area: '0.0.0.0', peer: '', as: null, peerAs: null };
  protected rv = signal<EmuRoutingView | null>(null);
  protected rtFrom = '';
  protected home = '';
  protected readonly v4modes = ['dhcp', 'static', 'off'];
  protected readonly v6modes = ['auto', 'static', 'off'];
  protected tab = signal('desktop');
  protected app = signal<App>('ip');
  protected port = signal(1);
  protected busy = signal(false);
  protected out = signal('');
  protected msg = signal('');
  protected src = signal(false);
  protected target = '';
  protected six = false;
  protected v4 = 'off'; protected addr4 = ''; protected mask = '255.255.255.0'; protected gw = ''; protected dns = '';
  protected v6 = 'off'; protected addr6 = ''; protected gw6 = '';
  protected ports = computed(() => Array.from({ length: Math.max(1, Math.min(this.dev().ports || 1, 16)) }, (_, i) => i + 1));
  protected appName = computed(() => this.apps.find(a => a.k === this.app())?.name ?? '');
  protected pageText = computed(() => {
    const h = this.out().replace(/^[^\n]*\n/, '');
    if (!/<[a-z!]/i.test(h)) return h;
    const d = new DOMParser().parseFromString(h, 'text/html');
    d.querySelectorAll('script,style').forEach(e => e.remove());
    return (d.title ? d.title + '\n\n' : '') + (d.body?.innerText ?? d.body?.textContent ?? '').replace(/\n{3,}/g, '\n\n').trim();
  });

  constructor() {
    let first = true;
    effect(() => { // the form shows the port's configuration (and the first port with one when it opens)
      const d = this.dev(), p = this.port();
      if (first) { first = false; const has = Object.keys({ ...(d.addrs ?? {}), ...(d.addrs6 ?? {}) }).map(Number).sort((a, b) => a - b)[0]; if (has && has !== p) { this.port.set(has); return; } }
      this.load(d, p);
    });
  }
  private loaded = '';
  private load(d: LabDevice, p: number) {
    const key = d.id + '|' + p + '|' + JSON.stringify([d.addrs?.[p], d.addrs6?.[p], d.dataGw, d.dataGw6, d.dns]);
    if (key === this.loaded) return;
    this.loaded = key;
    const a = d.addrs?.[p] ?? '', a6 = d.addrs6?.[p] ?? '';
    this.v4 = a === 'dhcp' ? 'dhcp' : a ? 'static' : 'off';
    if (this.v4 === 'static') { const [ip, bits] = a.split('/'); this.addr4 = ip; this.mask = maskOf(+bits || 24); } else this.addr4 = '';
    this.v6 = a6 === 'auto' ? 'auto' : a6 ? 'static' : 'off'; this.addr6 = this.v6 === 'static' ? a6 : '';
    this.gw = d.dataGw ?? ''; this.gw6 = d.dataGw6 ?? ''; this.dns = d.dns ?? '';
  }
  linkOf(p: number) { return this.links()[p] ?? ''; }
  pick(a: App) {
    this.app.set(a); this.out.set(''); this.msg.set('');
    const r = this.dev().routing;
    if (a === 'routes' && r) { this.rt = { proto: r.proto, port: r.port, routes: r.routes, prefix: r.prefix, area: r.area || '0.0.0.0', peer: r.peer ?? '', as: r.as || null, peerAs: r.peerAs || null }; void this.readRoutes(); }
  }
  async setRoutes() {
    this.msg.set(''); this.busy.set(true);
    const r = this.rt;
    try { this.rv.set(await this.backend.emuSetRouting(this.dev().id, { proto: r.proto, port: +r.port, routes: +r.routes, prefix: r.prefix.trim(), area: r.proto === 'ospf' ? r.area.trim() : '', as: +(r.as ?? 0) || 0, peer: r.proto === 'bgp' ? r.peer.trim() : '', peerAs: +(r.peerAs ?? 0) || 0 })); await this.lab.refresh(); }
    catch (e) { this.msg.set(String((e as Error)?.message ?? e)); } finally { this.busy.set(false); }
  }
  async readRoutes() { try { this.rv.set(await this.backend.emuRouting(this.dev().id)); } catch (e) { this.msg.set(String((e as Error)?.message ?? e)); } }
  async stopRoutes() {
    this.busy.set(true);
    try { await this.backend.emuSetRouting(this.dev().id, { proto: '' }); this.rv.set(null); await this.lab.refresh(); } catch (e) { this.msg.set(String((e as Error)?.message ?? e)); } finally { this.busy.set(false); }
  }
  async routeStream() {
    try { await this.backend.emuRoutingStream(this.dev().id, this.rtFrom, 1000); this.ui.toast(tr('Stream added on the Traffic page'), 'success'); } catch (e) { this.ui.error(e); }
  }
  async setDesktop(on: boolean) {
    this.busy.set(true);
    try { this.lab.emu.set(await this.backend.emuSetDesktop(this.dev().id, on, this.home.trim())); this.ui.toast(on ? tr('Desktop set up: restart it once for its mouse, then Open Screen') : tr('The desktop is off'), 'success'); }
    catch (e) { this.ui.error(e); } finally { this.busy.set(false); }
  }
  cmdOf() { return this.app() === 'ping' && this.six ? 'ping6' : this.app(); }
  async apply() {
    this.msg.set('');
    let v4 = '';
    if (this.v4 === 'dhcp') v4 = 'dhcp';
    if (this.v4 === 'static') { const b = bitsOf(this.mask.trim()); if (b < 0) { this.msg.set(tr('{0} is not a subnet mask', this.mask)); return; } v4 = this.addr4.trim().includes('/') ? this.addr4.trim() : this.addr4.trim() + '/' + b; }
    const v6 = this.v6 === 'auto' ? 'auto' : this.v6 === 'static' ? this.addr6.trim() : '';
    this.busy.set(true);
    try { await this.backend.emuSetComputerNet(this.dev().id, { port: this.port(), ipv4: v4, ipv6: v6, gw: this.v4 === 'off' ? '' : this.gw.trim(), gw6: this.v6 === 'static' ? this.gw6.trim() : '', dns: this.v4 === 'off' ? '' : this.dns.trim() }); await this.lab.refresh(); }
    catch (e) { this.msg.set(String((e as Error)?.message ?? e)); }
    finally { this.busy.set(false); }
  }
  async run(app: string, target: string) {
    this.busy.set(true); this.out.set(app === 'net' ? '' : tr('Running on its console…'));
    try { const o = await this.backend.emuComputerRun(this.dev().id, app, target); if (app === 'net') await this.lab.refresh(); else this.out.set(o); }
    catch (e) { if (app === 'net') this.ui.error(e); else this.out.set(String((e as Error)?.message ?? e)); }
    finally { this.busy.set(false); }
  }
}

function maskOf(bits: number) { const m = bits ? (0xffffffff << (32 - bits)) >>> 0 : 0; return [24, 16, 8, 0].map(s => (m >>> s) & 255).join('.'); }
/** The /bits of a subnet mask (or "/24", "24"), -1 when it is not one. */
function bitsOf(mask: string) {
  const b = mask.replace(/^\//, '');
  if (/^\d{1,2}$/.test(b) && +b <= 32) return +b;
  const p = mask.split('.').map(Number);
  if (p.length !== 4 || p.some(x => !(x >= 0 && x <= 255))) return -1;
  const n = ((p[0] << 24) | (p[1] << 16) | (p[2] << 8) | p[3]) >>> 0, bits = n.toString(2).replace(/0+$/, '');
  return /^1*$/.test(bits) ? bits.length : -1;
}
