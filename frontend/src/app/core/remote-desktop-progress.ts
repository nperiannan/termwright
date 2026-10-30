/**
 * Turns what the Set Up Remote Desktop script prints into one line for the
 * user: the step it is on, a progress value, warnings and the error.
 */

export interface RdProgress {
  step: string;      // the step, in plain words
  detail: string;    // the latest line of output (muted, e.g. which package)
  progress: number;  // 0..100
  warnings: string[];
  error: string;
}

export const emptyProgress = (): RdProgress => ({ step: 'Starting…', detail: '', progress: 2, warnings: [], error: '' });

// eslint-disable-next-line no-control-regex
const ANSI = /\u001b\[[0-9;?]*[ -/]*[@-~]/g;
export const cleanLine = (l: string) => l.replace(ANSI, '').replace(/[\u0000-\u0008\u000b-\u001f]/g, '').trimEnd();

/** The script's own steps ("[script] text") in the order they happen. */
const STEPS: { re: RegExp; say: (m: RegExpExecArray) => string; at: number }[] = [
  { re: /^Creating user (\S+)/, say: m => `Creating the user ${m[1]}`, at: 6 },
  { re: /^Installing packages/, say: () => 'Installing the desktop and remote desktop packages (a few minutes)', at: 12 },
  { re: /^Creating the XFCE session launcher/, say: () => 'Preparing the desktop session', at: 70 },
  { re: /^Configuring xrdp/, say: () => 'Configuring RDP', at: 76 },
  { re: /^Configuring TigerVNC/, say: () => 'Configuring VNC', at: 82 },
  { re: /^Set a VNC password/, say: () => 'Setting the VNC password', at: 84 },
  { re: /^Starting xrdp/, say: () => 'Starting RDP', at: 88 },
  { re: /^Starting tigervnc/, say: () => 'Starting VNC', at: 91 },
  { re: /^Firewall/, say: () => 'Setting the firewall', at: 93 },
  { re: /^Checking/, say: () => 'Checking that everything runs', at: 96 },
  { re: /^Check only/, say: () => 'Check finished - nothing was changed', at: 100 },
  { re: /^Nothing to do/, say: () => 'Nothing to do - the services already run', at: 100 },
  { re: /^Done/, say: () => 'Done', at: 100 },
];

const NO_SUDO = /(^|[ :])sudo: (command )?not found/i;
const SUDO_BAD = /incorrect password|sorry, try again|a password is required|no password was provided|a terminal is required/i;

/** Applies one line of output to the progress (returns a new object). */
export function applyLine(p: RdProgress, raw: string): RdProgress {
  const line = cleanLine(raw);
  if (!line.trim()) return p;
  const next: RdProgress = { ...p, warnings: p.warnings };
  const step = /^\[[^\]]+\] (.+)$/.exec(line.trim());
  if (step) {
    const text = step[1];
    const known = STEPS.find(s => s.re.test(text));
    if (known) {
      next.step = known.say(known.re.exec(text)!);
      next.progress = Math.max(p.progress, known.at);
    } else next.step = text;
    next.detail = '';
    return next;
  }
  if (/^ERROR:/i.test(line)) { next.error = line.replace(/^ERROR:\s*/i, ''); return next; }
  if (NO_SUDO.test(line)) { next.error = 'sudo is not installed on this machine: install it (as root: apt install sudo) or give this login sudo rights.'; return next; }
  if (SUDO_BAD.test(line)) { next.error = 'The sudo password was not accepted.'; return next; }
  if (/^WARNING:/i.test(line)) { next.warnings = [...p.warnings, line.replace(/^WARNING:\s*/i, '')]; return next; }
  // apt and friends: keep the latest line as muted detail
  next.detail = line.trim().slice(0, 110);
  return next;
}

/** The ordinary report lines of --check ("  RDP user x: will be created"). */
export const reportLines = (log: string[]) => log.map(cleanLine).filter(l => /^\s{2}\S/.test(l) || /^Desktop users/.test(l));
