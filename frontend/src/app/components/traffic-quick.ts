import { Component, OnInit, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { EmuRadiusUser, EmuStream, EmuTest, EmuTester, EmuTrafficView } from '../core/models';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';

export type QuickKey = 'l2' | 'l3' | 'custom' | 'adv' | 'latency' | 'jitter' | 'load' | 'rfc2544' | 'bad' | 'fuzz' | 'vlaniso' | 'mtu' | 'dot1x' | 'dhcp6' | 'vrrp' | 'bfd' | 'tcpsess' | 'tcpblock';

const ITEMS: { k: QuickKey; name: string; what: string; group: string }[] = [
  { k: 'l2', name: 'L2 stream', what: 'Same VLAN, switched', group: 'Streams' },
  { k: 'l3', name: 'L3 stream', what: 'Between subnets, routed', group: 'Streams' },
  { k: 'custom', name: 'Custom frame', what: 'Your bytes, in hex', group: 'Streams' },
  { k: 'adv', name: 'Every field', what: 'The full stream editor', group: 'Streams' },
  { k: 'latency', name: 'Latency', what: 'How long each frame takes', group: 'Tests' },
  { k: 'jitter', name: 'Jitter', what: 'Steady delay, as voice needs', group: 'Tests' },
  { k: 'load', name: 'Loss under load', what: 'No frame lost at a rate', group: 'Tests' },
  { k: 'rfc2544', name: 'Throughput (RFC 2544)', what: 'The highest rate with no loss', group: 'Tests' },
  { k: 'bad', name: 'Bad packets', what: 'Faulty frames dropped, good ones not', group: 'Tests' },
  { k: 'fuzz', name: 'Fuzz', what: 'Random bytes; the switch keeps forwarding', group: 'Tests' },
  { k: 'vlaniso', name: 'VLAN isolation', what: 'No frame crosses VLANs', group: 'Tests' },
  { k: 'mtu', name: 'MTU and jumbo frames', what: 'Large frames forwarded or dropped', group: 'Tests' },
  { k: 'dot1x', name: '802.1X clients', what: 'Many users log in by RADIUS', group: 'Tests' },
  { k: 'tcpsess', name: 'TCP sessions', what: 'Real connections: setup time, connections a second', group: 'Tests' },
  { k: 'tcpblock', name: 'Blocked TCP port (ACL)', what: 'Connections refused or dropped', group: 'Tests' },
  { k: 'dhcp6', name: 'DHCPv6 clients', what: 'Many clients take IPv6 addresses', group: 'Tests' },
  { k: 'vrrp', name: 'VRRP failover', what: 'The switch takes over the virtual IP', group: 'Tests' },
  { k: 'bfd', name: 'BFD failure detection', what: 'The switch sees a dead link in time', group: 'Tests' },
];

/** The faults a stream can put in its frames (as BadFrames in internal/emu/traffic.go): who should drop them. */
export const BAD_FRAMES: { k: string; name: string; router: boolean; sw: boolean; tcp?: boolean }[] = [
  { k: 'ipcsum', name: 'Bad IP checksum', router: true, sw: false }, { k: 'l4csum', name: 'Bad UDP/TCP checksum', router: false, sw: false },
  { k: 'iplen-long', name: 'IP length too long', router: true, sw: false }, { k: 'iplen-short', name: 'IP length too short', router: true, sw: false },
  { k: 'ihl', name: 'IP header too short', router: true, sw: false }, { k: 'ipver', name: 'Wrong IP version', router: true, sw: false },
  { k: 'ttl0', name: 'TTL 0', router: true, sw: false }, { k: 'ttl1', name: 'TTL 1', router: true, sw: false },
  { k: 'ipopts', name: 'IP options', router: false, sw: false }, { k: 'frag-first', name: 'First fragment only', router: false, sw: false },
  { k: 'frag-tiny', name: 'Tiny fragment', router: false, sw: false }, { k: 'src-martian', name: 'Martian source (127.0.0.1)', router: true, sw: false },
  { k: 'src-bcast', name: 'Broadcast source', router: true, sw: false }, { k: 'land', name: 'LAND (source = destination)', router: false, sw: false },
  { k: 'tcp-synfin', name: 'TCP SYN+FIN', router: false, sw: false, tcp: true }, { k: 'tcp-null', name: 'TCP no flags', router: false, sw: false, tcp: true },
  { k: 'tcp-xmas', name: 'TCP Xmas', router: false, sw: false, tcp: true }, { k: 'smac-mcast', name: 'Multicast source MAC', router: true, sw: true },
  { k: 'smac-zero', name: 'Zero source MAC', router: false, sw: false }, { k: 'runt', name: 'Runt frame (48 bytes)', router: true, sw: true },
  { k: 'vlan4095', name: 'VLAN 4095', router: true, sw: true }, { k: 'qinq', name: 'Two VLAN tags', router: false, sw: false },
  { k: 'etype', name: 'Unknown EtherType', router: false, sw: false },
];

/** A row of "What Termwright fills in": a stream (or test) to create, editable before it is. */
interface Row { s: EmuStream; open?: boolean; test?: EmuTest }
interface PortChange { t: EmuTester; set: Partial<EmuTester>; text: string }

/**
 * Quick streams and tests: choose what to check, answer the few questions only you know (which test ports, the
 * VLAN on the switch port, whether the switch routes), and Termwright fills in the rest - shown in a table to
 * change before anything is created.
 */
@Component({
  selector: 'app-traffic-quick',
  imports: [FormsModule],
  template: `
    <div class="ov" (click)="closed.emit()">
      <div class="dl" role="dialog" [attr.aria-label]="tr('Quick streams and tests')" (click)="$event.stopPropagation()">
        <div class="dh"><b>{{ tr('Quick streams and tests') }}</b><span class="muted">{{ tr('Answer a few questions; Termwright fills in the rest, and you check it before it runs') }}</span><span class="sp"></span><a class="x" (click)="closed.emit()" [title]="tr('Close')">✕</a></div>
        <div class="db">
          <nav class="ls">
            @for (g of groups; track g) {
              <div class="gh">{{ tr(g) }}</div>
              @for (it of itemsOf(g); track it.k) { <a [class.on]="key() === it.k" (click)="choose(it.k)"><b>{{ tr(it.name) }}</b><small>{{ tr(it.what) }}</small></a> }
            }
            <a class="lib" (click)="library.emit()">{{ tr('More ready tests in the Library…') }}</a>
          </nav>
          <section class="dt">
            <h3>{{ tr(cur().name) }}</h3>
            <p class="desc">{{ tr(help[key()]) }}</p>
            @if (testers().length < 2 && !single()) { <p class="warn">⚠ {{ tr('It needs two test ports: add them with ＋ Test Port (on two switch ports), then come back.') }}</p> }
            <div class="qs">
              <div class="q"><span>{{ tr(single() ? 'Test port' : 'From') }}</span><select [(ngModel)]="a" (ngModelChange)="fill()">@for (t of testers(); track t.id) { <option [value]="t.id">{{ t.name }} · {{ t.portName || t.port }}</option> }</select>
                @if (!single()) { <span class="to">→</span><select [(ngModel)]="b" (ngModelChange)="fill()">@for (t of testers(); track t.id) { @if (t.id !== a) { <option [value]="t.id">{{ t.name }} · {{ t.portName || t.port }}</option> } }</select> }</div>
              @if (key() === 'l2' || key() === 'latency' || key() === 'jitter' || key() === 'load' || key() === 'rfc2544' || key() === 'fuzz' || key() === 'mtu' || (key() === 'bad' && path === 'l2')) {
                <div class="q"><span>{{ tr('The switch ports carry') }}</span><span class="seg"><i [class.on]="!tagged" (click)="tagged = false; fill()">{{ tr('untagged frames (access port)') }}</i><i [class.on]="tagged" (click)="tagged = true; fill()">{{ tr('VLAN tagged frames') }}</i></span>
                  @if (tagged) { <input class="n" type="number" min="1" max="4094" [(ngModel)]="vlan" (ngModelChange)="fill()" [title]="tr('The VLAN ID tagged on the switch ports')" /> }</div>
              }
              @if (key() === 'bad' || key() === 'tcpsess' || key() === 'tcpblock') {
                <div class="q"><span>{{ tr('The path is') }}</span><span class="seg"><i [class.on]="path === 'l2'" (click)="path = 'l2'; fill()">{{ tr('switched (same VLAN)') }}</i><i [class.on]="path === 'l3'" (click)="path = 'l3'; fill()">{{ tr('routed (two subnets)') }}</i></span></div>
              }
              @if (key() === 'l3' || ((key() === 'bad' || key() === 'tcpsess' || key() === 'tcpblock') && path === 'l3')) {
                <div class="q"><span>{{ tr('IP interfaces on the switch') }}</span><span class="seg"><i [class.on]="routed" (click)="routed = true; fill()">{{ tr('configured') }}</i><i [class.on]="!routed" (click)="routed = false; fill()">{{ tr('not yet') }}</i></span></div>
                <div class="q two"><span>{{ nameOf(a) }}</span><input [(ngModel)]="ipA" (ngModelChange)="fill()" [title]="tr('Its address, with the prefix')" /><span class="mut">{{ tr('gateway') }}</span><input [(ngModel)]="gwA" (ngModelChange)="fill()" [title]="tr('The switch\\'s IP interface in that subnet')" /></div>
                <div class="q two"><span>{{ nameOf(b) }}</span><input [(ngModel)]="ipB" (ngModelChange)="fill()" /><span class="mut">{{ tr('gateway') }}</span><input [(ngModel)]="gwB" (ngModelChange)="fill()" /></div>
                @if (!routed) { <p class="note">{{ tr('The switch routes between the two subnets only with an IP interface in each, at the gateway addresses above, and routing on. Add them on the switch, then start the stream.') }}</p> }
              }
              @if (key() === 'latency') { <div class="q"><span>{{ tr('Pass when latency is at most') }}</span><input class="n" type="number" min="0" step="0.1" [(ngModel)]="latMax" (ngModelChange)="fill()" /><span class="mut">ms</span><label class="cb"><input type="checkbox" [(ngModel)]="rtt" (ngModelChange)="fill()" /> {{ tr('round trip (test ports on two hosts)') }}</label></div> }
              @if (key() === 'jitter') { <div class="q"><span>{{ tr('Pass when jitter is at most') }}</span><input class="n" type="number" min="0" step="0.1" [(ngModel)]="jitMax" (ngModelChange)="fill()" /><span class="mut">ms</span></div> }
              @if (key() === 'load') { <div class="q"><span>{{ tr('Rate') }}</span><input class="n" type="number" min="1" max="100000" [(ngModel)]="pps" (ngModelChange)="fill()" /><span class="mut">{{ tr('frames a second; an emulated switch forwards a few thousand') }}</span><label class="cb"><input type="checkbox" [(ngModel)]="both" (ngModelChange)="fill()" /> {{ tr('both ways') }}</label></div> }
              @if (key() === 'rfc2544') { <div class="q"><span>{{ tr('Highest rate tried') }}</span><input class="n" type="number" min="100" max="100000" [(ngModel)]="maxPps" (ngModelChange)="fill()" /><span class="mut">{{ tr('frames a second') }}</span></div> }
              @if (key() === 'bad') {
                <div class="q top"><span>{{ tr('Faults') }}</span><div class="chips">@for (f of badList(); track f.k) { <label class="chip" [class.on]="faults.has(f.k)"><input type="checkbox" [ngModel]="faults.has(f.k)" (ngModelChange)="toggle(f.k)" />{{ tr(f.name) }}</label> }</div></div>
              }
              @if (key() === 'fuzz') {
                <div class="q top"><span>{{ tr('Fuzz') }}</span><div class="chips">@for (z of fuzzParts; track z.k) { <label class="chip" [class.on]="parts.has(z.k)"><input type="checkbox" [ngModel]="parts.has(z.k)" (ngModelChange)="togglePart(z.k)" />{{ tr(z.name) }}</label> }</div></div>
                <div class="q"><span>{{ tr('Seed') }}</span><input class="n" type="number" min="1" [(ngModel)]="seed" (ngModelChange)="fill()" /><span class="mut">{{ tr('the same seed sends the same bytes again, to repeat a failure') }}</span></div>
              }
              @if (key() === 'vlaniso') { <div class="q two"><span>{{ tr('VLAN of the switch ports') }}</span><input class="n" type="number" min="1" max="4094" [(ngModel)]="vlanA" (ngModelChange)="fill()" [title]="nameOf(a)" /><span class="mut">{{ tr('and') }}</span><input class="n" type="number" min="1" max="4094" [(ngModel)]="vlanB" (ngModelChange)="fill()" [title]="nameOf(b)" /></div> }
              @if (key() === 'mtu') { <div class="q"><span>{{ tr('Jumbo frames on the switch') }}</span><span class="seg"><i [class.on]="jumbo" (click)="jumbo = true; fill()">{{ tr('on (9216)') }}</i><i [class.on]="!jumbo" (click)="jumbo = false; fill()">{{ tr('off (1518)') }}</i></span></div> }
              @if (key() === 'dot1x') {
                <div class="q"><span>{{ tr('Method') }}</span><span class="seg"><i [class.on]="method === 'md5'" (click)="method = 'md5'; fill()">EAP-MD5</i><i [class.on]="method === 'peap'" (click)="method = 'peap'; fill()">PEAP-MSCHAPv2</i></span>@if (method === 'peap') { <span class="mut">{{ tr('the lab\\'s RADIUS server answers it with a certificate Termwright makes') }}</span> }</div>
                <div class="q"><span>{{ tr('Clients') }}</span><input class="n" type="number" min="1" max="4000" [(ngModel)]="nClients" (ngModelChange)="fill()" /><span class="mut">{{ tr('user names') }}</span><input class="u" [(ngModel)]="userPrefix" (ngModelChange)="fill()" /><span class="mono mut">{{ userRange() }}</span></div>
                <div class="q"><span>{{ tr('Password') }}</span><input [(ngModel)]="userPass" (ngModelChange)="fill()" autocomplete="off" /><span class="mut">VLAN</span><input class="n" [(ngModel)]="userVlan" (ngModelChange)="fill()" [placeholder]="tr('the port\\'s')" /><label class="cb"><input type="checkbox" [(ngModel)]="clientDhcp" (ngModelChange)="fill()" /> {{ tr('DHCP after login') }}</label></div>
                <div class="q"><span></span><label class="cb"><input type="checkbox" [(ngModel)]="toRadius" (ngModelChange)="fill()" /> {{ tr('Add the users to the lab\\'s RADIUS server (and turn it on)') }}</label></div>
                <p class="note">{{ tr('The switch port needs 802.1X with the lab\\'s RADIUS server: its address and secret are on the RADIUS page.') }}</p>
              }
              @if (key() === 'tcpsess') {
                <div class="q"><span>{{ tr('Server port') }}</span><input class="n" type="number" min="1" max="65535" [(ngModel)]="tPort" (ngModelChange)="fill()" /><label class="cb"><input type="checkbox" [(ngModel)]="tHttp" (ngModelChange)="fill()" /> {{ tr('HTTP GET') }}</label></div>
                <div class="q"><span>{{ tr('Sessions') }}</span><input class="n" type="number" min="1" [(ngModel)]="tCount" (ngModelChange)="fill()" /><span class="mut">{{ tr('at once') }}</span><input class="n" type="number" min="1" [(ngModel)]="tConc" (ngModelChange)="fill()" /><span class="mut">{{ tr('new a second') }}</span><input class="n" type="number" min="1" [(ngModel)]="tRate" (ngModelChange)="fill()" /></div>
                <div class="q"><span>{{ tr('Pass when setup is at most') }}</span><input class="n" type="number" min="0" step="0.1" [(ngModel)]="tSetup" (ngModelChange)="fill()" /><span class="mut">ms</span></div>
              }
              @if (key() === 'tcpblock') {
                <div class="q"><span>{{ tr('Blocked port') }}</span><input class="n" type="number" min="1" max="65535" [(ngModel)]="tPort" (ngModelChange)="fill()" /></div>
                <div class="q"><span>{{ tr('The switch should') }}</span><span class="seg"><i [class.on]="tBlock === 'refused'" (click)="tBlock = 'refused'; fill()">{{ tr('refuse them (reset)') }}</i><i [class.on]="tBlock === 'dropped'" (click)="tBlock = 'dropped'; fill()">{{ tr('drop them (no answer)') }}</i></span></div>
              }
              @if (key() === 'dhcp6') {
                <div class="q"><span>{{ tr('Clients') }}</span><input class="n" type="number" min="1" max="4000" [(ngModel)]="nClients" (ngModelChange)="fill()" /><label class="cb"><input type="checkbox" [(ngModel)]="also4" (ngModelChange)="fill()" /> {{ tr('IPv4 by DHCP too') }}</label></div>
                <p class="note">{{ tr('A DHCPv6 server must answer on the switch port\\'s VLAN: the switch\\'s own, a relay to a lab server, or a Linux server in the lab.') }}</p>
              }
              @if (key() === 'vrrp') {
                <div class="q"><span>VRRP</span><span class="seg"><i [class.on]="vver === 3" (click)="vver = 3; fill()">VRRPv3</i><i [class.on]="vver === 2" (click)="vver = 2; fill()">VRRPv2</i></span><span class="mut">VRID</span><input class="n" type="number" min="1" max="255" [(ngModel)]="vrid" (ngModelChange)="fill()" /></div>
                <div class="q"><span>{{ tr('Virtual IP') }}</span><input [(ngModel)]="vip" (ngModelChange)="fill()" placeholder="10.10.0.254" /><span class="mut">{{ tr('as configured on the switch') }}</span></div>
                <div class="q"><span>{{ tr('The test port starts as') }}</span><span class="seg"><i [class.on]="vMaster" (click)="vMaster = true; fill()">{{ tr('master (priority 254)') }}</i><i [class.on]="!vMaster" (click)="vMaster = false; fill()">{{ tr('backup (priority 50)') }}</i></span></div>
                <p class="note">{{ tr('The switch needs VRRP on the test port\\'s VLAN with the same VRID and virtual IP.') }}</p>
              }
              @if (key() === 'bfd') {
                <div class="q"><span>{{ tr('The switch\\'s address') }}</span><input [(ngModel)]="bPeer" (ngModelChange)="fill()" placeholder="10.10.0.1" /><span class="mut">{{ tr('on this link') }}</span></div>
                <div class="q"><span>{{ tr('Interval') }}</span><input class="n" type="number" min="10" [(ngModel)]="bInt" (ngModelChange)="fill()" /><span class="mut">ms ×</span><input class="n" type="number" min="1" max="255" [(ngModel)]="bMult" (ngModelChange)="fill()" /><span class="mut">{{ tr('= detect time {0} ms', (+bInt || 300) * (+bMult || 3)) }}</span></div>
                <p class="note">{{ tr('The switch needs BFD on that interface (or on a static route or OSPF neighbour to the test port\\'s address).') }}</p>
              }
              @if (key() === 'custom') {
                <div class="q top"><span>{{ tr('Frame') }}</span><textarea [(ngModel)]="hex" (ngModelChange)="fill()" rows="4" [placeholder]="tr('Paste the frame in hex: Wireshark → Copy → …as a Hex Stream, or a hex dump')"></textarea></div>
              }
            </div>
            @if (key() !== 'adv') {
              <div class="fills">
                <div class="fh">{{ tr('What Termwright fills in') }}<span class="mut">{{ tr('change anything before you create it') }}</span></div>
                @for (p of changes(); track p.t.id) { <div class="pc"><i class="tp">{{ tr('test port') }}</i><b>{{ p.t.name }}</b><span>{{ p.text }}</span></div> }
                @for (x of extra(); track $index) { <div class="pc"><i class="tp">{{ tr('lab') }}</i><span>{{ x }}</span></div> }
                @if (rows().length) {
                  <table><tr><th>{{ tr('Name') }}</th><th>{{ tr('From → To') }}</th><th>{{ tr('Frame') }}</th><th>{{ tr('Rate') }}</th><th>{{ tr('For') }}</th><th>{{ tr('Pass when') }}</th><th></th></tr>
                    @for (r of rows(); track $index) {
                      <tr><td><input class="nm" [(ngModel)]="r.s.name" /></td><td class="mono">{{ nameOf(r.s.from) }} → {{ r.s.to ? nameOf(r.s.to) : tr('all') }}</td>
                        <td class="mono">{{ frameText(r) }}</td>
                        <td>@if (r.test) { <span class="mono">≤ {{ r.test.maxPps }} pps</span> } @else if (r.s.session) { <input class="n" type="number" min="1" [(ngModel)]="r.s.session.rate" /><span class="mut"> {{ tr('a second') }}</span> } @else { <input class="n" type="number" min="1" max="100000" [(ngModel)]="r.s.pps" /> }</td>
                        <td>@if (!r.test) { <input class="n" type="number" min="1" [(ngModel)]="r.s.seconds" /><span class="mut"> s</span> }</td>
                        <td>@if (r.test) { <span class="mut">{{ tr('the zero-loss rate per frame size') }}</span> } @else {
                          @if (r.s.session) { <select [(ngModel)]="r.s.session.expect"><option value="">{{ tr('connect') }}</option><option value="refused">{{ tr('refused') }}</option><option value="dropped">{{ tr('no answer') }}</option></select> }
                          @else { <select [(ngModel)]="r.s.expect"><option value="">{{ tr('arrives') }}</option><option value="drop">{{ tr('dropped') }}</option><option value="any">{{ tr('either (counted)') }}</option></select> }
                          @if (!r.s.expect) { @if (r.s.maxLatency) { <span class="mut"> · ≤ {{ r.s.maxLatency }} ms</span> }@if (r.s.maxJitter) { <span class="mut"> · {{ tr('jitter') }} ≤ {{ r.s.maxJitter }} ms</span> }@if (!r.s.maxLatency && !r.s.maxJitter) { <span class="mut"> · {{ tr('loss') }} ≤ {{ r.s.maxLoss }} %</span> } } }</td>
                        <td>@if (!r.test) { <a class="lnk" (click)="r.open = !r.open">{{ r.open ? tr('Less') : tr('Addresses') }}</a> }</td></tr>
                      @if (r.open) {
                        <tr class="ad"><td colspan="7"><span>{{ tr('Source MAC') }}</span><input [value]="macOf(r.s.from)" disabled /><span>{{ tr('Destination MAC') }}</span><input [(ngModel)]="r.s.dstMac" [placeholder]="r.s.to ? tr('{0}\\'s, or the gateway\\'s', nameOf(r.s.to)) : tr('broadcast')" />
                          <span>{{ tr('Source IP') }}</span><input [(ngModel)]="r.s.srcIp" [placeholder]="ipOf(r.s.from)" /><span>{{ tr('Destination IP') }}</span><input [(ngModel)]="r.s.dstIp" [placeholder]="r.s.to ? ipOf(r.s.to) : ''" /></td></tr>
                      }
                    }
                  </table>
                }
              </div>
            }
            <div class="ac"><span class="mut">{{ summary() }}</span><span class="sp"></span>
              @if (key() === 'adv') { <button type="button" class="primary" (click)="editor.emit(null)">{{ tr('Open the Stream Editor') }}</button> }
              @else { <button type="button" (click)="create(false)" [disabled]="!ready() || busy()">{{ tr('Create') }}</button><button type="button" class="primary" (click)="create(true)" [disabled]="!ready() || busy()">▶ {{ tr('Create and Start') }}</button> }</div>
          </section>
        </div>
      </div>
    </div>`,
  styles: [`
    .ov { position: fixed; inset: 0; z-index: 100; background: rgba(0,0,0,.45); display: grid; place-items: center; }
    .dl { width: min(1360px, 96vw); height: min(800px, 92vh); background: var(--surface); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 18px 50px rgba(0,0,0,.45); display: grid; grid-template-rows: auto minmax(0, 1fr); overflow: hidden; }
    .dh { display: flex; align-items: baseline; gap: 10px; padding: 12px 16px; border-bottom: 1px solid var(--line); b { font-size: 15px; } .x { cursor: pointer; color: var(--muted); } }
    .sp { flex: 1; } .muted, .mut { color: var(--muted); } .mono { font-family: var(--mono, monospace); font-size: 11.5px; }
    .db { display: grid; grid-template-columns: 250px minmax(0, 1fr); min-height: 0; }
    .ls { border-right: 1px solid var(--line); overflow: auto; padding: 8px; background: var(--surface-2);
      .gh { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); padding: 10px 8px 4px; }
      a { display: flex; flex-direction: column; gap: 1px; padding: 6px 10px; border-radius: 8px; cursor: pointer; small { color: var(--muted); font-size: 11px; } &:hover { background: var(--surface); } &.on { background: var(--accent-soft); b { color: var(--accent); } } }
      a.lib { margin-top: 10px; color: var(--accent); font-size: 12px; } }
    .dt { padding: 14px 18px; overflow: auto; display: flex; flex-direction: column; gap: 12px; h3 { margin: 0; font-size: 16px; } .desc { margin: 0; color: var(--muted); line-height: 1.45; } }
    .warn { margin: 0; color: var(--warning, #e39b17); font-size: 12px; } .note { margin: 0 0 0 190px; color: var(--muted); font-size: 12px; line-height: 1.45; }
    .qs { display: flex; flex-direction: column; gap: 8px; }
    .q { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; > span:first-child { width: 182px; flex: none; color: var(--muted); font-size: 12.5px; } select { width: 230px; } input { width: 150px; } input.n { width: 80px; } input.u { width: 110px; } input[type=checkbox] { width: auto; margin: 0; }
      .to { color: var(--muted); } textarea { flex: 1; min-width: 300px; font-family: var(--mono, monospace); font-size: 12px; } .cb { display: inline-flex; align-items: center; gap: 5px; font-size: 12.5px; margin-left: 6px; } }
    .q.top { align-items: flex-start; > span:first-child { padding-top: 5px; } }
    .seg { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; i { font-style: normal; padding: 4px 11px; cursor: pointer; font-size: 12.5px; color: var(--muted); } i.on { background: var(--accent-soft); color: var(--accent); } }
    .chips { display: flex; flex-wrap: wrap; gap: 5px; flex: 1; .chip { display: inline-flex; align-items: center; gap: 5px; padding: 3px 9px; border: 1px solid var(--line); border-radius: 14px; font-size: 12px; cursor: pointer; input { margin: 0; } } .chip.on { border-color: color-mix(in srgb, var(--accent) 50%, transparent); background: var(--accent-soft); } }
    .fills { border: 1px solid var(--line); border-radius: 10px; overflow: auto; flex: none;
      .fh { display: flex; gap: 10px; align-items: baseline; padding: 8px 12px; font-weight: 600; font-size: 12.5px; background: var(--surface-2); border-bottom: 1px solid var(--line); .mut { font-weight: 400; } }
      .pc { display: flex; gap: 10px; align-items: center; padding: 6px 12px; border-bottom: 1px solid var(--line); font-size: 12.5px; i { font-style: normal; font-size: 10.5px; font-weight: 600; text-transform: uppercase; border-radius: 9px; padding: 1px 8px; color: var(--s4, #e39b17); background: color-mix(in srgb, var(--s4, #e39b17) 14%, transparent); } }
      table { width: 100%; border-collapse: collapse; font-size: 12.5px; th { text-align: left; font-size: 11px; color: var(--muted); font-weight: 600; padding: 6px 8px; border-bottom: 1px solid var(--line); } td { padding: 4px 8px; border-bottom: 1px solid var(--line); white-space: nowrap; vertical-align: middle; } td:last-child { text-align: right; }
        input.nm { width: 180px; } input.n { width: 64px; } select { width: 150px; } input[type=checkbox] { width: auto; } .lnk { color: var(--accent); cursor: pointer; font-size: 12px; } }
      tr.ad td { background: var(--surface-2); span { color: var(--muted); font-size: 11.5px; margin: 0 6px 0 10px; } input { width: 150px; } } }
    .ac { display: flex; gap: 8px; align-items: center; margin-top: auto; padding-top: 4px; }`],
})
export class TrafficQuick implements OnInit {
  readonly traf = input<EmuTrafficView | null>(null);
  readonly start = input<QuickKey>('l2');
  readonly closed = output<void>();
  readonly done = output<EmuTrafficView>();
  readonly editor = output<Partial<EmuStream> | null>();
  readonly library = output<void>();
  private backend = inject(Backend);
  private ui = inject(Ui);
  protected tr = tr;
  protected groups = ['Streams', 'Tests'];
  protected help: Record<QuickKey, string> = {
    l2: 'Frames from one test port to another in the same VLAN, switched by MAC address. Choose the test ports and how the switch ports carry the VLAN.',
    l3: 'Frames from one subnet to another, routed by the switch. Each test port gets an address and the switch\'s IP interface as its gateway.',
    custom: 'A frame you write byte by byte: paste it from Wireshark or a Scapy hex dump. Termwright works out lengths and checksums and adds its counting signature at the end.',
    adv: 'Every option of a stream: changing fields, user-defined fields, bursts, sizes, faults, fuzzing and pcap replay.',
    latency: 'A light stream that measures how long each frame takes from one test port to the other: average, p95, p99 and the maximum.',
    jitter: 'A voice-like stream (200 bytes, 50 a second, DSCP 46) that passes when the delay stays steady.',
    load: 'A steady stream at the rate you choose; it passes when no frame is lost.',
    rfc2544: 'Trials at falling rates find the highest rate each frame size passes without loss, both ways.',
    bad: 'Faulty frames, one stream per fault, beside a stream of good frames. The good frames must all arrive while the faulty ones come; each fault passes when the switch handles it the expected way.',
    fuzz: 'Frames with random bytes in the parts you choose, beside a stream of good frames that must all arrive: the switch keeps forwarding however odd the traffic.',
    vlaniso: 'Frames between two test ports on switch ports in different VLANs: none may arrive.',
    mtu: 'Frames of 1518, 1600 and 9000 bytes: the large ones arrive only with jumbo frames on.',
    dot1x: 'Many clients behind one test port log in by 802.1X with numbered user names; the users are added to the lab\'s RADIUS server for you.',
    tcpsess: 'Real TCP connections from one test port to another (Termwright answers there): handshake, data both ways, close. It shows how many connected, the setup time and how many a second the path takes - through ACLs, firewalls and NAT, up to session limits.',
    tcpblock: 'Connections to a port the switch should block (an ACL): each one must be refused (reset) or get no answer at all, as you expect.',
    dhcp6: 'Many clients behind one test port each take an IPv6 address by DHCPv6 (solicit, advertise, request, reply), each with its own MAC and DUID.',
    vrrp: 'The test port is a second VRRP router beside the switch. Go Silent and the switch must take over the virtual IP within its master-down time; Take Over and it must give it back. The test port times both.',
    bfd: 'A BFD session between the test port and the switch. Go Silent for 5 s and the switch must take the session down within its detect time; the test port times it.',
  } as Record<QuickKey, string>;
  protected fuzzParts = [{ k: 'ip', name: 'IP header' }, { k: 'l4', name: 'UDP header' }, { k: 'etype', name: 'EtherType' }, { k: 'payload', name: 'Payload' }];

  protected key = signal<QuickKey>('l2');
  protected rows = signal<Row[]>([]);
  protected changes = signal<PortChange[]>([]);
  protected extra = signal<string[]>([]);
  protected busy = signal(false);
  protected a = ''; protected b = '';
  protected tagged = false; protected vlan = 10;
  protected path: 'l2' | 'l3' = 'l2';
  protected tPort = 80; protected tHttp = false; protected tCount = 1000; protected tConc = 100; protected tRate = 100; protected tSetup = 10; protected tBlock: 'refused' | 'dropped' = 'dropped';
  protected routed = true; protected ipA = ''; protected gwA = ''; protected ipB = ''; protected gwB = '';
  protected latMax = 1; protected rtt = false; protected jitMax = 1; protected pps = 1000; protected both = true; protected maxPps = 10000;
  protected faults = new Set(BAD_FRAMES.filter(f => !f.tcp).map(f => f.k).concat(['tcp-xmas']));
  protected parts = new Set(['ip', 'l4']); protected seed = 1;
  protected vlanA = 10; protected vlanB = 20; protected jumbo = false;
  protected method: 'md5' | 'peap' = 'md5'; protected also4 = false; protected vver: 2 | 3 = 3; protected vrid = 1; protected vip = ''; protected vMaster = true;
  protected bPeer = ''; protected bInt = 300; protected bMult = 3;
  protected single() { return ['dot1x', 'dhcp6', 'vrrp', 'bfd'].includes(this.key()); }
  protected nClients = 100; protected userPrefix = 'user'; protected userPass = 'lab123'; protected userVlan = ''; protected clientDhcp = true; protected toRadius = true;
  protected hex = '';

  protected testers() { return this.traf()?.testers ?? []; }
  protected cur() { return ITEMS.find(i => i.k === this.key())!; }
  protected itemsOf(g: string) { return ITEMS.filter(i => i.group === g); }
  protected badList() { return BAD_FRAMES; }
  protected nameOf(id: string) { return this.testers().find(t => t.id === id)?.name ?? id; }
  protected macOf(id: string) { return this.testers().find(t => t.id === id)?.mac ?? ''; }
  protected ipOf(id: string) { return (this.testers().find(t => t.id === id)?.ip ?? '').split('/')[0]; }
  protected userRange() { return this.userPrefix + '1 … ' + this.userPrefix + this.nClients; }

  ngOnInit() {
    const ts = this.testers();
    this.a = ts[0]?.id ?? ''; this.b = ts[1]?.id ?? '';
    const t1 = ts[0], t2 = ts[1];
    const gwFor = (ip: string) => { const [a, b, c, d] = ip.split('/')[0].split('.'); return [a, b, c, d === '1' ? '254' : '1'].join('.'); };
    this.ipA = t1?.ip || '10.10.1.10/24'; this.gwA = t1?.gw || gwFor(this.ipA);
    this.ipB = t2?.ip && t2.ip.split('.').slice(0, 3).join('.') !== this.ipA.split('.').slice(0, 3).join('.') ? t2.ip : '10.10.2.10/24'; this.gwB = t2?.gw || '10.10.2.1';
    if (t1?.vlan) { this.tagged = true; this.vlan = t1.vlan; }
    this.choose(this.start());
  }
  protected choose(k: QuickKey) { this.key.set(k); this.fill(); }
  protected toggle(k: string) { this.faults.has(k) ? this.faults.delete(k) : this.faults.add(k); this.fill(); }
  protected togglePart(k: string) { this.parts.has(k) ? this.parts.delete(k) : this.parts.add(k); this.fill(); }

  private st(name: string, from: string, to: string, f: Partial<EmuStream> = {}): EmuStream {
    return { id: '', name, from, to, kind: 'udp', size: 512, pps: 1000, seconds: 30, maxLoss: 0, expect: '', ...f } as EmuStream;
  }

  /** The table, filled in again from the answers. */
  protected fill() {
    const k = this.key(), A = this.a, B = this.b, ts = this.testers();
    const ta = ts.find(t => t.id === A), tb = ts.find(t => t.id === B);
    const rows: Row[] = [], ch: PortChange[] = [], ex: string[] = [];
    const vlan = this.tagged ? +this.vlan || 0 : 0;
    const l2ports = () => {
      const want = [[ta, '10.10.0.1/24'], [tb, '10.10.0.2/24']] as const;
      for (const [t, ip] of want) {
        if (!t) continue;
        const set: Partial<EmuTester> = {}, txt: string[] = [];
        if (!t.ip && !t.dhcp) { set.ip = ip; txt.push(tr('address {0} (it has none)', ip)); }
        if ((t.vlan ?? 0) !== vlan) { set.vlan = vlan; txt.push(vlan ? tr('VLAN {0} tag on its frames', vlan) : tr('untagged frames')); }
        if (txt.length) ch.push({ t, set, text: txt.join(' · ') });
      }
    };
    const l3ports = () => {
      for (const [t, ip, gw] of [[ta, this.ipA, this.gwA], [tb, this.ipB, this.gwB]] as const) {
        if (!t) continue;
        const set: Partial<EmuTester> = {}, txt: string[] = [];
        if (t.ip !== ip) { set.ip = ip; txt.push(tr('address {0}', ip)); }
        if ((t.gw ?? '') !== gw) { set.gw = gw; txt.push(tr('gateway {0} (the switch\'s IP interface)', gw)); }
        if (t.vlan) { set.vlan = 0; txt.push(tr('untagged frames')); }
        if (txt.length) ch.push({ t, set, text: txt.join(' · ') });
      }
    };
    const ctl = (secs: number) => this.st(tr('good frames (control)'), A, B, { size: 256, pps: 500, seconds: secs, vlan: this.path === 'l2' ? vlan : 0 });
    switch (k) {
      case 'l2': l2ports(); rows.push({ s: this.st(tr('L2 {0} to {1}', this.nameOf(A), this.nameOf(B)), A, B, { vlan }) }); break;
      case 'l3': l3ports(); rows.push({ s: this.st(tr('L3 {0} to {1}', this.nameOf(A), this.nameOf(B)), A, B) }); break;
      case 'custom': rows.push({ s: this.st(tr('custom frame'), A, B, { kind: 'custom', hex: this.hex.trim(), size: 0, pps: 100, seconds: 10 }) }); break;
      case 'latency': l2ports(); rows.push({ s: this.st(tr('latency'), A, B, { vlan, size: 128, pps: 100, seconds: 30, maxLatency: +this.latMax || 1, rtt: this.rtt }) }); break;
      case 'jitter': l2ports(); rows.push({ s: this.st(tr('jitter (voice)'), A, B, { vlan, size: 200, pps: 50, seconds: 60, dscp: 46, maxJitter: +this.jitMax || 1, maxLatency: 10 }) }); break;
      case 'load':
        l2ports(); rows.push({ s: this.st(tr('load {0} to {1}', this.nameOf(A), this.nameOf(B)), A, B, { vlan, pps: +this.pps || 1000, seconds: 60 }) });
        if (this.both) rows.push({ s: this.st(tr('load {0} to {1}', this.nameOf(B), this.nameOf(A)), B, A, { vlan, pps: +this.pps || 1000, seconds: 60 }) });
        break;
      case 'rfc2544':
        l2ports();
        rows.push({ s: this.st('RFC 2544 throughput', A, B), test: { id: '', name: 'RFC 2544 throughput', kind: 'throughput', from: A, to: B, both: true, vlan, maxPps: +this.maxPps || 10000 } as EmuTest });
        break;
      case 'bad': {
        if (this.path === 'l3') l3ports(); else l2ports();
        rows.push({ s: ctl(30) });
        for (const f of BAD_FRAMES.filter(x => this.faults.has(x.k))) {
          const drop = this.path === 'l3' ? f.router : f.sw;
          rows.push({ s: this.st(f.name, A, B, { kind: f.tcp ? 'tcp' : 'udp', size: 128, pps: 100, seconds: 30, bad: f.k, expect: drop ? 'drop' : 'any', vlan: this.path === 'l2' ? vlan : 0 }) });
        }
        break;
      }
      case 'fuzz': {
        l2ports();
        rows.push({ s: ctl(60) });
        const o = vlan ? 4 : 0, size = 256;
        const range: Record<string, [number, number]> = { ip: [14 + o, 34 + o], l4: [34 + o, 42 + o], etype: [12 + o, 14 + o], payload: [66 + o, size - 4] };
        for (const z of this.fuzzParts.filter(p => this.parts.has(p.k))) {
          const [from, to] = range[z.k];
          rows.push({ s: this.st(tr('fuzz {0}', tr(z.name)), A, B, { size, pps: 200, seconds: 60, vlan, expect: 'any', fuzz: { from, to, seed: +this.seed || 1 } }) });
        }
        break;
      }
      case 'vlaniso':
        for (const [t, v] of [[ta, this.vlanA], [tb, this.vlanB]] as const) if (t && t.vlan) ch.push({ t, set: { vlan: 0 }, text: tr('untagged frames (the switch port is in VLAN {0})', v) });
        rows.push({ s: this.st(tr('VLAN {0} to VLAN {1}', this.vlanA, this.vlanB), A, B, { kind: 'raw', size: 128, pps: 100, seconds: 20, expect: 'drop' }) });
        rows.push({ s: this.st(tr('VLAN {0} to VLAN {1}', this.vlanB, this.vlanA), B, A, { kind: 'raw', size: 128, pps: 100, seconds: 20, expect: 'drop' }) });
        break;
      case 'mtu':
        l2ports();
        for (const [n, size, ok] of [['1518 bytes', 1518, true], ['1600 bytes', 1600, this.jumbo], ['9000 bytes (jumbo)', 9000, this.jumbo]] as const) {
          rows.push({ s: this.st(n, A, B, { size: vlan && size === 1518 ? 1522 : size, pps: 50, seconds: 10, vlan, expect: ok ? '' : 'drop' }) });
        }
        break;
      case 'dot1x': {
        const m = this.method === 'peap' ? 'PEAP-MSCHAPv2' : 'EAP-MD5';
        if (ta) ch.push({ t: ta, set: { clients: { n: +this.nClients || 1, rate: 50, dhcp: this.clientDhcp, dot1x: { user: this.userPrefix + '{n}', password: this.userPass, method: this.method } } }, text: tr('{0} clients behind it, logging in as {1} by 802.1X ({2}){3}', this.nClients, this.userRange(), m, this.clientDhcp ? tr(', then DHCP') : '') });
        if (this.toRadius) ex.push(tr('RADIUS server: on, with {0} users {1}, password as above{2}', this.nClients, this.userRange(), this.userVlan ? tr(', VLAN {0}', this.userVlan) : ''));
        break;
      }
      case 'tcpsess':
        if (this.path === 'l3') l3ports(); else l2ports();
        rows.push({ s: this.st(tr('TCP sessions to port {0}', +this.tPort || 80), A, B, { kind: 'session', size: 0, pps: 0, seconds: 120, vlan: this.path === 'l2' ? vlan : 0, maxLoss: 0, maxLatency: +this.tSetup || 0,
          session: { dport: +this.tPort || 80, count: +this.tCount || 1000, concurrent: +this.tConc || 100, rate: +this.tRate || 100, req: this.tHttp ? 0 : 1000, resp: 10000, hold: 0, http: this.tHttp, expect: '' } }) });
        break;
      case 'tcpblock':
        if (this.path === 'l3') l3ports(); else l2ports();
        rows.push({ s: this.st(tr('TCP to blocked port {0}', +this.tPort || 22), A, B, { kind: 'session', size: 0, pps: 0, seconds: 60, vlan: this.path === 'l2' ? vlan : 0,
          session: { dport: +this.tPort || 22, count: 20, concurrent: 5, rate: 10, req: 100, resp: 0, hold: 0, expect: this.tBlock } }) });
        break;
      case 'dhcp6':
        if (ta) ch.push({ t: ta, set: { clients: { n: +this.nClients || 1, rate: 50, dhcp: this.also4, dhcp6: true } }, text: tr('{0} clients behind it, taking IPv6 addresses by DHCPv6{1}', this.nClients, this.also4 ? tr(' and IPv4 by DHCP') : '') });
        ex.push(tr('Passes when every client has an address: the test port\'s card counts them'));
        break;
      case 'vrrp': {
        const prio = this.vMaster ? 254 : 50, iv = 1000, down = Math.round((3 * iv + (256 - prio) * iv / 256) / 100) / 10;
        if (ta) ch.push({ t: ta, set: { vrrp: { ver: this.vver, vrid: +this.vrid || 1, prio, vip: this.vip.trim(), interval: iv, preempt: true } }, text: tr('VRRPv{0} router for {1} (VRID {2}), priority {3}: {4} at first', this.vver, this.vip.trim() || '?', +this.vrid || 1, prio, tr(this.vMaster ? 'master' : 'backup')) });
        ex.push(tr(this.vMaster ? 'Then Go Silent on the test port: the switch should be master within about {0} s, and the test port times it' : 'Then Take Over on the test port: it should be master at once, and Go Silent gives it back to the switch within about {0} s', down));
        break;
      }
      case 'bfd': {
        const det = (+this.bInt || 300) * (+this.bMult || 3);
        if (ta) ch.push({ t: ta, set: { bfd: { peer: this.bPeer.trim(), tx: +this.bInt || 300, rx: +this.bInt || 300, mult: +this.bMult || 3 } }, text: tr('BFD session with {0}: {1} ms × {2}', this.bPeer.trim() || '?', +this.bInt || 300, +this.bMult || 3) });
        ex.push(tr('Then Go Silent for 5 s on the test port: the switch should take the session down within {0} ms, and the test port times it', det));
        break;
      }
    }
    this.rows.set(rows); this.changes.set(ch); this.extra.set(ex);
  }

  protected frameText(r: Row) {
    const s = r.s;
    if (r.test) return tr('64 to 1518 B, trials');
    if (s.session) { const x = s.session; return [tr('TCP sessions'), (x.http ? 'HTTP ' : '') + tr('port {0}', x.dport ?? 80), tr('{0} in all, {1} at once', x.count ?? 0, x.concurrent ?? 0), x.req || x.resp ? tr('{0} B → {1} B', x.req ?? 0, x.resp ?? 0) : ''].filter(Boolean).join(' · '); }
    const kind = s.kind === 'custom' ? tr('custom') : s.kind === 'tcp' ? 'TCP' : s.kind === 'raw' ? 'L2' : s.kind.toUpperCase();
    return [kind, s.size ? s.size + ' B' : '', s.vlan ? 'VLAN ' + s.vlan : '', s.dscp ? 'DSCP ' + s.dscp : '', s.bad ? tr('fault') : '', s.fuzz ? tr('fuzzed {0}-{1}', s.fuzz.from, s.fuzz.to ?? '') : ''].filter(Boolean).join(' · ');
  }
  protected summary() {
    const n = this.rows().filter(r => !r.test).length, t = this.rows().filter(r => r.test).length, c = this.changes().length;
    return [n ? tr(n === 1 ? '1 stream' : '{0} streams', n) : '', t ? tr(t === 1 ? '1 test' : '{0} tests', t) : '', c ? tr(c === 1 ? '1 test port changed' : '{0} test ports changed', c) : ''].filter(Boolean).join(' · ');
  }
  protected ready() {
    const k = this.key();
    if (k === 'dot1x') return !!this.a && !!this.userPrefix.trim() && +this.nClients > 0;
    if (k === 'dhcp6') return !!this.a && +this.nClients > 0;
    if (k === 'vrrp') return !!this.a && !!this.vip.trim();
    if (k === 'bfd') return !!this.a && !!this.bPeer.trim();
    if (k === 'custom' && !this.hex.trim()) return false;
    return !!this.a && !!this.b && this.a !== this.b && this.rows().length > 0;
  }

  /** Changes the test ports, adds the streams and tests (and the RADIUS users), and starts them when asked. */
  protected async create(go: boolean) {
    this.busy.set(true);
    try {
      let v: EmuTrafficView | null = null;
      for (const p of this.changes()) v = await this.backend.emuTesterSet({ ...p.t, ...p.set } as EmuTester);
      if (this.key() === 'dot1x' && this.toRadius) {
        const cur = await this.backend.emuRadius();
        const have = new Set((cur.users ?? []).map(u => u.user));
        const add: EmuRadiusUser[] = [];
        for (let i = 1; i <= (+this.nClients || 0); i++) if (!have.has(this.userPrefix + i)) add.push({ user: this.userPrefix + i, password: this.userPass, vlan: this.userVlan || '' });
        await this.backend.emuRadiusSet({ on: true, port: cur.port && cur.port !== 1812 ? cur.port : 0, secret: cur.secret ?? '', users: [...(cur.users ?? []), ...add], mab: cur.mab ?? [], mabVlan: cur.mabVlan ?? '' });
      }
      const before = new Set((v ?? this.traf())?.streams?.map(s => s.id) ?? []);
      for (const r of this.rows()) {
        if (r.test) { await this.backend.emuTestSave(r.test); continue; }
        v = await this.backend.emuStreamSave({ ...r.s, pps: +r.s.pps, seconds: +(r.s.seconds ?? 0) });
      }
      if (v && go) {
        const ids = (v.streams ?? []).filter(s => !before.has(s.id)).map(s => s.id);
        if (ids.length) v = await this.backend.emuTrafficStart(ids);
      }
      this.ui.toast(tr('{0} added', tr(this.cur().name)), 'success');
      if (v) this.done.emit(v); else this.closed.emit();
    } catch (e) { this.ui.error(e); } finally { this.busy.set(false); }
  }
}
