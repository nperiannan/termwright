// Types mirroring the Go structs exposed by the backend (app.go / internal/*).

export interface LogonAction { expect: string; send: string; hide: boolean; trailingCr: boolean; credential: string; }

export interface Session {
  path: string; name: string; folder: string; protocol: string;
  hostname: string; port: number; username: string; identityFile: string; authMethods: string;
  emulation: string; termType: string; description: string[];
  rows: number; cols: number; scrollback: number; keepalive: boolean; keepaliveSec: number;
  comPort: string; baudRate: number; dataBits: number; stopBits: number; parity: number;
  logFile: string; logOnConnect: boolean; logAppend: boolean; logEachLine: string; logRaw: boolean; recordSession?: boolean; smartCard?: string; mac?: string;
  savePassword: boolean; localCommand: string; localDir: string; colorScheme: string;
  logonActions: LogonAction[];
  triggers: Trigger[]; portForwards: PortForward[];
  jumpSession: string; proxyType: '' | 'socks5' | 'http'; proxyHost: string; proxyPort: number; proxyUser: string;
  autoReconnect: boolean; x11Forward: boolean; agentForward: boolean;
  consoleServer?: string; consolePort?: number; consoleMethod?: string; consoleUser?: string; consoleBase?: number; consoleProtocol?: string;
  environment?: string; rdpDomain?: string; rdpFullScreen?: boolean; rdpSize?: string; vncViewOnly?: boolean; keywordSet: string; fontFamily: string; fontSize: number; backspace: '' | 'bs'; logonScript: string; serialDtr: boolean; serialRts: boolean;
  logonArgs: string; useLogonScript: boolean; automateLogon: boolean; initialCr: boolean;
  remoteCommand: string; useRemoteCommand: boolean; openAs: '' | 'sftp'; deviceType: string; ownLogin: boolean; sftpToTarget: boolean;
  kerberosPrincipal: string; kerberosSpn: string; certificateFile: string; serialFlow: '' | 'rtscts' | 'xonxoff';
}

/** A login profile for a kind of device (EXOS, VOSS, Cisco IOS …). */
export interface DeviceType {
  name: string; protocol: string; port: number; username: string; credential: string; initialCr: boolean;
  loginSteps: LogonAction[] | null; afterLogin: LogonAction[] | null; prompt: string; backupCommands?: string;
}
/** A saved command; {{name}} / {{name:default}} are asked for when sending. */
export interface Snippet { name: string; folder: string; text: string; description: string; deviceTypes?: string[] | null; }
export interface Workspace { name: string; layout: string; tabs: { session: string; kind: string }[]; }
export interface FolderDefault {
  folder: string; username: string; deviceType: string; jumpSession: string; identityFile: string; keywordSet: string; colorScheme: string;
  environment?: string; record?: boolean; smartCard?: string;
}
/** Variables used in a snippet, in order: [{name, def}]. */
export function snippetVars(text: string): { name: string; def: string }[] {
  const seen = new Map<string, string>();
  for (const m of text.matchAll(/\{\{\s*([\w.-]+)\s*(?::([^}]*))?\}\}/g)) if (!seen.has(m[1])) seen.set(m[1], m[2] ?? '');
  return [...seen].map(([name, def]) => ({ name, def }));
}
/** Device types a command is meant for: its own list, or a folder named after a device type ("VOSS / Troubleshooting"). */
export function snippetTypes(s: Snippet, known: string[]): string[] {
  if (s.deviceTypes?.length) return s.deviceTypes;
  const top = s.folder.split('/')[0].trim().toLowerCase();
  const t = known.find(k => k.toLowerCase() === top);
  return t ? [t] : [];
}
export function fillSnippet(text: string, values: Record<string, string>): string {
  return text.replace(/\{\{\s*([\w.-]+)\s*(?::[^}]*)?\}\}/g, (_m, n: string) => values[n] ?? '');
}

export interface AlertRule { pattern: string; regex: boolean; caseSensitive: boolean; enabled: boolean; sound: boolean; desktop: boolean; }
export interface AlertEvent { tabId: string; title: string; line: string; pattern: string; sound: boolean; desktop: boolean; }

/** Finds a device type by a name typed by the user (case and spaces ignored). */
export function findDeviceType(list: DeviceType[] | null | undefined, word: string): DeviceType | undefined {
  const w = word.toLowerCase().replace(/[\s_-]+/g, '');
  return (list ?? []).find(d => d.name.toLowerCase().replace(/[\s_-]+/g, '') === w);
}

export interface Trigger { pattern: string; regex: boolean; send: string; }
export interface PortForward {
  type: 'local' | 'remote' | 'dynamic'; bindAddr: string; bindPort: number; destHost: string; destPort: number; name: string;
}
export interface Keyword { text: string; regex: boolean; caseSensitive: boolean; color: string; }
export interface KeywordSet { name: string; keywords: Keyword[]; }

