import { Backend, errText } from './backend';
import { JumpUse } from './models';
import { Ui } from './ui.service';

/*
 * A session can be the jump host of other sessions (SSH, RDP and VNC reach a
 * device through it). Moving, renaming or deleting it breaks them, so these
 * ask what to do - always, whatever the settings say about confirmations.
 */

const base = (p: string) => p.slice(p.lastIndexOf('/') + 1);
const show = (u: JumpUse) => u.folder ? `${u.path}  (default jump host of the folder)` : `${u.path}  (${u.protocol || 'SSH2'})`;
const names = (l: string[]) => l.length === 1 ? '"' + base(l[0]) + '"' : l.length + ' sessions';

/** After sessions were moved or renamed: asks whether the sessions that use them as jump host follow. Returns the reverse map (for Undo) when they were updated. */
export async function followJump(ui: Ui, backend: Backend, pairs: { from: string; to: string }[]): Promise<Record<string, string> | null> {
  pairs = pairs.filter(p => p.from !== p.to);
  if (!pairs.length) return null;
  let users: JumpUse[];
  try { users = await backend.jumpUsers(pairs.map(p => p.from), false, []); } catch { return null; }
  if (!users.length) return null;
  const used = pairs.filter(p => users.some(u => u.jump === p.from));
  const what = used.length === 1
    ? `"${used[0].from}" was ${base(used[0].from) !== base(used[0].to) ? 'renamed' : 'moved'} to "${used[0].to}".`
    : `${names(used.map(p => p.from))} that other sessions use as jump host were moved.`;
  const r = await ui.choose('Jump Host Moved',
    `${what}\n\nThese use ${used.length === 1 ? 'it' : 'them'} as their jump host and still point at the old location, so they cannot connect until they are updated:`,
    users.map(show),
    [{ label: users.length === 1 ? 'Update It' : `Update ${users.length} ${users.some(u => u.folder) ? 'Items' : 'Sessions'}`, value: 'update', primary: true }, { label: 'Leave Them', value: 'keep' }]);
  if (r !== 'update') return null;
  const moves: Record<string, string> = {};
  const back: Record<string, string> = {};
  for (const p of used) { moves[p.from] = p.to; back[p.to] = p.from; }
  try {
    const n = await backend.repointJumps(moves);
    ui.toast(`Updated the jump host of ${n} session${n === 1 ? '' : 's'}.`, 'success');
    await ui.refreshTree();
    return back;
  } catch (e) { ui.error(e); return null; }
}

/**
 * Before sessions are deleted: asks what happens to the sessions that use them as jump host.
 * Returns the sessions to delete as well and the deleted jump hosts to clear from folder defaults,
 * or null when the whole delete is cancelled.
 */
export async function askDeleteJump(ui: Ui, backend: Backend, paths: string[]): Promise<{ extra: string[]; clear: string[] } | null> {
  let users: JumpUse[];
  try { users = await backend.jumpUsers(paths, true, paths); } catch { return { extra: [], clear: [] }; }
  if (!users.length) return { extra: [], clear: [] };
  const sess = users.filter(u => !u.folder).map(u => u.path);
  const lead = paths.length === 1 ? `"${paths[0]}" is the jump host of` : 'Some of the sessions you are deleting are the jump host of';
  const note = sess.length
    ? 'Delete them too, or only the jump host? Sessions that stay cannot connect until you give them another jump host.'
    : 'The folder default is cleared.';
  const r = await ui.choose('Jump Host In Use', `${lead} ${users.length === 1 ? 'this' : 'these'}. ${note}`, users.map(show),
    [
      ...(sess.length ? [{ label: sess.length === 1 ? 'Delete It Too' : `Delete These ${sess.length} Too`, value: 'both', danger: true }] : []),
      { label: sess.length ? 'Delete Only the Jump Host' : 'Delete', value: 'only', primary: !sess.length },
    ], true);
  if (r === null) return null;
  return { extra: r === 'both' ? sess : [], clear: users.some(u => u.folder) ? paths : [] };
}

/** Clears deleted jump hosts from the folder defaults that named them. */
export async function clearJumpDefaults(ui: Ui, backend: Backend, paths: string[]) {
  if (!paths.length) return;
  try { await backend.clearJumpDefaults(paths); } catch (e) { ui.toast(errText(e), 'error'); }
}
