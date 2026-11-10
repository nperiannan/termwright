import { Component, computed, inject, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { DeviceName, SessionNode } from '../core/models';
import { Ui } from '../core/ui.service';
import { Tabs } from '../core/tabs.service';
import { tr } from '../core/i18n';
import { NAME_FORMATS, NameFormat, deviceName, isAddressName } from '../core/device-name';
import { Icon } from '../components/icon';

interface Row extends DeviceName { pick: boolean; newName: string; edited: boolean; reading: boolean; }

/** Name After Devices: sessions named by an address get the name the device gives itself. */
@Component({
  selector: 'app-name-after-dialog',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop" (click)="ui.close()">
    <div class="modal nad" (click)="$event.stopPropagation()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="edit" [size]="17" /></span>
        <span class="mh-t">{{ tr('Name After Devices') }}<span class="mh-s">{{ ui.nameAfter().title }} - {{ tr('{0} sessions', rows().length) }}</span></span></div>
      <div class="modal-body">
        <div class="bar"><span class="muted">{{ tr('Name') }}</span>
          <select [ngModel]="fmt()" (ngModelChange)="setFmt($event)" name="fmt">@for (f of formats; track f.v) { <option [ngValue]="f.v">{{ tr(f.label) }}</option> }</select>
          <label class="chk"><input type="checkbox" [ngModel]="onlyIP()" (ngModelChange)="onlyIP.set($event)" name="oip" /> {{ tr('Only sessions named by an address') }}</label>
          <span class="spacer"></span>
          <button type="button" (click)="read()" [disabled]="reading()">{{ reading() ? tr('Reading…') : tr('Read Again') }}</button></div>
        <div class="scroll"><table class="nt">
          <thead><tr><th class="c"><input type="checkbox" [checked]="allOn()" (change)="pickAll($any($event.target).checked)" [title]="tr('Select all')" /></th><th>{{ tr('Now') }}</th><th>{{ tr('New name') }}</th><th>{{ tr('Model') }}</th><th>{{ tr('Device') }}</th></tr></thead>
          <tbody>
          @for (r of shown(); track r.path) {
            <tr [class.off]="!can(r)">
              <td class="c"><input type="checkbox" [ngModel]="r.pick" (ngModelChange)="r.pick = $event; bump()" [name]="'p' + $index" [disabled]="!can(r)" /></td>
              <td class="mono" [title]="r.path">{{ r.name }}</td>
              <td>@if (can(r)) { <input class="nn" [ngModel]="r.newName" [name]="'n' + $index" (ngModelChange)="r.newName = $event; r.edited = true; r.pick = !!$event.trim(); bump()" spellcheck="false" /> }
                  @else if (r.hostname) { <span class="muted">{{ r.hostname }}</span> } @else { <span class="muted">–</span> }</td>
              <td>{{ r.model || '–' }}</td>
              <td>@if (r.reading) { <span class="pill">{{ tr('reading…') }}</span> } @else { <span class="pill" [class]="'pill ' + pillClass(r)" [title]="r.detail">{{ status(r) }}</span> }</td>
            </tr>
          } @empty { <tr><td colspan="5" class="muted empty">{{ onlyIP() ? tr('No session here is named by its address. Untick “Only sessions named by an address” to see them all.') : tr('No sessions.') }}</td></tr> }
          </tbody></table></div>
        <p class="muted small note">{{ tr('Read with each session’s own saved login in the background (show switch, show sys-info, show version, hostname) - nothing is typed in your tabs. The address stays the session’s host; a name that was not the address is kept in the description. Team devices are named by the team (owners: Admin page → Inventory → Name After Devices).') }}</p>
        @if (err()) { <div class="err small">{{ err() }}</div> }
      </div>
      <div class="modal-foot"><span class="muted small">{{ tr('{0} of {1} will be renamed', todo().length, shown().length) }}</span><span class="spacer"></span>
        <button type="button" (click)="ui.close()">{{ tr('Cancel') }}</button>
        <button type="button" class="primary" (click)="rename()" [disabled]="!todo().length || busy()">{{ todo().length === 1 ? tr('Rename 1 Session') : tr('Rename {0} Sessions', todo().length) }}</button></div>
    </div>
  </div>`,
  styles: [`
    .nad { width: min(900px, 96vw); }
    .bar { display: flex; gap: 8px; align-items: center; margin-bottom: 12px; flex-wrap: wrap; }
    .chk { display: inline-flex; gap: 6px; align-items: center; }
    .spacer { flex: 1; }
    .scroll { max-height: 52vh; overflow: auto; border: 1px solid var(--line); border-radius: 10px; }
    .nt { width: 100%; border-collapse: collapse; }
    .nt th { position: sticky; top: 0; background: var(--surface-2); text-align: left; font-size: 12px; font-weight: 600; padding: 7px 10px; }
    .nt td { padding: 5px 10px; border-top: 1px solid var(--line); vertical-align: middle; }
    .nt .c { width: 30px; }
    .nt tr.off td { color: var(--muted); }
    .nt input.nn { width: 100%; height: 26px; font-weight: 600; }
    .mono { font-family: var(--mono); font-size: 12px; }
    .empty { text-align: center; padding: 18px; }
    .pill { display: inline-block; padding: 1px 8px; border-radius: 10px; font-size: 11.5px; border: 1px solid var(--line); white-space: nowrap; }
    .pill.ok { color: var(--ok); border-color: color-mix(in srgb, var(--ok) 45%, transparent); background: color-mix(in srgb, var(--ok) 12%, transparent); }
    .pill.bad { color: var(--danger); border-color: color-mix(in srgb, var(--danger) 45%, transparent); }
    .pill.warn { color: var(--warn); border-color: color-mix(in srgb, var(--warn) 45%, transparent); }
    .note { margin: 10px 0 0; }
    .err { color: var(--danger); margin-top: 6px; }
  `],
})
export class NameAfterDialog implements OnInit {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  protected tr = tr;
  protected formats = NAME_FORMATS;
  protected rows = signal<Row[]>([]);
  protected reading = signal(false);
  protected busy = signal(false);
  protected err = signal('');
  protected onlyIP = signal(true);
  protected fmt = computed(() => (this.ui.settings()?.nameFormat || '') as NameFormat);
  private tick = signal(0); // rows change in place

  protected shown = computed(() => { this.tick(); return this.rows().filter(r => !this.onlyIP() || isAddressName(r.name, r.host)); });
  protected todo = computed(() => { this.tick(); return this.shown().filter(r => r.pick && this.can(r) && r.newName.trim() && r.newName.trim() !== r.name); });
  protected allOn = computed(() => { const s = this.shown().filter(r => this.can(r)); return !!s.length && s.every(r => r.pick); });

  ngOnInit() {
    const want = new Set(this.ui.nameAfter().paths);
    const rows: Row[] = [];
    const walk = (ns: SessionNode[]) => ns.forEach(n => n.isFolder ? walk(n.children ?? []) : want.has(n.path) && rows.push({
      path: n.path, name: n.name, host: n.hostname ?? '', hostname: '', model: '', team: !!n.team, state: '', detail: '', pick: false, newName: '', edited: false, reading: true }));
    const root = this.ui.tree(); if (root) walk([root]);
    this.rows.set(rows);
    void this.read();
  }

  /** Reads the hostnames, a few devices at a time, filling the rows as they come. */
  async read() {
    if (this.reading()) return;
    this.reading.set(true); this.err.set('');
    const list = this.rows().filter(r => !this.onlyIP() || isAddressName(r.name, r.host));
    list.forEach(r => r.reading = true); this.bump();
    let i = 0;
    const next = async (): Promise<void> => {
      const r = list[i++]; if (!r) return;
      try {
        const [d] = await this.backend.readDeviceNames([r.path]);
        if (d) Object.assign(r, { hostname: d.hostname, model: d.model, state: d.state, detail: d.detail, team: d.team, host: d.host || r.host });
      } catch (e) { r.state = 'failed'; r.detail = errText(e); }
      r.reading = false;
      if (!r.edited) { r.newName = deviceName(r.hostname, r.model, r.host, this.fmt()); r.pick = this.can(r) && !!r.newName && r.newName !== r.name; }
      this.bump();
      return next();
    };
    await Promise.all(Array.from({ length: Math.min(6, list.length) }, next));
    this.rows().forEach(r => r.reading = false);
    this.reading.set(false); this.bump();
  }

  can(r: Row) { return !r.reading && !r.team && r.state === 'read' && !!r.hostname; }
  status(r: Row): string {
    return ({ read: tr('✓ read'), team: tr('team device'), nologin: tr('no saved login'), norule: tr('no hostname rule'), notfound: tr('not found'), failed: tr('could not log in') } as Record<string, string>)[r.state] || '';
  }
  pillClass(r: Row) { return r.state === 'read' ? 'ok' : r.state === 'failed' || r.state === 'nologin' ? 'bad' : r.state === 'team' ? '' : 'warn'; }

  setFmt(f: NameFormat) {
    void this.ui.patchSettings({ nameFormat: f });
    this.rows().forEach(r => { if (!r.edited && r.hostname) r.newName = deviceName(r.hostname, r.model, r.host, f); });
    this.bump();
  }
  pickAll(on: boolean) { this.shown().forEach(r => r.pick = on && this.can(r) && !!r.newName.trim()); this.bump(); }
  protected bump() { this.rows.set([...this.rows()]); this.tick.update(v => v + 1); }

  async rename() {
    const items = this.todo().map(r => ({ path: r.path, name: r.newName.trim() }));
    if (!items.length) return;
    this.busy.set(true); this.err.set('');
    try {
      const paths = await this.tabs.renameSessions(items);
      const undo = items.map((it, i) => ({ path: paths[i], name: this.rows().find(r => r.path === it.path)?.name ?? '' })).filter((x, i) => x.path && x.name && x.path !== items[i].path);
      if (undo.length) {
        this.ui.toast(undo.length === 1 ? tr('Renamed 1 session after its device') : tr('Renamed {0} sessions after their devices', undo.length), 'success', 8000,
          [{ label: tr('Undo'), run: () => void this.tabs.renameSessions(undo, false) }]);
      }
      if (paths.every(p => p)) this.ui.close();
    } catch (e) { this.err.set(errText(e)); } finally { this.busy.set(false); }
  }
}
