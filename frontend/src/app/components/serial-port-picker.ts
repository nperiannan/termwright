import { Component, OnDestroy, OnInit, computed, inject, model, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { SerialPortInfo } from '../core/models';

const OTHER = '\u0000other';

/** Serial port list: the ports of this computer in order (COM2 before COM10,
 *  /dev/ttyUSB2 before /dev/ttyUSB10) with the adapter's name, refreshed
 *  every second and a half so a cable plugged in shows up (and is picked when
 *  no port was chosen yet). "Other port…" types a name by hand. */
@Component({
  selector: 'app-serial-port-picker',
  imports: [FormsModule],
  template: `
    <div class="spp">
      @if (typing()) {
        <input [name]="name + 'Typed'" [ngModel]="value()" (ngModelChange)="value.set($event)" placeholder="COM5, /dev/ttyUSB0, /dev/cu.usbserial-1410" />
        <button type="button" (click)="typing.set(false)" title="Back to the list of ports">List</button>
      } @else {
        <select [name]="name" [ngModel]="value()" (ngModelChange)="pick($event)">
          @if (!value()) { <option value="">{{ ports().length ? 'Choose a port…' : 'No serial port found - plug in the cable' }}</option> }
          @for (p of ports(); track p.name) {
            <option [value]="p.name">{{ p.name }}{{ p.label ? '  -  ' + p.label : '' }}{{ fresh().has(p.name) ? '  (new)' : '' }}</option>
          }
          @if (missing()) { <option [value]="value()">{{ value() }}  (not connected)</option> }
          <option [value]="other">Other port…</option>
        </select>
      }
    </div>
    @if (!typing()) {
      <div class="hint">{{ ports().length }} port{{ ports().length === 1 ? '' : 's' }} found · the list updates by itself when a cable is plugged in or removed</div>
    }
  `,
  styles: [`
    :host { display: block; min-width: 0; }
    .spp { display: flex; gap: 8px; align-items: center; select, input { flex: 1; min-width: 0; } }
    .hint { margin-top: 3px; }
  `],
})
export class SerialPortPicker implements OnInit, OnDestroy {
  private backend = inject(Backend);
  readonly value = model<string>('');
  name = 'comPort';
  readonly other = OTHER;
  readonly ports = signal<SerialPortInfo[]>([]);
  /** Ports that appeared while the list was open. */
  readonly fresh = signal<Set<string>>(new Set());
  readonly typing = signal(false);
  readonly missing = computed(() => !!this.value() && !this.ports().some(p => p.name === this.value()));
  private timer?: ReturnType<typeof setInterval>;
  private first = true;

  ngOnInit() {
    void this.load();
    this.timer = setInterval(() => void this.load(), 1500);
  }

  ngOnDestroy() { clearInterval(this.timer); }

  pick(v: string) {
    if (v === OTHER) { this.typing.set(true); return; }
    this.value.set(v);
  }

  private async load() {
    let list: SerialPortInfo[];
    try { list = (await this.backend.serialPortList()) ?? []; } catch { return; }
    const before = new Set(this.ports().map(p => p.name));
    const same = list.length === before.size && list.every(p => before.has(p.name));
    if (same && !this.first) return;
    const added = this.first ? [] : list.filter(p => !before.has(p.name)).map(p => p.name);
    this.ports.set(list);
    if (added.length) {
      this.fresh.set(new Set([...this.fresh(), ...added]));
      // a cable was just plugged in: take it when nothing usable is chosen
      if (!this.typing() && (!this.value() || this.missing())) this.value.set(added[0]);
    }
    if (this.first && !this.value() && list.length === 1) this.value.set(list[0].name);
    this.first = false;
  }
}
