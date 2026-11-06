import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';
import { TeamPushItem, TeamPushResult, TeamConn } from '../core/models';

interface Row { item: TeamPushItem; check?: TeamPushResult; pick: boolean; result?: TeamPushResult; }

/**
 * Push to Team: offers sessions (or Discover results) to a team's inventory.
 * Nothing leaves this computer until Push is clicked; devices the team has
 * already - under any name - are shown as such and left out. Owners and
 * co-owners add directly; what a member pushes waits for their approval.
 */
@Component({
  selector: 'app-team-push-dialog',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop" (click)="close()">
    <div class="modal push" (click)="$event.stopPropagation()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="upload" [size]="17" /></span>
        <span class="mh-t">Push to Team<span class="mh-s">Your sessions stay yours - this offers the devices to the team's inventory</span></span></div>
      <div class="modal-body">
        @if (teams().length > 1) {
          <div class="row"><span class="lbl">Team</span>
            <select [(ngModel)]="teamId" (ngModelChange)="check()">@for (t of teams(); track t.id) { <option [value]="t.id">{{ t.name }} ({{ role(t) }})</option> }</select></div>
        }
        <div class="row"><span class="lbl">Into the folder</span>
          <input [(ngModel)]="folder" placeholder="as in your Session Manager" /><span class="muted small">of the team inventory</span></div>
        <p class="note">{{ manager() ? 'You are ' + role(team()!) + ': the devices are added at once and appear in every member\\'s team folder.'
          : 'An owner or co-owner of ' + (team()?.name ?? 'the team') + ' approves them on the admin page; until then they stay only in your sessions.' }}</p>
        @if (loading()) { <p class="muted">Asking the team server which devices it has…</p> }
        <div class="list">
          @for (r of rows(); track r.item.host + r.item.port + r.item.name) {
            <label class="it" [class.off]="!canPick(r)">
              <input type="checkbox" [(ngModel)]="r.pick" [disabled]="!canPick(r) || done()" />
              <span class="nm">{{ r.item.name }}</span>
              <span class="mono muted">{{ r.item.host }}{{ r.item.port && r.item.port !== 22 && r.item.port !== 23 ? ':' + r.item.port : '' }}</span>
              <span class="muted">{{ r.item.deviceType }}</span>
              <span class="sp"></span>
              @if (r.result; as x) {
                <span class="st" [class]="x.status">{{ statusText(x) }}</span>
              } @else if (r.check; as c) {
                <span class="st" [class]="c.status">{{ statusText(c) }}</span>
              }
            </label>
          }
        </div>
        @if (error()) { <p class="err">{{ error() }}</p> }
      </div>
      <div class="modal-foot">
        <span class="muted">{{ summary() }}</span>
        <span class="spacer"></span>
        @if (done()) { <button type="button" class="primary" (click)="close()">Close</button> }
        @else {
          <button type="button" (click)="close()">Cancel</button>
          <button type="button" class="primary" [disabled]="busy() || !picked()" (click)="push()">{{ busy() ? 'Pushing…' : 'Push ' + picked() + ' to ' + (team()?.name ?? 'the team') }}</button>
        }
      </div>
    </div>
  </div>`,
  styles: [`
    .push { width: min(720px, 94vw); }
    .row { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; select, input { flex: 1; max-width: 340px; } }
    .lbl { width: 110px; color: var(--muted); }
    .muted { color: var(--muted); } .small { font-size: calc(12px * var(--ui-scale, 1)); }
    .note { margin: 4px 0 10px; padding: 8px 10px; border-radius: 8px; background: color-mix(in srgb, var(--accent) 8%, transparent); font-size: calc(12.5px * var(--ui-scale, 1)); }
    .list { max-height: min(420px, calc(100vh - 340px)); overflow: auto; border: 1px solid var(--border); border-radius: 8px; }
    .it { display: flex; align-items: center; gap: 10px; padding: 6px 10px; border-bottom: 1px solid var(--border); font-size: calc(12.5px * var(--ui-scale, 1)); }
    .it:last-child { border-bottom: 0; } .it.off .nm { color: var(--muted); }
    .nm { font-weight: 500; min-width: 140px; } .mono { font-family: var(--mono); font-size: calc(12px * var(--ui-scale, 1)); } .sp { flex: 1; }
    .st { font-size: calc(11.5px * var(--ui-scale, 1)); border-radius: 10px; padding: 1px 9px; white-space: nowrap; background: color-mix(in srgb, var(--text) 8%, transparent); }
    .st.new { color: var(--accent); } .st.added { color: #16a34a; background: color-mix(in srgb, #16a34a 14%, transparent); }
    .st.exists { color: var(--muted); } .st.pending { color: #b45309; background: color-mix(in srgb, #f59e0b 16%, transparent); } .st.invalid { color: var(--danger); }
    .err { color: var(--danger); margin: 10px 0 0; }
  `],
})
export class TeamPushDialog implements OnInit {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  readonly teams = computed<TeamConn[]>(() => this.ui.settings()?.teams ?? []);
  teamId = '';
  folder = '';
  readonly rows = signal<Row[]>([]);
  readonly loading = signal(false);
  readonly busy = signal(false);
  readonly done = signal(false);
  readonly error = signal('');

  close() { this.ui.teamPush.set(null); }
  team() { return this.teams().find(t => t.id === this.teamId); }
  role(t: TeamConn) { return ({ owner: 'owner', 'co-owner': 'co-owner', member: 'member' } as Record<string, string>)[t.role] ?? t.role; }
  manager() { const t = this.team(); return !!t && t.role !== 'member'; }
  canPick(r: Row) { return !r.check || r.check.status === 'new'; }
  picked() { return this.rows().filter(r => r.pick && this.canPick(r)).length; }
  statusText(x: TeamPushResult) {
    switch (x.status) {
      case 'new': return 'new for the team';
      case 'added': return 'added';
      case 'exists': return 'already in the team as ' + x.name + (x.folder ? ' (' + x.folder + ')' : '');
      case 'pending': return 'waiting for approval' + (x.msg ? ' - ' + x.msg : '');
      default: return x.msg || 'cannot be pushed';
    }
  }
  summary() {
    const r = this.rows();
    if (this.done()) {
      const a = r.filter(x => x.result?.status === 'added').length, p = r.filter(x => x.result?.status === 'pending').length;
      return a ? `${a} added to the team` : p ? `${p} sent for approval` : '';
    }
    const ex = r.filter(x => x.check && x.check.status !== 'new').length;
    return `${r.length} device${r.length === 1 ? '' : 's'}` + (ex ? ` · ${ex} the team has already` : '');
  }

  async ngOnInit() {
    const req = this.ui.teamPush();
    this.teamId = this.teams()[0]?.id ?? '';
    try {
      const items = req?.items ?? await this.backend.teamPushDevices(req?.paths ?? [], req?.base ?? '');
      this.rows.set(items.map(item => ({ item, pick: true })));
      if (!items.length) this.error.set('Nothing to push: only sessions with an address can go to a team (not serial, local shell or Wake-on-LAN).');
    } catch (e) { this.error.set(errText(e)); }
    await this.check();
  }

  private items() {
    const base = this.folder.trim().replace(/^\/+|\/+$/g, '');
    return this.rows().map(r => ({ ...r.item, folder: base ? (r.item.folder ? base + '/' + r.item.folder : base) : r.item.folder }));
  }

  async check() {
    if (!this.teamId || !this.rows().length) return;
    this.loading.set(true);
    this.error.set('');
    try {
      const res = await this.backend.teamCheck(this.teamId, this.items());
      this.rows.update(rs => rs.map((r, i) => ({ ...r, check: res[i], pick: res[i]?.status === 'new' })));
    } catch (e) { this.error.set(errText(e)); }
    finally { this.loading.set(false); }
  }

  async push() {
    const all = this.items();
    const idx = this.rows().map((r, i) => r.pick && this.canPick(r) ? i : -1).filter(i => i >= 0);
    this.busy.set(true);
    this.error.set('');
    try {
      const res = await this.backend.teamPush(this.teamId, idx.map(i => all[i]));
      this.rows.update(rs => rs.map((r, i) => { const k = idx.indexOf(i); return k >= 0 ? { ...r, result: res[k] } : r; }));
      this.done.set(true);
    } catch (e) { this.error.set(errText(e)); }
    finally { this.busy.set(false); }
  }
}
