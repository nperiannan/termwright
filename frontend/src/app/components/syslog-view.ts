import { Component, ElementRef, OnDestroy, OnInit, computed, effect, inject, input, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { SyslogMsg, SyslogQuery, SyslogRule, SyslogSendIn, SyslogSettings, SyslogState } from '../core/models';
import { TabView, Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { Combo, ComboOption } from './combo';

const SEV_CODE = ['emerg', 'alert', 'crit', 'err', 'warning', 'notice', 'info', 'debug'];
const SEV = ['Emergency', 'Alert', 'Critical', 'Error', 'Warning', 'Notice', 'Informational', 'Debug'];
const FAC = ['kern', 'user', 'mail', 'daemon', 'auth', 'syslog', 'lpr', 'news', 'uucp', 'cron', 'authpriv', 'ftp', 'ntp', 'audit', 'alert', 'clock', 'local0', 'local1', 'local2', 'local3', 'local4', 'local5', 'local6', 'local7'];
const COLORS = ['', 'red', 'amber', 'green', 'blue', 'purple'];
const MAX_ROWS = 20000;

/** The syslog server: messages from devices (live), filters, rules, settings, test messages. */
@Component({
  selector: 'app-syslog-view',
  imports: [FormsModule, Combo],
  template: `
  <div class="sl">
    <div class="head">
      <h2>Syslog</h2>
      @if (st(); as s) {
        @if (s.on) { <span class="chip ok">● Receiving on {{ s.addrs.join(' · ') }}</span><button type="button" (click)="stop()">Stop</button> }
        @else { <span class="chip">○ Not receiving</span><button type="button" class="pri" (click)="start()">Start Receiving</button> }
        @if (s.error) { <span class="chip bad">{{ s.error }}</span> }
      }
      <span class="sp"></span>
      <button type="button" (click)="panel.set(panel() === 'settings' ? '' : 'settings')" [class.on]="panel() === 'settings'">Settings</button>
      <button type="button" (click)="openRules()" [class.on]="panel() === 'rules'">Rules{{ rules().length ? ' (' + rules().length + ')' : '' }}</button>
      <button type="button" (click)="panel.set(panel() === 'send' ? '' : 'send')" [class.on]="panel() === 'send'">Send Test Message</button>
      <button type="button" (click)="export()" [disabled]="!rows().length">Export</button>
      <button type="button" (click)="clear()" [disabled]="!st()?.total">Clear</button>
    </div>
    <div class="sevs">
      @for (n of sevNames; track $index) {
        <button type="button" [class]="'sv s' + $index" [class.on]="maxSev() >= 0 && $index <= maxSev()" [class.has]="(st()?.counts?.[$index] ?? 0) > 0" (click)="setSev(maxSev() === $index ? -1 : $index)" [title]="tr2($index)">
          <span class="sn"><i></i>{{ n }}</span><b>{{ st()?.counts?.[$index] ?? 0 }}</b></button>
      }
    </div>
    <div class="sumline mut">{{ st()?.total ?? 0 }} messages · {{ hostCount() }} senders{{ st()?.folder ? ' · saved to ' + st()!.folder : '' }}{{ maxSev() >= 0 ? ' · showing ' + sevNames[maxSev()] + (maxSev() < 7 ? ' and worse' : '') : '' }}</div>
    <div class="bar">
      <input class="q" [ngModel]="query()" (ngModelChange)="setQuery($event)" placeholder="Search: words, or /regex/" spellcheck="false" />
      <app-combo class="snd" [value]="host()" (valueChange)="setHost($event)" [options]="senderOpts()" placeholder="All senders (type to search)" emptyText="No sender yet" name="sender" />
      <select class="fs" [ngModel]="maxSev()" (ngModelChange)="setSev(+$event)">
        <option [ngValue]="-1">All severities</option>
        @for (n of sevNames; track $index) { <option [ngValue]="$index">{{ n }}{{ $index < 7 ? ' or worse' : '' }}</option> }
      </select>
      <label class="chk"><input type="checkbox" [ngModel]="paused()" (ngModelChange)="pause($event)" /> Pause{{ paused() && newWhile() ? ' (' + newWhile() + ' new)' : '' }}</label>
      <label class="chk"><input type="checkbox" [(ngModel)]="follow" /> Follow</label>
    </div>

    @switch (panel()) {
      @case ('settings') {
        <div class="pnl">
          <div class="grid">
            <label>UDP port<input type="number" [(ngModel)]="set.udp" min="0" /></label>
            <label>TCP port<input type="number" [(ngModel)]="set.tcp" min="0" placeholder="0 = off" /></label>
            <label>TLS port<input type="number" [(ngModel)]="set.tls" min="0" placeholder="0 = off" /></label>
            <label>Listen on<input [(ngModel)]="set.bind" placeholder="all addresses" /></label>
            <label class="w2">TLS certificate (PEM)<input [(ngModel)]="set.cert" placeholder="empty: a self-signed one is made" /></label>
            <label class="w2">TLS key (PEM)<input [(ngModel)]="set.key" /></label>
          </div>
          <div class="checks">
            <label><input type="checkbox" [(ngModel)]="set.saveFiles" /> Save every message to a file per day</label>
            <label><input type="checkbox" [(ngModel)]="set.perHost" [disabled]="!set.saveFiles" /> One folder per sender</label>
            <label class="grow">Folder<input [(ngModel)]="set.folder" [disabled]="!set.saveFiles" placeholder="the syslog folder in Termwright's data folder" /></label>
            <label><input type="checkbox" [(ngModel)]="set.autoStart" /> Start receiving when Termwright starts</label>
          </div>
          <div class="hint">UDP 514 is the usual port. On macOS and Linux a port below 1024 may need administrator rights: use 1514 (and point the devices there) if it does not start. TCP takes one message per line or RFC 6587 octet counting; TLS is RFC 5425 (6514).</div>
          <div class="btns"><button type="button" class="pri" (click)="start()">{{ st()?.on ? 'Apply and Restart' : 'Save and Start' }}</button></div>
        </div>
      }
      @case ('rules') {
        <div class="pnl">
          <div class="hint">The first rule that matches a message colours it, notifies you, or hides it (noise). Match: words that must all be in it, or /regex/.</div>
          <table class="rt"><thead><tr><th>On</th><th>Name</th><th>Match</th><th>Severity</th><th>Sender</th><th>Colour</th><th>Notify</th><th>Hide</th><th></th></tr></thead><tbody>
            @for (r of editRules; track $index) {
              <tr><td><input type="checkbox" [ngModel]="!r.off" (ngModelChange)="r.off = !$event" /></td>
                <td><input [(ngModel)]="r.name" /></td><td><input [(ngModel)]="r.match" placeholder="link down, /BGP.*Idle/" spellcheck="false" /></td>
                <td><select class="fs" [(ngModel)]="r.maxSeverity"><option [ngValue]="-1">any</option>@for (n of sevNames; track $index) { <option [ngValue]="$index">{{ n }}{{ $index < 7 ? '+' : '' }}</option> }</select></td>
                <td><input [(ngModel)]="r.host" placeholder="any" /></td>
                <td><select class="fs" [(ngModel)]="r.color">@for (c of colors; track c) { <option [value]="c">{{ c || 'none' }}</option> }</select></td>
                <td><input type="checkbox" [(ngModel)]="r.notify" /></td><td><input type="checkbox" [(ngModel)]="r.hide" /></td>
                <td><a class="x" (click)="editRules.splice($index, 1)">✕</a></td></tr>
            }
          </tbody></table>
          <div class="btns"><button type="button" (click)="addRule()">+ Add Rule</button><span class="sp"></span>
            @if (ruleErr()) { <span class="bad">{{ ruleErr() }}</span> }
            <button type="button" (click)="panel.set('')">Cancel</button><button type="button" class="pri" (click)="saveRules()">Save Rules</button></div>
        </div>
      }
      @case ('send') {
        <div class="pnl">
          <div class="grid">
            <label class="w2">Syslog server<input [(ngModel)]="out.target" placeholder="host or host:port (this computer: 127.0.0.1)" /></label>
            <label>Transport<select class="fs" [(ngModel)]="out.proto"><option value="udp">UDP</option><option value="tcp">TCP</option><option value="tls">TLS</option></select></label>
            <label>Format<select class="fs" [(ngModel)]="out.format"><option value="5424">RFC 5424</option><option value="3164">RFC 3164 (BSD)</option></select></label>
            <label>Facility<select class="fs" [(ngModel)]="out.facility">@for (f of facNames; track f) { <option [value]="f">{{ f }}</option> }</select></label>
            <label>Severity<select class="fs" [(ngModel)]="out.severity">@for (n of sevNames; track n) { <option [value]="sevCodes[$index]">{{ n }}</option> }</select></label>
            <label>Host name<input [(ngModel)]="out.host" placeholder="this computer" /></label>
            <label>Program<input [(ngModel)]="out.app" placeholder="termwright" /></label>
            <label class="w3">Message<input [(ngModel)]="out.text" (keydown.enter)="send()" /></label>
            <label>Times<input type="number" min="1" max="1000" [(ngModel)]="out.count" /></label>
          </div>
          <div class="btns">@if (out.proto === 'tls') { <label class="chk"><input type="checkbox" [(ngModel)]="out.insecure" /> Do not check the server's certificate</label> }
            <span class="sp"></span>@if (sentMsg()) { <span class="mut mono">{{ sentMsg() }}</span> }
            <button type="button" class="pri" (click)="send()" [disabled]="!out.target.trim() || !out.text.trim()">Send</button></div>
        </div>
      }
    }

    <div class="table" #list (scroll)="onScroll()">
      <table>
        <thead><tr><th class="t">Received</th><th>From</th><th>Severity</th><th>Facility</th><th>Host</th><th>Program</th><th class="msg">Message</th></tr></thead>
        <tbody>
          @for (m of rows(); track m.id) {
            <tr [class]="'c-' + (m.color || '')" [class.sel]="sel()?.id === m.id" (click)="sel.set(sel()?.id === m.id ? null : m)">
              <td class="t mono">{{ time(m.at) }}</td>
              <td translate="no">{{ m.session ? m.session.split('/').pop() : m.from }}</td>
              <td><span [class]="'sb s' + m.severity">{{ sevNames[m.severity] ?? m.severityName }}</span></td>
              <td class="mut">{{ m.facilityName }}</td>
              <td translate="no">{{ m.host }}</td><td translate="no">{{ m.app }}</td>
              <td class="msg" translate="no">@if (m.rule) { <span class="rl">{{ m.rule }}</span> }{{ m.text }}</td>
            </tr>
          }
        </tbody>
      </table>
      @if (!rows().length) {
        <div class="empty">
          @if (st()?.on) { Waiting for messages. Point the devices' syslog (logging host) at this computer: {{ myAddrs() }}. }
          @else { Start Receiving, then point the devices' syslog (logging host) at this computer. Use Send Test Message to try it. }
        </div>
      }
    </div>
    @if (sel(); as m) {
      <div class="det">
        <div class="dh"><b translate="no">{{ m.session || m.from }}</b><span class="mut">{{ m.at }} · {{ m.proto.toUpperCase() }} · RFC {{ m.format }} · {{ m.facilityName }}.{{ m.severityName }} ({{ m.facility * 8 + m.severity }})</span><span class="sp"></span>
          @if (m.session) { <button type="button" (click)="connect(m.session)">Connect</button> }
          <button type="button" (click)="filterHost(m.from)">Only This Sender</button><button type="button" (click)="copy(m.raw)">Copy</button><a class="x" (click)="sel.set(null)">✕</a></div>
        <dl translate="no">
          @if (m.time) { <dt>Device time</dt><dd>{{ m.time }}</dd> }
          @if (m.procId || m.msgId) { <dt>Process / ID</dt><dd>{{ m.procId || '-' }} / {{ m.msgId || '-' }}</dd> }
          @if (m.sd) { <dt>Structured data</dt><dd class="mono">{{ m.sd }}</dd> }
          <dt>Message</dt><dd>{{ m.text }}</dd>
          <dt>Raw</dt><dd class="mono">{{ m.raw }}</dd>
        </dl>
      </div>
    }
  </div>
  `,
  styles: [`
    :host { display: block; height: 100%; }
    .sl { height: 100%; display: flex; flex-direction: column; gap: 8px; padding: 12px 14px; box-sizing: border-box; background: var(--bg); color: var(--text); font-size: 13px; }
    .head, .bar, .btns, .checks { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
    h2 { margin: 0 6px 0 0; font-size: 17px; font-weight: 600; }
    .sp { flex: 1; } .mut { color: var(--muted); font-size: 12px; } .mono { font-family: var(--mono); font-size: 12px; } .bad { color: var(--danger); }
    .chip { border: 1px solid var(--border); border-radius: 12px; padding: 2px 10px; font-size: 12px; color: var(--muted); }
    .chip.ok { border-color: color-mix(in srgb, var(--ok) 55%, transparent); color: var(--ok); } .chip.bad { border-color: color-mix(in srgb, var(--danger) 55%, transparent); color: var(--danger); }
    button.pri { background: var(--accent); color: #fff; border-color: var(--accent); } button.on { border-color: var(--accent); color: var(--accent); }
    .sevs { display: grid; grid-template-columns: repeat(8, minmax(0, 1fr)); gap: 8px; }
    @media (max-width: 900px) { .sevs { grid-template-columns: repeat(4, minmax(0, 1fr)); } }
    .sv { --c: var(--faint); display: flex; flex-direction: column; align-items: flex-start; gap: 4px; min-width: 0; height: auto !important; padding: 8px 12px !important;
      border: 1px solid var(--border) !important; border-radius: 8px !important; background: var(--panel) !important; color: var(--muted) !important; cursor: pointer; text-align: left; box-shadow: none !important; }
    .sv:hover { border-color: color-mix(in srgb, var(--c) 55%, var(--border)) !important; }
    .sv .sn { display: flex; align-items: center; gap: 6px; font-size: 11.5px; font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 100%; }
    .sv .sn i { width: 8px; height: 8px; border-radius: 50%; background: var(--c); flex: none; }
    .sv b { font-size: 20px; line-height: 1.1; font-weight: 600; color: var(--muted); font-variant-numeric: tabular-nums; }
    .sv.has b { color: var(--text); } .sv.has.s0 b, .sv.has.s1 b, .sv.has.s2 b, .sv.has.s3 b { color: var(--c); }
    .sv.on { background: color-mix(in srgb, var(--c) 10%, var(--panel)) !important; border-color: var(--c) !important; }
    .sv.s0 { --c: #be123c; } .sv.s1 { --c: #e11d48; } .sv.s2 { --c: #f43f5e; } .sv.s3 { --c: var(--danger); } .sv.s4 { --c: var(--warn); }
    .sv.s5 { --c: var(--accent); } .sv.s6 { --c: var(--ok); } .sv.s7 { --c: var(--faint); }
    .sumline { font-size: 12px; }
    .snd { width: 260px; }
    .bar .q { flex: 1; min-width: 220px; font-family: var(--mono); }
    .chk { display: inline-flex; gap: 5px; align-items: center; color: var(--muted); }
    .pnl { background: var(--panel); border: 1px solid var(--border); border-radius: var(--r-lg); padding: 12px 14px; display: flex; flex-direction: column; gap: 10px; }
    .grid { display: grid; grid-template-columns: repeat(4, minmax(120px, 1fr)); gap: 8px 12px; }
    .grid label, .checks label.grow { display: flex; flex-direction: column; gap: 4px; color: var(--muted); font-size: 12px; }
    .checks label.grow { flex: 1; min-width: 240px; } .checks label { display: inline-flex; gap: 5px; align-items: center; color: var(--muted); }
    .w2 { grid-column: span 2; } .w3 { grid-column: span 3; }
    .hint { color: var(--muted); font-size: 12px; line-height: 1.5; }
    .rt { width: 100%; border-collapse: collapse; } .rt th { text-align: left; font-weight: 500; color: var(--muted); padding: 4px; } .rt td { padding: 3px 4px; }
    .rt input:not([type=checkbox]) { width: 100%; box-sizing: border-box; } .x { cursor: pointer; color: var(--muted); }
    .table { flex: 1; min-height: 0; overflow: auto; background: var(--panel); border: 1px solid var(--border); border-radius: var(--r-lg); }
    table { width: 100%; border-collapse: collapse; }
    th { text-align: left; font-weight: 500; color: var(--muted); padding: 7px 10px; position: sticky; top: 0; background: var(--panel); border-bottom: 1px solid var(--border); white-space: nowrap; }
    td { padding: 4px 10px; border-bottom: 1px solid var(--line); white-space: nowrap; vertical-align: top; }
    td.msg { white-space: normal; word-break: break-word; width: 100%; } .t { width: 1%; }
    tbody tr { cursor: pointer; } tbody tr:hover { background: var(--hover); } tr.sel { background: var(--selected); }
    tr.c-red { box-shadow: inset 3px 0 0 #ef4444; background: color-mix(in srgb, #ef4444 9%, transparent); }
    tr.c-amber { box-shadow: inset 3px 0 0 #f59e0b; background: color-mix(in srgb, #f59e0b 9%, transparent); }
    tr.c-green { box-shadow: inset 3px 0 0 #22c55e; background: color-mix(in srgb, #22c55e 8%, transparent); }
    tr.c-blue { box-shadow: inset 3px 0 0 #3b82f6; background: color-mix(in srgb, #3b82f6 8%, transparent); }
    tr.c-purple { box-shadow: inset 3px 0 0 #a855f7; background: color-mix(in srgb, #a855f7 8%, transparent); }
    .sb { font-size: 11px; padding: 1px 7px; border-radius: 9px; border: 1px solid currentColor; }
    .sb.s0, .sb.s1, .sb.s2 { color: #fff; background: #e11d48; border-color: #e11d48; } .sb.s3 { color: var(--danger); } .sb.s4 { color: var(--warn); }
    .sb.s5 { color: var(--accent); } .sb.s6 { color: var(--ok); } .sb.s7 { color: var(--faint); }
    .rl { font-size: 11px; color: var(--muted); border: 1px solid var(--border); border-radius: 4px; padding: 0 5px; margin-right: 6px; }
    .empty { padding: 40px; text-align: center; color: var(--muted); line-height: 1.7; }
    .det { background: var(--panel); border: 1px solid var(--border); border-radius: var(--r-lg); padding: 10px 14px; max-height: 32%; overflow: auto; }
    .dh { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; margin-bottom: 6px; }
    .det dl { display: grid; grid-template-columns: 130px 1fr; gap: 4px 10px; margin: 0; } .det dt { color: var(--muted); } .det dd { margin: 0; word-break: break-word; }
  `],
})
export class SyslogView implements OnInit, OnDestroy {
  readonly tab = input.required<TabView>();
  readonly visible = input(true);
  private readonly backend = inject(Backend);
  private readonly tabs = inject(Tabs);
  readonly ui = inject(Ui);
  private readonly list = viewChild<ElementRef<HTMLElement>>('list');

  readonly sevCodes = SEV_CODE; readonly sevNames = SEV; readonly facNames = FAC; readonly colors = COLORS;
  readonly st = signal<SyslogState | null>(null);
  readonly rows = signal<SyslogMsg[]>([]);
  readonly query = signal('');
  readonly host = signal('');
  readonly maxSev = signal(-1);
  readonly paused = signal(false);
  readonly newWhile = signal(0);
  readonly sel = signal<SyslogMsg | null>(null);
  readonly panel = signal<'' | 'settings' | 'rules' | 'send'>('');
  readonly rules = signal<SyslogRule[]>([]);
  readonly ruleErr = signal('');
  readonly sentMsg = signal('');
  readonly hosts = computed(() => Object.entries(this.st()?.hosts ?? {}).map(([k, n]) => ({ k, n })).sort((a, b) => b.n - a.n));
  readonly hostCount = computed(() => this.hosts().length);
  /** Senders with their saved session (searchable by address or name), the busiest first. */
  readonly senderOpts = computed<ComboOption[]>(() => this.hosts().map(h => ({ value: h.k, label: this.sessionName(h.k), hint: tr('{0} messages', h.n) } as ComboOption)));
  private sessionName(addr: string) {
    let name = '';
    const walk = (n: { children?: any[]; isFolder: boolean; hostname?: string; path: string } | null) => n?.children?.forEach(c => { if (!name) { if (c.isFolder) walk(c); else if (c.hostname === addr) name = c.path; } });
    walk(this.ui.tree());
    return name;
  }
  setHost(v: string) {
    const t = v.trim();
    if (t === this.host() || (t && !this.hosts().some(h => h.k === t))) return; // a sender is chosen when typed in full or picked
    this.host.set(t); void this.reload();
  }
  follow = true;
  set: SyslogSettings = { udp: 514, tcp: 0, tls: 0 };
  editRules: SyslogRule[] = [];
  out: SyslogSendIn & { proto: string; format: string; facility: string; severity: string; count: number } = { target: '127.0.0.1', proto: 'udp', format: '5424', facility: 'local7', severity: 'notice', text: 'Test message from Termwright', count: 1 };
  private lastId = 0;
  private offs: (() => void)[] = [];
  private qTimer: ReturnType<typeof setTimeout> | undefined;
  private atBottom = true;

  constructor() {
    effect(() => { const p = this.tab().syslogPage; if (p?.startsWith('send')) this.panel.set('send'); });
  }

  async ngOnInit() {
    this.set = await this.backend.syslogSettings().catch(() => this.set);
    this.out.target = '127.0.0.1' + (this.set.udp && this.set.udp !== 514 ? ':' + this.set.udp : '');
    this.rules.set(await this.backend.syslogRules().catch(() => []));
    await this.refreshState();
    await this.reload();
    this.offs.push(this.backend.on('syslog:msgs', () => void this.more()));
  }
  ngOnDestroy() { this.offs.forEach(f => f()); clearTimeout(this.qTimer); }

  tr2(i: number) { return tr('Show {0} and worse', SEV[i]); }
  private q(after = 0): SyslogQuery { return { after, text: this.query(), host: this.host(), maxSeverity: this.maxSev() >= 0 ? this.maxSev() : undefined, limit: after ? 5000 : 5000 }; }
  async refreshState() { this.st.set(await this.backend.syslogState().catch(() => null)); }
  async reload() {
    try {
      const l = await this.backend.syslogMessages(this.q());
      this.rows.set(l);
      this.lastId = Math.max(this.lastId, l.length ? l[l.length - 1].id : 0);
      this.newWhile.set(0);
      this.scrollDown();
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  /** New messages arrived: the matching ones are added (not while paused). */
  async more() {
    await this.refreshState();
    if (this.paused()) { this.newWhile.update(n => n + 1); return; }
    try {
      const l = await this.backend.syslogMessages(this.q(this.lastId));
      if (!l.length) return;
      this.lastId = l[l.length - 1].id;
      this.rows.update(r => { const n = [...r, ...l]; return n.length > MAX_ROWS ? n.slice(n.length - MAX_ROWS) : n; });
      this.scrollDown();
    } catch { /* a bad search shows on reload */ }
  }
  private scrollDown() {
    if (!this.follow || !this.atBottom) return;
    setTimeout(() => { const el = this.list()?.nativeElement; if (el) el.scrollTop = el.scrollHeight; });
  }
  onScroll() { const el = this.list()?.nativeElement; if (el) this.atBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - 40; }

  setQuery(v: string) { this.query.set(v); clearTimeout(this.qTimer); this.qTimer = setTimeout(() => void this.reload(), 250); }
  setSev(v: number) { this.maxSev.set(v); void this.reload(); }
  pause(on: boolean) { this.paused.set(on); if (!on) void this.reload(); }
  filterHost(h: string) { this.host.set(h); this.sel.set(null); void this.reload(); }

  async start() {
    try {
      const st = await this.backend.syslogStart({ ...this.set, udp: Number(this.set.udp) || 0, tcp: Number(this.set.tcp) || 0, tls: Number(this.set.tls) || 0 });
      this.st.set(st);
      this.panel.set('');
      this.ui.toast(tr('Syslog: receiving on {0}', st.addrs.join(', ')), 'success');
    } catch (e) {
      await this.refreshState();
      this.panel.set('settings');
      this.ui.toast(errText(e), 'error');
    }
  }
  async stop() { await this.backend.syslogStop().catch(() => {}); await this.refreshState(); }
  async clear() {
    if (!(await this.ui.confirm(tr('Clear Messages'), tr('Forget the {0} messages received? Files already written stay.', this.st()?.total ?? 0), tr('Clear')))) return;
    await this.backend.syslogClear().catch(() => {}); this.rows.set([]); this.sel.set(null); await this.refreshState();
  }
  async export() { try { const p = await this.backend.syslogExport(this.q()); if (p) this.ui.toast(tr('Saved {0}', p), 'success'); } catch (e) { this.ui.toast(errText(e), 'error'); } }

  openRules() {
    if (this.panel() === 'rules') { this.panel.set(''); return; }
    this.editRules = structuredClone(this.rules());
    if (!this.editRules.length) this.editRules = [{ name: 'Links', match: '/link (up|down)|changed state to (up|down)/', maxSeverity: -1, color: 'amber' }, { name: 'Errors', match: '', maxSeverity: 3, color: 'red', notify: true }];
    this.ruleErr.set('');
    this.panel.set('rules');
  }
  addRule() { this.editRules.push({ name: '', match: '', maxSeverity: -1, color: 'blue' }); }
  async saveRules() {
    const l = this.editRules.filter(r => r.name.trim() || r.match?.trim()).map(r => ({ ...r, name: r.name.trim() || r.match!.trim() }));
    try { await this.backend.syslogSetRules(l); this.rules.set(l); this.panel.set(''); await this.reload(); } catch (e) { this.ruleErr.set(errText(e)); }
  }

  async send() {
    try {
      const m = await this.backend.syslogSend({ ...this.out, count: Number(this.out.count) || 1 });
      this.sentMsg.set(tr('Sent: {0}', m.length > 110 ? m.slice(0, 110) + '…' : m));
    } catch (e) { this.sentMsg.set(''); this.ui.toast(errText(e), 'error'); }
  }

  connect(path: string) { void this.tabs.openSession(path); }
  copy(t: string) { void navigator.clipboard?.writeText(t).then(() => this.ui.toast(tr('Copied'), 'success')).catch(() => {}); }
  time(at: string) {
    const d = new Date(at), p = (n: number, w = 2) => String(n).padStart(w, '0');
    if (isNaN(+d)) return at;
    const today = new Date().toDateString() === d.toDateString();
    return (today ? '' : `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} `) + `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`;
  }
  myAddrs() { return (this.st()?.addrs ?? []).join(', '); }
}
