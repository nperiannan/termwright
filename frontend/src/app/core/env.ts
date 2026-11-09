import { signal } from '@angular/core';
import { EnvDef, FolderDefault } from './models';

/** The ten environment colours. None of them is a folder level colour (amber, violet, blue, teal,
 *  pink, lime - see folder-icon.ts), so a folder's colour tells its environment apart from its depth. */
export const ENV_COLORS = [
  { c: '#e5484d', n: 'Red' }, { c: '#f97316', n: 'Orange' }, { c: '#facc15', n: 'Yellow' }, { c: '#16a34a', n: 'Green' },
  { c: '#4f46e5', n: 'Indigo' }, { c: '#9f1239', n: 'Crimson' }, { c: '#92400e', n: 'Brown' }, { c: '#1e3a8a', n: 'Navy' },
  { c: '#64748b', n: 'Slate' }, { c: '#d4d4d8', n: 'Silver' },
] as const;

/** The built-in environments (Settings → Environments can rename, recolour or add to them). */
export const DEFAULT_ENVIRONMENTS: EnvDef[] = [
  { id: 'production', label: 'Production', short: 'PROD', color: '#e5484d', protect: true },
  { id: 'staging', label: 'Staging', short: 'STAGE', color: '#f97316' },
  { id: 'test', label: 'Test / Lab', short: 'LAB', color: '#16a34a' },
  { id: 'development', label: 'Development', short: 'DEV', color: '#4f46e5' },
];

/** The environments in use: the settings' list, or the built-in four. Set by the app when settings load. */
export const envList = signal<EnvDef[]>(DEFAULT_ENVIRONMENTS);
export function setEnvironments(list: EnvDef[] | null | undefined) { envList.set(list?.length ? list : DEFAULT_ENVIRONMENTS); }
/** @deprecated the list changes with the settings: read envList() */
export const ENVIRONMENTS = DEFAULT_ENVIRONMENTS;

/** Text colour on an environment colour: dark on light colours (yellow, silver), else white. */
export function envFg(color: string): string {
  const m = /^#?([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(color || '');
  if (!m) return '#fff';
  const [r, g, b] = m.slice(1).map(h => parseInt(h, 16) / 255);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.6 ? '#1b1c1f' : '#fff';
}

/** Folder and environment colours (Settings → General): soft (the default), vivid or steel blue. */
export type FolderScheme = 'soft' | 'vivid' | 'steel';
export const folderScheme = signal<FolderScheme>('soft');

const TONED: Record<'soft' | 'steel', Record<string, string>> = {
  soft: { '#e5484d': '#d2575b', '#f97316': '#d9813f', '#16a34a': '#3f9d62', '#4f46e5': '#6366d6', '#64748b': '#7b8594' },
  steel: { '#e5484d': '#bd5a5f', '#f97316': '#c4824f', '#16a34a': '#539a6d', '#4f46e5': '#6d70c0', '#64748b': '#8a93a1' },
};
/** An environment colour as the scheme shows it: calmer in soft and steel blue (an own colour is mixed with grey). */
export function toned(color: string, scheme: FolderScheme = folderScheme()): string {
  if (scheme === 'vivid') return color;
  const c = color.toLowerCase();
  if (TONED[scheme][c]) return TONED[scheme][c];
  const m = /^#?([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(c);
  if (!m) return color;
  const k = scheme === 'soft' ? 0.15 : 0.3;
  return '#' + m.slice(1).map(h => Math.round(parseInt(h, 16) * (1 - k) + 0x80 * k).toString(16).padStart(2, '0')).join('');
}

/** An environment with the colour to show (toned by the scheme) and its text colour - in soft and
 *  steel blue the tag is tinted or outlined, so its text is the colour itself. */
export function envInfo(id: string | null | undefined): (EnvDef & { fg: string }) | null {
  if (!id) return null;
  const e = envList().find(x => x.id === id);
  if (!e) return null;
  const s = folderScheme();
  const color = toned(e.color, s);
  return { ...e, color, fg: s === 'vivid' ? envFg(color) : color };
}

/** Pastes are confirmed and the tab shows the tag, as for production. */
export function envProtected(id: string | null | undefined) { return !!envInfo(id)?.protect; }

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
