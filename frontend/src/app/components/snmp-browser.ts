import { Component, OnDestroy, OnInit, computed, effect, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { SlicePipe } from '@angular/common';
import { Backend, errText } from '../core/backend';
import { Session, SessionNode, SNMPMibNode, SNMPResult, SNMPRow, SNMPTrapsView } from '../core/models';
import { TabView, Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';

interface TreeItem { node: SNMPMibNode; depth: number; open: boolean; }
interface HistItem { at: number; op: string; oid: string; session: string; n: number; ms: number; err?: string; }

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
  imports: [FormsModule, SlicePipe],
  template: `
    <div class="sb">
      <div class="head">
        <h2>SNMP Browser</h2>
        <select class="sess" [ngModel]="path()" (ngModelChange)="choose($event)" title="Session the requests go to (address and credentials from the Session Manager)">
          <option value="">Choose a session…</option>
          @for (s of sessions(); track s.path) { <option [value]="s.path">{{ s.path }}{{ s.hostname ? ' · ' + s.hostname : '' }}</option> }
        </select>
        @if (sess(); as s) {
          <span class="cred" [class.v3]="ver() === '3'" translate="no">{{ credText() }}</span>
          <a class="lnk" (click)="openSettings(s.path)">SNMP settings…</a>
        }
        <span class="sp"></span>
        <button type="button" (click)="loadMib()">Load MIB…</button>
        <button type="button" (click)="view.set('traps')">Traps</button>
        <button type="button" (click)="exportCsv()" [disabled]="!result()?.rows?.length">Export CSV</button>
      </div>
      <div class="bar">
        <label class="op">Operation
          <select [(ngModel)]="op">@for (o of ops; track o.v) { <option [value]="o.v">{{ o.l }}</option> }</select>
        </label>
        <input class="oid" [(ngModel)]="oid" (keydown.enter)="run()" placeholder="OID or name: sysDescr.0, IF-MIB::ifTable, 1.3.6.1.2.1.2.2" spellcheck="false" translate="no" />
        @if (op === 'set') {
          <select class="ty" [(ngModel)]="setType" title="Type (empty = from the MIB)">@for (t of setTypes; track t) { <option [value]="t">{{ t || 'Type from MIB' }}</option> }</select>
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
                  <div class="tn" [class.on]="sel()?.oid === it.node.oid" [style.padding-left.px]="8 + it.depth * 14" (click)="pick(it.node)" (dblclick)="runOn(it.node)" translate="no">
                    <span class="tw" (click)="toggle(it, $event)">{{ it.node.children ? (it.open ? '▾' : '▸') : '' }}</span>
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
            <a [class.on]="view() === 'traps'" (click)="view.set('traps')">Traps <i>{{ traps().traps.length }}</i></a>
            <a [class.on]="view() === 'history'" (click)="view.set('history')">History</a>
          </div>
          <div class="body">
            @if (error()) { <div class="err">{{ error() }}</div> }
            @switch (view()) {
              @case ('results') {
                @if (result(); as r) {
                  <table><thead><tr><th>Name</th><th>Index</th><th>Type</th><th>Value</th><th>OID</th></tr></thead><tbody>
                    @for (x of r.rows; track $index) {
                      <tr (click)="oid = x.oid" translate="no"><td>{{ x.object || x.name }}</td><td>{{ x.instance }}</td><td>{{ x.type }}</td>
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
              @case ('traps') {
                <div class="tbar">
                  @if (traps().state.on) {
                    <span class="ok">● Listening on UDP {{ traps().state.port }}</span><button type="button" (click)="trapStop()">Stop</button>
                  } @else {
                    <label class="sm">UDP port <input type="number" [(ngModel)]="trapPort" /></label><button type="button" class="pri" (click)="trapStart()">Start Receiver</button>
                    @if (traps().state.error) { <span class="bad">{{ traps().state.error }}</span> }
                  }
                  <span class="sp"></span><button type="button" (click)="trapClear()" [disabled]="!traps().traps.length">Clear</button>
                </div>
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
    </div>
  `,
  styles: [`
    :host { display: block; height: 100%; }
    .sb { height: 100%; display: flex; flex-direction: column; gap: 10px; padding: 12px 14px; box-sizing: border-box; background: var(--bg); color: var(--text); font-size: 13px; }
    .head, .bar, .tbar, .foot { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
    h2 { margin: 0 6px 0 0; font-size: 17px; font-weight: 600; }
    .sp { flex: 1; }
    .sess { max-width: 320px; }
    .cred { border: 1px solid var(--border); border-radius: 12px; padding: 2px 10px; color: var(--muted); font-size: 12px; }
    .cred.v3 { border-color: color-mix(in srgb, var(--ok) 55%, transparent); color: var(--ok); }
    .lnk { color: var(--accent); cursor: pointer; font-size: 12px; }
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
    .tn .tw { width: 10px; color: var(--muted); display: inline-block; }
    .tn .o { color: var(--faint); font-size: 11px; overflow: hidden; text-overflow: ellipsis; }
    .tn .tb { color: var(--accent); }
    .det dl { display: grid; grid-template-columns: 90px 1fr; gap: 5px 10px; margin: 0; padding: 10px 12px; max-height: 260px; overflow: auto; }
    .det dt { color: var(--muted); } .det dd { margin: 0; } .desc { color: var(--muted); white-space: pre-wrap; }
    .star { margin-left: 6px !important; cursor: pointer; color: var(--warn); }
    .favs { max-height: 160px; } .favs .tn { padding: 3px 12px; }
    .right { min-width: 0; }
    .tabs { display: flex; gap: 4px; border-bottom: 1px solid var(--border); padding: 0 10px; }
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
  private readonly tabs = inject(Tabs);
  readonly ui = inject(Ui);

  readonly ops = OPS; readonly setTypes = SET_TYPES;
  op = 'walk'; oid = ''; setType = ''; setValue = ''; maxRep = 25; timeout = 3; trapPort = 162;

  readonly path = signal('');
  readonly sess = signal<Session | null>(null);
  readonly sessions = computed(() => flat(this.ui.tree()).filter(n => !['RDP', 'VNC', 'Local Shell', 'WOL', 'Serial'].includes(n.protocol ?? '')));
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
  readonly view = signal<'results' | 'table' | 'traps' | 'history'>('results');
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
  ngOnDestroy() { this.off?.(); clearTimeout(this.searchT); }

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
      if (this.view() !== 'table' || op !== 'table') this.view.set(op === 'table' ? 'table' : 'results');
      this.addHist({ at: Date.now(), op, oid, session: this.path(), n: r.rows.length, ms: r.ms });
    } catch (e) {
      this.error.set(errText(e));
      this.addHist({ at: Date.now(), op, oid, session: this.path(), n: 0, ms: Date.now() - t0, err: errText(e) });
    }
    this.busy.set(false);
  }
  private addHist(h: HistItem) { this.hist.update(l => [h, ...l].slice(0, 100)); }
  replay(h: HistItem) { this.op = h.op; this.oid = h.oid; void this.choose(h.session).then(() => this.run()); }

  async loadMib() {
    try {
      const mods = await this.backend.snmpLoadMibs();
      if (mods?.length) {
        this.ui.toast(tr('Loaded {0}', mods.join(', ')), 'success');
        this.modules.set((await this.backend.snmpMibModules()).length);
        this.items.set(this.items().map(i => ({ ...i, open: false })).filter(i => i.depth === 0));
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
