import { Injectable, inject, signal } from '@angular/core';
import { Backend } from './backend';
import { Session, SessionNode } from './models';
import { Ui } from './ui.service';

/** One change in the Session Manager that can be undone. */
interface UndoStep {
  title: string; // "Move core-sw1"
  undo: string[]; // what undoing does, one line each
  run: () => Promise<void>;
}

const parentOf = (p: string) => (p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '');
const nameOf = (p: string) => p.slice(p.lastIndexOf('/') + 1);
const join = (folder: string, name: string) => (folder ? folder + '/' + name : name);
const where = (folder: string) => (folder ? '"' + folder + '"' : 'the top level');

/**
 * Session Manager editing: move (with a confirmation, on by default), copy /
 * cut and paste of sessions and folders, delete - and Undo of the last three
 * of these, each telling first what it will do.
 */
@Injectable({ providedIn: 'root' })
export class SessionOps {
  private backend = inject(Backend);
  private ui = inject(Ui);
  /** Copied or cut sessions and folders, waiting for Paste. */
  readonly clip = signal<{ op: 'copy' | 'cut'; items: { path: string; folder: boolean }[] } | null>(null);
  readonly undoSteps = signal<UndoStep[]>([]);

  private remember(step: UndoStep) {
    this.undoSteps.update(l => [step, ...l].slice(0, 3));
    this.ui.toast(step.title + '.', 'success', 7000, [{ label: 'Undo', run: () => void this.undo() }]);
  }

  /** Undoes the last change, after saying what will happen. */
  async undo() {
    const step = this.undoSteps()[0];
    if (!step) { this.ui.toast('Nothing to undo.', 'info'); return; }
    if (!(await this.ui.confirm('Undo: ' + step.title, step.undo.join('\n'), 'Undo'))) return;
    this.undoSteps.update(l => l.slice(1));
    try { await step.run(); } catch (e) { this.ui.error(e); }
    await this.ui.refreshTree();
  }

  // ---- move ----

  /** Moves sessions and folders into a folder ("" = top level); asks first unless turned off. */
  async move(items: { path: string; folder: boolean }[], to: string) {
    items = items.filter(i => parentOf(i.path) !== to && i.path !== to && !(i.folder && (to + '/').startsWith(i.path + '/')));
    if (!items.length) return;
    const what = items.length === 1 ? (items[0].folder ? 'the folder ' : '') + '"' + nameOf(items[0].path) + '"' : items.length + ' items';
    if (!this.ui.settings()?.noMoveConfirm) {
      const from = [...new Set(items.map(i => parentOf(i.path)))];
      const msg = `Move ${what} from ${from.length === 1 ? where(from[0]) : 'their folders'} to ${where(to)}?` +
        '\n\nTurn this question off in Settings → General → Confirm before moving sessions. Undo is in the Session Manager\'s ⋯ menu.';
      if (!(await this.ui.confirm('Move ' + (items.length === 1 ? (items[0].folder ? 'Folder' : 'Session') : items.length + ' Items'), msg, 'Move'))) return;
    }
    const done: { from: string; to: string; folder: boolean }[] = [];
    try {
      for (const i of items) {
        const np = join(to, nameOf(i.path));
        if (i.folder) await this.backend.renameFolder(i.path, np);
        else { const s = await this.backend.getSession(i.path); s.folder = to; const r = await this.backend.saveSession(s, i.path); done.push({ from: i.path, to: r.path, folder: false }); continue; }
        done.push({ from: i.path, to: np, folder: true });
      }
    } catch (e) { this.ui.error(e); }
    await this.ui.refreshTree();
    if (!done.length) return;
    this.remember({
      title: `Moved ${done.length === 1 ? '"' + nameOf(done[0].from) + '"' : done.length + ' items'} to ${where(to)}`,
      undo: done.map(d => `${d.folder ? 'Folder' : 'Session'} "${nameOf(d.from)}" will be moved from ${where(parentOf(d.to))} back to ${where(parentOf(d.from))}.`),
      run: async () => {
        for (const d of [...done].reverse()) {
          if (d.folder) await this.backend.renameFolder(d.to, d.from);
          else { const s = await this.backend.getSession(d.to); s.folder = parentOf(d.from); await this.backend.saveSession(s, d.to); }
        }
      },
    });
  }

  // ---- copy / cut / paste ----

  copy(items: { path: string; folder: boolean }[]) { this.clip.set({ op: 'copy', items }); this.ui.toast(`Copied ${this.count(items)} - right-click a folder → Paste.`, 'info', 4000); }
  cut(items: { path: string; folder: boolean }[]) { this.clip.set({ op: 'cut', items }); this.ui.toast(`Cut ${this.count(items)} - right-click a folder → Paste to move it there.`, 'info', 4000); }
  private count(items: { path: string; folder: boolean }[]) { return items.length === 1 ? '"' + nameOf(items[0].path) + '"' : items.length + ' items'; }

