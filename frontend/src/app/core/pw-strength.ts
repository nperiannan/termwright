/** How strong a password looks, for the meter under master-password fields.
 *  Only the length is required (4 characters); upper case, digits and
 *  symbols make it stronger but are never demanded. */
export interface Strength { score: 0 | 1 | 2 | 3 | 4; label: string; color: string; ok: boolean; }

export const MIN_MASTER = 4;

export function pwStrength(pw: string): Strength {
  const n = [...pw].length;
  if (!n) return { score: 0, label: '', color: 'var(--line)', ok: false };
  if (n < MIN_MASTER) return { score: 1, label: `Too short - at least ${MIN_MASTER} characters`, color: 'var(--danger)', ok: false };
  const kinds = [/[a-z]/, /[A-Z]/, /\d/, /[^A-Za-z0-9]/].filter(r => r.test(pw)).length;
  const words = pw.trim().split(/\s+/).length;
  let pts = n >= 16 ? 3 : n >= 12 ? 2 : n >= 8 ? 1 : 0;
  if (kinds >= 3) pts++;
  if (words >= 3) pts++;
  if (/^(.)\1+$/.test(pw) || /^(1234|password|qwerty|admin)/i.test(pw)) pts = 0;
  const score = Math.min(4, pts + 1) as 1 | 2 | 3 | 4;
  const map: Record<1 | 2 | 3 | 4, [string, string]> = { 1: ['Weak', 'var(--danger)'], 2: ['Fair', '#e5a93b'], 3: ['Good', 'var(--ok)'], 4: ['Strong', 'var(--ok)'] };
  return { score, label: map[score][0], color: map[score][1], ok: true };
}
