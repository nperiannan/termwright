import { Component, ElementRef, OnDestroy, OnInit, computed, effect, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { SlicePipe } from '@angular/common';
import { Backend, errText } from '../core/backend';
import { Session, SessionNode, SNMPMibNode, SNMPModule, SNMPResult, SNMPRow, SNMPTrapsView } from '../core/models';
import { TabView, Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { SessionSelect } from './session-select';
import { Combo } from './combo';
import { Recorder } from '../core/recorder.service';

interface TreeItem { node: SNMPMibNode; depth: number; open: boolean; }
interface HistItem { at: number; op: string; oid: string; session: string; n: number; ms: number; err?: string; rows?: SNMPRow[]; }

const OPS: { v: string; l: string }[] = [
  { v: 'get', l: 'Get' }, { v: 'getnext', l: 'Get Next' }, { v: 'bulk', l: 'Get Bulk' },
  { v: 'walk', l: 'Walk' }, { v: 'table', l: 'Table' }, { v: 'set', l: 'Set' },
];
const SET_TYPES = ['', 'Integer', 'OctetString', 'ObjectIdentifier', 'IpAddress', 'Counter32', 'Gauge32', 'TimeTicks', 'Counter64', 'Unsigned32'];
const ROOTS = ['1.3.6.1.2.1', '1.3.6.1.4.1', '1.3.6.1.6.3'];
const FAV_KEY = 'tw.snmp.favourites';

/** SNMP Browser: get / walk / table / set against a saved session (credentials from the Session Manager), the MIB tree, traps. */
@Component({
  selector: 'app-snmp-browser',
  imports: [FormsModule, SlicePipe, SessionSelect, Combo],
  template: `
    <div class="sb">
      <div class="head">
        <h2>SNMP Browser</h2>
        <app-session-select class="sess" [value]="path()" (changed)="choose($event)" [filter]="snmpFilter" title="Choose the device" placeholder="Choose a device…"
          hint="The address and SNMP credentials come from the session (session properties → SNMP)." />
        @if (sess(); as s) {
          <span class="cred" [class.v3]="ver() === '3'" translate="no">{{ credText() }}</span>
          <button type="button" class="setb" (click)="openSettings(s.path)" title="Version, community or v3 user and passwords, port, writes">⚙ SNMP Settings</button>
        }
        @for (x of extra(); track x) { <span class="chip2" translate="no">+ {{ x.split('/').pop() }}<a (click)="dropExtra(x)" title="Remove this device">✕</a></span> }
        @if (path()) {
          <app-session-select class="addd" [value]="''" (changed)="addExtra($event)" [filter]="snmpFilter" title="Run on more devices" placeholder="+ Device"
            hint="The same request runs on every device chosen; the results get a Device column." />
        }
        <span class="sp"></span>
        <button type="button" (click)="openMibs()" title="The MIB files: built in and loaded, where they are, load and remove">MIBs…</button>
        <button type="button" (click)="view.set('traps')">Traps</button>
      </div>
      <div class="bar">
        <label class="op">Operation
          <select class="fs" [(ngModel)]="op">@for (o of ops; track o.v) { <option [value]="o.v">{{ o.l }}</option> }</select>
        </label>
        <app-combo class="oid" [value]="oid" (valueChange)="oid = $event" (keydown.enter)="run()" [options]="recentOpts()" placeholder="OID or name: sysDescr.0, IF-MIB::ifTable, 1.3.6.1.2.1.2.2" emptyText="The objects you run are kept here" name="oid" label="OID or name" />
        @if (op === 'set') {
          <select class="fs ty" [(ngModel)]="setType" title="Type (empty = from the MIB)">@for (t of setTypes; track t) { <option [value]="t">{{ t || 'Type from MIB' }}</option> }</select>
          <input class="val" [(ngModel)]="setValue" (keydown.enter)="run()" placeholder="Value (enum names work: up, down)" spellcheck="false" />
        }
        @if (op === 'bulk' || op === 'walk' || op === 'table') {
          <label class="sm">Max repetitions <input type="number" min="1" max="200" [(ngModel)]="maxRep" /></label>
        }
        <label class="sm">Timeout <input type="number" min="1" max="60" [(ngModel)]="timeout" /> s</label>
        <button type="button" class="pri" (click)="run()" [disabled]="busy() || !oid.trim() || !path()">{{ busy() ? 'Running…' : '▶ Run' }}</button>
      </div>
      <div class="main">
        <div class="left">
          <div class="card tree">
            <div class="ch"><b>MIB tree</b><span class="mut">{{ modules() }} MIBs loaded</span></div>
            <input class="q" [ngModel]="query()" (ngModelChange)="search($event)" placeholder="Search names or OIDs" spellcheck="false" />
            <div class="tl">
              @if (query().trim()) {
                @for (n of found(); track n.oid) {
                  <div class="tn" [class.on]="sel()?.oid === n.oid" (click)="pick(n)" (dblclick)="runOn(n)" translate="no"><span class="nm">{{ n.module }}::{{ n.name }}</span><span class="o">{{ n.oid }}</span></div>
                } @empty { <div class="mut pad">Nothing found</div> }
              } @else {
                @for (it of items(); track it.node.oid) {
                  <div class="tn" [class.on]="sel()?.oid === it.node.oid" [style.padding-left.px]="6 + it.depth * 16" (click)="pick(it.node)" (dblclick)="it.node.children ? toggle(it) : runOn(it.node)" translate="no">
                    <span class="tw" [class.open]="it.open" (click)="toggle(it, $event)" [title]="it.node.children ? (it.open ? 'Collapse' : 'Expand') : ''">@if (it.node.children) { <svg viewBox="0 0 12 12" width="12" height="12" aria-hidden="true"><path d="M4.2 2.5L7.8 6l-3.6 3.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg> }</span>
                    <span class="ki" [class]="'ki k-' + it.node.kind" [title]="kindText(it.node)">{{ kindMark(it.node) }}</span>
                    <span class="nm" [class.tb]="it.node.kind === 'table'">{{ it.node.name }}</span>
                    <span class="o">{{ tail(it) }}{{ it.node.kind === 'table' ? ' · table' : it.node.base ? ' · ' + it.node.base : '' }}</span>
                  </div>
                }
              }
            </div>
          </div>
          @if (sel(); as n) {
            <div class="card det">
              <div class="ch"><b translate="no">{{ n.name }}</b><span class="mut" translate="no">{{ n.module }}</span>
                <a class="star" (click)="fav(n)" [title]="isFav(n) ? 'Remove from favourites' : 'Add to favourites'">{{ isFav(n) ? '★' : '☆' }}</a></div>
              <dl>
                <dt>OID</dt><dd class="mono" translate="no">{{ n.oid }}</dd>
                <dt>Kind</dt><dd>{{ kindText(n) }}</dd>
                @if (n.syntax) { <dt>Syntax</dt><dd translate="no">{{ n.syntax }}{{ n.base && n.base !== n.syntax ? ' (' + n.base + ')' : '' }}{{ n.units ? ' · ' + n.units : '' }}</dd> }
                @if (enumText(n); as e) { <dt>Values</dt><dd translate="no">{{ e }}</dd> }
                @if (n.access) { <dt>Access</dt><dd>{{ n.access }}</dd> }
                @if (n.status) { <dt>Status</dt><dd>{{ n.status }}</dd> }
                @if (n.description) { <dt>Description</dt><dd class="desc" translate="no">{{ n.description }}</dd> }
              </dl>
            </div>
          }
          <div class="card favs">
            <div class="ch"><b>Favourites</b><span class="mut">{{ favs().length }}</span></div>
            @for (f of favs(); track f) { <div class="tn" (click)="oid = f" (dblclick)="oid = f; run()" translate="no">★ {{ f }}</div> }
            @empty { <div class="mut pad">Star an object to keep it here.</div> }
          </div>
        </div>
        <div class="right card">
          <div class="tabs">
            <a [class.on]="view() === 'results'" (click)="view.set('results')">Results <i>{{ result()?.rows?.length ?? 0 }}</i></a>
            <a [class.on]="view() === 'table'" (click)="view.set('table')">Table view</a>
            <a [class.on]="view() === 'compare'" (click)="view.set('compare')">Compare</a>
            <a [class.on]="view() === 'poll'" (click)="view.set('poll')">Poll{{ polling() ? ' ●' : '' }}</a>
            <a [class.on]="view() === 'traps'" (click)="view.set('traps')">Traps <i>{{ traps().traps.length }}</i></a>
            <a [class.on]="view() === 'history'" (click)="view.set('history')">History</a>
            <span class="sp"></span><button type="button" class="tbtn" (click)="exportCsv()" [disabled]="!result()?.rows?.length">Export CSV</button>
          </div>
          <div class="body">
            @if (error()) { <div class="err">{{ error() }}</div> }
            @switch (view()) {
              @case ('results') {
                @if (multi().length > 1) {
                  <table><thead><tr><th>Device</th><th>Name</th><th>Index</th><th>Type</th><th>Value</th></tr></thead><tbody>
                    @for (d of multi(); track d.session) {
                      @if (d.err) { <tr><td translate="no">{{ d.session.split('/').pop() }}</td><td colspan="4" class="bad">{{ d.err }}</td></tr> }
                      @for (x of d.rows; track $index) {
                        <tr (click)="oid = x.oid" translate="no"><td>{{ d.session.split('/').pop() }}</td><td>{{ x.object || x.name }}</td><td>{{ x.instance }}</td><td>{{ x.type }}</td><td [class]="tone(x)">{{ x.display }}</td></tr>
                      }
                    }
                  </tbody></table>
                } @else if (result(); as r) {
                  @if (r.unknown) {
                    <div class="unk">⚠ {{ r.unknown }} of these values are not described by the loaded MIBs, so they have no names or meanings.
                      @if (r.suggest?.length) { They are most likely in <b translate="no">{{ r.suggest!.join(', ') }}</b>: load it from the vendor's MIB download. }
                      @else { Load the device vendor's MIB files. }
                      <button type="button" class="sm2" (click)="loadMib()">Load MIB Files…</button><button type="button" class="sm2" (click)="openMibs()">MIBs…</button></div>
                  }
                  <table><thead><tr><th>Name</th><th>Index</th><th>Type</th><th>Value</th><th>OID</th></tr></thead><tbody>
                    @for (x of r.rows; track $index) {
                      <tr (click)="oid = x.oid" translate="no" [class.unkr]="x.unknown"><td [title]="x.unknown ? 'Not described by the loaded MIBs' + (x.suggest ? ' - most likely in ' + x.suggest : '') : (x.module ? x.module + '::' + x.object : '')">@if (x.unknown) { <span class="uq">?</span> }{{ x.object || x.name }}</td><td>{{ x.instance }}</td><td>{{ x.type }}</td>
                        <td [class]="tone(x)">{{ x.display }}</td><td class="mono mut">{{ x.oid }}</td></tr>
                    }
                  </tbody></table>
                } @else { <div class="empty">Choose a session, an object in the MIB tree (double-click runs it) and press Run.</div> }
              }
              @case ('table') {
                @if (pivot(); as p) {
                  <table><thead><tr><th>Index</th>@for (c of p.cols; track c) { <th translate="no">{{ c }}</th> }</tr></thead><tbody>
                    @for (row of p.rows; track row.idx) { <tr translate="no"><td>{{ row.idx }}</td>@for (c of p.cols; track c) { <td>{{ row.v[c] ?? '' }}</td> }</tr> }
                  </tbody></table>
                } @else { <div class="empty">Walk a table (for example IF-MIB::ifTable) to see it as rows and columns.</div> }
              }
              @case ('compare') {
                <div class="tbar">
                  <label>A <select class="fs cmp" [ngModel]="cmpA()" (ngModelChange)="cmpA.set($event)">@for (o of cmpOpts(); track o.k) { <option [value]="o.k">{{ o.l }}</option> }</select></label>
                  <label>B <select class="fs cmp" [ngModel]="cmpB()" (ngModelChange)="cmpB.set($event)">@for (o of cmpOpts(); track o.k) { <option [value]="o.k">{{ o.l }}</option> }</select></label>
                  <label class="chk"><input type="checkbox" [ngModel]="onlyDiff()" (ngModelChange)="onlyDiff.set($event)" /> Only differences</label>
                  <span class="sp"></span><span class="mut">{{ diff().changed }} changed · {{ diff().onlyA }} only in A · {{ diff().onlyB }} only in B</span>
                </div>
                @if (cmpOpts().length < 2) { <div class="empty">Compare two walks: run the same walk twice (before and after a change), or on two devices (+ Device), then choose A and B here.</div> }
                @else {
                  <table><thead><tr><th>Object</th><th>A</th><th>B</th><th></th></tr></thead><tbody>
                    @for (d of diff().rows; track d.k) { <tr translate="no" [class]="'d-' + d.kind"><td>{{ d.k }}</td><td>{{ d.a ?? '' }}</td><td>{{ d.b ?? '' }}</td><td class="mut">{{ d.kind === 'same' ? '' : d.kind === 'changed' ? 'changed' : d.kind === 'a' ? 'only in A' : 'only in B' }}</td></tr> }
                  </tbody></table>
                }
              }
              @case ('poll') {
                <div class="tbar">
                  <span class="mut">Reads</span><b class="mono" translate="no">{{ pollOid || oid || '(choose an object)' }}</b>
                  <label class="sm">every <input type="number" min="1" max="3600" [(ngModel)]="pollSecs" /> s</label>
                  @if (polling()) { <button type="button" (click)="stopPoll()">Stop</button> } @else { <button type="button" class="pri" (click)="startPoll()" [disabled]="!path() || !oid.trim()">▶ Start Polling</button> }
                  <button type="button" (click)="clearPoll()" [disabled]="!series().length">Clear</button>
                  <span class="sp"></span><span class="mut">Counters are shown per second; {{ pollN() }} samples</span>
                </div>
                @if (series().length) {
                  <svg class="chart" viewBox="0 0 1000 260" preserveAspectRatio="none">
                    @for (g of grid(); track $index) { <line x1="0" x2="1000" [attr.y1]="g.y" [attr.y2]="g.y" class="gl" /><text x="4" [attr.y]="g.y - 3" class="gt">{{ g.l }}</text> }
                    @for (sr of series(); track sr.key; let i = $index) { <polyline [attr.points]="line(sr)" fill="none" [attr.stroke]="color(i)" stroke-width="2" vector-effect="non-scaling-stroke" /> }
                  </svg>
                  <table><thead><tr><th></th><th>Object</th><th>Last</th><th>Min</th><th>Max</th><th>Average</th></tr></thead><tbody>
                    @for (sr of series(); track sr.key; let i = $index) { <tr translate="no"><td><span class="dot" [style.background]="color(i)"></span></td><td>{{ sr.key }}{{ sr.rate ? ' /s' : '' }}</td><td>{{ fmt(sr.pts[sr.pts.length - 1]?.v) }}</td><td>{{ fmt(stat(sr).min) }}</td><td>{{ fmt(stat(sr).max) }}</td><td>{{ fmt(stat(sr).avg) }}</td></tr> }
                  </tbody></table>
                } @else { <div class="empty">Choose a value to watch (sysUpTime.0, ifHCInOctets.3, or a column such as IF-MIB::ifHCInOctets for every port) and press Start Polling. Counters are drawn as a rate per second.</div> }
              }
              @case ('traps') {
                <div class="tbar">
                  @if (traps().state.on) {
                    <span class="ok">● Listening on UDP {{ traps().state.port }}</span><button type="button" (click)="trapStop()">Stop</button>
                  } @else {
                    <label class="sm">UDP port <input type="number" [(ngModel)]="trapPort" /></label><button type="button" class="pri" (click)="trapStart()">Start Receiver</button>
                    @if (traps().state.error) { <span class="bad">{{ traps().state.error }}</span> }
                  }
                  <span class="sp"></span><button type="button" (click)="sendOpen.set(!sendOpen())" [class.on]="sendOpen()">Send Trap…</button><button type="button" (click)="trapClear()" [disabled]="!traps().traps.length">Clear</button>
                </div>
                @if (sendOpen()) {
                  <div class="sendt">
                    <label>To<input [(ngModel)]="tr.address" placeholder="receiver host or host:port (162)" spellcheck="false" /></label>
                    <label>Version<select class="fs" [(ngModel)]="tr.version"><option value="1">v1</option><option value="2c">v2c</option><option value="3">v3</option></select></label>
                    @if (tr.version !== '3') { <label>Community<input [(ngModel)]="tr.community" placeholder="public" /></label> }
                    @else {
                      <label>User<input [(ngModel)]="tr.user" /></label>
                      <label>Auth<select class="fs" [(ngModel)]="tr.auth"><option value="">none</option>@for (a of auths; track a) { <option [value]="a">{{ a }}</option> }</select></label>
                      <label>Auth password<input type="password" [(ngModel)]="tr.authPass" /></label>
                      <label>Priv<select class="fs" [(ngModel)]="tr.priv"><option value="">none</option>@for (a of privs; track a) { <option [value]="a">{{ a }}</option> }</select></label>
                      <label>Priv password<input type="password" [(ngModel)]="tr.privPass" /></label>
                    }
                    <label class="w2">Notification<input [(ngModel)]="tr.trapOid" placeholder="IF-MIB::linkDown, SNMPv2-MIB::coldStart or an OID" spellcheck="false" /></label>
                    <label class="w3">Values (one per line: object = value)<textarea [(ngModel)]="trapVals" rows="3" placeholder="ifIndex.3 = 3&#10;ifOperStatus.3 = down" spellcheck="false"></textarea></label>
                    <div class="w3 btns"><span class="mut">{{ trapMsg() }}</span><span class="sp"></span><button type="button" class="pri" (click)="sendTrap()" [disabled]="!tr.address.trim() || !tr.trapOid.trim()">Send</button></div>
                  </div>
                }
                @for (t of traps().traps; track $index) {
                  <div class="trap" translate="no">
                    <div class="th"><b>{{ t.name || t.trapOid }}</b><span>{{ t.kind }} · v{{ t.version }} · {{ t.session || t.from }} · {{ t.at | slice:11:19 }}</span></div>
                    @for (x of t.rows; track $index) { <div class="tv"><span>{{ x.name }}</span> {{ x.display }}</div> }
                    @if (t.note) { <div class="mut">{{ t.note }}</div> }
                  </div>
                } @empty { <div class="empty">No traps yet. Point the devices' trap host at this computer; v3 traps use the users of the SNMP sessions.</div> }
              }
              @case ('history') {
                <table><thead><tr><th>Time</th><th>Operation</th><th>Object</th><th>Session</th><th>Result</th></tr></thead><tbody>
                  @for (h of hist(); track h.at) {
                    <tr (click)="replay(h)" title="Run again"><td>{{ time(h.at) }}</td><td>{{ h.op }}</td><td translate="no">{{ h.oid }}</td><td translate="no">{{ h.session }}</td>
                      <td [class.bad]="!!h.err">{{ h.err || (h.n + ' values · ' + h.ms + ' ms') }}</td></tr>
                  }
                </tbody></table>
              }
            }
          </div>
          <div class="foot">
            @if (result(); as r) { <span>{{ opName(lastOp()) }} · {{ r.rows.length }} values in {{ (r.ms / 1000).toFixed(2) }} s · {{ r.requests }} requests · SNMP{{ r.version === '3' ? 'v3' : 'v' + r.version }} {{ r.address }}{{ r.truncated ? ' · stopped at the limit' : '' }}</span> }
            <span class="sp"></span>
            <span class="mut">{{ writeText() }}</span>
          </div>
        </div>
      </div>
      @if (mibOpen()) {
        <div class="ov" (click)="mibOpen.set(false)"><div class="dlg" (click)="$event.stopPropagation()">
          <div class="dh"><h3>MIBs</h3><span class="mut">{{ mibs().length }} modules · {{ userMibs() }} of yours{{ missingAll().length ? ' · ' + missingAll().length + ' needed but not loaded' : '' }}</span></div>
          <div class="mut">The standard IETF and IANA MIBs are built in. Your MIB files are kept in <span class="mono" translate="no">{{ mibFolder() }}</span>. A MIB often needs others (its IMPORTS): load them too, from the same vendor download.</div>
          @if (missingAll().length) { <div class="need">To load next: @for (n of missingAll(); track n) { <b translate="no">{{ n }}</b> } <span class="mut">(needed by your MIBs; until then some of their objects are not in the tree)</span></div> }
          <div class="tbar"><input class="q2" [(ngModel)]="mibQ" placeholder="Filter modules or files" spellcheck="false" /><span class="sp"></span>
            <button type="button" (click)="showMibFolder()" title="Open the folder in the file manager">Open Folder</button><button type="button" class="pri" (click)="loadMib()">Load MIB Files…</button></div>
          <div class="mtab"><table><thead><tr><th>Module</th><th>From</th><th>Objects</th><th>Needs</th><th>Used by</th><th></th></tr></thead><tbody>
            @for (m of mibShown(); track m.name) {
              <tr translate="no"><td><b>{{ m.name }}</b>@if (m.unplaced?.length) { <div class="warn" [title]="m.unplaced!.join('\n')">{{ m.unplaced!.length }} objects wait for a MIB that is not loaded</div> }</td>
                <td class="mut">{{ m.builtIn ? 'built in' : m.file }}</td><td>{{ m.objects }}</td>
                <td class="deps">@for (i of needs(m); track i) { <span class="dep" [class.miss]="m.missing?.includes(i)" [title]="m.missing?.includes(i) ? 'Not loaded: load it' : 'Loaded'">{{ m.missing?.includes(i) ? '✕' : '✓' }} {{ i }}</span> }</td>
                <td class="deps">@for (d of m.dependents ?? []; track d) { <span class="dep">{{ d }}</span> }</td>
                <td class="act">@if (m.root) { <button type="button" class="sm2" (click)="showInTree(m)" title="Open the tree at this module">Show in Tree</button> }
                  @if (!m.builtIn) { <button type="button" class="sm2" (click)="removeMib(m)" title="Stop loading it (the file is moved to the removed folder)">Remove</button> }</td></tr>
            }
          </tbody></table></div>
          <div class="dfoot"><span class="sp"></span><button type="button" (click)="mibOpen.set(false)">Close</button></div>
        </div></div>
      }
    </div>
  `,
  styles: [`
    :host { display: block; height: 100%; position: relative; }
    .sb { height: 100%; display: flex; flex-direction: column; gap: 10px; padding: 12px 14px; box-sizing: border-box; background: var(--bg); color: var(--text); font-size: 13px; }
    .head, .bar, .tbar, .foot { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
    h2 { margin: 0 6px 0 0; font-size: 17px; font-weight: 600; white-space: nowrap; } .setb { white-space: nowrap; }
    .sp { flex: 1; }
    .sess { width: 300px; max-width: 34vw; } .head { flex-wrap: nowrap; } .cred { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; flex: 0 1 auto; } .tbtn { height: 26px; margin: 4px 0; }
    .setb { height: 30px; }
    .chip2 { border: 1px solid var(--border); border-radius: 12px; padding: 2px 6px 2px 10px; font-size: 12px; display: inline-flex; gap: 6px; align-items: center; }
    .chip2 a { cursor: pointer; color: var(--muted); } .addd { width: 130px; } button.on { border-color: var(--accent); color: var(--accent); }
    .cmp { max-width: 380px; margin-left: 6px; } .chk { gap: 5px; }
    tr.d-changed td { background: color-mix(in srgb, var(--warn) 12%, transparent); } tr.d-a td { background: color-mix(in srgb, var(--danger) 10%, transparent); } tr.d-b td { background: color-mix(in srgb, var(--ok) 10%, transparent); }
    .chart { width: 100%; height: 240px; display: block; border-bottom: 1px solid var(--line); } .gl { stroke: var(--line); stroke-width: 1; vector-effect: non-scaling-stroke; } .gt { fill: var(--faint); font-size: 11px; }
    .dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%; }
    .sendt { display: grid; grid-template-columns: repeat(3, minmax(140px, 1fr)); gap: 8px 12px; padding: 10px 12px; border-bottom: 1px solid var(--line); }
    .sendt label { flex-direction: column; align-items: stretch; gap: 4px; font-size: 12px; } .sendt textarea { font-family: var(--mono); font-size: 12px; }
    .w2 { grid-column: span 2; } .w3 { grid-column: span 3; } .btns { display: flex; gap: 8px; align-items: center; }
    .ov { position: absolute; inset: 0; background: rgba(0,0,0,.45); display: flex; align-items: center; justify-content: center; z-index: 20; }
    .dlg { background: var(--panel); border: 1px solid var(--border); border-radius: var(--r-lg); box-shadow: var(--shadow); padding: 14px 16px; width: min(900px, 94%); max-height: 86%; display: flex; flex-direction: column; gap: 8px; }
    .dlg h3 { margin: 0; font-size: 15px; } .dh { display: flex; gap: 10px; align-items: baseline; } .x { cursor: pointer; color: var(--muted); }
    .unk { margin: 8px 12px; padding: 8px 10px; border-radius: var(--r); background: color-mix(in srgb, var(--warn) 13%, transparent); font-size: 12.5px; line-height: 1.6; }
    tr.unkr td:first-child { color: var(--muted); font-style: italic; } .uq { display: inline-block; width: 14px; height: 14px; border-radius: 50%; background: var(--warn); color: #fff; font: 700 10px/14px var(--ui-font); text-align: center; margin-right: 6px; font-style: normal; }
    .q2 { width: 260px; } .need { padding: 6px 10px; border-radius: var(--r); background: color-mix(in srgb, var(--warn) 12%, transparent); font-size: 12px; } .need b { margin-right: 8px; }
    .deps { white-space: normal !important; } .dep { display: inline-block; font-size: 11.5px; margin: 1px 6px 1px 0; color: var(--muted); } .dep.miss { color: var(--danger); font-weight: 600; }
    .act { white-space: nowrap; text-align: right; } .sm2 { height: 24px; padding: 0 8px; font-size: 12px; margin-left: 4px; }
    .dfoot { display: flex; gap: 8px; padding-top: 4px; } .mtab { overflow: auto; min-height: 0; flex: 1; border: 1px solid var(--line); border-radius: var(--r); } .warn { color: var(--warn); font-size: 12px; cursor: help; }
    .cred { border: 1px solid var(--border); border-radius: 12px; padding: 2px 10px; color: var(--muted); font-size: 12px; }
    .cred.v3 { border-color: color-mix(in srgb, var(--ok) 55%, transparent); color: var(--ok); }
    .lnk { color: var(--accent); cursor: pointer; font-size: 12px; }
    .bar select, .bar input, .bar button, .head button { height: 30px; box-sizing: border-box; }
    .bar .oid { flex: 1; min-width: 240px; font-family: var(--mono); }
    .bar .val { min-width: 180px; }
    .sm input { width: 58px; margin-left: 4px; }
    .op select { margin-left: 6px; }
    label { color: var(--muted); display: inline-flex; align-items: center; }
    button.pri { background: var(--accent); color: #fff; border-color: var(--accent); }
    .main { flex: 1; min-height: 0; display: grid; grid-template-columns: minmax(260px, 380px) 1fr; gap: 10px; }
    .left { display: flex; flex-direction: column; gap: 10px; min-height: 0; }
    .card { background: var(--panel); border: 1px solid var(--border); border-radius: var(--r-lg); display: flex; flex-direction: column; min-height: 0; }
    .ch { display: flex; align-items: center; gap: 8px; padding: 9px 12px; border-bottom: 1px solid var(--border); }
    .ch .mut { margin-left: auto; }
    .mut { color: var(--muted); font-size: 12px; }
    .pad { padding: 8px 12px; }
    .tree { flex: 1; }
    .q { margin: 8px 10px; }
    .tl { overflow: auto; flex: 1; padding-bottom: 6px; }
    .tn { display: flex; align-items: baseline; gap: 6px; padding: 3px 10px; cursor: pointer; white-space: nowrap; }
    .tn:hover { background: var(--hover); } .tn.on { background: var(--selected); }
    .tn .tw { width: 18px; height: 18px; flex: none; display: inline-flex; align-items: center; justify-content: center; color: var(--muted); border-radius: 4px; transition: transform .12s; }
    .tn .tw:hover { background: var(--hover); color: var(--text); } .tn .tw.open { transform: rotate(90deg); }
    .tn { align-items: center !important; }
    .ki { flex: none; width: 16px; height: 16px; border-radius: 4px; font: 600 9.5px/16px var(--mono); text-align: center; color: #fff; background: var(--faint); }
    .k-module { background: #7c3aed; } .k-node, .k-group { background: #64748b; } .k-table { background: #2563eb; } .k-row { background: #0891b2; } .k-column { background: #0d9488; }
    .k-scalar { background: #16a34a; } .k-notification { background: #d97706; } .k-compliance { background: #94a3b8; }
    .oid ::ng-deep input { font-family: var(--mono); height: 30px; box-sizing: border-box; }
    .tn .o { color: var(--faint); font-size: 11px; overflow: hidden; text-overflow: ellipsis; }
    .tn .tb { color: var(--accent); }
    .det dl { display: grid; grid-template-columns: 90px 1fr; gap: 5px 10px; margin: 0; padding: 10px 12px; max-height: 260px; overflow: auto; }
    .det dt { color: var(--muted); } .det dd { margin: 0; } .desc { color: var(--muted); white-space: pre-wrap; }
    .star { margin-left: 6px !important; cursor: pointer; color: var(--warn); }
    .favs { max-height: 160px; } .favs .tn { padding: 3px 12px; }
    .right { min-width: 0; }
    .tabs { display: flex; gap: 4px; align-items: center; border-bottom: 1px solid var(--border); padding: 0 10px; }
    .tabs a { padding: 9px 12px; cursor: pointer; color: var(--muted); border-bottom: 2px solid transparent; }
    .tabs a.on { color: var(--text); border-bottom-color: var(--accent); }
    .tabs i { font-style: normal; font-size: 11px; color: var(--faint); margin-left: 4px; }
    .body { flex: 1; overflow: auto; min-height: 0; }
    table { width: 100%; border-collapse: collapse; }
    th { text-align: left; font-weight: 500; color: var(--muted); padding: 7px 12px; position: sticky; top: 0; background: var(--panel); border-bottom: 1px solid var(--border); }
    td { padding: 6px 12px; border-bottom: 1px solid var(--line); }
    tbody tr { cursor: pointer; } tbody tr:hover { background: var(--hover); }
    .mono { font-family: var(--mono); font-size: 12px; }
    .ok { color: var(--ok); } .bad { color: var(--danger); } .en { color: var(--accent); }
    .empty { padding: 30px; color: var(--muted); text-align: center; }
    .err { margin: 10px 12px; padding: 8px 10px; border-radius: var(--r); background: color-mix(in srgb, var(--danger) 14%, transparent); color: var(--danger); }
    .tbar { padding: 10px 12px; border-bottom: 1px solid var(--line); }
    .trap { padding: 8px 12px; border-bottom: 1px solid var(--line); }
    .trap .th { display: flex; gap: 10px; align-items: baseline; } .trap .th span { color: var(--muted); font-size: 12px; }
    .trap .tv { font-size: 12px; padding-left: 10px; } .trap .tv span { color: var(--muted); }
    .foot { padding: 8px 12px; border-top: 1px solid var(--border); color: var(--muted); font-size: 12px; }
  `],
})
export class SnmpBrowser implements OnInit, OnDestroy {
  readonly tab = input.required<TabView>();
  readonly visible = input(true);
  private readonly backend = inject(Backend);
  private readonly rec = inject(Recorder);
  private readonly tabs = inject(Tabs);
  readonly ui = inject(Ui);
  private readonly host = inject(ElementRef<HTMLElement>);

  readonly ops = OPS; readonly setTypes = SET_TYPES;
  op = 'walk'; oid = ''; setType = ''; setValue = ''; maxRep = 25; timeout = 3; trapPort = 162;

  readonly path = signal('');
  readonly sess = signal<Session | null>(null);
  readonly sessions = computed(() => flat(this.ui.tree()).filter(this.snmpFilter));
  readonly snmpFilter = (n: SessionNode) => !['RDP', 'VNC', 'Local Shell', 'WOL', 'Serial', 'API'].includes(n.protocol ?? '');
  readonly ver = computed(() => this.sess()?.snmpVersion || '2c');
  readonly items = signal<TreeItem[]>([]);
  readonly modules = signal(0);
  readonly query = signal('');
  readonly found = signal<SNMPMibNode[]>([]);
  readonly sel = signal<SNMPMibNode | null>(null);
  readonly favs = signal<string[]>(loadFavs());
  readonly result = signal<SNMPResult | null>(null);
  readonly lastOp = signal('');
  readonly error = signal('');
  readonly busy = signal(false);
  readonly view = signal<'results' | 'table' | 'compare' | 'poll' | 'traps' | 'history'>('results');
  readonly extra = signal<string[]>([]);
  readonly multi = signal<{ session: string; rows: SNMPRow[]; err?: string }[]>([]);
  readonly traps = signal<SNMPTrapsView>({ state: { on: false, port: 0, count: 0 }, traps: [] });
  readonly hist = signal<HistItem[]>([]);
  readonly pivot = computed(() => pivot(this.result()?.rows ?? []));
  private off: (() => void) | null = null;
  private searchT: ReturnType<typeof setTimeout> | undefined;

  constructor() {
    // the menu and the session's right-click menu ask for a page (traps) on an open tab
    effect(() => { const p = this.tab().snmpPage; if (p?.startsWith('traps')) this.view.set('traps'); });
  }

  async ngOnInit() {
    if (!this.ui.tree()) this.ui.tree.set(await this.backend.getSessionTree().catch(() => null));
    if (this.tab().snmpSession) await this.choose(this.tab().snmpSession!);
    const roots = await Promise.all(ROOTS.map(o => this.backend.snmpMibInfo(o).catch(() => null)));
    this.items.set(roots.filter((n): n is SNMPMibNode => !!n).map(node => ({ node, depth: 0, open: false })));
    if (this.items().length) await this.toggle(this.items()[0]);
    this.modules.set((await this.backend.snmpMibModules().catch(() => [])).length);
    this.traps.set(await this.backend.snmpTraps().catch(() => this.traps()));
    this.off = this.backend.on('snmp:trap', () => void this.backend.snmpTraps().then(v => this.traps.set(v)).catch(() => {}));
  }
  ngOnDestroy() { this.off?.(); clearTimeout(this.searchT); clearInterval(this.pollTimer); }

  async choose(p: string) {
    this.path.set(p);
    this.sess.set(p ? await this.backend.getSession(p).catch(() => null) : null);
  }

  openSettings(p: string) { this.ui.editorPage.set('snmp'); void this.ui.editSession(p, undefined, true).then(() => this.choose(p)); }

  credText(): string {
    const s = this.sess();
    if (!s) return '';
    const host = (s.snmpHost || s.hostname) + (s.snmpPort && s.snmpPort !== 161 ? ':' + s.snmpPort : '');
    if (this.ver() !== '3') return `${host} · SNMPv${this.ver()} · community from Session Manager`;
    const lvl = s.snmpPriv ? 'authPriv' : s.snmpAuth ? 'authNoPriv' : 'noAuthNoPriv';
    return `${host} · v3 ${lvl} · user ${s.snmpUser || '?'}${s.snmpAuth ? ' · ' + s.snmpAuth : ''}${s.snmpPriv ? ' / ' + s.snmpPriv : ''}`;
  }
  writeText(): string {
    const s = this.sess();
    if (!s) return '';
    return s.snmpWrite ? tr('Set is allowed for this session') + (s.environment === 'production' ? tr(' (asks first: Production)') : '') : tr('Read-only: turn on Set in the session’s SNMP settings');
  }

  async toggle(it: TreeItem, ev?: Event) {
    ev?.stopPropagation();
    if (!it.node.children) return;
    const list = [...this.items()];
    const i = list.indexOf(it);
    if (i < 0) return;
    if (it.open) {
      let j = i + 1;
      while (j < list.length && list[j].depth > it.depth) j++;
      list.splice(i + 1, j - i - 1);
      list[i] = { ...it, open: false };
    } else {
      const kids = await this.backend.snmpMibChildren(it.node.oid).catch(() => []);
      list.splice(i + 1, 0, ...kids.map(node => ({ node, depth: it.depth + 1, open: false })));
      list[i] = { ...it, open: true };
    }
    this.items.set(list);
  }
  tail(it: TreeItem): string { return it.depth ? '.' + it.node.oid.split('.').pop() : it.node.oid; }

  search(q: string) {
    this.query.set(q);
    clearTimeout(this.searchT);
    this.searchT = setTimeout(async () => this.found.set(q.trim() ? await this.backend.snmpMibSearch(q.trim()).catch(() => []) : []), 200);
  }

  pick(n: SNMPMibNode) {
    this.sel.set(n);
    this.oid = n.module ? n.module + '::' + n.name + (n.kind === 'scalar' ? '.0' : '') : n.oid;
    if (n.kind === 'scalar' && (this.op === 'walk' || this.op === 'table')) this.op = 'get';
    else if (n.kind !== 'scalar' && this.op === 'get') this.op = 'walk';
  }
  runOn(n: SNMPMibNode) { this.pick(n); void this.run(); }

  async run() {
    const oid = this.oid.trim();
    if (!oid || !this.path() || this.busy()) return;
    this.error.set('');
    this.busy.set(true);
    const op = this.op;
    const req = {
      session: this.path(), op, timeout: Number(this.timeout) || 3, maxRep: Number(this.maxRep) || 25,
      ...(op === 'set' ? { values: [{ oid, type: this.setType, value: this.setValue }] } : { oids: [oid] }),
    };
    const t0 = Date.now();
    this.remember(oid);
    if (this.extra().length && op !== 'set') { await this.runMany(req, op, oid); return; }
    this.multi.set([]);
    try {
      let r: SNMPResult;
      try {
        r = await this.backend.snmpRun(req);
      } catch (e) {
        // a Set on a Production session is confirmed here first
        if (!/production session/i.test(errText(e))) throw e;
        const ok = await this.ui.confirm(tr('Write to a Production device?'), tr('Set {0} = {1} on the Production session {2}.', oid, this.setValue, this.path()), tr('Set'), true);
        if (!ok) { this.busy.set(false); return; }
        r = await this.backend.snmpRun({ ...req, confirmed: true });
      }
      this.result.set(r);
      this.lastOp.set(op);
      if (this.rec.recording()) this.rec.snmp({ session: this.path(), op, ...(op === 'set' ? { values: [{ oid, type: this.setType || undefined, value: this.setValue }] } : { oids: [oid] }) }, r.rows);
      if (this.view() !== 'table' || op !== 'table') this.view.set(op === 'table' ? 'table' : 'results');
      this.addHist({ at: Date.now(), op, oid, session: this.path(), n: r.rows.length, ms: r.ms, rows: r.rows.slice(0, 5000) });
    } catch (e) {
      this.error.set(errText(e));
      this.addHist({ at: Date.now(), op, oid, session: this.path(), n: 0, ms: Date.now() - t0, err: errText(e) });
    }
    this.busy.set(false);
  }
  /** The same request on every chosen device at once; each device's rows are kept for Compare. */
  private async runMany(req: { session: string; op: string; oids?: string[]; timeout: number; maxRep: number }, op: string, oid: string) {
    const all = [this.path(), ...this.extra()];
    const res = await Promise.all(all.map(async sess => {
      try { const r = await this.backend.snmpRun({ ...req, session: sess }); return { session: sess, rows: r.rows, ms: r.ms }; }
      catch (e) { return { session: sess, rows: [] as SNMPRow[], err: errText(e), ms: 0 }; }
    }));
    this.multi.set(res);
    const first = res[0];
    this.result.set(first.err ? null : { rows: first.rows, address: '', version: this.ver(), ms: first.ms, requests: 0 });
    this.lastOp.set(op);
    this.view.set('results');
    for (const r of [...res].reverse()) this.addHist({ at: Date.now() + res.indexOf(r), op, oid, session: r.session, n: r.rows.length, ms: r.ms, err: r.err, rows: r.rows.slice(0, 5000) });
    const ok = res.filter(r => !r.err).length;
    if (ok < res.length) this.error.set(tr('{0} of {1} devices answered', ok, res.length));
    // recording: one step per device that answered (a device that did not answer is left out)
    if (this.rec.recording()) for (const r of res) if (!r.err) this.rec.snmp({ session: r.session, op, oids: [oid] }, r.rows);
    this.busy.set(false);
  }
  addExtra(p: string) { if (p && p !== this.path() && !this.extra().includes(p)) this.extra.update(l => [...l, p]); }
  dropExtra(p: string) { this.extra.update(l => l.filter(x => x !== p)); if (!this.extra().length) this.multi.set([]); }

  // ---- Compare: two walks (history items with their rows) ----
  readonly cmpA = signal('');
  readonly cmpB = signal('');
  readonly onlyDiff = signal(true);
  readonly cmpOpts = computed(() => this.hist().filter(h => h.rows?.length).map(h => ({ k: String(h.at), l: `${this.time(h.at)} · ${h.session.split('/').pop()} · ${h.op} ${h.oid} (${h.n})` })));
  readonly diff = computed(() => {
    const opts = this.cmpOpts();
    const ka = this.cmpA() || opts[1]?.k || '', kb = this.cmpB() || opts[0]?.k || '';
    const A = this.hist().find(h => String(h.at) === ka)?.rows ?? [], B = this.hist().find(h => String(h.at) === kb)?.rows ?? [];
    const key = (r: SNMPRow) => (r.object ? r.object + (r.instance ? '.' + r.instance : '') : r.name);
    const ma = new Map(A.map(r => [key(r), r.display])), mb = new Map(B.map(r => [key(r), r.display]));
    const rows: { k: string; a?: string; b?: string; kind: 'same' | 'changed' | 'a' | 'b' }[] = [];
    let changed = 0, onlyA = 0, onlyB = 0;
    for (const [k, a] of ma) {
      const b = mb.get(k);
      const kind = b === undefined ? 'a' : a === b ? 'same' : 'changed';
      if (kind === 'a') onlyA++; else if (kind === 'changed') changed++;
      if (!this.onlyDiff() || kind !== 'same') rows.push({ k, a, b, kind });
    }
    for (const [k, b] of mb) if (!ma.has(k)) { onlyB++; rows.push({ k, b, kind: 'b' }); }
    return { rows, changed, onlyA, onlyB };
  });

  // ---- Poll: a value (or a column) read every few seconds, drawn as a chart ----
  pollSecs = 5; pollOid = '';
  readonly polling = signal(false);
  readonly series = signal<{ key: string; rate: boolean; pts: { t: number; v: number }[]; last?: { t: number; raw: number } }[]>([]);
  readonly pollN = signal(0);
  private pollTimer: ReturnType<typeof setInterval> | undefined;
  startPoll() {
    this.pollOid = this.oid.trim();
    if (!this.pollOid || !this.path()) return;
    this.polling.set(true);
    void this.pollOnce();
    this.pollTimer = setInterval(() => void this.pollOnce(), Math.max(1, Number(this.pollSecs) || 5) * 1000);
  }
  stopPoll() { this.polling.set(false); clearInterval(this.pollTimer); }
  clearPoll() { this.series.set([]); this.pollN.set(0); }
  private async pollOnce() {
    const scalar = /\.\d+$/.test(this.pollOid) && !/^[\d.]+$/.test(this.pollOid) || /\.0$/.test(this.pollOid);
    try {
      const r = await this.backend.snmpRun({ session: this.path(), op: scalar ? 'get' : 'walk', oids: [this.pollOid], max: 64, timeout: Number(this.timeout) || 3 });
      const now = Date.now();
      const list = [...this.series()];
      for (const row of r.rows.slice(0, 8)) {
        const raw = Number(row.value);
        if (!isFinite(raw)) continue;
        const key = (row.object ? row.object + (row.instance ? '.' + row.instance : '') : row.name);
        let sr = list.find(x => x.key === key);
        if (!sr) { sr = { key, rate: /^Counter/i.test(row.type), pts: [] }; list.push(sr); }
        if (sr.rate) {
          if (sr.last && now > sr.last.t) { const d = raw - sr.last.raw; sr.pts = [...sr.pts, { t: now, v: Math.max(0, d) / ((now - sr.last.t) / 1000) }]; }
          sr.last = { t: now, raw };
        } else sr.pts = [...sr.pts, { t: now, v: raw }];
        if (sr.pts.length > 180) sr.pts = sr.pts.slice(-180);
      }
      this.series.set(list.map(x => ({ ...x })));
      this.pollN.update(n => n + 1);
    } catch (e) { this.error.set(errText(e)); this.stopPoll(); }
  }
  private range() {
    const vs = this.series().flatMap(s => s.pts.map(p => p.v));
    const lo = Math.min(0, ...vs), hi = Math.max(1, ...vs);
    return { lo, hi: hi === lo ? lo + 1 : hi };
  }
  private tRange() { const ts = this.series().flatMap(s => s.pts.map(p => p.t)); return { a: Math.min(...ts), b: Math.max(...ts) }; }
  line(sr: { pts: { t: number; v: number }[] }) {
    const { lo, hi } = this.range(), { a, b } = this.tRange(), w = Math.max(1, b - a);
    return sr.pts.map(p => `${((p.t - a) / w) * 1000},${250 - ((p.v - lo) / (hi - lo)) * 235}`).join(' ');
  }
  grid() { const { lo, hi } = this.range(); return [0, 0.25, 0.5, 0.75, 1].map(f => ({ y: 250 - f * 235, l: this.fmt(lo + f * (hi - lo)) })); }
  stat(sr: { pts: { v: number }[] }) { const v = sr.pts.map(p => p.v); return v.length ? { min: Math.min(...v), max: Math.max(...v), avg: v.reduce((x, y) => x + y, 0) / v.length } : { min: NaN, max: NaN, avg: NaN }; }
  fmt(v: number | undefined) {
    if (v === undefined || !isFinite(v)) return '';
    const a = Math.abs(v);
    return a >= 1e9 ? (v / 1e9).toFixed(2) + ' G' : a >= 1e6 ? (v / 1e6).toFixed(2) + ' M' : a >= 1e4 ? (v / 1e3).toFixed(1) + ' k' : Number.isInteger(v) ? String(v) : v.toFixed(2);
  }
  color(i: number) { return ['#3b82f6', '#22c55e', '#f59e0b', '#ef4444', '#a855f7', '#14b8a6', '#ec4899', '#64748b'][i % 8]; }

  // ---- Send a trap ----
  readonly auths = ['MD5', 'SHA', 'SHA-224', 'SHA-256', 'SHA-384', 'SHA-512'];
  readonly privs = ['DES', 'AES-128', 'AES-192', 'AES-256'];
  readonly sendOpen = signal(false);
  readonly trapMsg = signal('');
  tr = { address: '127.0.0.1:162', version: '2c', community: 'public', user: '', auth: '', authPass: '', priv: '', privPass: '', trapOid: 'IF-MIB::linkDown' };
  trapVals = 'ifIndex.3 = 3\nifAdminStatus.3 = up\nifOperStatus.3 = down';
  async sendTrap() {
    const values = this.trapVals.split('\n').map(l => l.trim()).filter(Boolean).map(l => { const i = l.indexOf('='); return i < 0 ? { oid: l, value: '' } : { oid: l.slice(0, i).trim(), value: l.slice(i + 1).trim() }; });
    try {
      await this.backend.snmpSendTrap({ ...this.tr, values });
      this.trapMsg.set(tr('Sent {0} to {1}', this.tr.trapOid, this.tr.address));
    } catch (e) { this.trapMsg.set(errText(e)); }
  }

  // ---- MIBs ----
  readonly mibOpen = signal(false);
  readonly mibs = signal<SNMPModule[]>([]);
  readonly mibFolder = signal('');
  mibQ = '';
  async openMibs() {
    this.mibOpen.set(true);
    this.mibs.set(await this.backend.snmpMibModules().catch(() => []));
    this.mibFolder.set(await this.backend.snmpMibFolder().catch(() => ''));
  }
  userMibs() { return this.mibs().filter(m => !m.builtIn).length; }
  mibShown() { const q = this.mibQ.trim().toLowerCase(); return this.mibs().filter(m => !q || m.name.toLowerCase().includes(q) || (m.file ?? '').toLowerCase().includes(q)); }
  needs(m: SNMPModule) { return (m.imports ?? []).filter(i => !['SNMPv2-SMI', 'SNMPv2-TC', 'SNMPv2-CONF', 'RFC1155-SMI', 'RFC-1212', 'RFC-1215'].includes(i)); }
  missingAll() { return [...new Set(this.mibs().flatMap(m => m.missing ?? []))].sort(); }
  async removeMib(m: SNMPModule) {
    const deps = m.dependents ?? [];
    const msg = tr('Stop loading {0}? Its file {1} is moved to the removed folder next to the MIB files, so it can be put back.', m.name, m.file ?? '') +
      (deps.length ? '\n\n' + tr('These MIBs import it and lose the objects that hang under it: {0}', deps.join(', ')) : '');
    if (!(await this.ui.confirm(tr('Remove {0}', m.name), msg, tr('Remove'), true))) return;
    try { await this.backend.snmpRemoveMib(m.name); await this.afterMibs(); } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  /** Opens the tree along a module's top object and selects it. */
  async showInTree(m: SNMPModule) {
    if (!m.root) return;
    this.mibOpen.set(false);
    this.query.set('');
    await this.reveal(m.root);
  }
  async reveal(oid: string) {
    const parts = oid.split('.');
    for (let n = 1; n <= parts.length; n++) {
      const pre = parts.slice(0, n).join('.');
      const it = this.items().find(i => i.node.oid === pre);
      if (!it) continue;
      if (pre === oid) { this.pick(it.node); break; }
      if (!it.open && it.node.children) await this.toggle(it);
    }
    setTimeout(() => this.host.nativeElement.querySelector('.tn.on')?.scrollIntoView({ block: 'center' }));
  }
  kindMark(n: SNMPMibNode) { return ({ module: 'M', table: 'T', row: 'R', column: 'C', scalar: 'S', notification: 'N', group: 'G', compliance: '✓' } as Record<string, string>)[n.kind] ?? '•'; }

  // ---- the OID box keeps the last objects run ----
  readonly recent = signal<string[]>(loadRecent());
  recentOpts() { return this.recent().map(v => ({ value: v })); }
  private remember(o: string) {
    const l = [o, ...this.recent().filter(x => x !== o)].slice(0, 15);
    this.recent.set(l);
    try { localStorage.setItem('tw.snmp.recent', JSON.stringify(l)); } catch { /* not kept */ }
  }

  showMibFolder() { void this.backend.showInFolder(this.mibFolder()).catch(e => this.ui.toast(errText(e), 'error')); }
  private async afterMibs() {
    this.mibs.set(await this.backend.snmpMibModules().catch(() => []));
    this.modules.set(this.mibs().length);
    this.items.set(this.items().map(i => ({ ...i, open: false })).filter(i => i.depth === 0));
  }

  private addHist(h: HistItem) { this.hist.update(l => [h, ...l].slice(0, 100)); }
  replay(h: HistItem) { this.op = h.op; this.oid = h.oid; void this.choose(h.session).then(() => this.run()); }

  async loadMib() {
    try {
      const mods = await this.backend.snmpLoadMibs();
      if (mods?.length) {
        this.ui.toast(tr('Loaded {0}', mods.join(', ')), 'success');
        await this.afterMibs();
      }
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }

  async trapStart() { try { this.traps.set({ ...this.traps(), state: await this.backend.snmpTrapsStart(Number(this.trapPort) || 162) }); } catch (e) { this.traps.set({ ...this.traps(), state: { ...this.traps().state, on: false, error: errText(e) } }); } }
  async trapStop() { await this.backend.snmpTrapsStop().catch(() => {}); this.traps.set(await this.backend.snmpTraps()); }
  async trapClear() { await this.backend.snmpTrapsClear().catch(() => {}); this.traps.set(await this.backend.snmpTraps()); }

  isFav(n: SNMPMibNode) { return this.favs().includes(n.module + '::' + n.name); }
  fav(n: SNMPMibNode) {
    const k = n.module + '::' + n.name;
    this.favs.update(l => l.includes(k) ? l.filter(x => x !== k) : [...l, k]);
    try { localStorage.setItem(FAV_KEY, JSON.stringify(this.favs())); } catch { /* not kept */ }
  }

  exportCsv() {
    const r = this.result();
    if (!r) return;
    const q = (v: string) => '"' + String(v ?? '').replace(/"/g, '""') + '"';
    const csv = ['Name,Index,Type,Value,OID', ...r.rows.map(x => [x.object || x.name, x.instance ?? '', x.type, x.display, x.oid].map(q).join(','))].join('\n');
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob([csv], { type: 'text/csv' }));
    a.download = 'snmp-' + (this.path().split('/').pop() || 'result') + '.csv';
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  }

  tone(x: SNMPRow): string {
    if (/^(up|true|active|forwarding)\b/.test(x.display)) return 'ok';
    if (/^(down|false|notPresent|lowerLayerDown|blocking)\b/.test(x.display)) return 'bad';
    return /\(\d+\)$/.test(x.display) ? 'en' : '';
  }
  kindText(n: SNMPMibNode): string {
    if (n.kind === 'table') return 'Table';
    if (n.kind === 'row') return 'Row · index ' + (n.index?.join(', ') || n.augments || '');
    return n.kind.charAt(0).toUpperCase() + n.kind.slice(1);
  }
  enumText(n: SNMPMibNode): string { return n.enums ? Object.entries(n.enums).map(([k, v]) => `${v}(${k})`).join(' ') : ''; }
  opName(op: string) { return OPS.find(o => o.v === op)?.l ?? op; }
  time(t: number) { return new Date(t).toLocaleTimeString(); }
}

function flat(n: SessionNode | null, out: SessionNode[] = []): SessionNode[] {
  if (!n) return out;
  if (!n.isFolder && n.path) out.push(n);
  for (const c of n.children ?? []) flat(c, out);
  return out;
}

function loadRecent(): string[] {
  try { const v = JSON.parse(localStorage.getItem('tw.snmp.recent') || 'null'); if (Array.isArray(v)) return v.slice(0, 15); } catch { /* none */ }
  return [];
}

function loadFavs(): string[] {
  try { const v = JSON.parse(localStorage.getItem(FAV_KEY) || 'null'); if (Array.isArray(v)) return v; } catch { /* none */ }
  return ['SNMPv2-MIB::sysUpTime', 'IF-MIB::ifOperStatus', 'LLDP-MIB::lldpRemSysName'];
}

/** Columns of a walked table: objects across, instances down (null when the rows are not a table). */
export function pivot(rows: SNMPRow[]): { cols: string[]; rows: { idx: string; v: Record<string, string> }[] } | null {
  const cols: string[] = [];
  const byIdx = new Map<string, Record<string, string>>();
  for (const r of rows) {
    if (!r.object || !r.instance || r.instance === '0') continue;
    if (!cols.includes(r.object)) cols.push(r.object);
    const v = byIdx.get(r.instance) ?? {};
    v[r.object] = r.display;
    byIdx.set(r.instance, v);
  }
  if (cols.length < 2 || !byIdx.size) return null;
  return { cols, rows: [...byIdx.entries()].map(([idx, v]) => ({ idx, v })) };
}
