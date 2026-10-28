import { Backend } from './backend';
import { SessionNode, defaultPort, findDeviceType } from './models';
import { Tabs } from './tabs.service';
import { Ui } from './ui.service';

const VERBS = ['ssh', 'ssh2', 'telnet', 'serial', 'raw', 'rdp', 'vnc'];

/** True when the text reads like something to connect to: "ssh user@host",
 *  "telnet host 2001", "serial COM3 115200", "voss 10.1.1.1", an address,
 *  "user@host" or "host:port". */
export function looksLikeConnect(text: string, ui: Ui): boolean {
  const t = text.trim();
  if (!t) return false;
  const words = t.split(/\s+/);
  if (VERBS.includes(words[0].toLowerCase()) && words.length > 1) return true;
  const hosty = (w: string) => /^(\S+@)?(\d{1,3}(\.\d{1,3}){3}|\[[0-9a-f:]+\]|[a-z0-9-]+(\.[a-z0-9-]+)+)(:\d+)?$/i.test(w) || /^\S+@\S+$/.test(w);
  if (words.length === 1) return hosty(words[0]);
  // a device type name and an address: "voss 10.1.1.1"
  return words.some(hosty) && words.some(w => !!findDeviceType(ui.settings()?.deviceTypes, w));
}

/** The saved session whose path or name is the text. */
export function findSessionByName(ui: Ui, q: string): string | null {
  const ql = q.toLowerCase();
  let found: string | null = null;
  const walk = (n: SessionNode) => (n.children ?? []).forEach(c => {
    if (found) return;
    if (c.isFolder) walk(c);
    else if (c.path.toLowerCase() === ql || c.name.toLowerCase() === ql) found = c.path;
  });
  const root = ui.tree();
  if (root) walk(root);
  return found;
}

/** What a connect line asks for (see connectString); null when it names no host or port. */
export interface ConnectParts { protocol: string; host: string; port: number; user: string; deviceType: string; comPort: string; baud: number; explicitPort: boolean; }

export function parseConnectLine(text: string, ui: Ui): ConnectParts | null {
  const words = text.trim().split(/\s+/).filter(Boolean);
  if (!words.length) return null;
  const cmd = words[0].toLowerCase();
  const explicit = VERBS.includes(cmd);
  let args = explicit ? words.slice(1) : words;
  const dev = args.map(w => findDeviceType(ui.settings()?.deviceTypes, w)).find(Boolean);
  if (dev) args = args.filter(w => !findDeviceType([dev], w));
  const proto = cmd === 'telnet' ? 'Telnet' : cmd === 'serial' ? 'Serial' : cmd === 'raw' ? 'Raw' : cmd === 'rdp' ? 'RDP' : cmd === 'vnc' ? 'VNC' : !explicit && dev?.protocol ? dev.protocol : 'SSH2';
  const out: ConnectParts = { protocol: proto, host: '', port: (dev?.port && !explicit ? dev.port : defaultPort(proto)) || 22, user: dev?.username ?? '', deviceType: dev?.name ?? '', comPort: '', baud: 9600, explicitPort: false };
  const pos: string[] = [];
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '-p' && args[i + 1]) { out.port = Number(args[++i]); out.explicitPort = true; }
    else if (args[i] === '-l' && args[i + 1]) out.user = args[++i];
    else pos.push(args[i]);
  }
  if (proto === 'Serial') {
    out.comPort = pos[0] ?? '';
    out.baud = Number(pos[1]) || 9600;
    return out;
  }
  let host = pos[0] ?? '';
  const at = host.lastIndexOf('@');
  if (at > 0) { out.user = host.slice(0, at); host = host.slice(at + 1); }
  const m = /^([^:\[\]]+):(\d+)$/.exec(host) ?? /^\[(.+)\]:(\d+)$/.exec(host);
  if (m) { host = m[1]; out.port = Number(m[2]); out.explicitPort = true; }
  if (pos[1] && /^\d+$/.test(pos[1])) { out.port = Number(pos[1]); out.explicitPort = true; }
  out.host = host;
  return out;
}

/** Connects from one line of text: "ssh [-p port] [-l user] user@host[:port]",
 *  "telnet host [port]", "serial PORT [baud]", "raw host port",
 *  "rdp [user@]host[:port]", "vnc host[:port]", a saved session name, or a bare
 *  host (SSH2). A device type name anywhere in the line ("voss 10.1.1.1")
 *  sets the device type, its user name and protocol. Returns false (after
 *  showing the error) when the line could not be used. */
export async function connectString(text: string, ui: Ui, tabs: Tabs, backend: Backend): Promise<boolean> {
  text = text.trim();
  if (!text) return false;
  const words = text.split(/\s+/);
  const cmd = words[0].toLowerCase();
  const explicit = VERBS.includes(cmd);
  const saved = findSessionByName(ui, text);
  if (saved && !explicit) {
    await tabs.openSession(saved);
    return true;
  }
  try {
    const s = await backend.newSessionDefaults();
    Object.assign(s, { name: '', logonActions: [], triggers: [], portForwards: [], description: [] });
    let args = explicit ? words.slice(1) : words;
    const dev = args.map(w => findDeviceType(ui.settings()?.deviceTypes, w)).find(Boolean);
    if (dev) {
      args = args.filter(w => !findDeviceType([dev], w));
      s.deviceType = dev.name;
      if (dev.username) s.username = dev.username;
    }
    const proto = cmd === 'telnet' ? 'Telnet' : cmd === 'serial' ? 'Serial' : cmd === 'raw' ? 'Raw' : cmd === 'rdp' ? 'RDP' : cmd === 'vnc' ? 'VNC' : !explicit && dev?.protocol ? dev.protocol : 'SSH2';
    s.protocol = proto;
    s.port = defaultPort(proto) || 22;
    if (dev?.port && !explicit) s.port = dev.port;
    const pos: string[] = [];
    for (let i = 0; i < args.length; i++) {
      if (args[i] === '-p' && args[i + 1]) s.port = Number(args[++i]);
      else if (args[i] === '-l' && args[i + 1]) s.username = args[++i];
      else pos.push(args[i]);
    }
    if (proto === 'Serial') {
      if (!pos[0]) throw new Error('usage: serial PORT [baud]');
      s.comPort = pos[0];
      s.baudRate = Number(pos[1]) || 9600;
    } else {
      let host = pos[0] ?? '';
      const at = host.lastIndexOf('@');
      if (at > 0) { s.username = host.slice(0, at); host = host.slice(at + 1); }
      const m = /^([^:\[\]]+):(\d+)$/.exec(host) ?? /^\[(.+)\]:(\d+)$/.exec(host);
      if (m) { host = m[1]; s.port = Number(m[2]); }
      if (pos[1] && /^\d+$/.test(pos[1])) s.port = Number(pos[1]);
      if (!host) throw new Error('no host given');
      s.hostname = host;
    }
    await tabs.openAdHoc(s);
    return true;
  } catch (e) {
    ui.error(e);
    return false;
  }
}
