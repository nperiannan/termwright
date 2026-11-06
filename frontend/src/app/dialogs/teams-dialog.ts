import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';
import { Autofocus } from '../core/autofocus';
import { SessionNode, TeamDetails, TeamInfo, TeamMember, TeamSetUpResult, TeamSetUpStep, TeamView } from '../core/models';

const ROLE: Record<string, string> = { owner: 'Owner', 'co-owner': 'Co-owner', member: 'Member' };

/**
 * Teams: the team servers this Termwright joined, to see who uses which
 * device. Off until a team is joined or set up. Owners and co-owners see the
 * invite and manage the members; anyone can set up a team server for a lab on
 * a Linux host with Docker.
 */
@Component({
  selector: 'app-teams-dialog',
  imports: [FormsModule, Icon, Autofocus],
  template: `
  <div class="modal-backdrop" (click)="close()">
    <div class="modal teams" (click)="$event.stopPropagation()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="sessions" [size]="17" /></span>
        <span class="mh-t">{{ view() === 'setup' ? 'Set Up a Team Server' : view() === 'join' ? 'Join a Team' : 'Teams' }}
          <span class="mh-s">{{ view() === 'setup' ? 'A small Docker container on a Linux host of your lab - your team connects to it'
            : view() === 'join' ? 'Paste the invite the team\\'s owner sent you' : 'Who uses which device - shared with your team servers only' }}</span></span></div>

      @switch (view()) {
      @case ('list') {
      <div class="modal-body">
        @if (loading() && !teams().length) { <p class="muted">Asking the team servers…</p> }
        @if (!loading() && !teams().length) {
          <div class="empty">
            <b>Teams are off.</b> Termwright works on its own as always. With a team, everyone sees which devices the others have open or reserved.
            <br />Join your team with an invite from its owner, or set up a team server for your lab.
          </div>
        }
        @for (t of teams(); track t.id) {
          <div class="team" [class.sel]="t.id === sel()" (click)="pick(t.id)">
            <span class="dot" [class.off]="!t.reachable"></span>
            <div class="tm">
              <div><b>{{ t.name }}</b> <span class="pill" [class.own]="t.role !== 'member'">{{ role(t.role) }}</span></div>
              <div class="mono muted">{{ t.address }}@if (t.reachable) { · {{ t.members }} member{{ t.members === 1 ? '' : 's' }} · {{ t.online }} online }</div>
              @if (t.error) { <div class="err">{{ t.error }}</div> }
            </div>
            <button type="button" (click)="$event.stopPropagation(); leave(t)">{{ t.role === 'owner' ? 'Leave…' : 'Leave' }}</button>
          </div>
        }
        @if (det(); as d) {
          @if (d.inviteLink) {
            <div class="lbl">Invite to {{ d.name }} <span class="muted">- send it to your team by chat or e-mail</span></div>
            <div class="inv"><span class="mono">{{ d.inviteLink }}</span>
              <button type="button" (click)="copy(d.inviteLink)">{{ copied() ? 'Copied' : 'Copy' }}</button>
              <button type="button" (click)="rotate(d)" title="A new invite - the old one stops working">New Invite</button></div>
          }
          <div class="lbl">Members</div>
          <div class="members">
            @for (m of d.members; track m.id) {
              <div class="mem">
                <span class="dot" [class.off]="!m.online" [title]="m.online ? 'online' : 'offline'"></span>
                <span class="mn">{{ m.name }}@if (m.id === d.me) { <span class="muted"> (you)</span> }</span>
                <span class="mono muted">{{ m.user }}&#64;{{ m.machine }}</span>
                <span class="pill" [class.own]="m.role !== 'member'">{{ role(m.role) }}</span>
                <span class="sp"></span>
                @if (canManage(d, m)) {
                  <a (click)="setRole(d, m, m.role === 'member' ? 'co-owner' : 'member')">{{ m.role === 'member' ? 'Make co-owner' : 'Make member' }}</a>
                  <a class="danger" (click)="removeMember(d, m)">Remove</a>
                }
              </div>
            }
          </div>
        }
        @if (error()) { <p class="err">{{ error() }}</p> }
        <div class="me">You appear as
          <input name="dn" [(ngModel)]="displayName" (blur)="saveName()" (keydown.enter)="saveName()" [placeholder]="ident().name" />
          <span class="mono muted">{{ ident().user }}&#64;{{ ident().machine }}</span></div>
      </div>
      <div class="modal-foot">
        <button type="button" (click)="toJoin()">Join a Team…</button>
        <button type="button" (click)="toSetup()">Set Up a Team Server…</button>
        <span class="spacer"></span>
        <button type="button" class="primary" (click)="close()">Done</button>
      </div>
      }

      @case ('join') {
      <form class="modal-body" (ngSubmit)="join()" id="joinf">
        <div class="lbl">Invite</div>
        <textarea name="inv" class="mono" rows="3" [(ngModel)]="invite" (ngModelChange)="checkInvite()" appAutofocus spellcheck="false"
          placeholder="termwright-team://10.234.179.141:7443/?k=…&fp=…"></textarea>
        @if (info(); as i) {
          <div class="found"><app-icon name="check" [size]="14" /> <b>{{ i.name }}</b> · {{ i.members }} member{{ i.members === 1 ? '' : 's' }}, {{ i.online }} online · server {{ i.version }}</div>
        }
        <p class="hint">The invite holds the server's address, the team key and the fingerprint of its certificate: Termwright talks only to that server.
          Your name, user name and computer name are shown to the team - never passwords, keys or sessions.</p>
        @if (error()) { <p class="err">{{ error() }}</p> }
      </form>
      <div class="modal-foot">
        <button type="button" (click)="back()">Back</button>
        <span class="spacer"></span>
        <button type="submit" form="joinf" class="primary" [disabled]="busy() || !invite.trim()">{{ busy() ? 'Joining…' : 'Join' }}</button>
      </div>
      }

      @case ('setup') {
      <form class="modal-body" (ngSubmit)="setUp()" id="setf">
        <div class="lbl">Host <span class="muted">- an SSH session of a Linux host with Docker; it logs in as the session does (best with a key)</span></div>
        <select name="host" [(ngModel)]="host" [disabled]="!!run()">
          <option value="" disabled>Pick an SSH session…</option>
          @for (p of sshSessions(); track p) { <option [value]="p">{{ p }}</option> }
        </select>
        <div class="row2">
          <div><div class="lbl">Team name</div><input name="tn" [(ngModel)]="teamName" [disabled]="!!run()" placeholder="e.g. Switching Lab B" /></div>
          <div><div class="lbl">Port</div><input name="pt" class="mono" [(ngModel)]="port" [disabled]="!!run()" placeholder="free one from 7443" /></div>
        </div>
        <div class="lbl">Container image</div>
        <div class="seg">
          <button type="button" [class.on]="image === 'download'" (click)="image = 'download'" [disabled]="!!run()">Download (GitHub)</button>
          <button type="button" [class.on]="image === 'copy'" (click)="image = 'copy'" [disabled]="!!run()">Copy over SSH</button>
        </div>
        <span class="muted small"> {{ image === 'download' ? 'the host downloads it; falls back to copying when the host has no internet' : 'this computer downloads it and copies it to the host - for labs without internet' }}</span>
        @if (steps().length) {
          <div class="steps">
            @for (s of steps(); track s.step + s.text) {
              <div [class]="s.state"><span class="st">{{ s.state === 'ok' ? '✓' : s.state === 'fail' ? '✕' : s.state === 'info' ? 'i' : '●' }}</span>{{ s.text }}</div>
            }
          </div>
        }
        @if (done(); as r) {
          @if (r.error) { <p class="err">{{ r.error }}</p> }
          @else {
            <div class="found"><app-icon name="check" [size]="14" /> The team server runs. Send this invite to your team:</div>
            <div class="inv"><span class="mono">{{ r.invite }}</span><button type="button" (click)="copy(r.invite)">{{ copied() ? 'Copied' : 'Copy' }}</button></div>
          }
        }
        @if (!run() && !done()) {
          <p class="hint">Termwright checks Docker, picks a free port (it never touches 80, 8080 or anything in use), starts <span class="mono">termwright-team-{{ slugOf() || 'name' }}</span>
            with its own data volume, restarting with the host, and makes you its owner. Several teams can use the same host. Nothing else on the host is changed.</p>
        }
        @if (error()) { <p class="err">{{ error() }}</p> }
      </form>
      <div class="modal-foot">
        @if (done()?.teamId) { <span class="spacer"></span><button type="button" class="primary" (click)="back()">Done</button> }
        @else {
          <button type="button" (click)="run() ? cancelSetup() : back()">{{ run() ? 'Cancel' : 'Back' }}</button>
          <span class="spacer"></span>
          <button type="submit" form="setf" class="primary" [disabled]="!!run() || !host || !teamName.trim()">{{ run() ? 'Setting up…' : done() ? 'Try Again' : 'Set Up' }}</button>
        }
      </div>
      }
      }
    </div>
  </div>`,
  styles: [`
    .teams { width: min(700px, 94vw); }
    .modal-body { max-height: min(640px, calc(100vh - 180px)); overflow: auto; }
    .muted { color: var(--muted); } .small { display: block; margin-top: 5px; font-size: calc(12px * var(--ui-scale, 1)); }
    .empty { padding: 14px; border: 1px dashed var(--border); border-radius: 10px; line-height: 1.55; color: var(--muted); b { color: var(--text); } }
    .team { display: flex; gap: 10px; align-items: center; padding: 9px 12px; border: 1px solid var(--border); border-radius: 10px; margin-bottom: 8px; cursor: pointer; }
    .team.sel { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 7%, transparent); }
    .tm { flex: 1; min-width: 0; .mono { font-size: calc(12px * var(--ui-scale, 1)); } }
    .dot { width: 8px; height: 8px; border-radius: 50%; background: #22c55e; flex: none; } .dot.off { background: var(--muted); opacity: .5; }
    .pill { margin-left: 4px; font-size: calc(11px * var(--ui-scale, 1)); border-radius: 5px; padding: 1px 7px; background: color-mix(in srgb, var(--text) 8%, transparent); }
    .pill.own { background: color-mix(in srgb, var(--accent) 16%, transparent); color: var(--accent); }
    .lbl { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); margin: 12px 0 5px; }
    .inv { display: flex; gap: 8px; align-items: center; padding: 6px 6px 6px 10px; border: 1px solid var(--border); border-radius: 8px;
      .mono { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: calc(12px * var(--ui-scale, 1)); } }
    .members { display: flex; flex-direction: column; gap: 2px; }
    .mem { display: flex; align-items: center; gap: 8px; padding: 4px 2px; font-size: calc(12.5px * var(--ui-scale, 1));
      .mono { font-size: calc(11.5px * var(--ui-scale, 1)); } a { color: var(--accent); cursor: pointer; } a.danger { color: var(--danger); } }
    .mn { font-weight: 500; } .sp { flex: 1; }
    .me { display: flex; align-items: center; gap: 8px; margin-top: 16px; padding-top: 12px; border-top: 1px solid var(--border); color: var(--muted);
      input { width: 180px; } .mono { font-size: calc(12px * var(--ui-scale, 1)); } }
    textarea { width: 100%; resize: vertical; }
    select, .row2 input { width: 100%; }
    .row2 { display: grid; grid-template-columns: 1fr 170px; gap: 10px; }
    .seg { display: inline-flex; border: 1px solid var(--border); border-radius: 8px; overflow: hidden;
      button { border: 0; border-radius: 0; background: transparent; padding: 5px 12px; white-space: nowrap; } button.on { background: color-mix(in srgb, var(--text) 10%, transparent); font-weight: 600; } }
    .steps { margin-top: 12px; border: 1px solid var(--border); border-radius: 10px; padding: 8px 12px; font-family: var(--mono); font-size: calc(12px * var(--ui-scale, 1)); line-height: 1.7;
      .st { display: inline-block; width: 18px; } .ok .st { color: #22c55e; } .fail { color: var(--danger); } .run .st { color: var(--accent); } .info { color: var(--muted); } }
    .found { display: flex; align-items: center; gap: 6px; margin: 10px 0 6px; app-icon { color: #22c55e; } }
    .hint { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); margin: 10px 0 0; line-height: 1.45; }
    .err { color: var(--danger); margin: 10px 0 0; white-space: pre-wrap; }
  `],
})
export class TeamsDialog implements OnInit, OnDestroy {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  readonly view = signal<'list' | 'join' | 'setup'>('list');
  readonly teams = signal<TeamView[]>([]);
  readonly loading = signal(false);
  readonly sel = signal('');
  readonly det = signal<TeamDetails | null>(null);
  readonly error = signal('');
  readonly busy = signal(false);
  readonly copied = signal(false);
  readonly ident = signal({ name: '', user: '', machine: '' });
  displayName = '';
  // join
  invite = '';
  readonly info = signal<TeamInfo | null>(null);
  // setup
  host = '';
  teamName = '';
  port = '';
  image: 'download' | 'copy' = 'download';
  readonly run = signal('');
  readonly steps = signal<TeamSetUpStep[]>([]);
  readonly done = signal<TeamSetUpResult | null>(null);
  private offs: (() => void)[] = [];

