import { Component, OnDestroy, OnInit, computed, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { EmuMatrix, EmuMatrixHost, EmuMatrixImage, EmuReach } from '../core/models';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';

/** Images on the Hosts: images down the side, hosts across the top; which
 *  host has which image, copying them so every host has every image, and a
 *  panel per host with its numbers, checks, setup and which hosts it reaches. */
@Component({
  selector: 'app-lab-host-images',
  imports: [FormsModule],
  template: `
  <div class="wrap" [class.withp]="!!cur()">
    <div class="main">
      <div class="bar">
        <div class="sum"><div><i>{{ tr('Hosts') }}</i><em>{{ hosts().length }}</em></div><div><i>{{ tr('Images') }}</i><em>{{ images().length }}</em></div>
          <div><i>{{ tr('Copies missing') }}</i><em [class.wn]="missing().n">{{ missing().n }}</em></div><div><i>{{ tr('To copy') }}</i><em>{{ size(missing().bytes) }}</em></div></div>
        <span class="sp"></span>
        <label class="tg" [title]="tr('An image copied to one host, or downloaded, goes to every host')"><input type="checkbox" [ngModel]="m()?.syncAll" (ngModelChange)="settings($event, m()?.keep20 ?? true)" />{{ tr('Keep all hosts the same') }}</label>
        <label class="tg" [title]="tr('A copy that would leave less than a fifth of a host’s disk free waits, with a warning and Copy Anyway')"><input type="checkbox" [ngModel]="m()?.keep20" (ngModelChange)="settings(m()?.syncAll ?? false, $event)" />{{ tr('Keep 20 % disk free') }}</label>
        <button type="button" (click)="load()" [disabled]="loading()">↻ {{ loading() ? tr('Checking…') : tr('Check Again') }}</button>
        <button type="button" class="primary" (click)="sync([], [], false)" [disabled]="!missing().n">⇉ {{ tr('Make All Hosts the Same') }}</button>
      </div>
      @if (!m() && loading()) { <p class="muted pad">{{ tr('Reading the hosts…') }}</p> }
      @else if (!hosts().length) { <p class="muted pad">{{ tr('No host yet: add one under Hosts.') }}</p> }
      @else {
        <div class="tw"><table>
          <tr><th class="ic">{{ tr('Image') }}</th>
            @for (h of hosts(); track h.session) {
              <th class="hh" [class.on]="sel() === h.session" (click)="sel.set(sel() === h.session ? '' : h.session)" [title]="tr('Details, checks and set up of {0}', h.name)">
                <div class="hn"><i class="dot" [style.background]="col($index)"></i><b>{{ h.name }}</b></div>
                <div class="pl">
                  <span class="pill" [class]="'pill ' + (h.kvm === 'yes' ? 'g' : 'o')">{{ h.kvm === 'yes' ? tr('KVM · fast') : tr('no KVM · slow') }}</span>
                  @if (issues(h).length) { <span class="pill r">{{ tr('{0} to fix', issues(h).length) }}</span> } @else if (!h.error) { <span class="pill g">✓ {{ tr('ready') }}</span> }
                </div>
                @if (h.error) { <div class="small bad">{{ tr('not reached') }}</div> }
                @else { <div class="small muted">{{ tr('{0} free of {1}', size(h.diskFree), size(h.diskTotal)) }}</div><div class="meter"><i [style.width.%]="used(h)" [style.background]="col($index)"></i></div> }
              </th>
            }
            <th class="ra"></th></tr>
          @for (im of images(); track im.id) {
            <tr>
              <td class="ic"><b>{{ im.name }}</b>@if (im.switches) { <span class="use">{{ tr('{0} switches', im.switches) }}</span> }
                <div class="small muted">{{ im.version }} · {{ size(im.size) }} · {{ im.local ? tr('on this computer') : tr('not on this computer') }}</div></td>
              @for (h of hosts(); track h.session) {
                @let st = cell(h.session, im.id);
                <td class="c">
                  @switch (kind(st)) {
                    @case ('ok') { <span class="ok">✓ {{ tr('there') }}</span> }
                    @case ('copying') { <span class="cp">{{ pctText(st) }}</span><div class="meter sm"><i [style.width.%]="pct(st)"></i></div><small>{{ rest(st) }}</small> }
                    @case ('queued') { <span class="muted">{{ tr('waiting to copy') }}</span> }
                    @case ('low') { <span class="wn">⚠ {{ tr('low space after copy') }}</span><small>{{ st.slice(5) }} · <a (click)="sync([h.session], [im.id], true)">{{ tr('Copy Anyway') }}</a></small> }
                    @case ('failed') { <span class="bad">{{ tr('failed') }}</span><small>{{ st.slice(8) }} · <a (click)="sync([h.session], [im.id], true)">{{ tr('Retry') }}</a></small> }
                    @case ('different') { <span class="wn">{{ tr('different file') }}</span><small>{{ tr('same name, other checksum') }} · <a (click)="sync([h.session], [im.id], true)">{{ tr('Replace') }}</a></small> }
                    @case ('partial') { <span class="wn">{{ tr('copy broke off') }}</span><small><a (click)="sync([h.session], [im.id], false)">{{ tr('Go On') }}</a></small> }
                    @case ('unknown') { <span class="muted">?</span> }
                    @default { <span class="muted">{{ tr('missing') }}</span><a class="ml" (click)="sync([h.session], [im.id], false)">{{ tr('Copy Here') }}</a> }
                  }
                </td>
              }
              <td class="ra">@if (lacking(im).length) { <a (click)="sync([], [im.id], false)">{{ tr('Copy to All Hosts') }}</a> } @else { <span class="muted">{{ tr('on every host') }}</span> }</td>
            </tr>
          } @empty { <tr><td class="muted" [attr.colspan]="hosts().length + 2">{{ tr('No image with a file yet: Images → Download or Choose.') }}</td></tr> }
        </table></div>
        <p class="muted small pad">{{ tr('Images used by switches come first. A file not on this computer is copied from a host that has it. When every host has a switch’s image, moving the switch copies only its own disk.') }}</p>
      }
    </div>
    @if (cur(); as h) {
      <aside class="side">
        <div class="sh"><i class="dot" [style.background]="col(hi(h))"></i><b>{{ h.name }}</b><span class="pill" [class]="'pill ' + (h.kvm === 'yes' ? 'g' : 'o')">{{ h.kvm === 'yes' ? tr('KVM · fast') : tr('no KVM · slow') }}</span><span class="sp"></span><a class="x" (click)="sel.set('')">✕</a></div>
        <div class="kv">
          <span>{{ tr('Address') }}</span><span>{{ h.addr || '-' }}</span>
          <span>QEMU</span><span>@if (h.qemu) { <span class="ok">✓ {{ h.qemu }}</span> } @else { <span class="bad">✕ {{ tr('not installed') }}</span> }</span>
          <span>CPU</span><span class="mt">{{ tr('{0} cores · load {1}', h.cpus, h.load) }}<span class="meter"><i [style.width.%]="pctOf(h.load, h.cpus)" [style.background]="col(hi(h))"></i></span></span>
          <span>{{ tr('Memory') }}</span><span class="mt">{{ tr('{0} free of {1}', size(h.memFree), size(h.memTotal)) }}<span class="meter"><i [style.width.%]="pctOf(h.memTotal - h.memFree, h.memTotal)" [style.background]="col(hi(h))"></i></span></span>
          <span>{{ tr('Disk') }}</span><span class="mt">{{ tr('{0} free of {1}', size(h.diskFree), size(h.diskTotal)) }}<span class="meter"><i [style.width.%]="used(h)" [style.background]="col(hi(h))"></i></span></span>
          <span>{{ tr('Images in') }}</span><span><code>{{ h.imagesDir || '-' }}</code> <a (click)="moveDir(h)">{{ tr('Change…') }}</a></span>
        </div>
        <div class="sec">{{ tr('Checks') }}</div>
        <div class="chk">
          @for (c of checks(h); track c.text) {
            <span [class]="c.ok === true ? 'ok' : c.ok === false ? 'bad' : 'wn'">{{ c.ok === true ? '✓' : c.ok === false ? '✕' : '!' }}</span><span>{{ c.text }}</span>
            <span>@if (c.fix) { <a class="fix" (click)="c.fix()">{{ c.fixText }}</a> }</span>
          }
        </div>
        <div class="sec">{{ tr('Reaches the other hosts') }} <span class="muted">· {{ tr('cables: UDP 20000-29999 · live moves: TCP 29400-29999') }}</span></div>
        <table class="rt">
          <tr><th></th><th>{{ tr('from {0}', h.name) }}</th><th>{{ tr('to {0}', h.name) }}</th></tr>
          @for (o of others(h); track o.session) {
            <tr><td><i class="dot" [style.background]="col(hi(o))"></i> {{ o.name }}</td>
              <td [title]="reach(h.session, o.session)?.text ?? ''">@if (reach(h.session, o.session); as r) { <span [class]="r.udp === 'ok' ? 'ok' : 'bad'">{{ r.udp === 'ok' ? '✓' : '✕' }} UDP</span> · <span [class]="r.tcp === 'ok' ? 'ok' : 'bad'">{{ r.tcp === 'ok' ? '✓' : '✕' }} {{ tr('live') }}</span> } @else { <span class="muted">{{ tr('not checked') }}</span> }</td>
              <td [title]="reach(o.session, h.session)?.text ?? ''">@if (reach(o.session, h.session); as r) { <span [class]="r.udp === 'ok' ? 'ok' : 'bad'">{{ r.udp === 'ok' ? '✓' : '✕' }} UDP</span> · <span [class]="r.tcp === 'ok' ? 'ok' : 'bad'">{{ r.tcp === 'ok' ? '✓' : '✕' }} {{ tr('live') }}</span> } @else { <span class="muted">{{ tr('not checked') }}</span> }</td></tr>
          } @empty { <tr><td colspan="3" class="muted">{{ tr('The only host: add another under Hosts.') }}</td></tr> }
        </table>
        @if (others(h).length) { <div><button type="button" (click)="checkReach()" [disabled]="m()?.checking">{{ m()?.checking ? tr('Checking… (about 15 s)') : tr('Check Between All Hosts') }}</button></div> }
        @if (ports(); as p) {
          <div class="ports"><div class="small">{{ tr('Run on {0} (and on the hosts it does not reach), as root - Termwright does not change firewalls itself:', h.name) }}</div><pre>{{ p }}</pre><button type="button" class="sm" (click)="copy(p)">{{ tr('Copy') }}</button></div>
        }
        <div class="sec">{{ tr('Images') }}</div>
        <div class="small">{{ tr('{0} of {1} here', haveN(h), images().length) }}@if (lackN(h).n) { · {{ tr('{0} missing ({1})', lackN(h).n, size(lackN(h).bytes)) }} }</div>
        <div class="row"><button type="button" class="primary" (click)="sync([h.session], [], false)" [disabled]="!lackN(h).n">{{ tr('Copy the Missing Ones Here') }}</button></div>
      </aside>
    }
  </div>
  `,
  styles: [`
    :host { display: block; min-height: 0; overflow: hidden; }
    .wrap { display: grid; grid-template-columns: minmax(0, 1fr); height: 100%; min-height: 0; }
    .wrap.withp { grid-template-columns: minmax(0, 1fr) 380px; }
    .main { overflow: auto; min-width: 0; }
    .pad { padding: 0 16px; }
    .bar { display: flex; align-items: center; gap: 12px; padding: 12px 16px; flex-wrap: wrap; }
    .sum { display: flex; gap: 20px; div { display: flex; flex-direction: column; } i { font-style: normal; color: var(--muted); font-size: 11.5px; } em { font-style: normal; font-size: 17px; font-weight: 600; } }
    .sp { flex: 1; } .muted { color: var(--muted); } .small { font-size: 12px; }
    .tg { display: flex; align-items: center; gap: 6px; font-size: 12.5px; color: var(--muted); cursor: pointer; }
    .ok { color: var(--success, #34c77b); } .bad { color: var(--danger, #e5484d); } .wn { color: var(--warning, #e5a93b); } .cp { color: var(--accent); }
    a { color: var(--accent); cursor: pointer; } a.ml { margin-left: 8px; }
    .tw { padding: 0 16px; }
    table { border-collapse: separate; border-spacing: 0; width: 100%; }
    th, td { border-bottom: 1px solid var(--line); padding: 9px 10px; text-align: left; vertical-align: middle; font-size: 12.5px; }
    th { font-weight: 500; background: var(--surface-2); position: sticky; top: 0; z-index: 1; vertical-align: top; }
    th.ic, td.ic { min-width: 220px; } th.ra, td.ra { width: 130px; white-space: nowrap; }
    th.hh { width: 200px; cursor: pointer; } th.hh:hover { background: color-mix(in srgb, var(--accent) 8%, var(--surface-2)); }
    th.hh.on { background: color-mix(in srgb, var(--accent) 14%, var(--surface-2)); box-shadow: inset 0 -2px var(--accent); }
    .hn { display: flex; align-items: center; gap: 7px; } .dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; flex: none; }
    .pl { display: flex; gap: 5px; flex-wrap: wrap; margin: 6px 0 4px; }
    .pill { font-size: 10.5px; padding: 1px 7px; border-radius: 9px; border: 1px solid var(--line); color: var(--muted); font-weight: 500; white-space: nowrap; }
    .pill.g { color: var(--success, #34c77b); border-color: color-mix(in srgb, var(--success, #34c77b) 45%, transparent); }
    .pill.o { color: var(--warning, #e5a93b); border-color: color-mix(in srgb, var(--warning, #e5a93b) 45%, transparent); }
    .pill.r { color: var(--danger, #e5484d); border-color: color-mix(in srgb, var(--danger, #e5484d) 45%, transparent); }
    .meter { display: block; height: 4px; border-radius: 2px; background: var(--line); overflow: hidden; margin-top: 4px; i { display: block; height: 100%; background: var(--accent); } }
    .meter.sm { width: 140px; }
    .use { font-size: 10.5px; padding: 1px 6px; border-radius: 8px; background: color-mix(in srgb, var(--accent) 15%, transparent); color: var(--accent); margin-left: 6px; }
    td.c small { display: block; color: var(--muted); font-size: 10.5px; margin-top: 2px; }
    .side { border-left: 1px solid var(--line); background: var(--surface-2); padding: 14px 16px; overflow: auto; display: grid; gap: 10px; align-content: start; font-size: 12.5px; }
    .sh { display: flex; align-items: center; gap: 8px; b { font-size: 14px; } .x { color: var(--muted); } }
    .kv { display: grid; grid-template-columns: 74px minmax(0, 1fr); gap: 5px 10px; align-items: center; > span:nth-child(odd) { color: var(--muted); } code { font-size: 11.5px; word-break: break-all; } }
    .mt { display: flex; align-items: center; gap: 8px; .meter { flex: 1; margin: 0; min-width: 40px; } }
    .sec { font: 600 10.5px/1.4 inherit; letter-spacing: .07em; text-transform: uppercase; color: var(--muted); margin-top: 6px; span { text-transform: none; letter-spacing: 0; font-weight: 400; } }
    .chk { display: grid; grid-template-columns: 16px minmax(0, 1fr) auto; gap: 6px 8px; align-items: center; }
    .fix { display: inline-block; padding: 2px 9px; border-radius: 7px; border: 1px solid color-mix(in srgb, var(--warning, #e5a93b) 50%, transparent); color: var(--warning, #e5a93b); background: color-mix(in srgb, var(--warning, #e5a93b) 10%, transparent); white-space: nowrap; }
    .rt { width: 100%; th, td { padding: 6px 6px; font-size: 12px; position: static; } }
    .ports { border: 1px solid var(--line); border-radius: 8px; padding: 8px 10px; display: grid; gap: 6px; pre { margin: 0; font-size: 11px; white-space: pre-wrap; word-break: break-all; } }
    .row { display: flex; gap: 8px; }
  `]
})
export class LabHostImages implements OnInit, OnDestroy {
  readonly hostsPage = output<string>();
  private backend = inject(Backend);
  private ui = inject(Ui);
  protected tr = tr;
  protected m = signal<EmuMatrix | null>(null);
  protected loading = signal(false);
  protected sel = signal('');
  protected ports = signal('');
  private timer: ReturnType<typeof setTimeout> | null = null;
  protected hosts = computed(() => this.m()?.hosts ?? []);
  protected images = computed(() => this.m()?.images ?? []);
  protected cur = computed(() => this.hosts().find(h => h.session === this.sel()) ?? null);
  protected missing = computed(() => {
    let n = 0, bytes = 0;
    for (const im of this.images()) for (const h of this.hosts()) if (!h.error && this.kind(this.cell(h.session, im.id)) !== 'ok') { n++; bytes += im.size; }
    return { n, bytes };
  });

  ngOnInit() { void this.load(); }
  ngOnDestroy() { if (this.timer) clearTimeout(this.timer); }
  async load() {
    this.loading.set(true);
    try { this.m.set(await this.backend.emuImageMatrix()); } catch (e) { this.ui.error(errText(e)); } finally { this.loading.set(false); }
    this.again();
  }
  /** While copies or checks run, read again every few seconds. */
  private again() {
    if (this.timer) clearTimeout(this.timer);
    const m = this.m();
    if (m && (m.busy || m.checking)) this.timer = setTimeout(() => void this.quiet(), 3000);
  }
  private async quiet() { try { this.m.set(await this.backend.emuImageMatrix()); } catch { /* next time */ } this.again(); }
  async sync(hosts: string[], images: string[], force: boolean) {
    try { this.m.set(await this.backend.emuSyncImages(hosts, images, force)); } catch (e) { this.ui.toast(errText(e), 'info'); }
    this.again();
  }
  async settings(syncAll: boolean, keep20: boolean) { try { this.m.set(await this.backend.emuSyncSettings(syncAll, keep20)); } catch (e) { this.ui.error(errText(e)); } this.again(); }
  async checkReach() { try { this.m.set(await this.backend.emuCheckReach()); } catch (e) { this.ui.error(errText(e)); } this.again(); }
  async moveDir(h: EmuMatrixHost) {
    const to = await this.ui.input(tr('Images of {0}', h.name), tr('Folder on {0} for its images (for example a bigger disk such as /data/termwright/images). They move there; a link stays in their old place. Stop its switches first.', h.name), h.imagesDir);
    if (!to || to === h.imagesDir) return;
    this.loading.set(true);
    try { this.m.set(await this.backend.emuSetImagesDir(h.session, to)); this.ui.toast(tr('The images of {0} are in {1} now', h.name, to), 'success'); } catch (e) { this.ui.error(errText(e)); } finally { this.loading.set(false); }
  }
  async showPorts(h: EmuMatrixHost) { try { this.ports.set(await this.backend.emuOpenPortsText(h.session)); } catch (e) { this.ui.error(errText(e)); } }
  async copy(t: string) { await this.backend.clipboardSet(t); this.ui.toast(tr('Copied'), 'success', 1500); }

  cell(host: string, id: string) { return this.m()?.cells?.[host]?.[id] ?? 'missing'; }
  kind(st: string) { return st.startsWith('copying') ? 'copying' : st.startsWith('low:') ? 'low' : st.startsWith('failed:') ? 'failed' : st; }
  pct(st: string) { const m = /(\d+) %/.exec(st); return m ? +m[1] : 0; }
  pctText(st: string) { return tr('copying {0} %', this.pct(st)); }
  rest(st: string) { const i = st.indexOf('% · '); return i >= 0 ? st.slice(i + 4) : ''; }
  lacking(im: EmuMatrixImage) { return this.hosts().filter(h => !h.error && !['ok', 'copying', 'queued', 'unknown'].includes(this.kind(this.cell(h.session, im.id)))); }
  haveN(h: EmuMatrixHost) { return this.images().filter(im => this.cell(h.session, im.id) === 'ok').length; }
  lackN(h: EmuMatrixHost) { const l = this.images().filter(im => this.cell(h.session, im.id) !== 'ok'); return { n: l.length, bytes: l.reduce((a, im) => a + im.size, 0) }; }
  others(h: EmuMatrixHost) { return this.hosts().filter(o => o.session !== h.session); }
  reach(from: string, to: string): EmuReach | undefined { return (this.m()?.reach ?? []).find(r => r.from === from && r.to === to); }
  hi(h: EmuMatrixHost) { return this.hosts().findIndex(x => x.session === h.session); }
  col(i: number) { return ['#3b8cf6', '#a46cf0', '#1fb5a5', '#e3842a', '#d65db1', '#8fb339'][Math.max(0, i) % 6]; }
  used(h: EmuMatrixHost) { return this.pctOf(h.diskTotal - h.diskFree, h.diskTotal); }
  pctOf(a: number, b: number) { return b > 0 ? Math.min(100, Math.max(0, (a / b) * 100)) : 0; }
  size(n: number) { return !n ? '-' : n >= 2 ** 40 ? (n / 2 ** 40).toFixed(1) + ' TB' : n >= 2 ** 30 ? (n / 2 ** 30).toFixed(1) + ' GB' : n >= 2 ** 20 ? Math.round(n / 2 ** 20) + ' MB' : Math.round(n / 1024) + ' KB'; }
  /** What is wrong with a host, for its "to fix" pill. */
  issues(h: EmuMatrixHost) { return this.checks(h).filter(c => c.ok === false); }
  checks(h: EmuMatrixHost) {
    const setup = () => this.hostsPage.emit(h.session);
    const ports = () => void this.showPorts(h);
    const out: { ok: boolean | null; text: string; fix?: () => void; fixText?: string }[] = [];
    if (h.error) out.push({ ok: false, text: tr('Not reached: {0}', h.error) });
    out.push(h.qemu ? { ok: true, text: tr('QEMU {0}', h.qemu) + (h.qemuImg ? ' · qemu-img' : '') } : { ok: false, text: tr('QEMU is not installed'), fix: setup, fixText: tr('Set Up QEMU…') });
    out.push(h.kvm === 'yes' ? { ok: true, text: tr('KVM: switches run at full speed') }
      : { ok: false, text: h.kvm === 'noaccess' ? tr('KVM is there, but its user may not use it') : h.vt ? tr('KVM is not loaded') : h.virt && h.virt !== 'none' ? tr('No KVM: CPU virtualisation is off in this VM ({0})', h.virt) : tr('No KVM: the CPU has no virtualisation, or it is off in the BIOS'), fix: h.vt || h.kvm === 'noaccess' ? setup : undefined, fixText: tr('Set Up KVM…') });
    out.push(h.sudo ? { ok: true, text: tr('sudo without a password') } : { ok: null, text: tr('No sudo without a password: only port forwarding for its switches'), fix: setup, fixText: tr('Set Up…') });
    out.push(h.guestfs ? { ok: true, text: tr('libguestfs (personas)') } : { ok: null, text: tr('No libguestfs: personas cannot be written'), fix: setup, fixText: tr('Set Up…') });
    const bad = (this.m()?.reach ?? []).filter(r => (r.from === h.session || r.to === h.session) && (r.udp !== 'ok' || r.tcp !== 'ok'));
    if (bad.length) out.push({ ok: false, text: tr('{0} of its links to other hosts are blocked', bad.length), fix: ports, fixText: tr('Open the Ports…') });
    if (h.diskTotal) { const lack = this.lackN(h).bytes, left = h.diskFree - lack; if (lack && left * 5 < h.diskTotal) out.push({ ok: null, text: tr('Disk: {0} % free after copying the missing images', Math.max(0, Math.round(left * 100 / h.diskTotal))) }); }
    for (const p of h.problems ?? []) if (!out.some(c => c.text === p)) out.push({ ok: false, text: p, fix: h.canSetup ? setup : undefined, fixText: tr('Set Up…') });
    return out;
  }
}
