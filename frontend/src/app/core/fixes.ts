import { Session } from './models';

/** A suggested fix for a connection error: a short explanation and buttons. */
export interface Fix { hint: string; actions: FixAction[]; }
export type FixAction = 'reconnect' | 'edit' | 'edit-jump' | 'device-types' | 'forget-password' | 'copy';

const AUTH = /unable to authenticate|authentication fail|permission denied|no supported methods|login incorrect|access denied|auth fail/i;
const HOSTKEY = /host key/i;
const NET = /refused|timed? ?out|no route|unreachable|connection reset|broken pipe|eof/i;
const DNS = /no such host|lookup .* (failed|no such)|name resolution|unknown host/i;
const JUMP = /jump host/i;
const KRB = /kerberos|gssapi/i;
const PROXY = /proxy/i;

/** Explains a connection error and picks the buttons that can fix it. */
export function fixFor(message: string, s: Session | null | undefined): Fix {
  const saved = !!s?.path;
  const edit: FixAction[] = saved ? ['edit'] : [];
  const usesType = !!s?.deviceType && !s?.ownLogin;
  if (JUMP.test(message)) {
    return { hint: `The jump host ${s?.jumpSession ? '"' + s.jumpSession + '" ' : ''}could not be reached or refused the login.`,
      actions: ['reconnect', ...(s?.jumpSession ? ['edit-jump' as FixAction] : []), ...edit] };
  }
  if (HOSTKEY.test(message)) {
    return { hint: 'The server\'s host key is not the saved one. Reconnect to see the new key and replace it if the change is expected (new server, reinstall).',
      actions: ['reconnect', ...edit] };
  }
  if (AUTH.test(message)) {
    return { hint: usesType
        ? `The login was refused. The username and password come from the device type "${s!.deviceType}".`
        : 'The login was refused: check the username, the password or the key.',
      actions: ['reconnect', ...(usesType ? ['device-types' as FixAction] : []), ...edit, ...(saved ? ['forget-password' as FixAction] : [])] };
  }
  if (KRB.test(message)) return { hint: 'Kerberos login failed: check the principal (user@REALM) and that you have a ticket (kinit).', actions: ['reconnect', ...edit] };
  if (PROXY.test(message)) return { hint: 'The proxy refused or could not be reached: check the proxy settings of the session.', actions: ['reconnect', ...edit] };
  if (DNS.test(message)) return { hint: 'The host name could not be found: check its spelling, or use the IP address.', actions: [...edit, 'reconnect'] };
  if (NET.test(message)) {
    return { hint: s?.jumpSession ? 'No answer through the jump host: check the host name and port, and that the jump host can reach it.'
        : 'The device did not answer: check the host name, the port and that the device is up (a VPN may be needed).',
      actions: ['reconnect', ...edit, ...(s?.jumpSession ? ['edit-jump' as FixAction] : [])] };
  }
  return { hint: '', actions: ['reconnect', ...edit] };
}

export const FIX_LABELS: Record<FixAction, string> = {
  reconnect: 'Reconnect', edit: 'Edit Session', 'edit-jump': 'Edit Jump Host', 'device-types': 'Device Type Login',
  'forget-password': 'Forget Saved Password', copy: 'Copy Error',
};

/** SFTP "browse as" errors from sudo/su. */
export const SUDO_ERR = /sudoers|must be run from a terminal|a terminal is required|password is required|sudo did not answer|incorrect password|authentication failure/i;
