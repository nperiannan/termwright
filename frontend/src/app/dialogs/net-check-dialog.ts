import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { Tabs, TabView } from '../core/tabs.service';
import { Icon } from '../components/icon';
import { IperfInfo, IperfTest, NCResult } from '../core/models';
import { envInfo, envOf } from '../core/env';
import { tr } from '../core/i18n';

/** What a device types for ping and traceroute, by device type. Read-only commands.
 *  The 4th to 6th fields: the same in the management VR / VRF (tried when the
 *  default one does not answer - a switch's management port is often there). */
const DEVICE_CMDS: [RegExp, string, string, string?, string?, string?][] = [
  [/exos|switch engine/i, 'ping count 4 {ip}', 'traceroute {ip}', 'ping count 4 vr VR-Mgmt {ip}', 'traceroute vr VR-Mgmt {ip}', 'VR-Mgmt'],
  [/voss|fabric engine|vsp/i, 'ping {ip} count 4', 'traceroute {ip}', 'ping {ip} count 4 vrf MgmtRouter', 'traceroute {ip} vrf MgmtRouter', 'MgmtRouter'],
  [/boss/i, 'ping {ip} count 4', 'traceroute {ip}'],
  [/nx-?os/i, 'ping {ip} count 4', 'traceroute {ip}', 'ping {ip} count 4 vrf management', 'traceroute {ip} vrf management', 'management'],
  [/ios|cisco|arista|\beos\b/i, 'ping {ip} repeat 4', 'traceroute {ip}'],
  [/junos|juniper/i, 'ping {ip} count 4 rapid', 'traceroute {ip}'],
  [/linux|unix|ubuntu|debian|centos|rhel|server|bsd|mac/i, 'ping -c 4 -W 1 {ip}', 'traceroute -n -m 15 {ip}'],
];
/** The ping or traceroute a device types; mgmt = in its management VR / VRF ('' when it has none). */
export function deviceCommand(deviceType: string, ip: string, trace: boolean, mgmt = false): string {
  const m = DEVICE_CMDS.find(([re]) => re.test(deviceType || ''));
  if (mgmt) return m?.[3] ? (trace ? m[4]! : m[3]).replace('{ip}', ip) : '';
  return (m ? (trace ? m[2] : m[1]) : (trace ? 'traceroute {ip}' : 'ping {ip}')).replace('{ip}', ip);
}
/** The name of a device type's management VR / VRF ('' = none). */
export function mgmtVR(deviceType: string): string {
  return DEVICE_CMDS.find(([re]) => re.test(deviceType || ''))?.[5] ?? '';
}

/** The same host: equal ignoring case, a port or a trailing dot. */
export function sameHost(a: string, b?: string): boolean {
  const n = (x: string) => x.trim().toLowerCase().replace(/:\d+$/, '').replace(/\.$/, '');
  return !!b && n(a) === n(b);
}

/** A device's ping output: answered or not, the average time when it says. */
export function parseDevicePing(out: string): { state: 'ok' | 'bad' | 'na'; summary: string; ms: number } {
  const o = out.replace(/\r/g, '');
  let sent = -1, recv = -1, ms = 0;
  let m = /(\d+) (?:packets )?transmitted, (\d+) (?:packets )?received/i.exec(o);
  if (m) { sent = +m[1]; recv = +m[2]; }
  m = /Success rate is (\d+) percent \((\d+)\/(\d+)\)/i.exec(o);
  if (m) { recv = +m[2]; sent = +m[3]; }
  m = /(?:min\/avg\/max|round-trip)[^=]*=\s*[\d.]+\/([\d.]+)/i.exec(o);
  if (m) ms = +m[1];
  if (recv < 0 && /is alive|bytes from|!!!!/i.test(o)) { recv = 1; sent = 1; }
  if (recv < 0 && /no answer|timed out|unreachable|100% (?:packet )?loss|request timeout|\.\.\.\.\./i.test(o)) { recv = 0; sent = Math.max(sent, 4); }
  const t = ms ? (ms >= 10 ? `${Math.round(ms)} ms` : `${ms.toFixed(1)} ms`) : '';
  if (recv > 0) return { state: 'ok', ms, summary: (t || tr('answers')) + (sent > recv ? ` · ${recv} ${tr('of')} ${sent}` : '') };
  if (recv === 0 && (sent === 0 || /transmit error|no route/i.test(o))) return { state: 'bad', ms: 0, summary: tr('no route') };
  if (recv === 0) return { state: 'bad', ms: 0, summary: tr('0 of {0} answered', sent > 0 ? sent : 4) };
  return { state: 'na', ms: 0, summary: tr('no clear answer') };
}