export interface SessionNode {
  name: string; path: string; isFolder: boolean; protocol?: string; hostname?: string;
  username?: string; port?: number; description?: string; hopTarget?: string; openAs?: string; children?: SessionNode[];
  deviceType?: string; environment?: string; jump?: string; proxy?: boolean; console?: string; mac?: string;
}

export interface Button { label: string; send: string; toAll: boolean; action: string; script: string; }

export interface Settings {
  vaultAddr?: string; keyPassphrases?: string[];
  sessionsDir: string; logDir: string; downloadDir: string; fontFamily: string; fontSize: number; theme: string;
  terminalScheme: string; cursorStyle: 'block' | 'underline' | 'bar'; cursorBlink: boolean;
  scrollback: number; copyOnSelect: boolean; rightClickPaste: boolean; confirmClose: boolean;
  showButtonBar: boolean; showCommandWindow: boolean; showSessionManager: boolean; sessionManagerRight?: boolean;
  sftpCwdAuto?: boolean; sftpFollowTerminal?: boolean; sftpFollowUser?: boolean;
  pasteLineDelayMs: number; buttons: Button[]; recentSessions: string[];
  favorites?: string[] | null; lastConnected?: Record<string, string> | null; showReachability?: boolean;
  backupDir?: string; backupJobs?: BackupJob[] | null;
  checklists?: Checklist[] | null; checklistDir?: string; recordHistory?: boolean | null;
  recordSessions?: boolean; recordDir?: string; recordKeepDays?: number;
  smartCardLibraries?: string[] | null; smartCardPinEachTime?: boolean;
  collections?: Collection[] | null; collectDir?: string;
  keywordSets: KeywordSet[] | null; defaultKeywordSet: string; showConnectBar: boolean; keyMap: KeyBinding[] | null;
  customSchemes: { name: string; colors: Record<string, string> }[] | null; accentColor: string;
  restoreTabs: boolean; openTabs: string[] | null; inWindowMenu: boolean; forceTerminalScheme: boolean; pythonPath: string;
  deviceTypes: DeviceType[] | null; alerts: AlertRule[] | null; confirmPaste: boolean;
  snippets: Snippet[] | null; workspaces: Workspace[] | null; folderDefaults: FolderDefault[] | null;
  credentials: Credential[] | null; masterHash: string;
  bookmarks: Bookmark[] | null; updateChannel: 'stable' | 'nightly' | 'off'; updateRepo: string; updateOnStart: boolean;
}

/** Saved SFTP folder; session is empty for local-folder bookmarks. */
export interface Bookmark { name: string; session: string; side: 'local' | 'remote'; path: string; }

export interface UpdateInfo {
  current: string; latest: string; version: string; newer: boolean; notes: string; page: string; published: string;
  assetName: string; assetUrl: string; assetSize: number;
}

/** A device found by File → Discover Devices. */
export interface DiscoverResult {
  ip: string; name: string; dns: string; sysName: string; sysDescr: string; sysObjectId: string; services: string[];
  banner: string; telnet: string; web: { server: string; title: string; realm: string; cert: string; hints: string[] | null };
  sip: string; mdns: string; mdnsServices?: string[] | null; coap?: string; mac: string; macVendor: string; self: boolean; kind: string; vendor: string; os: string; platform?: string; evidence: string[] | null;
  protocol: string; port: number;
}

/** Newest stable release and newest nightly build (null when none is published). */
export interface UpdateStatus {
  current: string; runningNightly: boolean;
  stable: UpdateInfo | null; stableErr: string;
  nightly: UpdateInfo | null; nightlyErr: string;
}

/** A named username/password pair usable from logon actions (\s, \w). The password lives in the OS keychain. */
export interface Credential { name: string; username: string; }

export interface TabInfo {
  id: string; title: string; state: string; info: string; session: Session; logFile: string;
}

export interface PromptField { label: string; secret: boolean; value: string; }
export interface Prompt {
  id: string; tabId: string; kind: 'credentials' | 'hostkey' | 'confirm'; title: string; message: string;
  fields: PromptField[] | null; choices: string[] | null; allowRemember: boolean; rememberLabel?: string; danger: boolean;
}
export interface PromptReply { ok: boolean; values: string[]; choice: string; remember: boolean; }

export interface ImportOptions { targetFolder: string; conflict: 'rename' | 'overwrite' | 'skip'; includeDefault: boolean; configPassphrase?: string; }
export interface ImportResult { imported: string[] | null; renamed: string[] | null; skipped: string[] | null; errors: string[] | null;
  passwordsImported?: number; passwordErrors?: string[] | null; }

export interface AppInfo { name: string; version: string; os: string; arch: string; dataDir: string; sessionsDir: string; initError: string; locked: boolean; hasMaster: boolean; }

