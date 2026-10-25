import { Component, computed, inject, signal } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { SessionNode } from '../core/models';
import { Tabs } from '../core/tabs.service';
import { MenuItem, Ui } from '../core/ui.service';
import { Icon } from './icon';
import { SessionIcon, SessionKind } from './session-icon';

const EXPANDED_KEY = 'termwright.expanded';

/** Session Manager: folder tree with search, context menu and drag & drop. */
@Component({
  selector: 'app-session-tree',
  imports: [NgTemplateOutlet, FormsModule, Icon, SessionIcon],
  template: `
    <div class="sm-head">
      <span class="title" title="Session Manager">Sessions</span>
      <button class="icon-btn" title="New session" (click)="ui.newSession(ui.currentFolder())"><app-icon name="plus" /></button>
      <button class="icon-btn" title="New folder" (click)="newFolder(ui.currentFolder())"><app-icon name="folder-plus" /></button>
      <button class="icon-btn" title="Refresh" (click)="ui.refreshTree()"><app-icon name="refresh" /></button>
      <button class="icon-btn" [title]="ui.settings()?.sessionManagerRight ? 'Move the Session Manager to the left' : 'Move the Session Manager to the right'"
        (click)="ui.actionHandler('toggle-session-manager-side')"><app-icon [name]="ui.settings()?.sessionManagerRight ? 'dock-left' : 'dock-right'" /></button>
      <button class="icon-btn" title="Hide the Session Manager (View → Session Manager or the toolbar shows it again)"
        (click)="ui.actionHandler('toggle-session-manager')"><app-icon name="hide-panel" /></button>
    </div>
    <div class="sm-search">
      <input type="search" placeholder="Filter sessions" [(ngModel)]="filterText" (ngModelChange)="filter.set($event)" />
    </div>
    <div class="sm-body" (contextmenu)="rootMenu($event)"
         (dragover)="allowDrop($event)" (drop)="dropOn($event, '')">
      @if (filter()) {
        @for (n of matches(); track n.path) {
          <div class="row session" [class.sel]="ui.selectedPath() === n.path"
               (click)="select(n)" (dblclick)="connect(n)" (contextmenu)="nodeMenu($event, n)" [title]="tooltip(n)">
            <app-session-icon [kind]="protoIcon(n)" [via]="!!n.hopTarget" />
            <span class="name">{{ n.name }}</span>
            <span class="host">{{ n.path }}</span>
          </div>
        } @empty {
          <div class="empty">No matching sessions</div>
        }
      } @else {
        @if (ui.tree(); as root) {
          @for (n of root.children ?? []; track n.path) {
            <ng-container [ngTemplateOutlet]="nodeTpl" [ngTemplateOutletContext]="{ $implicit: n, depth: 0 }" />
          } @empty {
            <div class="empty">
              No sessions yet.<br />
              <a (click)="ui.newSession('')">Create a session</a> or
              <a (click)="ui.open('import')">import from SecureCRT</a>.
            </div>
          }
        }
      }
    </div>

    <ng-template #nodeTpl let-n let-depth="depth">
      <div class="row" [class.folder]="n.isFolder" [class.session]="!n.isFolder"
           [class.sel]="ui.selectedPath() === n.path" [class.drop]="dropTarget() === n.path"
           [style.padding-left.px]="8 + depth * 14" [title]="tooltip(n)"
           draggable="true" (dragstart)="dragStart($event, n)" (dragend)="dropTarget.set('')"
           (dragover)="allowDrop($event, n.isFolder ? n.path : parentOf(n.path))" (dragleave)="dropTarget.set('')"
           (drop)="dropOn($event, n.isFolder ? n.path : parentOf(n.path))"
           (click)="select(n)" (dblclick)="n.isFolder ? toggle(n.path) : connect(n)"
           (contextmenu)="nodeMenu($event, n)">
        @if (n.isFolder) {
          <span class="twisty" [class.open]="expanded().has(n.path)" (click)="toggle(n.path); $event.stopPropagation()"><svg viewBox="0 0 12 12" aria-hidden="true"><path d="M4 2.5 L8 6 L4 9.5" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg></span>
          <app-session-icon [kind]="expanded().has(n.path) ? 'folder-open' : 'folder'" />
        } @else {
          <span class="twisty"></span>
          <app-session-icon [kind]="protoIcon(n)" [via]="!!n.hopTarget" />
        }
        <span class="name">{{ n.name }}</span>
      </div>
      @if (n.isFolder && expanded().has(n.path)) {
        @for (c of n.children ?? []; track c.path) {
          <ng-container [ngTemplateOutlet]="nodeTpl" [ngTemplateOutletContext]="{ $implicit: c, depth: depth + 1 }" />
        }
      }
    </ng-template>
  `,
  styleUrl: './session-tree.scss',
})
export class SessionTree {
  protected ui = inject(Ui);
  private tabs = inject(Tabs);
  private backend = inject(Backend);

