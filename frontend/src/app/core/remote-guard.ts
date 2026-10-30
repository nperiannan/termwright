import { SessionNode } from './models';

/**
 * RDP and VNC to the same machine are two views of one desktop (and one user's
 * home folder): having both open at once makes them fight over the session.
 * These helpers tell which open remote-desktop tabs are on the same machine
 * as a session that is about to open.
 */

export type RemoteKind = 'rdp' | 'vnc';

/** What is needed of a session to tell which machine it reaches. */
export interface RemoteTarget { path?: string; hostname: string; jumpSession?: string; port?: number }

const LOOPBACK = /^(127\.\d+\.\d+\.\d+|localhost|::1|\[::1\])$/i;

/** Finds a session of the tree by path. */
export function nodeAt(root: SessionNode | null | undefined, path: string): SessionNode | undefined {
  if (!root || !path) return undefined;
  let hit: SessionNode | undefined;
  const walk = (n: SessionNode) => {
    for (const c of n.children ?? []) {
      if (c.path === path) hit = c;
      if (c.isFolder && !hit) walk(c);
    }
  };
  walk(root);
  return hit;
}

/**
 * The machine a remote-desktop session reaches. Sessions made by "Set Up
 * Remote Desktop" connect to 127.0.0.1 through the SSH session, so their
 * machine is the SSH session's host, not 127.0.0.1.
 */
export function machineOf(t: RemoteTarget, root: SessionNode | null | undefined): string {
  const host = (t.hostname || '').trim().toLowerCase();
  if (t.jumpSession && LOOPBACK.test(host)) {
    const jump = nodeAt(root, t.jumpSession);
    if (jump?.hostname) return jump.hostname.trim().toLowerCase();
  }
  return host;
}

/** The SSH session through which the machine can be asked who is connected, if one is known. */
export function sshPathFor(t: RemoteTarget, root: SessionNode | null | undefined): string {
  if (t.jumpSession && nodeAt(root, t.jumpSession)?.protocol === 'SSH2') return t.jumpSession;
  const machine = machineOf(t, root);
  let hit = '';
  const walk = (n: SessionNode) => {
    for (const c of n.children ?? []) {
      if (c.isFolder) walk(c);
      else if (!hit && c.protocol === 'SSH2' && !c.jump && !c.proxy && (c.hostname || '').trim().toLowerCase() === machine) hit = c.path;
    }
  };
  if (root) walk(root);
  return hit;
}

/** One line that prints "TWRD <port> <connections>" for each of the given ports that has connections. */
export function connectionsCommand(ports: number[]): string {
  const re = ports.filter(p => Number.isInteger(p) && p > 0 && p < 65536).join('|');
  return `ss -Htn state established 2>/dev/null | awk '$3 ~ /:(${re})$/ {n=split($3,a,":"); c[a[n]]++} END {for (p in c) print "TWRD", p, c[p]}'`;
}

/** Reads the output of connectionsCommand: connections per port. */
export function parseConnections(out: string): Map<number, number> {
  const m = new Map<number, number>();
  for (const line of out.split(/\r?\n/)) {
    const x = /^TWRD (\d+) (\d+)\s*$/.exec(line.trim());
    if (x) m.set(+x[1], (m.get(+x[1]) ?? 0) + +x[2]);
  }
  return m;
}
