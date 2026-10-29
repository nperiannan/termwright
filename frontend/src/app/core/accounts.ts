import { Session, Settings } from './models';

/** Who a session logs in as when it keeps "Default". */
export interface DefaultLogin { kind: 'account' | 'device' | 'own'; name?: string; user?: string; from?: string; }

/** The session's Account choice: '' default, '-' own username and password, else an account. */
export function accountChoice(s: Session): string { return s.account || (s.ownLogin && s.deviceType ? '-' : ''); }

export function setAccountChoice(s: Session, v: string) {
  s.account = v;
  s.ownLogin = v === '-';
}

/** What "Default" means for a session: the folder's account, the device type's account or login, or its own. */
export function defaultLogin(st: Settings | null | undefined, s: Session): DefaultLogin {
  const defs = st?.folderDefaults ?? [];
  let f = (s.folder || (s.path?.includes('/') ? s.path.slice(0, s.path.lastIndexOf('/')) : '')).replace(/^\/+|\/+$/g, '');
  for (;;) {
    const d = defs.find(x => x.folder.replace(/^\/+|\/+$/g, '') === f);
    if (d?.account) return { kind: 'account', name: d.account, user: userOf(st, d.account), from: 'folder "' + (f || 'top level') + '"' };
    if (!f) break;
    f = f.includes('/') ? f.slice(0, f.lastIndexOf('/')) : '';
  }
  const dt = (st?.deviceTypes ?? []).find(d => d.name.toLowerCase() === (s.deviceType ?? '').toLowerCase());
  if (dt?.credential) return { kind: 'account', name: dt.credential, user: userOf(st, dt.credential), from: 'device type ' + dt.name };
  if (dt?.username) return { kind: 'device', user: dt.username, from: 'device type ' + dt.name };
  return { kind: 'own' };
}

export function userOf(st: Settings | null | undefined, account: string) { return (st?.credentials ?? []).find(c => c.name === account)?.username ?? ''; }

/** One line for "Default · …" in the Account list. */
export function defaultText(d: DefaultLogin) {
  if (d.kind === 'account') return `${d.name} (${d.from})`;
  if (d.kind === 'device') return `the ${d.from?.replace('device type ', '')} login - ${d.user}`;
  return 'own username and password';
}
