import { Component, OnDestroy, OnInit, computed, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { EmuRadiusLogin, EmuRadiusUser, EmuRadiusView } from '../core/models';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';

/**
 * The lab's RADIUS server: on or off, its port and shared secret, where the switches reach it on each host, the users
 * (names or patterns) with their password and VLAN, MAC logins, and the last logins it answered.
 */
@Component({
  selector: 'app-radius-dialog',
  imports: [FormsModule],
  template: `
    <div class="ov" (click)="closed.emit()">
      <div class="dl" role="dialog" [attr.aria-label]="tr('RADIUS server')" (click)="$event.stopPropagation()">
        <div class="dh"><b>{{ tr('RADIUS server') }}</b><span class="muted">{{ tr('802.1X and MAC logins for the lab\\'s switches') }}</span><span class="sp"></span>
          <label class="tg"><input type="checkbox" class="cbx" [(ngModel)]="on" /> {{ on ? tr('On') : tr('Off') }}</label><a class="x" (click)="closed.emit()" [title]="tr('Close')">✕</a></div>
        <div class="db">
          <section class="lft">
            <div class="row3">
              <label><span>{{ tr('Port') }}</span><input type="number" min="1" max="65534" [(ngModel)]="port" placeholder="1812" [title]="tr('Authentication; accounting is on the next port')" /></label>
              <label class="sec"><span>{{ tr('Shared secret') }}</span><div class="pw"><input [type]="show() ? 'text' : 'password'" [(ngModel)]="secret" autocomplete="off" [placeholder]="tr('made up when empty')" /><a (click)="show.set(!show())" [title]="show() ? tr('Hide') : tr('Show')">{{ show() ? '◉' : '◎' }}</a></div></label>
            </div>
            <div class="hosts">
              <div class="sh">{{ tr('Where the switches reach it') }}</div>
              @for (h of view()?.hosts ?? []; track h.host) {
                <div class="hr"><i [class.ok]="h.running" [class.bad]="view()?.on && !h.running"></i><b>{{ h.host }}</b><span class="mono">{{ h.address ? h.address + ':' + (port || 1812) : tr('the host\\'s address on their network') }}</span><span class="sp"></span>
                  <span class="muted small">@if (view()?.on) { @if (h.running) { {{ tr('{0} in · {1} refused', h.accept, h.reject) }} } @else { {{ h.err || tr('not running') }} } } @else { {{ tr('off') }} }</span></div>
              }
            </div>
            <div class="sh">{{ tr('Users') }}<span class="muted">{{ tr('the first match wins; a name can be a pattern: user*') }}</span></div>
            <div class="ut">
              <div class="uh"><span>{{ tr('User') }}</span><span>{{ tr('Password') }}</span><span>VLAN</span><span></span></div>
              @for (u of users(); track $index) {
                <div class="ur"><input [(ngModel)]="u.user" placeholder="user*" /><input [type]="show() ? 'text' : 'password'" [(ngModel)]="u.password" autocomplete="off" /><input [(ngModel)]="u.vlan" [placeholder]="tr('the port\\'s')" /><a class="x" (click)="users.set(drop(users(), $index))" [title]="tr('Remove')">✕</a></div>
              } @empty { <p class="muted small">{{ tr('No user yet: every 802.1X login is refused.') }}</p> }
              <button type="button" class="sm" (click)="users.set([...users(), { user: '', password: '', vlan: '' }])">＋ {{ tr('User') }}</button>
            </div>
            <div class="sh">{{ tr('MAC logins') }}<span class="muted">{{ tr('MAC authentication (MAB) for hosts without 802.1X') }}</span></div>
            <div class="row3">
              <label class="sec"><span>{{ tr('MAC addresses') }}</span><input [(ngModel)]="mab" placeholder="02:63:*, 00:04:96:aa:bb:cc" [title]="tr('Addresses or patterns, separated by commas; * lets every MAC in')" /></label>
              <label><span>VLAN</span><input [(ngModel)]="mabVlan" [placeholder]="tr('the port\\'s')" /></label>
            </div>
          </section>
          <section class="rgt">
            <div class="sh">{{ tr('Last logins') }}<span class="muted">{{ logins().length }}</span><span class="sp"></span><a (click)="load()" [title]="tr('Refresh')">↻</a></div>
            <div class="lg">
              <div class="lh"><span>{{ tr('Time') }}</span><span>{{ tr('User') }}</span><span>{{ tr('How') }}</span><span>{{ tr('Switch') }}</span><span>{{ tr('Result') }}</span></div>
              @for (x of logins(); track $index) {
                <div class="lr"><span class="mono">{{ hhmmss(x.at) }}</span><span class="mono" [title]="x.user">{{ x.user }}</span><span>{{ x.how }}</span><span class="mono">{{ x.switch }}</span><span [class.ok]="x.result === 'accept'" [class.bad]="x.result !== 'accept'">{{ x.result === 'accept' ? tr('in') : tr('refused') }}@if (x.vlan) { · VLAN {{ x.vlan }} }</span></div>
              } @empty { <p class="muted small">{{ tr('No login yet. Point the switches\\' 802.1X at the address on the left with the shared secret.') }}</p> }
            </div>
          </section>
        </div>
        <div class="df"><span class="muted small">{{ tr('EAP-MD5, PAP and MAC authentication; accepted users get their VLAN (Tunnel-Private-Group-Id).') }}</span><span class="sp"></span>
          <button type="button" (click)="closed.emit()">{{ tr('Cancel') }}</button><button type="button" class="primary" (click)="save()">{{ tr('Save') }}</button></div>
      </div>
    </div>`,
  styles: [`
    .ov { position: fixed; inset: 0; z-index: 100; background: rgba(0,0,0,.45); display: grid; place-items: center; }
    .dl { width: min(1060px, 94vw); height: min(640px, 88vh); background: var(--surface); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 18px 50px rgba(0,0,0,.45); display: grid; grid-template-rows: auto minmax(0, 1fr) auto; overflow: hidden; font-size: 13px; }
    .dh { display: flex; align-items: center; gap: 10px; padding: 12px 16px; border-bottom: 1px solid var(--line); b { font-size: 15px; } .x { cursor: pointer; color: var(--muted); margin-left: 6px; } }
    .tg { display: inline-flex; align-items: center; gap: 6px; font-weight: 600; }
    .sp { flex: 1; } .muted { color: var(--muted); } .small { font-size: 11.5px; } .mono { font-family: var(--mono, monospace); font-size: 11.5px; }
    .db { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); min-height: 0; }
    .lft { padding: 14px 16px; overflow: auto; display: flex; flex-direction: column; gap: 10px; border-right: 1px solid var(--line); }
    .rgt { padding: 14px 16px; overflow: hidden; display: flex; flex-direction: column; gap: 8px; background: var(--surface-2); }
    .row3 { display: grid; grid-template-columns: 110px minmax(0, 1fr); gap: 10px; label { display: flex; flex-direction: column; gap: 4px; span { font-size: 11.5px; color: var(--muted); } } }
    .pw { display: flex; align-items: center; gap: 6px; input { flex: 1; min-width: 0; } a { cursor: pointer; color: var(--muted); } }
    .sh { display: flex; align-items: baseline; gap: 8px; font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); margin-top: 4px; .muted { text-transform: none; letter-spacing: 0; font-weight: 400; } a { cursor: pointer; font-size: 14px; } }
    .hosts { border: 1px solid var(--line); border-radius: 10px; padding: 8px 10px; display: flex; flex-direction: column; gap: 6px; background: var(--surface-2); }
    .hr { display: flex; align-items: center; gap: 8px; i { width: 8px; height: 8px; border-radius: 50%; background: var(--muted); } i.ok { background: var(--ok, #1fa45a); } i.bad { background: var(--danger); } }
    .ut { display: flex; flex-direction: column; gap: 5px; button { align-self: flex-start; }
      .uh, .ur { display: grid; grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr) 90px 18px; gap: 6px; align-items: center; } .uh span { font-size: 11px; color: var(--muted); }
      .x { cursor: pointer; color: var(--muted); } .x:hover { color: var(--danger); } input { min-width: 0; } }
    .lg { overflow: auto; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); min-height: 0; flex: 1;
      .lh, .lr { display: grid; grid-template-columns: 72px minmax(0, 1.2fr) 70px minmax(0, 1fr) 104px; gap: 8px; padding: 5px 10px; border-bottom: 1px solid var(--line); align-items: center; span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } }
      .lh { position: sticky; top: 0; background: var(--surface); font-size: 11px; color: var(--muted); } .lr { font-size: 12px; } p { padding: 10px; margin: 0; } }
    .ok { color: var(--ok, #1fa45a); } .bad { color: var(--danger); }
    .df { display: flex; align-items: center; gap: 8px; padding: 10px 16px; border-top: 1px solid var(--line); }
  `],
})
export class RadiusDialog implements OnInit, OnDestroy {
  private backend = inject(Backend);
  private ui = inject(Ui);
  readonly closed = output<void>();
  protected readonly tr = tr;
  protected view = signal<EmuRadiusView | null>(null);
  protected show = signal(false);
  protected users = signal<EmuRadiusUser[]>([]);
  protected on = false; protected port: number | null = null; protected secret = ''; protected mab = ''; protected mabVlan = '';
  protected logins = computed(() => (this.view()?.hosts ?? []).flatMap(h => h.logins ?? []).sort((a, b) => b.at - a.at).slice(0, 200) as EmuRadiusLogin[]);
  private timer?: ReturnType<typeof setInterval>;

  async ngOnInit() { await this.load(true); this.timer = setInterval(() => void this.load(), 5000); }
  ngOnDestroy() { clearInterval(this.timer); }
  async load(fill = false) {
    try {
      const v = await this.backend.emuRadius(); this.view.set(v);
      if (fill) { this.on = v.on; this.port = v.port && v.port !== 1812 ? v.port : null; this.secret = v.secret ?? ''; this.users.set((v.users ?? []).map(u => ({ ...u }))); this.mab = (v.mab ?? []).join(', '); this.mabVlan = v.mabVlan ?? ''; }
    } catch (e) { if (fill) this.ui.error(e); }
  }
  protected drop<T>(a: T[], i: number) { return a.filter((_, j) => j !== i); }
  protected hhmmss(at: number) { return new Date(at * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }); }
  async save() {
    try {
      const v = await this.backend.emuRadiusSet({ on: this.on, port: +(this.port ?? 0) || 0, secret: this.secret.trim(), users: this.users().filter(u => u.user.trim()), mab: this.mab.split(/[\s,]+/).filter(Boolean), mabVlan: this.mabVlan.trim() });
      this.view.set(v); this.secret = v.secret ?? ''; this.ui.toast(v.on ? tr('The RADIUS server is on') : tr('The RADIUS server is off'), 'success');
    } catch (e) { this.ui.error(e); }
  }
}
