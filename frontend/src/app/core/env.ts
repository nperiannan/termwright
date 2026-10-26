import { FolderDefault } from './models';

/** Environments a session or folder can be marked with. */
export const ENVIRONMENTS = [
  { id: 'production', label: 'Production', short: 'PROD', color: '#e5484d' },
  { id: 'staging', label: 'Staging', short: 'STAGE', color: '#f59e0b' },
  { id: 'test', label: 'Test / Lab', short: 'LAB', color: '#22c55e' },
  { id: 'development', label: 'Development', short: 'DEV', color: '#3b82f6' },
] as const;

export function envInfo(id: string | null | undefined) {
  return ENVIRONMENTS.find(e => e.id === id) ?? null;
}

/** The environment of a session: its own, or the nearest folder's default. */
export function envOf(path: string, own: string | null | undefined, defaults: FolderDefault[] | null | undefined): string {
  if (own) return own;
  const byFolder = new Map((defaults ?? []).filter(d => d.environment).map(d => [d.folder.replace(/^\/+|\/+$/g, ''), d.environment as string]));
  let f = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '';
  for (;;) {
    const e = byFolder.get(f);
    if (e) return e;
    if (!f) return '';
    f = f.includes('/') ? f.slice(0, f.lastIndexOf('/')) : '';
  }
}

/** "3 hours ago" style text for an RFC 3339 time. */
export function ago(t: string | undefined): string {
  if (!t) return '';
  const s = (Date.now() - new Date(t).getTime()) / 1000;
  if (!isFinite(s)) return '';
  if (s < 60) return 'just now';
  const f = (n: number, u: string) => `${n} ${u}${n === 1 ? '' : 's'} ago`;
  if (s < 3600) return f(Math.floor(s / 60), 'minute');
  if (s < 86400) return f(Math.floor(s / 3600), 'hour');
  if (s < 86400 * 30) return f(Math.floor(s / 86400), 'day');
  return new Date(t).toLocaleDateString();
}
