/** Name After Devices: a session named by its address gets the device's own name. */

const IPV4 = /^\d{1,3}(\.\d{1,3}){3}$/;
const IPV6 = /^[0-9a-f]*:[0-9a-f:.]+$/i;

/** A session name that is just its address (or any IP address). */
export function isAddressName(name: string | undefined, host: string | undefined): boolean {
  const n = (name ?? '').trim();
  if (!n) return false;
  return n.toLowerCase() === (host ?? '').trim().toLowerCase() || IPV4.test(n) || IPV6.test(n);
}

export type NameFormat = '' | 'model' | 'octet';
export const NAME_FORMATS: { v: NameFormat; label: string }[] = [
  { v: '', label: 'Hostname' }, { v: 'model', label: 'Hostname (model)' }, { v: 'octet', label: 'Hostname · last octet' },
];

/** The new name: hostname, hostname (model) or hostname · the address's last part. */
export function deviceName(hostname: string, model: string | undefined, host: string | undefined, fmt: NameFormat | undefined): string {
  const h = (hostname ?? '').trim().replace(/[\\/]/g, '-');
  if (!h) return '';
  if (fmt === 'model' && model?.trim()) return `${h} (${model.trim()})`;
  if (fmt === 'octet' && host) { const last = host.trim().split(/[.:]/).filter(Boolean).pop(); if (last) return `${h} · ${last}`; }
  return h;
}

/** Sessions told to keep their address name (per computer). */
export function keptNames(): string[] { try { return JSON.parse(localStorage.getItem('termwright.keepNames') || '[]'); } catch { return []; } }
export function keepName(path: string) { try { localStorage.setItem('termwright.keepNames', JSON.stringify([...keptNames().filter(p => p !== path), path].slice(-500))); } catch { /* storage off */ } }
