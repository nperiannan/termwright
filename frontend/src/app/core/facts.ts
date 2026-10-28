import { ago } from './env';

const ORDER = ['Model', 'Version', 'Serial', 'MAC', 'Uptime'];

/** A session's facts as [name, value] in a fixed order (Model, Version,
 *  Serial, MAC, Uptime, then the others by name); "_at" left out. */
export function factList(f: Record<string, string> | null | undefined): [string, string][] {
  if (!f) return [];
  const keys = Object.keys(f).filter(k => k !== '_at' && f[k]);
  keys.sort((a, b) => {
    const i = ORDER.indexOf(a), j = ORDER.indexOf(b);
    return (i < 0 ? 99 : i) - (j < 0 ? 99 : j) || a.localeCompare(b);
  });
  return keys.map(k => [k, f[k]]);
}

/** "read 3 minutes ago" for the facts, or ''. */
export function factsAge(f: Record<string, string> | null | undefined): string {
  return f?.['_at'] ? 'read ' + ago(f['_at']) : '';
}
