import { Component, computed, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { FolderDefault, SessionNode, Workspace } from '../core/models';
import { Layout, Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { ENVIRONMENTS } from '../core/env';

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
  imports: [FormsModule],
  template: `
  <div class="modal-backdrop">
    <div class="modal">
      <div class="modal-head">Workspaces</div>
      <div class="modal-body">
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
      </div>
      <div class="modal-foot">
        <button type="button" (click)="saveCurrent()">Save Current…</button>
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Close</button>
      </div>
    </div>
  </div>`,
  styles: [`td .hint { font-size: calc(11px * var(--ui-scale, 1)); } .nowrap { white-space: nowrap; text-align: right; } td { padding: 6px 4px; }`],
})
export class Workspaces {
  protected ui = inject(Ui);
  private tabs = inject(Tabs);
  readonly list = computed(() => this.ui.settings()?.workspaces ?? []);

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
  imports: [FormsModule],
  template: `
  <div class="modal-backdrop">
    <form class="modal" (ngSubmit)="save()">
      <div class="modal-head">Folder Defaults - {{ folder || 'All sessions' }}</div>
      <div class="modal-body">
        <p class="hint">Sessions in this folder and its sub-folders use these values when their own field is empty. The nearest
          folder wins. Handy for a lab where every device uses the same user, device type or jump host.</p>
        <div class="grid">
          <label>Username</label><input name="u" [(ngModel)]="d.username" placeholder="(not set)" />
          <label>Device type</label>
          <select name="dt" [(ngModel)]="d.deviceType"><option value="">(not set)</option>
            @for (t of deviceTypes(); track t) { <option [value]="t">{{ t }}</option> }</select>
          <label>Jump session</label>
          <select name="j" [(ngModel)]="d.jumpSession"><option value="">(not set)</option>
            @for (p of jumpChoices(); track p) { <option [value]="p">{{ p }}</option> }</select>
          <label>Identity file</label><input name="i" [(ngModel)]="d.identityFile" placeholder="(not set) e.g. ~/.ssh/lab_ed25519" />
          <label>Keyword set</label>
          <select name="k" [(ngModel)]="d.keywordSet"><option value="">(not set)</option>
            @for (k of keywordSets(); track k) { <option [value]="k">{{ k }}</option> }</select>
          <label>Color scheme</label><input name="c" [(ngModel)]="d.colorScheme" placeholder="(not set)" />
          <label>Environment</label>
          <select name="env" [(ngModel)]="d.environment">
            <option value="">(not set)</option>
            @for (v of envs; track v.id) { <option [value]="v.id">{{ v.label }}</option> }
          </select>
          <label>Smart card</label>
          <select name="card" [(ngModel)]="d.smartCard">
            <option value="">(not set)</option>
            <option value="any">Any key on the inserted cards</option>
          </select>
          <label>Recording</label>
          <label class="inline"><input type="checkbox" name="rec" [(ngModel)]="d.record" /> Record every connection of the sessions in this folder</label>
        </div>
      </div>
      <div class="modal-foot">
        <button type="button" (click)="clear()">Remove Defaults</button>
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
  readonly envs = ENVIRONMENTS;
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

