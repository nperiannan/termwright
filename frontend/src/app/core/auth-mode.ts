import { Backend } from './backend';
import { Session } from './models';

/** How an SSH session logs in, in three plain words: its authentication order (s.authMethods) behind them. */
export type AuthMode = 'password' | 'key' | 'both';

export function authModeOf(s: Session | null | undefined): AuthMode {
  const m = (s?.authMethods ?? '').split(',').filter(Boolean);
  const key = m.includes('publickey'), pw = m.includes('password') || m.includes('keyboard-interactive');
  return key && !pw ? 'key' : !key ? 'password' : 'both';
}

/** Sets the order for a mode; keyboard-interactive goes with password (servers use either for the same password and
 *  for one-time codes); other methods (e.g. gssapi-with-mic) stay in front. */
export function setAuthMode(s: Session, mode: AuthMode) {
  const keep = (s.authMethods ?? '').split(',').filter(a => a && !['publickey', 'keyboard-interactive', 'password'].includes(a));
  const base = mode === 'password' ? ['keyboard-interactive', 'password'] : mode === 'key' ? ['publickey'] : ['publickey', 'keyboard-interactive', 'password'];
  s.authMethods = [...keep, ...base].join(',');
}

/** A new Ed25519 key for this server in the .ssh folder (no passphrase), or the one made before.
 *  Returns its path and what to tell the user. */
export async function generateKeyFor(backend: Backend, host: string): Promise<{ path: string; message: string }> {
  const d = await backend.keySetupDefaults(host.trim());
  const file = d.defaultPath.split(/[\\/]/).pop();
  if (d.existing.includes(d.defaultPath)) return { path: d.defaultPath, message: `✓ ${file} already exists - it is used` };
  await backend.generateKeyPair('ed25519', d.defaultPath, d.comment, '');
  return { path: d.defaultPath, message: `✓ ${file} created (Ed25519)` };
}
