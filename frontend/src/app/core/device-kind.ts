/** Device types of Linux / Unix hosts: their SSH sessions open the Files panel by default. */
export function isUnixType(name: string | null | undefined): boolean {
  return /linux|ubuntu|debian|cent ?os|rhel|red ?hat|rocky|alma|fedora|suse|unix|bsd|macos|darwin/i.test(name ?? '');
}

/** A session moved to a Linux device type (SSH) gets the Files panel - the user can still turn it off. */
export function filesPanelFor(s: { protocol?: string; deviceType?: string; filesPanel?: boolean }) {
  if ((s.protocol ?? 'SSH2') === 'SSH2' && isUnixType(s.deviceType)) s.filesPanel = true;
}
