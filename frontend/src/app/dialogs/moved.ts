import { Component, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { MovedDevice } from '../core/models';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';

interface Item extends MovedDevice { selected: boolean; done?: boolean; error?: string; }

/**
 * Devices with a new address: sessions whose device (by its MAC address)
 * answers at another IP, e.g. after the DHCP lease ran out. Nothing changes
 * until the user updates the ticked sessions.
 */
@Component({
  selector: 'app-moved',
  imports: [FormsModule],
  template: `
  <div class="modal-backdrop">
    <div class="modal moved">
      <div class="modal-head">Devices With a New Address</div>
      <div class="modal-body">
        @if (searching()) {
          <p>Looking for <b>{{ findPath() }}</b> by its MAC address on this computer's network…</p>
          <progress></progress>
        } @else {
          @if (err()) { <p class="err">{{ err() }}</p> }
          @if (items().length) {
            <p class="hint">These sessions' devices - recognised by their MAC address - now answer at another IP address (for example a new DHCP lease).
              Tick the sessions to update. The SSH host key is checked so a different device is not taken for yours.</p>
            <table class="list">
              <thead><tr><th class="c"><input type="checkbox" [checked]="allOn()" (change)="setAll(!allOn())" /></th>
                <th>Session</th><th>Saved address</th><th>Now at</th><th>MAC address</th><th>Check</th></tr></thead>
              <tbody>
                @for (it of items(); track it.path) {
                  <tr [class.dim]="it.done">
                    <td class="c"><input type="checkbox" [(ngModel)]="it.selected" (ngModelChange)="rev.update(x => x + 1)" [disabled]="it.done || !it.hostKey" /></td>
                    <td><b>{{ it.path }}</b></td>
                    <td class="mono">{{ it.old }}</td>
                    <td class="mono new">{{ it.new }}</td>
                    <td class="mono">{{ it.mac }}</td>
                    <td>
                      @if (it.done) { <span class="ok">✓ updated</span> }
                      @else if (it.error) { <span class="bad">{{ it.error }}</span> }
                      @else {
                        @switch (it.hostKey) {
                          @case ('same') { <span class="ok" title="The SSH host key at the new address is the one saved for this session">✓ same SSH host key</span> }
                          @case ('different') { <span class="bad" title="Same MAC address but another SSH host key: the device was reinstalled, or this is not your device">⚠ different SSH host key</span> }
                          @case ('unknown') { <span class="hint" title="No SSH host key was saved for the old address yet">same MAC (no saved host key)</span> }
                          @case ('none') { <span class="hint">same MAC address</span> }
                          @default { <span class="hint">checking…</span> }
                        }
                      }
                    </td>
                  </tr>
                }
              </tbody>
            </table>
            @if (scanned()) { <p class="hint">Searched {{ scanned() }}.</p> }
          }
        }
      </div>
      <div class="modal-foot">
        <span class="hint">{{ selected().length ? selected().length + ' session' + (selected().length === 1 ? '' : 's') + ' to update' : '' }}</span>
        <span class="spacer"></span>
        <button type="button" (click)="close()">{{ anyDone() ? 'Close' : 'Cancel' }}</button>
        @if (tab && anyDone()) { <button type="button" (click)="reconnect()">Reconnect Tab</button> }
        <button type="button" class="primary" [disabled]="!selected().length || busy()" (click)="apply()">{{ busy() ? 'Updating…' : 'Update Sessions' }}</button>
      </div>
    </div>
  </div>`,
  styles: [`
    .moved { width: min(940px, 95vw); }
    table { width: 100%; } td { vertical-align: middle; font-size: calc(12.5px * var(--ui-scale, 1)); } .c { width: 28px; text-align: center; }
    .mono { font-family: var(--mono); font-size: calc(12px * var(--ui-scale, 1)); } .new { color: var(--accent); font-weight: 600; }
    .ok { color: var(--ok); } .bad { color: var(--danger); } tr.dim td { opacity: .65; }
    progress { width: 100%; accent-color: var(--accent); }
    .err { color: var(--danger); white-space: pre-wrap; } .spacer { flex: 1; }
  `],
})
export class Moved {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  readonly items = signal<Item[]>([]);
  readonly searching = signal(false);
  readonly busy = signal(false);
  readonly err = signal('');
  readonly scanned = signal('');
  readonly findPath = signal('');
  readonly rev = signal(0);
  tab = '';
  /** Shown inside another dialog (Discover Devices): closing returns there. */
  readonly embedded = input(false);
  readonly closed = output<void>();
  close() { if (this.embedded()) this.closed.emit(); else this.ui.close(); }
  readonly selected = computed(() => { this.rev(); return this.items().filter(i => i.selected && !i.done); });
  readonly anyDone = computed(() => { this.rev(); return this.items().some(i => i.done); });

  constructor() {
    const input = this.ui.movedInput();
    this.ui.movedInput.set(null);
    if (input && 'items' in input) void this.check(input.items);
    else if (input && 'find' in input) { this.tab = input.tab ?? ''; void this.find(input.find); }
  }

  private async check(list: MovedDevice[]) {
    this.items.set(list.map(m => ({ ...m, hostKey: '', selected: false })));
    try {
      const r = await this.backend.checkMovedDevices(list);
      this.items.set(r.map(m => ({ ...m, selected: m.hostKey !== 'different' })));
    } catch (e) { this.err.set(errText(e)); }
  }

  private async find(path: string) {
    this.findPath.set(path);
    this.searching.set(true);
    try {
      const r = await this.backend.findByMac(path);
      this.scanned.set(r.scanned ?? '');
      if (r.new === r.old) this.err.set(`${path} still answers at ${r.old} with its MAC address ${r.mac}: the address has not changed. Check the device and the network.`);
      else this.items.set([{ ...r, selected: r.hostKey !== 'different' }]);
    } catch (e) { this.err.set(errText(e)); }
    this.searching.set(false);
  }

  allOn() { const l = this.items().filter(i => !i.done && i.hostKey); return l.length > 0 && l.every(i => i.selected); }
  setAll(v: boolean) { for (const i of this.items()) if (!i.done && i.hostKey) i.selected = v; this.rev.update(x => x + 1); }

  async apply() {
    this.busy.set(true);
    for (const it of this.selected()) {
      try { await this.backend.moveSession(it.path, it.new); it.done = true; it.error = ''; }
      catch (e) { it.error = errText(e); }
    }
    this.busy.set(false);
    this.rev.update(x => x + 1);
    this.items.update(l => [...l]);
    await this.ui.refreshTree();
    const n = this.items().filter(i => i.done).length;
    if (n) this.ui.toast(`Updated ${n} session${n === 1 ? '' : 's'} to the new address`, 'success');
  }

  async reconnect() {
    const id = this.tab;
    this.close();
    if (id) await this.tabs.reconnect(id);
  }
}