/** The hops of a traceroute: one address (or *) per line that starts with a hop number. */
export function parseHops(out: string): string[] {
  const hops: string[] = [];
  for (const l of out.replace(/\r/g, '').split('\n')) {
    const m = /^\s*(\d{1,2})\s+(.*)$/.exec(l);
    if (!m) continue;
    const ip = /(\d{1,3}(?:\.\d{1,3}){3}|[0-9a-f]*:[0-9a-f:]+)/i.exec(m[2]);
    hops[+m[1] - 1] = ip ? ip[1] : '*';
  }
  return hops.filter(h => h !== undefined);
}

type Cell = NCResult & { vr?: string; running?: boolean; hops?: string[]; trace?: string; tracing?: boolean };
const SPECIAL = ['gateway', 'dns', 'internet'];
const LOCAL = 'local';

/**
 * Network Check: who can reach what - this computer and the open device tabs
 * against a list of targets (a matrix; a cell opens the path), and iPerf3.
 */
@Component({
  selector: 'app-net-check-dialog',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop" (click)="ui.close()">
    <div class="modal ncd" (click)="$event.stopPropagation()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="globe" [size]="17" /></span>
        <span class="mh-t">Network Check<span class="mh-s">{{ tab() === 'reach' ? tr('Who can reach what - this computer and the open device tabs') : tr('Throughput inside your network with iPerf3') }}</span></span></div>
      <div class="ntabs" role="tablist"><button type="button" role="tab" [class.on]="tab() === 'reach'" (click)="tab.set('reach')">{{ tr('Reachability') }}</button>
        <button type="button" role="tab" [class.on]="tab() === 'iperf'" (click)="tab.set('iperf'); loadIperf()">{{ tr('Speed (iPerf3)') }}</button></div>
      <div class="modal-body">
        @if (tab() === 'reach') {
          <div class="bar"><span class="muted">{{ tr('Targets') }}</span>
            <input class="tg mono" [(ngModel)]="targetsText" name="tg" spellcheck="false" (keydown.enter)="run()" [attr.aria-label]="tr('Targets')"
              [title]="tr('Addresses or names, separated by commas; host:port checks a TCP port. gateway, dns and internet are this computer’s own.')" />
            <button type="button" class="primary" (click)="run()" [disabled]="busy()">{{ busy() ? tr('Checking…') : tr('Run Checks') }}</button></div>
          <div class="srcs"><span class="muted small">{{ tr('From') }}</span><span class="chip on">{{ tr('This computer') }}</span>
            @for (t of termTabs(); track t.id) {
              <button type="button" class="chip" [class.on]="chosen().has(t.id)" (click)="toggle(t.id)" [title]="t.session.deviceType || ''">{{ t.title }}@if (isProd(t)) { <span class="prod">PROD</span> }</button>
            } @empty { <span class="muted small">{{ tr('Open a device tab to check from the device too') }}</span> }
          </div>
          <table class="mx">
            <thead><tr><th>{{ tr('Target') }}</th><th>{{ tr('This computer') }}</th>@for (s of sources(); track s.id) { <th>{{ s.title }} <small>{{ s.session.deviceType }}</small></th> }</tr></thead>
            <tbody>
              @for (t of targets(); track t) {
                <tr><td><b>{{ label(t) }}</b>@if (cell(LOCAL, t)?.address && cell(LOCAL, t)!.address !== t) { <small> {{ cell(LOCAL, t)!.address }}</small> }</td>
                  <td>@if (cell(LOCAL, t); as c) { <button type="button" class="cell" [class]="'cell ' + st(c)" [class.sel]="isSel(LOCAL, t)" (click)="select(LOCAL, t)">{{ mark(c) }} {{ c.summary }}</button> } @else { <span class="muted">–</span> }</td>
                  @for (s of sources(); track s.id) {
                    <td>@if (cell(s.id, t); as c) { <button type="button" [class]="'cell ' + st(c)" [class.sel]="isSel(s.id, t)" (click)="select(s.id, t)">{{ mark(c) }} {{ c.summary }}</button> } @else { <span class="muted">–</span> }</td>
                  }
                </tr>
              }
            </tbody>
          </table>
          @if (verdict()) { <div class="verdict">{{ verdict() }}</div> }
          @if (selected(); as s) {
            @if (cell(s.src, s.target); as c) {
              <div class="pathbox">
                <div class="ph"><b>{{ srcName(s.src) }} → {{ label(s.target) }}</b><span class="grow"></span>
                  @if (!special(s.target) || s.src === LOCAL) { <button type="button" (click)="trace(s.src, s.target)" [disabled]="!!c.tracing || c.running">{{ c.tracing ? tr('Tracing…') : tr('Traceroute') }}</button> }</div>
                <div class="path">
                  <div class="node"><b>{{ srcName(s.src) }}</b><small>{{ s.src === LOCAL ? tr('this computer') : tabOf(s.src)?.session?.hostname }}</small></div>
                  @for (h of c.hops ?? []; track $index; let last = $last) {
                    <div class="link" [class.bad]="h === '*'"></div>
                    <div class="node hop" [class.bad]="h === '*'"><b>{{ h === '*' ? tr('no answer') : h }}</b><small>{{ tr('hop {0}', $index + 1) }}</small></div>
                  }
                  <div class="link" [class.bad]="st(c) === 'bad'"><span>{{ c.ms ? c.summary : '' }}</span></div>
                  <div class="node" [class.bad]="st(c) === 'bad'" [class.okn]="st(c) === 'ok'"><b>{{ label(s.target) }}</b><small>{{ c.address || s.target }} · {{ c.summary }}</small></div>
                </div>
                @if (c.trace || c.detail) { <pre class="out">{{ c.trace ? c.trace : c.detail }}</pre> }
              </div>
            }
          }
        } @else {
          @if (ip(); as i) {
            @if (!i.path) {
              <div class="warnbox"><b>{{ tr('iperf3 is not installed on this computer.') }}</b> {{ tr('Install it, then open this tab again:') }}
                <code>{{ isMac ? 'brew install iperf3' : isWin ? 'winget install iperf3' : 'sudo apt install iperf3' }}</code></div>
            }
            <div class="ip">
              <div class="box"><h4><span class="dot" [class.on]="i.running"></span>{{ tr('iPerf3 server on this computer') }}<span class="grow"></span>
                @if (i.running) { <button type="button" (click)="stopServer()">Stop</button> } @else { <button type="button" class="primary" (click)="startServer()" [disabled]="!i.path">Start</button> }</h4>
                <div class="kv">{{ tr('Port') }} <input type="number" min="1" max="65535" [(ngModel)]="port" name="ipp" [disabled]="i.running" /> <span class="muted small">TCP</span>
                  <span class="muted small">{{ i.running ? tr('running on {0}', myIp() + ':' + i.port) : tr('stopped') }}</span></div>
                <div class="cmdline"><code>{{ clientCmd() }}</code>
                  @if (pasteTab(); as t) { <button type="button" class="primary" (click)="pasteCmd(t)" [disabled]="!i.running">{{ tr('Paste in') }} {{ t.title }}</button> }
                  <button type="button" (click)="copy(clientCmd())">Copy</button></div>
                <div class="muted small note">{{ tr('Run it on a Linux host or a device that has iperf3. iperf3 is its own program: the firewall may ask about it once. The server stops after 30 minutes without tests and when Termwright closes.') }}</div>
                @if (i.last; as l) { <pre class="out">{{ tr('Last test from {0}', l.peer) }}
{{ tr('sent') }}      {{ mbit(l.sentBps) }}   {{ l.seconds.toFixed(0) }} s
{{ tr('received') }}  {{ mbit(l.receivedBps) }}   {{ tr('retransmits') }} {{ l.retransmits }}</pre> }
                @if (i.error) { <div class="err small">{{ i.error }}</div> }
              </div>
              <div class="box"><h4>{{ tr('Test to a server') }}</h4>
                <div class="bar"><input class="mono grow" [(ngModel)]="server" name="ips" spellcheck="false" [placeholder]="tr('address of an iPerf3 server')" (keydown.enter)="client()" />
                  <select [(ngModel)]="secs" name="ipt"><option [ngValue]="5">5 s</option><option [ngValue]="10">10 s</option><option [ngValue]="30">30 s</option></select>
                  <select [(ngModel)]="both" name="ipb"><option [ngValue]="true">{{ tr('Both ways') }}</option><option [ngValue]="false">{{ tr('One way') }}</option></select>
                  <button type="button" class="primary" (click)="client()" [disabled]="!i.path || !server.trim() || testing()">{{ testing() ? tr('Testing…') : 'Start' }}</button></div>
                @if (results().length) {
                  <div class="res">
                    <div><small>{{ tr('This computer → server') }}</small><div class="big">{{ mbitN(results()[0].receivedBps || results()[0].sentBps) }} <small>Mbit/s</small></div></div>
                    @if (results()[1]; as d) { <div><small>{{ tr('Server → this computer') }}</small><div class="big">{{ mbitN(d.receivedBps || d.sentBps) }} <small>Mbit/s</small></div></div> }
                  </div>
                  <div class="muted small">{{ tr('Retransmits') }} {{ results()[0].retransmits }}</div>
                }
                @if (ipErr()) { <div class="err small">{{ ipErr() }}</div> }
              </div>
            </div>
            <div class="muted small note">{{ tr('A public internet speed test is left out on purpose: it measures your own line, not the devices.') }}</div>
          }
        }
      </div>
      <div class="modal-foot"><span class="muted small">{{ tr('Read-only: ping, DNS, TCP and traceroute. On a device, its own ping and traceroute are typed in its tab.') }}</span><span class="spacer"></span>
        @if (tab() === 'reach') { <button type="button" (click)="report()" [disabled]="!anyResult()">{{ tr('Copy Report') }}</button> }
        <button type="button" class="primary" (click)="ui.close()">Close</button></div>
    </div>
  </div>`,
  styles: [`
    .ncd { width: min(1000px, 96vw); }
    .ntabs { display: flex; gap: 4px; padding: 0 18px; border-bottom: 1px solid var(--line);
      button { border: 0; background: none; border-radius: 0; height: 36px; padding: 0 12px; color: var(--muted); border-bottom: 2px solid transparent; }
      button.on { color: var(--text); border-bottom-color: var(--accent); } }
    .bar { display: flex; gap: 8px; align-items: center; margin-bottom: 10px; select { width: auto; } }
    .tg { flex: 1; } .grow { flex: 1; min-width: 0; } .mono { font-family: var(--mono); }
    .srcs { display: flex; gap: 6px; align-items: center; flex-wrap: wrap; margin-bottom: 12px; }
    .chip { height: 26px; padding: 0 10px; border-radius: 999px; border: 1px solid var(--line); background: var(--surface); font-size: calc(12px * var(--ui-scale, 1)); display: inline-flex; align-items: center; gap: 6px; }
    .chip.on { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent); color: var(--text); }
    .prod { font-size: 9.5px; font-weight: 700; color: var(--danger); }
    .mx { width: 100%; border-collapse: separate; border-spacing: 0; border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
    .mx th, .mx td { padding: 8px 12px; border-top: 1px solid var(--line); text-align: left; font-size: calc(12.5px * var(--ui-scale, 1)); vertical-align: middle; }
    .mx thead th { border-top: 0; background: var(--surface-2); font-size: calc(12px * var(--ui-scale, 1)); font-weight: 600; }
    .mx small { color: var(--muted); font-weight: 400; }
    .cell { height: auto; display: inline-flex; gap: 6px; align-items: center; padding: 3px 10px; border-radius: 999px; font-weight: 600; font-size: calc(12px * var(--ui-scale, 1)); border: 1px solid transparent; background: var(--surface-2); color: var(--muted); }
    .cell.ok { color: var(--ok); background: color-mix(in srgb, var(--ok) 13%, transparent); }
    .cell.bad { color: var(--danger); background: color-mix(in srgb, var(--danger) 13%, transparent); }
    .cell.warn { color: var(--warn); background: color-mix(in srgb, var(--warn) 14%, transparent); }
    .cell.sel { border-color: currentColor; }
    .verdict { margin-top: 10px; font-size: calc(12.5px * var(--ui-scale, 1)); line-height: 1.5; }
    .pathbox { margin-top: 12px; border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; background: var(--surface-2); }
    .ph { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; button { height: 26px; } }
    .path { display: flex; align-items: stretch; flex-wrap: wrap; row-gap: 8px; }
    .node { border: 1px solid var(--line); border-radius: 10px; padding: 7px 10px; background: var(--surface); min-width: 110px; display: flex; flex-direction: column; gap: 2px;
      b { font-size: calc(12.5px * var(--ui-scale, 1)); } small { color: var(--muted); font-size: calc(11px * var(--ui-scale, 1)); } }
    .node.okn { border-color: color-mix(in srgb, var(--ok) 50%, var(--line)); } .node.bad { border-color: color-mix(in srgb, var(--danger) 55%, var(--line)); b { color: var(--danger); } }
    .link { width: 44px; position: relative; display: flex; align-items: center; justify-content: center;
      &::before { content: ''; position: absolute; left: 4px; right: 4px; top: 50%; height: 2px; background: var(--ok); }
      span { position: relative; background: var(--surface-2); padding: 0 3px; font-size: 10.5px; color: var(--muted); white-space: nowrap; } }
    .link.bad::before { background: repeating-linear-gradient(90deg, var(--danger) 0 5px, transparent 5px 9px); }
    .out { margin: 10px 0 0; padding: 8px 10px; border: 1px solid var(--line); border-radius: 8px; background: var(--input-bg); font: calc(11.5px * var(--ui-scale, 1)) / 1.5 var(--mono); white-space: pre-wrap; max-height: 220px; overflow: auto; user-select: text; }
    .ip { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
    .box { border: 1px solid var(--line); border-radius: 10px; padding: 12px; background: var(--surface-2); min-width: 0; }
    .box h4 { margin: 0 0 10px; font-size: calc(13px * var(--ui-scale, 1)); display: flex; align-items: center; gap: 8px; button { height: 26px; } }
    .dot { width: 9px; height: 9px; border-radius: 50%; background: var(--faint); } .dot.on { background: var(--ok); }
    .kv { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; input { width: 80px; } }
    .cmdline { display: flex; gap: 6px; align-items: center; code { flex: 1; min-width: 0; font: calc(12px * var(--ui-scale, 1)) var(--mono); padding: 6px 8px; border: 1px solid var(--line); border-radius: 6px; background: var(--input-bg); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } button { height: 28px; white-space: nowrap; } }
    .note { margin-top: 8px; line-height: 1.5; }
    .res { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin: 6px 0; .big { font-size: 28px; font-weight: 700; small { font-size: 13px; font-weight: 500; color: var(--muted); } } small { color: var(--muted); } }
    .warnbox { border-left: 3px solid var(--warn); background: color-mix(in srgb, var(--warn) 9%, transparent); padding: 8px 10px; border-radius: var(--r-sm); margin-bottom: 12px; font-size: calc(12.5px * var(--ui-scale, 1));
      code { font-family: var(--mono); margin-left: 6px; } }
    .err { color: var(--danger); margin-top: 6px; }
    @media (max-width: 760px) { .ip { grid-template-columns: 1fr; } }
  `],
})
export class NetCheckDialog implements OnInit, OnDestroy {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  protected readonly tr = tr;
  protected readonly LOCAL = LOCAL;
  readonly isMac = /mac/i.test(navigator.platform) || navigator.userAgent.includes('Mac OS');
  readonly isWin = /win/i.test(navigator.platform);

  readonly tab = signal<'reach' | 'iperf'>('reach');
  targetsText = 'gateway, dns, internet';
  readonly targets = signal<string[]>([]);
  readonly chosen = signal<Set<string>>(new Set());
  readonly cells = signal<Map<string, Cell>>(new Map());
  readonly busy = signal(false);
  readonly sel = signal<{ src: string; target: string } | null>(null);

  readonly termTabs = computed(() => this.tabs.tabs().filter(t => t.kind === 'terminal' && t.state === 'connected'));
  readonly sources = computed(() => this.termTabs().filter(t => this.chosen().has(t.id)));
  readonly pasteTab = computed(() => { const t = this.tabs.active(); return t && t.kind === 'terminal' && t.state === 'connected' ? t : null; });
  readonly selected = computed(() => this.sel());
  readonly anyResult = computed(() => this.cells().size > 0);

  // iPerf3
  readonly ip = signal<IperfInfo | null>(null);
  readonly results = signal<IperfTest[]>([]);
  readonly testing = signal(false);
  readonly ipErr = signal('');
  readonly myIp = signal('');
  port = 5201;
  server = '';
  secs = 10;
  both = true;
  private offs: (() => void)[] = [];

  ngOnInit() {
    const t = this.pasteTab();
    if (t) { this.chosen.set(new Set([t.id])); const h = t.session?.hostname; if (h) this.targetsText += ', ' + h; }
    this.targets.set(this.parse(this.targetsText));
    this.offs.push(this.backend.on('iperf:test', (x: IperfTest) => this.ip.update(i => i ? { ...i, last: x } : i)));
    this.offs.push(this.backend.on('iperf:state', (x: IperfInfo) => this.ip.set(x)));
  }
  ngOnDestroy() { this.offs.forEach(f => f()); }

  private parse(s: string) { return [...new Set(s.split(/[,\s]+/).map(x => x.trim()).filter(Boolean))].slice(0, 20); }
  special(t: string) { return SPECIAL.includes(t.toLowerCase()); }
  label(t: string) { const l = t.toLowerCase(); return l === 'gateway' ? tr('Gateway') : l === 'dns' ? tr('DNS') : l === 'internet' ? tr('Internet') : t; }
  key(src: string, t: string) { return src + '|' + t; }
  cell(src: string, t: string) { return this.cells().get(this.key(src, t)) ?? null; }
  st(c: Cell) { return c.running ? 'run' : c.state; }
  mark(c: Cell) { return c.running ? '…' : c.state === 'ok' ? '✓' : c.state === 'bad' ? '✕' : c.state === 'warn' ? '!' : '–'; }
  isSel(src: string, t: string) { const s = this.sel(); return !!s && s.src === src && s.target === t; }
  select(src: string, t: string) { this.sel.set(this.isSel(src, t) ? null : { src, target: t }); }
  tabOf(id: string) { return this.tabs.tabs().find(t => t.id === id) ?? null; }
  srcName(id: string) { return id === LOCAL ? tr('This computer') : this.tabOf(id)?.title ?? id; }
  isProd(t: TabView) { return !!envInfo(envOf(t.session.path, t.session.environment, this.ui.settings()?.folderDefaults))?.protect; }
  toggle(id: string) { const s = new Set(this.chosen()); if (s.has(id)) s.delete(id); else s.add(id); this.chosen.set(s); }
  private put(src: string, t: string, c: Cell) { const m = new Map(this.cells()); m.set(this.key(src, t), c); this.cells.set(m); }

  /** Who misses what that this computer reaches. */
  readonly verdict = computed(() => {
    const out: string[] = [];
    for (const t of this.targets()) {
      const mine = this.cell(LOCAL, t);
      for (const s of this.sources()) {
        const c = this.cell(s.id, t);
        if (c?.state === 'bad' && (mine?.state === 'ok' || mine?.state === 'warn')) out.push(tr('{0} cannot reach {1}, this computer can - look at the route on {0}’s way there (click the cell, then Traceroute).', s.title, this.label(t)));
      }
    }
    return out.slice(0, 3).join(' ');
  });

  async run() {
    const targets = this.parse(this.targetsText);
    if (!targets.length || this.busy()) return;
    const prod = this.sources().filter(t => this.isProd(t));
    if (prod.length && !(await this.ui.confirm(tr('Production devices'), tr('Ping is typed in {0} (production). It is read-only. Go on?', prod.map(t => t.title).join(', ')), tr('Run Checks')))) return;
    this.targets.set(targets); this.sel.set(null); this.busy.set(true);
    const m = new Map<string, Cell>();
    for (const t of targets) {
      m.set(this.key(LOCAL, t), { target: t, address: '', state: 'na', summary: '', ms: 0, detail: '', running: true });
      for (const s of this.sources()) m.set(this.key(s.id, t), this.special(t) ? { target: t, address: '', state: 'na', summary: '', ms: 0, detail: '' } : { target: t, address: '', state: 'na', summary: '', ms: 0, detail: '', running: true });
    }
    this.cells.set(m);
    const local = this.backend.netCheckLocal(targets).then(rs => rs.forEach(r => this.put(LOCAL, r.target, r)))
      .catch(e => targets.forEach(t => this.put(LOCAL, t, { target: t, address: '', state: 'bad', summary: errText(e), ms: 0, detail: '' })));
    const devices = this.sources().map(async s => {
      for (const t of targets) {
        if (this.special(t)) continue;
        const host = t.replace(/:\d+$/, '');
        const dt = s.session.deviceType ?? '';
        // a device does not ping its own address
        if (sameHost(host, s.session.hostname)) { this.put(s.id, t, { target: t, address: host, state: 'na', summary: tr('this device'), ms: 0, detail: '' }); continue; }
        let out = await this.runIn(s.id, deviceCommand(dt, host, false), 20000);
        let p = parseDevicePing(out), vr = '';
        // not in the default VR: try the management one (where a switch's management port is)
        const alt = p.state !== 'ok' ? deviceCommand(dt, host, false, true) : '';
        if (alt) {
          const out2 = await this.runIn(s.id, alt, 20000);
          const p2 = parseDevicePing(out2);
          if (p2.state === 'ok') { p = { ...p2, summary: p2.summary + ' · ' + mgmtVR(dt) }; vr = mgmtVR(dt); }
          out = out + '\n' + out2;
        }
        this.put(s.id, t, { target: t, address: host, state: p.state, summary: p.summary, ms: p.ms, detail: out, vr });
      }
    });
    await Promise.all([local, ...devices]);
    this.busy.set(false);
  }

  async trace(src: string, t: string) {
    const c = this.cell(src, t); if (!c) return;
    this.put(src, t, { ...c, tracing: true });
    let out = '';
    try {
      if (src === LOCAL) out = await this.backend.netCheckTrace(t);
      else out = await this.runIn(src, deviceCommand(this.tabOf(src)?.session.deviceType ?? '', t.replace(/:\d+$/, ''), true, !!c.vr), 60000);
    } catch (e) { out = errText(e); }
    const hops = parseHops(out);
    // the target itself is shown at the end of the path
    if (hops.length && hops[hops.length - 1] === (c.address || t)) hops.pop();
    this.put(src, t, { ...this.cell(src, t)!, tracing: false, trace: out, hops });
  }

  /** Types a command in a tab and returns the output once it settles. */
  private async runIn(id: string, cmd: string, max: number) {
    const before = this.tabs.bufferText(id).split('\n').length;
    await this.backend.sendInput(id, cmd + '\r');
    let last = '', changed = Date.now();
    const start = Date.now();
    while (Date.now() - start < max) {
      await new Promise(r => setTimeout(r, 400));
      const cur = this.tabs.bufferText(id);
      if (cur !== last) { last = cur; changed = Date.now(); continue; }
      if (Date.now() - changed > 2500 && Date.now() - start > 1500) break;
    }
    const lines = last.split('\n');
    return lines.slice(-Math.min(lines.length, Math.max(1, lines.length - before + 1))).join('\n');
  }

  async report() {
    const head = [tr('Target'), tr('This computer'), ...this.sources().map(s => s.title)];
    const rows = this.targets().map(t => [this.label(t), ...[LOCAL, ...this.sources().map(s => s.id)].map(src => { const c = this.cell(src, t); return c ? `${this.mark(c)} ${c.summary}`.trim() : '–'; })]);
    const w = head.map((h, i) => Math.max(h.length, ...rows.map(r => r[i].length)));
    const line = (r: string[]) => r.map((x, i) => x.padEnd(w[i])).join('  ');
    const text = [tr('Network Check') + ' - ' + new Date().toLocaleString(), '', line(head), ...rows.map(line), '', this.verdict()].join('\n').trim();
    await this.copy(text);
  }
  async copy(text: string) { await this.backend.clipboardSet(text); this.ui.toast(tr('Copied'), 'success'); }

  // ---- iPerf3 ----
  async loadIperf() {
    this.ip.set(await this.backend.iperfState().catch(() => ({ path: '', running: false, port: 5201 } as IperfInfo)));
    if (!this.myIp()) {
      const t = this.pasteTab();
      const a = await this.backend.fileServersAddresses(t?.session?.hostname ?? '').catch(() => []);
      this.myIp.set((a.find(x => x.reaches) ?? a[0])?.ip ?? '');
    }
    if (!this.server && this.pasteTab()?.session?.hostname) this.server = this.pasteTab()!.session.hostname!;
  }
  readonly clientCmd = computed(() => `iperf3 -c ${this.myIp() || '<this computer>'}${(this.ip()?.port ?? 5201) !== 5201 ? ' -p ' + this.ip()!.port : ''} -t 10`);
  async startServer() {
    this.ipErr.set('');
    try { this.ip.set(await this.backend.iperfServerStart(+this.port || 5201)); } catch (e) { this.ipErr.set(errText(e)); }
  }
  async stopServer() { await this.backend.iperfServerStop(); await this.loadIperf(); }
  pasteCmd(t: TabView) {
    void this.tabs.paste(t.id, this.clientCmd());
    this.ui.close(); this.tabs.focus(t.id);
    this.ui.toast(tr('Pasted - check it, then press Enter in the terminal'), 'info');
  }
  async client() {
    if (!this.server.trim() || this.testing()) return;
    this.testing.set(true); this.ipErr.set(''); this.results.set([]);
    try { this.results.set(await this.backend.iperfClient(this.server.trim(), this.secs, this.both)); } catch (e) { this.ipErr.set(errText(e)); }
    this.testing.set(false);
  }
  mbitN(bps: number) { const m = bps / 1e6; return m >= 100 ? Math.round(m).toString() : m.toFixed(1); }
  mbit(bps: number) { return this.mbitN(bps) + ' Mbit/s'; }
}
