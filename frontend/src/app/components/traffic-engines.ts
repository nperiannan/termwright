import { Component, OnInit, inject, output, signal } from '@angular/core';
import { DecimalPipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { EmuEngines, EmuTrex, EmuTrexPort } from '../core/models';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';

/**
 * Traffic engines: what sends the streams. On the KVM hosts the built-in engine (Python, nothing to install) or the
 * fast one (a compiled helper Termwright copies to the host); for line rate on real hardware, TRex servers you run,
 * whose ports become test ports.
 */
@Component({
  selector: 'app-traffic-engines',
  imports: [FormsModule, DecimalPipe],
  template: `
    <div class="ov" (click)="closed.emit()">
      <div class="dl" role="dialog" [attr.aria-label]="tr('Traffic engines')" (click)="$event.stopPropagation()">
        <div class="dh"><b>{{ tr('Traffic engines') }}</b><span class="muted">{{ tr('What sends and counts the frames') }}</span><span class="sp"></span><a class="x" (click)="closed.emit()" [title]="tr('Close')">✕</a></div>
        <div class="bd">
          <section>
            <h4>{{ tr('On the KVM hosts') }}</h4>
            <div class="eng">
              <a [class.on]="v()?.engine !== 'fast'" (click)="setEngine('python')"><b>{{ tr('Built in') }}</b><span>{{ tr('Python, nothing on the host but python3: about 100,000 frames a second per host') }}</span></a>
              <a [class.on]="v()?.engine === 'fast'" (click)="setEngine('fast')"><b>{{ tr('Fast') }}</b><span>{{ tr('A compiled helper Termwright copies to the host (Linux x86-64 or ARM64): several times faster, the same streams, counters and reports') }}</span></a>
            </div>
            <p class="hint">{{ tr('An emulated switch forwards a few thousand frames a second, so the engine matters for big labs - many switches on one host - and test ports wired to each other.') }}</p>
            <table>
              <tr><th>{{ tr('Host') }}</th><th>{{ tr('Engine') }}</th><th>{{ tr('CPU') }}</th><th class="r">{{ tr('Highest rate seen') }}</th><th>{{ tr('State') }}</th></tr>
              @for (h of v()?.hosts ?? []; track h.host) {
                <tr><td><b>{{ h.host }}</b></td><td>{{ h.engine === 'fast' ? tr('Fast') : tr('Built in') }}</td><td class="mono">{{ h.arch || '-' }}</td><td class="r">{{ h.fps ? (h.fps | number) + ' ' + tr('frames/s') : '-' }}</td>
                  <td [class.ok]="h.running" class="small">{{ h.running ? tr('running') : tr('starts with the first stream') }}@if (h.note) { · <span class="muted">{{ tr(h.note) }}</span> }</td></tr>
              } @empty { <tr><td colspan="5" class="muted">{{ tr('No KVM host in this lab yet.') }}</td></tr> }
            </table>
          </section>
          <section>
            <h4>{{ tr('TRex servers') }}<span class="muted">{{ tr('line rate on real hardware') }}</span></h4>
            <p class="hint">{{ tr('TRex runs on a server you set up, with DPDK network cards cabled to the devices under test. Termwright connects to its API (TCP 4501) and installs nothing there; its ports become test ports, and streams between them run in TRex with the same editor, results and reports.') }}</p>
            @for (t of v()?.trex ?? []; track t.id) {
              <div class="srv">
                <div class="sh"><b>{{ t.name }}</b><span class="mono muted">{{ t.addr }}:{{ t.port || 4501 }}</span>
                  <span class="st" [class.ok]="t.state === 'connected'" [class.bad]="t.state === 'unreachable'">{{ tr(t.state || 'not checked') }}</span>@if (t.version) { <span class="muted small">TRex {{ t.version }}</span> }
                  <span class="sp"></span><a class="lnk" (click)="check(t)">{{ tr('Check') }}</a><a class="lnk bad" (click)="remove(t)">{{ tr('Remove') }}</a></div>
                @if (t.err) { <p class="err">{{ t.err }}</p> }
                @if (t.ports?.length) {
                  <table>
                    <tr><th>{{ tr('Port') }}</th><th>{{ tr('Speed') }}</th><th>{{ tr('Link') }}</th><th>MAC</th><th>{{ tr('Driver') }}</th><th>{{ tr('Cabled to') }}</th><th>{{ tr('Test port') }}</th></tr>
                    @for (p of t.ports; track p.id) {
                      <tr><td><b>{{ p.id }}</b></td><td>{{ p.speed ? (p.speed >= 1000 ? p.speed / 1000 + ' Gbit/s' : p.speed + ' Mbit/s') : '-' }}</td><td [class.ok]="p.link" [class.bad]="p.link === false">{{ p.link ? tr('up') : tr('down') }}</td>
                        <td class="mono">{{ p.mac || '-' }}</td><td class="mono muted">{{ p.driver || '-' }}</td>
                        <td><input class="cab" [(ngModel)]="p.cabled" [placeholder]="tr('core-1 port 1/1')" (change)="port(t, p)" /></td>
                        <td><label class="cbl"><input type="checkbox" [(ngModel)]="p.use" (ngModelChange)="port(t, p)" /> {{ p.use ? (p.tester || tr('yes')) : tr('no') }}</label></td></tr>
                    }
                  </table>
                }
              </div>
            }
            @if (adding()) {
              <div class="add"><input [(ngModel)]="nName" [placeholder]="tr('Name')" /><input class="ad" [(ngModel)]="nAddr" [placeholder]="tr('Address')" /><input class="pt" type="number" [(ngModel)]="nPort" placeholder="4501" />
                <button type="button" class="primary" (click)="addSrv()" [disabled]="!nAddr.trim()">{{ tr('Add') }}</button><button type="button" (click)="adding.set(false)">{{ tr('Cancel') }}</button></div>
            } @else { <a class="lnk" (click)="adding.set(true)">＋ {{ tr('TRex Server…') }}</a> }
          </section>
        </div>
      </div>
    </div>`,
  styles: [`
    .ov { position: fixed; inset: 0; z-index: 100; background: rgba(0,0,0,.45); display: grid; place-items: center; }
    .dl { width: min(980px, 96vw); max-height: 92vh; background: var(--surface); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 18px 50px rgba(0,0,0,.45); display: grid; grid-template-rows: auto minmax(0, 1fr); overflow: hidden; }
    .dh { display: flex; align-items: baseline; gap: 10px; padding: 12px 16px; border-bottom: 1px solid var(--line); b { font-size: 15px; } .x { cursor: pointer; color: var(--muted); } }
    .bd { overflow: auto; padding: 6px 18px 16px; }
    .sp { flex: 1; } .muted { color: var(--muted); } .small { font-size: 12px; } .mono { font-family: var(--mono, monospace); font-size: 11.5px; } .r { text-align: right; }
    h4 { margin: 14px 0 8px; font-size: 13px; display: flex; gap: 10px; align-items: baseline; .muted { font-weight: 400; font-size: 12px; } }
    .eng { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; a { display: flex; flex-direction: column; gap: 3px; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; cursor: pointer; span { color: var(--muted); font-size: 12px; line-height: 1.4; } } a.on { border-color: var(--accent); } }
    .hint { color: var(--muted); font-size: 12px; line-height: 1.45; margin: 8px 0; }
    table { width: 100%; border-collapse: collapse; font-size: 12.5px; margin-top: 4px; th { text-align: left; font-size: 11px; color: var(--muted); font-weight: 600; padding: 5px 8px; border-bottom: 1px solid var(--line); } td { padding: 5px 8px; border-bottom: 1px solid var(--line); } }
    .ok { color: var(--ok, #2da44e); } .bad { color: var(--danger, #e5534b); } .err { color: var(--danger, #e5534b); font-size: 12px; margin: 4px 0; }
    .srv { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; margin-bottom: 10px; }
    .sh { display: flex; gap: 10px; align-items: baseline; .st { font-size: 12px; color: var(--muted); } }
    .lnk { color: var(--accent); cursor: pointer; font-size: 12.5px; } .lnk.bad { color: var(--danger, #e5534b); }
    input.cab { width: 170px; } .cbl { display: inline-flex; gap: 6px; align-items: center; input { width: auto; } }
    .add { display: flex; gap: 8px; align-items: center; input { width: 170px; } input.ad { width: 200px; } input.pt { width: 80px; } }
  `],
})
export class TrafficEngines implements OnInit {
  private backend = inject(Backend);
  private ui = inject(Ui);
  readonly closed = output<void>();
  protected v = signal<EmuEngines | null>(null);
  protected adding = signal(false);
  protected nName = ''; protected nAddr = ''; protected nPort: number | null = null;
  protected tr = tr;
  async ngOnInit() { try { this.v.set(await this.backend.emuEngines()); } catch (e) { this.ui.error(e); } }
  protected async setEngine(e: string) { try { this.v.set(await this.backend.emuEngineSet(e)); } catch (x) { this.ui.error(x); } }
  protected async addSrv() {
    try { this.v.set(await this.backend.emuTrexSave({ id: '', name: this.nName.trim() || this.nAddr.trim(), addr: this.nAddr.trim(), port: +(this.nPort ?? 0) || 4501 })); this.adding.set(false); this.nName = this.nAddr = ''; this.nPort = null; } catch (e) { this.ui.error(e); }
  }
  protected async check(t: EmuTrex) { try { this.v.set(await this.backend.emuTrexSave(t)); } catch (e) { this.ui.error(e); } }
  protected async remove(t: EmuTrex) { try { this.v.set(await this.backend.emuTrexRemove(t.id)); } catch (e) { this.ui.error(e); } }
  protected async port(t: EmuTrex, p: EmuTrexPort) { try { this.v.set(await this.backend.emuTrexPort(t.id, p.id, !!p.use, p.cabled ?? '')); } catch (e) { this.ui.error(e); } }
}