  /** Pastes into a folder: a cut moves, a copy makes copies (folders with everything in them). */
  async paste(to: string) {
    const c = this.clip();
    if (!c) return;
    if (c.op === 'cut') { this.clip.set(null); await this.move(c.items, to); return; }
    const created: { path: string; folder: boolean }[] = [];
    try {
      for (const i of c.items) {
        if (i.folder) await this.copyFolder(i.path, to, created);
        else created.push({ path: await this.copySession(i.path, to), folder: false });
      }
    } catch (e) { this.ui.error(e); }
    await this.ui.refreshTree();
    if (!created.length) return;
    const top = created.filter(x => !created.some(y => y !== x && y.folder && x.path.startsWith(y.path + '/')));
    this.remember({
      title: `Pasted ${top.length === 1 ? '"' + nameOf(top[0].path) + '"' : top.length + ' items'} into ${where(to)}`,
      undo: top.map(t => `${t.folder ? 'Folder' : 'Session'} "${nameOf(t.path)}" in ${where(parentOf(t.path))} will be deleted${t.folder ? ' with what is in it' : ''}.`),
      run: async () => {
        for (const t of top) { if (t.folder) await this.backend.deleteFolder(t.path); else await this.backend.deleteSession(t.path); }
      },
    });
  }

  /** A copy of a session in folder to, under its own name when that is free there. */
  private async copySession(path: string, to: string): Promise<string> {
    const dup = await this.backend.duplicateSession(path);
    const s = await this.backend.getSession(dup);
    const want = nameOf(path);
    const taken = (n: string) => !!this.ui.findNode(join(to, n));
    s.folder = to;
    if (!taken(want)) s.name = want;
    else if (parentOf(dup) !== to) {
      let k = 2;
      while (taken(`${want} (${k})`)) k++;
      s.name = `${want} (${k})`;
    }
    return (await this.backend.saveSession(s, dup)).path;
  }

  private async copyFolder(path: string, to: string, created: { path: string; folder: boolean }[]) {
    const node = this.ui.findNode(path);
    if (!node) return;
    if ((to + '/').startsWith(path + '/')) throw new Error('A folder cannot be copied into itself.');
    let name = node.name, k = 2;
    while (this.ui.findNode(join(to, name))) name = `${node.name} (${k++})`;
    const dest = join(to, name);
    await this.backend.createFolder(dest);
    created.push({ path: dest, folder: true });
    for (const c of node.children ?? []) {
      if (c.isFolder) await this.copyFolderInto(c, dest, created);
      else created.push({ path: await this.copySession(c.path, dest), folder: false });
    }
  }
  private async copyFolderInto(n: SessionNode, dest: string, created: { path: string; folder: boolean }[]) {
    const d = join(dest, n.name);
    await this.backend.createFolder(d);
    created.push({ path: d, folder: true });
    for (const c of n.children ?? []) {
      if (c.isFolder) await this.copyFolderInto(c, d, created);
      else created.push({ path: await this.copySession(c.path, d), folder: false });
    }
  }

  // ---- delete (kept for Undo) ----

  /** Deletes sessions and folders; Undo re-creates them (saved passwords are not kept). */
  async remove(items: { path: string; folder: boolean }[]) {
    const keep: { sessions: Session[]; folders: string[] } = { sessions: [], folders: [] };
    const walk = (n: SessionNode) => {
      keep.folders.push(n.path);
      for (const c of n.children ?? []) if (c.isFolder) walk(c); else keep.sessions.push({ path: c.path } as Session);
    };
    for (const i of items) {
      if (i.folder) { const n = this.ui.findNode(i.path); if (n) walk(n); }
      else keep.sessions.push({ path: i.path } as Session);
    }
    try {
      keep.sessions = await Promise.all(keep.sessions.map(s => this.backend.getSession(s.path)));
      for (const i of items) { if (i.folder) await this.backend.deleteFolder(i.path); else await this.backend.deleteSession(i.path); }
    } catch (e) { this.ui.error(e); }
    await this.ui.refreshTree();
    this.remember({
      title: `Deleted ${this.count(items)}`,
      undo: [
        ...items.map(i => `${i.folder ? 'Folder' : 'Session'} "${nameOf(i.path)}" will be re-created in ${where(parentOf(i.path))}` +
          (i.folder ? ` with its ${keep.sessions.filter(s => s.path.startsWith(i.path + '/')).length} sessions.` : '.')),
        'Saved passwords were removed with the sessions: they are asked again when connecting.',
      ],
      run: async () => {
        for (const f of keep.folders) await this.backend.createFolder(f).catch(() => {});
        for (const s of keep.sessions) await this.backend.saveSession({ ...s }, '');
      },
    });
  }

}