export interface FileFilter { displayName: string; pattern: string; }

export const PROTOCOLS = ['SSH2', 'Telnet', 'Serial', 'Local Shell', 'Raw', 'Console', 'RDP', 'VNC'] as const;
/** Console server access methods (Console sessions). */
export const CONSOLE_METHODS: { id: string; label: string }[] = [
  { id: 'opengear-ssh', label: 'SSH as user:portNN (Opengear)' },
  { id: 'avocent-ssh', label: 'SSH as user:ttySN (Avocent ACS)' },
  { id: 'ssh-3000', label: 'SSH to port 3000 + N (Opengear, Lantronix, Perle)' },
  { id: 'telnet-2000', label: 'Telnet to port 2000 + N (Opengear, Lantronix, Cisco)' },
  { id: 'telnet-7000', label: 'Telnet to port 7000 + N (Avocent, Cyclades)' },
  { id: 'custom', label: 'Custom' },
];
/** Protocols shown in their own window or viewer instead of a terminal. */
export const isRemoteDesktop = (p: string | undefined) => p === 'RDP' || p === 'VNC';

/** Configuration backup job: sessions or folders ("Folder/") backed up on demand or on a schedule. */
export interface BackupJob { name: string; targets: string[]; schedule: '' | 'daily' | 'weekdays'; at: string; keep: number; saveAll: boolean; commands: string; }
export interface BackupStatus { time: string; ok: boolean; error?: string; changed: boolean; file?: string; copies: number; log?: string; }
export interface BackupFile { name: string; path: string; time: string; size: number; }
export interface BackupResult { runId: string; path: string; ok: boolean; error?: string; changed: boolean; file?: string; done: number; total: number; }

/** Change checklist: show commands per device type, run before and after a change. */
export interface Checklist { name: string; targets: string[]; commands: { deviceType: string; commands: string }[]; }
/** Run and Collect: commands on many devices, values from the output in a table. */
export interface Collection { name: string; targets: string[]; commands: { deviceType: string; commands: string }[]; columns: CollectColumn[]; }
export interface CollectColumn { name: string; pattern: string; deviceType: string; }
export interface CheckPhaseResult { time: string; ok: boolean; error?: string; file?: string; log?: string; }
export interface CheckRun { id: string; created: string; note?: string; devices: Record<string, { before?: CheckPhaseResult; after?: CheckPhaseResult; changed?: boolean | null }>; }
export interface CheckResult { list: string; run: string; phase: 'before' | 'after'; path: string; ok: boolean; error?: string; changed?: boolean | null; done: number; total: number; }
/** A remembered command (history search, autocomplete). */
export interface HistoryEntry { cmd: string; session?: string; title?: string; deviceType?: string; time: string; count: number; }

/** An open RDP or VNC tab (backend RemoteInfo). */
export interface RemoteInfo { id: string; kind: 'rdp' | 'vnc'; title: string; addr: string; via: string; client: string; wsUrl: string; hasPass: boolean; note: string; session: Session; }
export const EMULATIONS = ['Xterm', 'VT100', 'VT102', 'VT220', 'ANSI', 'Linux'] as const;
export const BAUD_RATES = [300, 1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600];

export function defaultPort(protocol: string): number {
  return protocol === 'Telnet' ? 23 : protocol === 'SSH2' ? 22 : protocol === 'RDP' ? 3389 : protocol === 'VNC' ? 5900 : 0;
}

/** Expands \r \n \t \e \b \\ and \xHH escapes, matching the Go backend. */
export function unescape(s: string): string {
  return s.replace(/\\(x[0-9a-fA-F]{2}|.)/g, (_m, c: string) => {
    if (c.length === 3) return String.fromCharCode(parseInt(c.slice(1), 16));
    switch (c) {
      case 'r': return '\r';
      case 'n': return '\n';
      case 't': return '\t';
      case 'e': return '\x1b';
      case 'b': return '\b';
      case '\\': return '\\';
      default: return '\\' + c;
    }
  });
}

export interface KeyBinding { keys: string; send: string; action: string; script: string; }
export interface FileEntry { name: string; size: number; mode: string; modTime: string; isDir: boolean; isLink: boolean; }
export interface Transfer {
  id: string; direction: 'up' | 'down' | 'xmodem' | 'xmodem-down' | 'zmodem-up' | 'zmodem-down' | 'scp-up' | 'scp-down' | 'ymodem-up' | 'ymodem-down' | 's2s'; file: string; bytes: number; total: number;
  filesDone: number; filesTotal: number; state: 'running' | 'done' | 'error' | 'cancelled'; error: string; resumable: boolean;
}

