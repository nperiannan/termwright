import { ServerCommand } from './models';

/**
 * Commands a device runs to fetch a file from (or send one to) Termwright's
 * file servers. Built-in ones for common systems; the user can change them or
 * add their own per device type (saved in the File Servers settings).
 *
 * In a template: {url} the whole address of the file, {ip} {port} {file} (the
 * path in the folder) {base} (its name without folders) {name} (the name to
 * save as, for uploads) {user} {password}.
 */
export type Proto = 'tftp' | 'ftp' | 'http';

export const ANY = 'Any device (URL only)';

const BUILTIN: Record<string, { get: Partial<Record<Proto, string>>; put: Partial<Record<Proto, string>> }> = {
  [ANY]: { get: { tftp: '{url}', ftp: '{url}', http: '{url}' }, put: { tftp: '{url}', ftp: '{url}' } },
  'EXOS': {
    // a software image (.xos) with download image, other files with tftp get
    get: { tftp: 'tftp get {ip} vr "VR-Mgmt" {file} {base}' },
    put: { tftp: 'upload configuration {ip} {name} vr "VR-Mgmt"' },
  },
  'Cisco IOS': {
    get: { tftp: 'copy {url} flash:{base}', ftp: 'copy {url} flash:{base}', http: 'copy {url} flash:{base}' },
    put: { tftp: 'copy running-config {url}', ftp: 'copy running-config {url}' },
  },
  'Cisco NX-OS': {
    get: { tftp: 'copy {url} bootflash:{base} vrf management', ftp: 'copy {url} bootflash:{base} vrf management', http: 'copy {url} bootflash:{base} vrf management' },
    put: { tftp: 'copy running-config {url} vrf management', ftp: 'copy running-config {url} vrf management' },
  },
  'Arista EOS': {
    get: { tftp: 'copy {url} flash:{base}', ftp: 'copy {url} flash:{base}', http: 'copy {url} flash:{base}' },
    put: { tftp: 'copy running-config {url}', ftp: 'copy running-config {url}' },
  },
  'Juniper Junos': {
    get: { ftp: 'file copy {url} /var/tmp/{base}', http: 'file copy {url} /var/tmp/{base}' },
    put: { ftp: 'file copy /config/juniper.conf.gz {url}' },
  },
  'Linux': {
    get: { tftp: 'curl -o {base} {url}', ftp: 'curl -o {base} {url}', http: 'curl -o {base} {url}' },
    put: { tftp: 'curl -T {name} {url}', ftp: 'curl -T {name} {url}' },
  },
};

export const BUILTIN_TYPES = Object.keys(BUILTIN);

/** The template for a device type, protocol and direction: the user's own first. */
export function template(own: ServerCommand[], deviceType: string, proto: Proto, put: boolean, file = ''): { text: string; own: boolean } | null {
  const o = own.find(c => same(c.deviceType, deviceType) && c.protocol === proto && !!c.put === put);
  if (o) return { text: o.text, own: true };
  const key = BUILTIN_TYPES.find(k => same(k, deviceType));
  if (!key) return null;
  if (key === 'EXOS' && proto === 'tftp' && !put && /\.xos$/i.test(file)) return { text: 'download image {ip} {file} vr "VR-Mgmt"', own: false };
  const t = (put ? BUILTIN[key].put : BUILTIN[key].get)[proto];
  return t ? { text: t, own: false } : null;
}

/** The device types with any command (built in or the user's), the built-in "any device" first. */
export function deviceTypes(own: ServerCommand[], known: string[]): string[] {
  const out = [...BUILTIN_TYPES];
  for (const t of [...known, ...own.map(c => c.deviceType)]) if (t && !out.some(x => same(x, t))) out.push(t);
  return out;
}

export interface Fill { proto: Proto; ip: string; port: number; file: string; name: string; user: string; password: string; }

const DEFAULT_PORT: Record<Proto, number> = { tftp: 69, ftp: 21, http: 80 };

export function url(f: Fill, put: boolean) {
  const host = f.ip + (f.port && f.port !== DEFAULT_PORT[f.proto] ? ':' + f.port : '');
  const path = (put ? f.name : f.file).split('/').map(encodeURIComponent).join('/');
  const auth = f.proto === 'ftp' ? `${encodeURIComponent(f.user)}:${encodeURIComponent(f.password)}@` : '';
  return `${f.proto}://${auth}${host}/${path}`;
}

export function fill(text: string, f: Fill, put: boolean) {
  const base = f.file.split('/').pop() ?? f.file;
  const v: Record<string, string> = { url: url(f, put), ip: f.ip, port: String(f.port), file: f.file, base, name: f.name, user: f.user, password: f.password };
  return text.replace(/\{(url|ip|port|file|base|name|user|password)\}/g, (_, k) => v[k]);
}

/** A template that cannot name the port works only on the standard one. */
export function needsStandardPort(text: string, f: Fill) {
  return f.port !== DEFAULT_PORT[f.proto] && !/\{(url|port)\}/.test(text);
}

function same(a: string, b: string) { return a.trim().toLowerCase() === b.trim().toLowerCase(); }
