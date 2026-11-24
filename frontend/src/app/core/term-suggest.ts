import { HistoryEntry, Snippet, snippetTypes } from './models';

/** Whose history the terminal suggestions use: a device type, '*' every one, '@' this session's commands only. */
export type SuggestSource = string;

export interface SuggestItem { cmd: string; count: number; time: string; library?: string }

/** The suggestions for what is typed on the terminal line: that device type's history (most used, then
 * newest) and its Command Library commands, starting with the typed text (any case); nothing typed: the
 * most used. Never another device type's commands. */
export function termSuggestions(entries: HistoryEntry[], snippets: Snippet[], knownTypes: string[], source: SuggestSource,
  session: string, typed: string, opts: { library: boolean; untyped: boolean }, max = 8): { items: SuggestItem[]; total: number } {
  const q = typed.trim().toLowerCase();
  const src = source.toLowerCase();
  const fits = (e: HistoryEntry) => {
    if (source === '@') return !!session && e.session === session;
    if (source === '*') return true;
    const dt = (e.deviceType ?? '').toLowerCase();
    return dt === src || (opts.untyped && !dt);
  };
  const by = new Map<string, SuggestItem>();
  for (const e of entries) {
    if (!fits(e)) continue;
    const old = by.get(e.cmd);
    if (old) { old.count += e.count; if (e.time > old.time) old.time = e.time; }
    else by.set(e.cmd, { cmd: e.cmd, count: e.count, time: e.time });
  }
  let all = [...by.values()];
  if (opts.library && source !== '@') {
    for (const s of snippets) {
      if (s.text.includes('\n')) continue;
      const types = snippetTypes(s, knownTypes);
      if (source !== '*' && types.length && !types.some(t => t.toLowerCase() === src)) continue;
      if (!by.has(s.text)) all.push({ cmd: s.text, count: 0, time: '', library: s.name });
    }
  }
  if (q) all = all.filter(i => i.cmd.toLowerCase().startsWith(q) && i.cmd.toLowerCase() !== q);
  all.sort((a, b) => (b.count - a.count) || (b.time < a.time ? -1 : b.time > a.time ? 1 : 0) || a.cmd.localeCompare(b.cmd));
  return { items: all.slice(0, max), total: all.length };
}

/** "today", "yesterday" or "Nov 20". */
export function whenText(iso: string, now = new Date()): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(+d)) return '';
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((day(now) - day(d)) / 86400000);
  if (diff <= 0) return 'today';
  if (diff === 1) return 'yesterday';
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}