/** Actions that menus, key bindings and buttons can trigger. */
export const ACTIONS: Record<string, string> = {
  'quick-connect': 'Quick Connect', collect: 'Run and Collect...', discover: 'Discover Devices...', backups: 'Configuration Backups...', checklists: 'Change Checklists...', history: 'Command History...', 'local-shell': 'Local Shell', reconnect: 'Reconnect', disconnect: 'Disconnect',
  'clone-tab': 'Clone Session', 'close-tab': 'Close Tab', 'next-tab': 'Next Tab', 'prev-tab': 'Previous Tab',
  copy: 'Copy', paste: 'Paste', 'copy-paste': 'Copy and Paste', find: 'Find', 'clear-scrollback': 'Clear Scrollback',
  'clear-screen': 'Clear Screen', reset: 'Reset Terminal', 'log-toggle': 'Start/Stop Log', 'save-scrollback': 'Save Scrollback',
  'toggle-session-manager': 'Toggle Session Manager', 'toggle-session-manager-side': 'Move Session Manager Left / Right', 'toggle-command-window': 'Toggle Command Window',
  'toggle-button-bar': 'Toggle Button Bar', 'sftp-tab': 'Open SFTP Tab', 'run-script': 'Run Script...',
  'record-toggle': 'Start/Stop Script Recording', 'cast-toggle': 'Start/Stop Session Recording', 'cast-marker': 'Add Marker to Recording', recordings: 'Recordings...', 'cancel-scripts': 'Cancel Scripts', lock: 'Lock Session',
  'layout-tabs': 'Tabbed View', 'layout-tile-v': 'Tile Side by Side', 'layout-tile-h': 'Tile Stacked', 'layout-grid': 'Tile Grid',
  'send-break': 'Send Break', 'send-file': 'Send Text File...', 'save-selection': 'Save Selection As...',
  'xmodem': 'Send XMODEM...', 'xmodem-receive': 'Receive XMODEM...', 'scp-upload': 'SCP Upload File...', 'scp-upload-folder': 'SCP Upload Folder...', 'scp-download': 'SCP Download...',
  'ymodem-send': 'Send YMODEM...', 'ymodem-receive': 'Receive YMODEM', passwords: 'Passwords & Credentials...', 'check-updates': 'Check for Updates...', 'key-setup': 'Set Up Key Login...', 'quick-open': 'Quick Open...', 'command-library': 'Command Library...', 'workspaces': 'Workspaces...',
  'save-workspace': 'Save Workspace...', 'compare': 'Compare Outputs...',
  'zoom-in': 'Zoom In', 'zoom-out': 'Zoom Out', 'global-options': 'Global Options', 'session-options': 'Session Options',
};

/** Normalised key-combination string for a keyboard event, e.g. "Ctrl+Shift+F1". */
export function comboOf(ev: KeyboardEvent): string {
  const k = ev.key;
  if (['Control', 'Shift', 'Alt', 'Meta'].includes(k)) return '';
  const parts: string[] = [];
  if (ev.ctrlKey) parts.push('Ctrl');
  if (ev.altKey) parts.push('Alt');
  if (ev.shiftKey) parts.push('Shift');
  if (ev.metaKey) parts.push('Cmd');
  let name = k.length === 1 ? k.toUpperCase() : k;
  if (k === ' ') name = 'Space';
  if (ev.altKey && /^Key[A-Z]$/.test(ev.code)) name = ev.code.slice(3); // macOS Option produces symbols
  if (ev.altKey && /^Digit\d$/.test(ev.code)) name = ev.code.slice(5);
  parts.push(name);
  return parts.join('+');
}

export function formatBytes(n: number): string {
  if (n < 1024) return n + ' B';
  const u = ['KB', 'MB', 'GB', 'TB'];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return n.toFixed(n < 10 ? 1 : 0) + ' ' + u[i];
}

/** A session recording (asciinema v2 .cast). */
export interface RecordingInfo { path: string; session: string; title: string; host: string; started: string; duration: number; markers: number; size: number; }
export interface RecordingData {
  header: { version: number; width: number; height: number; timestamp: number; title?: string; termwright_session?: string; termwright_host?: string; termwright_user?: string };
  /** [seconds, code, data]: "o" output, "r" resize ("COLSxROWS"), "m" marker, "i" input */
  events: [number, string, string][];
}

/** A key on a smart card (PKCS#11). */
export interface SmartCardKey { library: string; token: string; serial: string; slot: number; id: string; label: string; type: string; bits: number; fingerprint: string; authorizedKey: string; subject?: string; }
export interface SmartCardReport { library: string; error?: string; tokens: string[]; keys: SmartCardKey[]; }
export interface SmartCardInfo { available: boolean; found: string[] | null; reports: SmartCardReport[] | null; loggedIn: number; }

/** A session whose device (same MAC address) answers at another IP address. */
export interface MovedDevice { path: string; old: string; new: string; mac: string; hostKey?: 'same' | 'different' | 'unknown' | 'none' | ''; scanned?: string; }