  filterText = '';
  readonly filter = signal('');
  readonly dropTarget = signal('');
  readonly expanded = signal<Set<string>>(this.loadExpanded());
  private dragged: SessionNode | null = null;

  readonly matches = computed(() => {
    const q = this.filter().toLowerCase().trim();
    const out: SessionNode[] = [];
    const walk = (n: SessionNode) => {
      for (const c of n.children ?? []) {
        if (c.isFolder) walk(c);
        else if ([c.name, c.hostname, c.username, c.path].some(v => v?.toLowerCase().includes(q))) out.push(c);
      }
    };
    const root = this.ui.tree();
    if (root) walk(root);
    return out;
  });

  private loadExpanded(): Set<string> {
    try { return new Set(JSON.parse(localStorage.getItem(EXPANDED_KEY) ?? '[]')); } catch { return new Set(); }
  }

  toggle(path: string) {
    const s = new Set(this.expanded());
    if (s.has(path)) s.delete(path); else s.add(path);
    this.expanded.set(s);
    try { localStorage.setItem(EXPANDED_KEY, JSON.stringify([...s])); } catch { /* storage unavailable */ }
  }

  select(n: SessionNode) {
    this.ui.selectedPath.set(n.path);
    this.ui.selectedIsFolder.set(n.isFolder);
  }

  parentOf(p: string): string { return p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : ''; }

  connect(n: SessionNode) { void this.tabs.openSession(n.path); }

  protoIcon(n: SessionNode): SessionKind {
    switch (n.protocol) {
      case 'Serial': return 'serial';
      case 'Local Shell': return 'shell';
      case 'Telnet': return 'telnet';
      case 'Raw': return 'raw';
      default: return n.openAs === 'sftp' ? 'sftp' : 'ssh';
    }
  }

  tooltip(n: SessionNode): string {
    if (n.isFolder) return n.path;
    const who = n.username ? n.username + '@' : '';
    const port = n.port ? ':' + n.port : '';
    const line = `${n.protocol ?? ''} ${who}${n.hostname ?? ''}${port}`.trim();
    return n.description ? line + '\n' + n.description : line;
  }

  async newFolder(parent: string) {
    const name = await this.ui.input('New Folder', 'Folder name');
    if (!name) return;
    const path = parent ? parent + '/' + name : name;
    try {
      await this.backend.createFolder(path);
      if (parent) this.expanded.update(s => new Set(s).add(parent));
      await this.ui.refreshTree();
    } catch (e) { this.ui.error(e); }
  }

  rootMenu(ev: MouseEvent) {
    this.ui.showMenu(ev, [
      { label: 'New Session...', action: () => this.ui.newSession('') },
      { label: 'New Folder...', action: () => this.newFolder('') },
      { separator: true },
      { label: 'Import Sessions...', action: () => this.ui.open('import') },
      { label: 'Export Sessions...', action: () => this.ui.open('export') },
      { label: 'Refresh', action: () => this.ui.refreshTree() },
    ]);
  }

