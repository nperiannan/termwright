import { Backend, errText } from './backend';
import { Ui } from './ui.service';

/** Password-manager references stored instead of a password (resolved at connect time). */
export const REF_EXAMPLES = 'op://Vault/Item/password · bw://item · vault://secret/path#password · cmd://command';

export function isRef(s: string | null | undefined): boolean {
  return /^(op|bw|vault|cmd):\/\//.test((s ?? '').trim());
}

/** "ref:op://…" password state → the reference, else ''. */
export function refOf(state: string): string {
  return state.startsWith('ref:') ? state.slice(4) : '';
}

/** Asks for a reference, tests it (the secret never reaches the UI) and returns it, or null. */
export async function askRef(ui: Ui, backend: Backend, current = ''): Promise<string | null> {
  const ref = (await ui.input('Password from a Password Manager',
    '1Password op://Vault/Item/password, Bitwarden bw://item (or bw://item#field), HashiCorp Vault vault://secret/path#password, or cmd://any command',
    current || 'op://'))?.trim();
  if (!ref || ref === 'op://') return null;
  if (!isRef(ref)) { ui.toast('A reference starts with op://, bw://, vault:// or cmd://', 'error'); return null; }
  try {
    ui.toast(await backend.testSecretRef(ref), 'success');
    return ref;
  } catch (e) {
    return (await ui.confirm('Password Manager', `Could not read ${ref}:\n${errText(e)}\n\nUse the reference anyway?`, 'Use It')) ? ref : null;
  }
}
