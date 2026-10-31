import { Component, computed, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { FolderDefault, SessionNode, Workspace } from '../core/models';
import { Layout, Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { envList } from '../core/env';
import { Icon } from '../components/icon';
import { FolderIcon } from '../components/folder-icon';

/** Saves the open tabs and layout under a name. */
export async function saveWorkspace(ui: Ui, tabs: Tabs) {
  const list = tabs.tabs().filter(t => t.session.path && (t.kind === 'terminal' || !t.parentId));
  if (!list.length) { ui.toast('Open some saved sessions first - ad-hoc connections cannot be saved in a workspace.', 'error'); return; }
  const name = await ui.input('Save Workspace', 'Workspace name', 'Workspace ' + ((ui.settings()?.workspaces?.length ?? 0) + 1));
  if (!name) return;
  const ws: Workspace = { name, layout: tabs.layout(), tabs: list.map(t => ({ session: t.session.path, kind: t.kind })) };
  const s = ui.settings();
  if (!s) return;
  await ui.saveSettings({ ...s, workspaces: [...(s.workspaces ?? []).filter(w => w.name !== name), ws] });
  ui.toast(`Workspace "${name}" saved (${ws.tabs.length} tabs)`, 'success');
}

/** Opens every tab of a workspace and restores its layout. */
export async function openWorkspace(tabs: Tabs, ws: Workspace) {
  tabs.setLayout((ws.layout || 'tabs') as Layout);
  for (const t of ws.tabs) await tabs.openSession(t.session, t.kind === 'sftp' ? 'sftp' : 'terminal');
}

/** File → Workspaces: open, rename or delete saved workspaces. */
@Component({
  selector: 'app-workspaces',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop">
    <div class="modal" [class.ws-m]="modern()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="layers" [size]="17" /></span>
        <span class="mh-t">Workspaces<span class="mh-s">A set of sessions and how they are laid out - open them all at once</span></span></div>
      <div class="modal-body">
        @if (modern()) {
          <div class="cards">
            @for (w of list(); track w.name) {
              <div class="ws">
                <span class="lg" [class]="'lg ' + (w.layout || 'tabs')"><i></i><i></i><i></i><i></i></span>
                <div class="wi"><b>{{ w.name }}</b><div class="hint">{{ w.tabs.length }} session{{ w.tabs.length === 1 ? '' : 's' }} · {{ layoutName(w.layout) }}</div>
                  <div class="chips">@for (c of chips(w); track $index) { <span class="chip2" [title]="c.path"><span class="dot" [class.on]="c.open"></span>{{ c.name }}</span> }</div></div>
                <button type="button" class="icon-btn" title="Rename" (click)="rename(w)"><app-icon name="edit" [size]="15" /></button>
                <button type="button" class="icon-btn" title="Delete" (click)="remove(w)"><app-icon name="trash" [size]="15" /></button>
                <button type="button" class="primary" (click)="open(w)">Open</button>
              </div>
            } @empty { <div class="empty">No workspaces yet. Open the sessions you work with together, arrange them, then <b>Save current tabs…</b></div> }
          </div>
        } @else {
        <p class="hint">A workspace remembers a set of sessions and the layout (tabs, side by side, grid). Save the current tabs with
          <b>Save Current…</b>; open one to reconnect all of its sessions.</p>
        <table class="list">
          <tbody>
            @for (w of list(); track w.name) {
              <tr>
                <td><b>{{ w.name }}</b><div class="hint">{{ w.tabs.length }} sessions · {{ w.layout }} · {{ names(w) }}</div></td>
                <td class="nowrap">
                  <button type="button" class="primary" (click)="open(w)">Open</button>
                  <button type="button" (click)="rename(w)">Rename</button>
                  <button type="button" class="icon-btn" title="Delete" (click)="remove(w)">✕</button>
                </td>
              </tr>
            } @empty { <tr><td class="hint">No workspaces yet.</td></tr> }
          </tbody>
        </table>
        }
      </div>
      <div class="modal-foot">
        <button type="button" (click)="saveCurrent()"><app-icon name="plus" [size]="14" class="md-only" />{{ modern() ? ' Save current tabs…' : 'Save Current…' }}</button>
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Close</button>
      </div>
    </div>
  </div>`,
  styles: [`td .hint { font-size: calc(11px * var(--ui-scale, 1)); } .nowrap { white-space: nowrap; text-align: right; } td { padding: 6px 4px; }
    .ws-m { width: 620px; }
    .cards { display: flex; flex-direction: column; gap: 8px; }
    .ws { display: flex; align-items: center; gap: 12px; padding: 12px 14px; border: 1px solid var(--line); border-radius: var(--r-lg); background: var(--surface);
      .wi { flex: 1; min-width: 0; } .icon-btn { width: 28px; height: 28px; color: var(--muted); } }
    .lg { width: 44px; height: 34px; flex: none; border-radius: 6px; border: 1px solid var(--line); background: var(--surface-2); display: grid; gap: 2px; padding: 3px;
      i { background: var(--accent-soft); border: 1px solid var(--accent); border-radius: 2px; }
      &.tabs { grid-template-rows: 6px 1fr; i:nth-child(n+3) { display: none; } i:first-child { opacity: .5; } }
      &.tile-v { grid-template-columns: 1fr 1fr; i:nth-child(n+3) { display: none; } }
      &.tile-h { grid-template-rows: 1fr 1fr; i:nth-child(n+3) { display: none; } }
      &.grid { grid-template: 1fr 1fr / 1fr 1fr; } }
    .chips { display: flex; gap: 5px; flex-wrap: wrap; margin-top: 6px; }
    .chip2 { display: inline-flex; align-items: center; gap: 5px; height: 22px; padding: 0 8px; border-radius: 6px; background: var(--surface-2); border: 1px solid var(--line);
      font-size: calc(11.5px * var(--ui-scale, 1)); white-space: nowrap; }
    .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--faint); &.on { background: var(--ok); } }
    .empty { color: var(--muted); padding: 18px 4px; text-align: center; }`],
})
export class Workspaces {
  protected ui = inject(Ui);
  private tabs = inject(Tabs);
  readonly list = computed(() => this.ui.settings()?.workspaces ?? []);
  readonly modern = () => this.ui.settings()?.layout !== 'classic';
  layoutName(l: string) { return ({ 'tile-v': 'side by side', 'tile-h': 'one above the other', grid: 'grid' } as Record<string, string>)[l] ?? 'tabs'; }
  /** The workspace's sessions; a green dot when one is open now. */
  chips(w: Workspace) {
    const open = new Set(this.tabs.tabs().map(t => t.session.path));
    return w.tabs.map(t => ({ path: t.session, name: (t.session.split('/').pop() ?? t.session) + (t.kind === 'sftp' && !/sftp/i.test(t.session) ? ' (SFTP)' : ''), open: open.has(t.session) }));
  }

  names(w: Workspace) {
    const n = w.tabs.map(t => t.session.split('/').pop()).join(', ');
    return n.length > 80 ? n.slice(0, 80) + '…' : n;
  }

  async open(w: Workspace) { this.ui.close(); await openWorkspace(this.tabs, w); }
  async saveCurrent() { await saveWorkspace(this.ui, this.tabs); }

  private async store(list: Workspace[]) {
    const s = this.ui.settings();
    if (s) await this.ui.saveSettings({ ...s, workspaces: list });
  }

  async rename(w: Workspace) {
    const n = await this.ui.input('Rename Workspace', 'Name', w.name);
    if (n && n !== w.name) await this.store(this.list().map(x => (x === w ? { ...x, name: n } : x)));
  }

  async remove(w: Workspace) {
    if (await this.ui.confirm('Delete Workspace', `Delete "${w.name}"? Sessions are not affected.`, 'Delete', true)) {
      await this.store(this.list().filter(x => x !== w));
    }
  }
}

/** Folder Defaults: values every session in a folder inherits when it leaves them empty. */
@Component({
  selector: 'app-folder-defaults',
  imports: [FormsModule, FolderIcon],
  styles: [`.mh-ic.fd { background: transparent; } .checks.rec { margin-top: 12px; }`],
  template: `
  <div class="modal-backdrop">
    <form class="modal" (ngSubmit)="save()">
      <div class="modal-head"><span class="mh-ic fd"><app-folder-icon [depth]="depth" [env]="d.environment || ''" [size]="26" /></span>
        <span class="mh-t">Folder Defaults · {{ folder || 'All sessions' }}<span class="mh-s">Used by every session in this folder and its sub-folders when its own field is empty</span></span></div>
      <div class="modal-body">
        <p class="hint cl-only">Sessions in this folder and its sub-folders use these values when their own field is empty. The nearest
          folder wins. Handy for a lab where every device uses the same user, device type or jump host.</p>
        <div class="grid">
          <div class="pair">
            <div class="f"><label>Account</label>
              <select name="acct" [ngModel]="d.account || ''" (ngModelChange)="d.account = $event || undefined"
                title="Sessions in the folder that keep the Default account log in with it (before the device type's)">
                <option value="">(not set)</option>
                @for (c of ui.settings()?.credentials ?? []; track c.name) { <option [value]="c.name">{{ c.name }}{{ c.username ? ' · ' + c.username : '' }}</option> }</select></div>
            <div class="f"><label>Username</label><input name="u" [(ngModel)]="d.username" [disabled]="!!d.account" [placeholder]="d.account ? '(from the account)' : '(not set)'" /></div>
          </div>
          <div class="pair">
            <div class="f"><label>Device type</label>
              <select name="dt" [(ngModel)]="d.deviceType"><option value="">(not set)</option>
                @for (t of deviceTypes(); track t) { <option [value]="t">{{ t }}</option> }</select></div>
          </div>
          <div class="pair">
            <div class="f"><label>Jump session</label>
              <select name="j" [(ngModel)]="d.jumpSession"><option value="">(not set)</option>
                @for (p of jumpChoices(); track p) { <option [value]="p">{{ p }}</option> }</select></div>
            <div class="f"><label>Identity file</label><input name="i" [(ngModel)]="d.identityFile" placeholder="(not set) e.g. ~/.ssh/lab_ed25519" /></div>
          </div>
          <div class="pair">
            <div class="f"><label>Keyword set</label>
              <select name="k" [(ngModel)]="d.keywordSet"><option value="">(not set)</option>
                @for (k of keywordSets(); track k) { <option [value]="k">{{ k }}</option> }</select></div>
            <div class="f"><label>Color scheme</label><input name="c" [(ngModel)]="d.colorScheme" placeholder="(not set)" /></div>
          </div>
          <div class="pair">
            <div class="f"><label>Environment</label>
              <select name="env" [ngModel]="d.environment || ''" (ngModelChange)="d.environment = $event">
                <option value="">(not set)</option>
                @for (v of envs(); track v.id) { <option [value]="v.id">{{ v.label }}</option> }
              </select></div>
            <div class="f"><label>Smart card</label>
              <select name="card" [ngModel]="d.smartCard || ''" (ngModelChange)="d.smartCard = $event">
                <option value="">(not set)</option>
                <option value="any">Any key on the inserted cards</option>
              </select></div>
          </div>
          <label class="cl-only">Recording</label>
          <div class="checks rec"><label><input type="checkbox" name="rec" [(ngModel)]="d.record" />
            @if (modern()) { <span class="tx">Record every connection<small>of the sessions in this folder</small></span> } @else { Record every connection of the sessions in this folder }</label></div>
        </div>
      </div>
      <div class="modal-foot">
        <button type="button" [class.danger-link]="modern()" (click)="clear()">Remove Defaults</button>
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Cancel</button>
        <button type="submit" class="primary">Save</button>
      </div>
    </form>
  </div>`,
})
export class FolderDefaults {
  protected ui = inject(Ui);
  readonly folder = this.ui.folderDefaultsFor();
  readonly modern = () => this.ui.settings()?.layout !== 'classic';
  readonly depth = Math.max(0, this.folder.split('/').filter(Boolean).length - 1);
  readonly envs = envList;
  d: FolderDefault = structuredClone((this.ui.settings()?.folderDefaults ?? []).find(x => x.folder === this.folder)
    ?? { folder: this.folder, username: '', deviceType: '', jumpSession: '', identityFile: '', keywordSet: '', colorScheme: '', environment: '' });
  readonly deviceTypes = computed(() => (this.ui.settings()?.deviceTypes ?? []).map(d => d.name));
  readonly keywordSets = computed(() => (this.ui.settings()?.keywordSets ?? []).map(k => k.name));
  readonly jumpChoices = computed(() => {
    const out: string[] = [];
    const walk = (n: SessionNode) => (n.children ?? []).forEach(c => c.isFolder ? walk(c) : c.protocol === 'SSH2' && out.push(c.path));
    const r = this.ui.tree();
    if (r) walk(r);
    return out;
  });

  private async store(keep: boolean) {
    const s = this.ui.settings();
    if (!s) return;
    const rest = (s.folderDefaults ?? []).filter(x => x.folder !== this.folder);
    const empty = !Object.entries(this.d).some(([k, v]) => k !== 'folder' && v);
    await this.ui.saveSettings({ ...s, folderDefaults: keep && !empty ? [...rest, { ...this.d, folder: this.folder }] : rest });
    this.ui.close();
  }

  save() { void this.store(true); }
  clear() { void this.store(false); }
}