  nodeMenu(ev: MouseEvent, n: SessionNode) {
    this.select(n);
    const items: MenuItem[] = n.isFolder
      ? [
          { label: 'Connect All in Folder', action: () => this.connectAll(n) },
          { separator: true },
          { label: 'New Session...', action: () => this.ui.newSession(n.path) },
          { label: 'New Folder...', action: () => this.newFolder(n.path) },
          { label: 'Rename...', action: () => this.renameFolder(n) },
          { label: 'Folder Defaults...', action: () => { this.ui.folderDefaultsFor.set(n.path); this.ui.open('folderDefaults'); } },
          { label: 'Export Folder...', action: () => this.ui.open('export') },
          { separator: true },
          { label: 'Delete Folder', action: () => this.deleteFolder(n) },
        ]
      : [
          ...(n.protocol === 'SSH2' ? [
            { label: 'Connect Terminal', action: () => void this.tabs.openSession(n.path, 'terminal') },
            ...(n.hopTarget ? [
              { label: `Connect SFTP Session to ${n.hostname}`, action: () => void this.tabs.openSession(n.path, 'sftp', 'jump') },
              { label: `Connect SFTP Session to ${n.hopTarget} (through ${n.hostname})`, action: () => void this.tabs.openSession(n.path, 'sftp', 'target') },
            ] : [{ label: 'Connect SFTP Session', action: () => void this.tabs.openSession(n.path, 'sftp') }]),
          ] : [{ label: 'Connect', action: () => this.connect(n) }]),
          { separator: true },
          { label: 'Properties...', action: () => this.ui.editSession(n.path) },
          { label: 'Duplicate', action: () => this.duplicate(n) },
          { label: 'Rename...', action: () => this.renameSession(n) },
          { label: 'Forget Saved Password', action: () => this.backend.forgetPassword(n.path).then(() => this.ui.toast('Password removed')) },
          { separator: true },
          { label: 'Delete', action: () => this.deleteSession(n) },
        ];
    this.ui.showMenu(ev, items);
  }

  private connectAll(n: SessionNode) {
    const walk = (x: SessionNode) => (x.children ?? []).forEach(c => (c.isFolder ? walk(c) : void this.tabs.openSession(c.path)));
    walk(n);
  }

  private async renameFolder(n: SessionNode) {
    const name = await this.ui.input('Rename Folder', 'Folder name', n.name);
    if (!name || name === n.name) return;
    const parent = n.path.includes('/') ? n.path.slice(0, n.path.lastIndexOf('/')) : '';
    try {
      await this.backend.renameFolder(n.path, parent ? parent + '/' + name : name);
      await this.ui.refreshTree();
    } catch (e) { this.ui.error(e); }
  }

  private async deleteFolder(n: SessionNode) {
    const count = this.countSessions(n);
    if (!(await this.ui.confirm('Delete Folder', `Delete folder "${n.path}" and ${count} session(s) in it?`, 'Delete', true))) return;
    try { await this.backend.deleteFolder(n.path); await this.ui.refreshTree(); } catch (e) { this.ui.error(e); }
  }

  private countSessions(n: SessionNode): number {
    return (n.children ?? []).reduce((a, c) => a + (c.isFolder ? this.countSessions(c) : 1), 0);
  }

  private async duplicate(n: SessionNode) {
    try {
      const p = await this.backend.duplicateSession(n.path);
      await this.ui.refreshTree();
      this.ui.selectedPath.set(p);
    } catch (e) { this.ui.error(e); }
  }

  private async renameSession(n: SessionNode) {
    const name = await this.ui.input('Rename Session', 'Session name', n.name);
    if (!name || name === n.name) return;
    try {
      const s = await this.backend.getSession(n.path);
      s.name = name;
      await this.backend.saveSession(s, n.path);
      await this.ui.refreshTree();
    } catch (e) { this.ui.error(e); }
  }

  private async deleteSession(n: SessionNode) {
    if (!(await this.ui.confirm('Delete Session', `Delete session "${n.path}"?`, 'Delete', true))) return;
    try { await this.backend.deleteSession(n.path); await this.ui.refreshTree(); } catch (e) { this.ui.error(e); }
  }

  dragStart(ev: DragEvent, n: SessionNode) {
    this.dragged = n;
    ev.dataTransfer?.setData('text/plain', n.path);
    if (ev.dataTransfer) ev.dataTransfer.effectAllowed = 'move';
  }

  allowDrop(ev: DragEvent, path = '') {
    if (!this.dragged) return;
    ev.preventDefault();
    ev.stopPropagation();
    this.dropTarget.set(path);
  }

  async dropOn(ev: DragEvent, folder: string) {
    ev.preventDefault();
    ev.stopPropagation();
    this.dropTarget.set('');
    const n = this.dragged;
    this.dragged = null;
    if (!n) return;
    const parent = n.path.includes('/') ? n.path.slice(0, n.path.lastIndexOf('/')) : '';
    if (parent === folder || n.path === folder) return;
    try {
      if (n.isFolder) {
        await this.backend.renameFolder(n.path, folder ? folder + '/' + n.name : n.name);
      } else {
        const s = await this.backend.getSession(n.path);
        s.folder = folder;
        await this.backend.saveSession(s, n.path);
      }
      if (folder) this.expanded.update(s => new Set(s).add(folder));
      await this.ui.refreshTree();
    } catch (e) { this.ui.error(e); }
  }
}