  readonly sshSessions = computed(() => {
    const out: string[] = [];
    const walk = (n: SessionNode) => (n.children ?? []).forEach(c => c.isFolder ? walk(c) : c.protocol === 'SSH2' && out.push(c.path));
    const r = this.ui.tree();
    if (r) walk(r);
    return out;
  });

  role(r: string) { return ROLE[r] ?? r; }
  slugOf() { return this.teamName.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 40).replace(/-+$/, ''); }

  ngOnInit() {
    this.displayName = this.ui.settings()?.teamDisplayName ?? '';
    void this.backend.teamIdentity().then(i => this.ident.set(i)).catch(() => {});
    void this.load();
    this.offs.push(this.backend.on('team:setup', (s: TeamSetUpStep) => {
      if (this.run() !== 'pending' && s.run !== this.run()) return;
      // a step replaces its own running line
      this.steps.update(l => [...l.filter(x => !(x.step === s.step && x.state === 'run')), s]);
    }));
    this.offs.push(this.backend.on('team:setup:done', (r: TeamSetUpResult) => {
      if (this.run() !== 'pending' && r.run !== this.run()) return;
      this.run.set('');
      this.done.set(r);
      if (r.teamId) { this.sel.set(r.teamId); void this.load(); }
    }));
  }
  ngOnDestroy() { this.offs.forEach(f => f()); }

  close() {
    if (this.run()) return; // a setup runs: Cancel first
    this.ui.close();
  }

  async load() {
    this.loading.set(true);
    try {
      const t = await this.backend.teams();
      this.teams.set(t ?? []);
      if (!t?.some(x => x.id === this.sel())) this.sel.set(t?.[0]?.id ?? '');
      await this.loadDetails();
    } catch (e) { this.error.set(errText(e)); }
    finally { this.loading.set(false); }
  }

  async loadDetails() {
    const id = this.sel();
    const t = this.teams().find(x => x.id === id);
    if (!t?.reachable) { this.det.set(null); return; }
    try { this.det.set(await this.backend.teamDetails(id)); } catch (e) { this.det.set(null); this.error.set(errText(e)); }
  }

  pick(id: string) { this.sel.set(id); this.error.set(''); void this.loadDetails(); }

  canManage(d: TeamDetails, m: TeamMember) {
    if (m.id === d.me || m.role === 'owner') return false;
    return d.role === 'owner' || (d.role === 'co-owner' && m.role === 'member');
  }

  async copy(text: string) {
    await this.backend.clipboardSet(text);
    this.copied.set(true);
    setTimeout(() => this.copied.set(false), 1500);
  }

  async rotate(d: TeamDetails) {
    if (!(await this.ui.confirm('New invite', `A new invite for ${d.name}: the old one stops working. People who already joined stay in the team.`, 'New Invite'))) return;
    try { await this.backend.teamRotateInvite(d.id); await this.loadDetails(); } catch (e) { this.error.set(errText(e)); }
  }

  async setRole(d: TeamDetails, m: TeamMember, role: string) {
    try { await this.backend.teamSetRole(d.id, m.id, role); await this.loadDetails(); } catch (e) { this.error.set(errText(e)); }
  }

  async removeMember(d: TeamDetails, m: TeamMember) {
    if (!(await this.ui.confirm('Remove ' + m.name, `${m.name} (${m.user}@${m.machine}) leaves ${d.name} and no longer sees or shares device status. They can join again only with a new invite - make one if they had the current one.`, 'Remove', true))) return;
    try { await this.backend.teamRemoveMember(d.id, m.id); await this.load(); } catch (e) { this.error.set(errText(e)); }
  }

  async leave(t: TeamView) {
    const owner = t.role === 'owner';
    if (!(await this.ui.confirm('Leave ' + t.name, owner
      ? `You are the owner. The owner can leave only when nobody else is in the team; the team server keeps running on ${t.host || 'its host'}` + (t.container ? ` (${t.container})` : '') + '.'
      : `You stop seeing and sharing device status with ${t.name}. You need a new invite to join again.`, 'Leave', true))) return;
    try { await this.backend.teamLeave(t.id, false); }
    catch (e) {
      const msg = errText(e);
      if (!(await this.ui.confirm('Forget ' + t.name + '?', msg + '\n\nForget the team on this computer anyway?', 'Forget', true))) return;
      await this.backend.teamLeave(t.id, true).catch(() => {});
    }
    await this.load();
  }

  async saveName() {
    const s = this.ui.settings();
    const v = this.displayName.trim();
    if (!s || (s.teamDisplayName ?? '') === v) return;
    await this.ui.saveSettings({ ...s, teamDisplayName: v });
  }

  toJoin() { this.error.set(''); this.info.set(null); this.invite = ''; this.view.set('join'); }
  toSetup() {
    this.error.set(''); this.steps.set([]); this.done.set(null);
    if (!this.host) this.host = this.sshSessions()[0] ?? '';
    this.view.set('setup');
  }
  back() { this.error.set(''); this.view.set('list'); void this.load(); }

  private infoTimer = 0;
  checkInvite() {
    clearTimeout(this.infoTimer);
    this.info.set(null);
    this.error.set('');
    const v = this.invite.trim();
    if (!v) return;
    this.infoTimer = window.setTimeout(async () => {
      try { this.info.set(await this.backend.teamInviteInfo(v)); } catch (e) { this.error.set(errText(e)); }
    }, 400);
  }

  async join() {
    this.busy.set(true);
    this.error.set('');
    try {
      const t = await this.backend.teamJoin(this.invite.trim());
      this.sel.set(t.id);
      this.ui.toast(`You joined ${t.name}`, 'success');
      this.back();
    } catch (e) { this.error.set(errText(e)); }
    finally { this.busy.set(false); }
  }

  async setUp() {
    this.error.set('');
    this.steps.set([]);
    this.done.set(null);
    const port = this.port.trim() ? Number(this.port.trim()) : 0;
    if (!Number.isInteger(port) || port < 0) { this.error.set('The port is a number (or empty for a free one).'); return; }
    try {
      this.run.set('pending');
      const id = await this.backend.teamSetUp({ sessionPath: this.host, name: this.teamName.trim(), port, image: this.image });
      if (this.run() === 'pending') this.run.set(id); // (done already when it ended at once)
    } catch (e) { this.run.set(''); this.error.set(errText(e)); }
  }

  cancelSetup() { if (this.run()) void this.backend.teamSetUpCancel(this.run()); }
}
