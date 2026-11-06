import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';
import { Autofocus } from '../core/autofocus';

/** Reserve a team device until a time, with a note the others see. */
@Component({
  selector: 'app-team-reserve-dialog',
  imports: [FormsModule, Icon, Autofocus],
  template: `
  @if (ui.teamReserve(); as r) {
  <div class="modal-backdrop" (click)="close()">
    <form class="modal reserve" (click)="$event.stopPropagation()" (ngSubmit)="save()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="lock" [size]="17" /></span>
        <span class="mh-t">{{ r.current ? 'Change the Reservation of ' : 'Reserve ' }}{{ r.node.name }}<span class="mh-s">Your team sees it as reserved by you; whoever connects anyway is asked first, and you get a notice</span></span></div>
      <div class="modal-body">
        <div class="lbl">Until</div>
        <div class="row">
          <input type="datetime-local" name="until" [(ngModel)]="until" />
          <div class="chips">@for (p of presets; track p.label) { <button type="button" (click)="pick(p.at())">{{ p.label }}</button> }</div>
        </div>
        <div class="lbl">Note <span class="muted">- what you are doing, for the others</span></div>
        <input name="note" [(ngModel)]="note" appAutofocus maxlength="200" placeholder="e.g. EXOS 33.1 upgrade test - please do not reboot" />
        @if (error()) { <p class="err">{{ error() }}</p> }
      </div>
      <div class="modal-foot">
        <span class="spacer"></span>
        <button type="button" (click)="close()">Cancel</button>
        <button type="submit" class="primary" [disabled]="busy() || !until">{{ busy() ? 'Reserving…' : r.current ? 'Save' : 'Reserve' }}</button>
      </div>
    </form>
  </div>
  }`,
  styles: [`
    .reserve { width: min(560px, 94vw); }
    .lbl { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); margin: 10px 0 5px; } .lbl:first-child { margin-top: 0; }
    .muted { color: var(--muted); }
    .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
    .chips { display: flex; gap: 6px; flex-wrap: wrap; button { padding: 3px 10px; } }
    input[name=note] { width: 100%; }
    .err { color: var(--danger); margin: 10px 0 0; }
  `],
})
export class TeamReserveDialog implements OnInit {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  until = '';
  note = '';
  readonly busy = signal(false);
  readonly error = signal('');
  readonly presets = [
    { label: '2 hours', at: () => new Date(Date.now() + 2 * 3600e3) },
    { label: '4 hours', at: () => new Date(Date.now() + 4 * 3600e3) },
    { label: 'Today 18:00', at: () => { const d = new Date(); d.setHours(18, 0, 0, 0); return d; } },
    { label: 'Tomorrow 18:00', at: () => { const d = new Date(); d.setDate(d.getDate() + 1); d.setHours(18, 0, 0, 0); return d; } },
    { label: 'Friday 18:00', at: () => { const d = new Date(); d.setDate(d.getDate() + ((5 - d.getDay() + 7) % 7 || 7)); d.setHours(18, 0, 0, 0); return d; } },
  ];

  ngOnInit() {
    const r = this.ui.teamReserve();
    if (r?.current) { this.pick(new Date(r.current.until)); this.note = r.current.note; }
    else this.pick(this.presets[1].at());
  }
  /** The local date and time for the datetime-local field. */
  pick(d: Date) {
    const p = (n: number) => String(n).padStart(2, '0');
    this.until = `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
  }
  close() { this.ui.teamReserve.set(null); }

  async save() {
    const r = this.ui.teamReserve();
    if (!r) return;
    const until = new Date(this.until);
    if (isNaN(+until) || +until <= Date.now()) { this.error.set('Pick a time in the future.'); return; }
    this.busy.set(true);
    this.error.set('');
    const n = r.node;
    try {
      await this.backend.teamReserve({ host: n.hostname ?? '', port: n.port || (n.protocol === 'Telnet' ? 23 : 22), protocol: n.protocol ?? 'SSH2', mac: n.mac ?? '', name: n.name },
        until.toISOString().replace(/\.\d{3}Z$/, 'Z'), this.note.trim());
      this.ui.toast(`${n.name} is reserved for you until ${until.toLocaleString()}`, 'success');
      this.close();
    } catch (e) { this.error.set(errText(e)); }
    finally { this.busy.set(false); }
  }
}
