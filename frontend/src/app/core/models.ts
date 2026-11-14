// Types mirroring the Go structs exposed by the backend (app.go / internal/*).

export interface LogonAction { expect: string; send: string; hide: boolean; trailingCr: boolean; credential: string; }

export interface Session {
  path: string; name: string; folder: string; protocol: string;
  hostname: string; port: number; username: string; identityFile: string; authMethods: string;
  team?: string;
  legacyAlgorithms: boolean; kexAlgorithms: string; ciphers: string; macs: string; hostKeyAlgorithms: string;
  emulation: string; termType: string; noCursorReport?: boolean; description: string[];
  rows: number; cols: number; scrollback: number; keepalive: boolean; keepaliveSec: number;
  comPort: string; baudRate: number; dataBits: number; stopBits: number; parity: number;
  logFile: string; logOnConnect: boolean; logAppend: boolean; logEachLine: string; logRaw: boolean; recordSession?: boolean; smartCard?: string; mac?: string; wolBroadcast?: string; wolPort?: number; wolFrom?: '' | 'local' | 'jump'; wolNetwork?: string; wakeSchedule?: '' | 'daily' | 'weekdays'; wakeAt?: string; wakeKnock?: string; wolScans?: number; wolScanSecs?: number; wolPingSecs?: number;
  savePassword: boolean; localCommand: string; localDir: string; colorScheme: string;
  logonActions: LogonAction[];
  triggers: Trigger[]; portForwards: PortForward[];
  jumpSession: string; proxyType: '' | 'socks5' | 'http'; proxyHost: string; proxyPort: number; proxyUser: string;
  /** A turned-off jump host / proxy is kept (offJump / offProxy) but not used. */
  jumpOff?: boolean; offJump?: string; proxyOff?: boolean; offProxy?: { type: '' | 'socks5' | 'http'; host: string; port: number; user: string } | null;
  autoReconnect: boolean; x11Forward: boolean; agentForward: boolean;
  consoleServer?: string; consolePort?: number; consoleMethod?: string; consoleUser?: string; consoleBase?: number; consoleProtocol?: string;
  environment?: string; rdpDomain?: string; rdpFullScreen?: boolean; rdpSize?: string; vncViewOnly?: boolean; keywordSet: string; fontFamily: string; fontSize: number; backspace: '' | 'bs'; logonScript: string; serialDtr: boolean; serialRts: boolean;
  logonArgs: string; useLogonScript: boolean; automateLogon: boolean; initialCr: boolean; loginAgain?: boolean;
  remoteCommand: string; useRemoteCommand: boolean; openAs: '' | 'sftp'; deviceType: string; ownLogin: boolean; account?: string; sftpToTarget: boolean; sftpOn?: '' | 'jump' | 'target'; filesPanel?: boolean;
  kerberosPrincipal: string; kerberosSpn: string; certificateFile: string; serialFlow: '' | 'rtscts' | 'xonxoff';
  serialAutoBaud?: boolean; timestamps?: boolean; noAutoScroll?: boolean;
  localEcho?: boolean; mosh?: boolean; telnetTls?: boolean; tlsAnyCert?: boolean;
}

/** A login profile for a kind of device (EXOS, VOSS, Cisco IOS …). */
export interface DeviceType {
  name: string; icon?: string; protocol: string; port: number; username: string; credential: string; initialCr: boolean;
  loginSteps: LogonAction[] | null; afterLogin: LogonAction[] | null; prompt: string; backupCommands?: string; aiReadOnly?: string;
  facts?: FactRule[] | null;
}
/** A value read from command output after connecting: the first group of regex. every = at each connect. */
export interface FactRule { name: string; command: string; regex: string; every: boolean; }
/** Names offered for facts (users can type others). */
export const FACT_NAMES = ['Model', 'Version', 'Serial', 'MAC', 'Uptime'];
/** Facts read at each connect by default (the others once per session). */
export const FACT_EVERY = ['Version', 'Uptime'];
/** A saved command; {{name}} / {{name:default}} are asked for when sending. */
export interface Snippet { name: string; folder: string; text: string; description: string; deviceTypes?: string[] | null; }
export interface Workspace { name: string; layout: string; tabs: { session: string; kind: string }[]; }
export interface FolderDefault {
  folder: string; username: string; deviceType: string; jumpSession: string; identityFile: string; keywordSet: string; colorScheme: string;
  environment?: string; record?: boolean; smartCard?: string; account?: string;
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
  deviceType?: string; environment?: string; jump?: string; proxy?: boolean; console?: string; mac?: string; wolPing?: number; wolNetwork?: string; account?: string; team?: string;
}

/** Simulated lab (internal/sim). */
export interface LabModel { sku: string; family: string; os: string[]; osName: Record<string, string>; ports: number; layout: string; }
export interface LabPack { id: string; name: string; version: string; vendor: string; description: string; license: string; hash: string; os: string[]; models: number; file?: string; accepted?: string; error?: string; }
export interface LabOS { name: string; short: string; configCommand: string; }
export interface LabDevice { id: string; os: string; model: string; version: string; serial: string; hwSerial?: string; mac: string; hostname: string; mgmtIp: string; port: number; ports: number; running: boolean; logins: number; unsaved: boolean; x: number; y: number;
  /** Emulated lab only. */
  state?: string; stateText?: string; cpu?: number; cores?: number; diskGb?: number; template?: string; persona?: EmuPersona | null; personaOk?: boolean; personaError?: string; personaInfo?: string; cpus?: number; ram?: number; console?: number; ssh?: string; error?: string; image?: string; memUsed?: number; host?: string; saved?: boolean; role?: string; uplinks?: string[] | null; iconStyle?: string; movingTo?: string; computer?: boolean; desktop?: boolean; routing?: EmuRouting | null; addrs?: Record<string, string> | null; addrsSeen?: Record<string, string> | null; cpuMax?: number; cpuWeight?: number; cpuReq?: number; cpuLim?: number; cpuStats?: EmuCpuStats | null; restart?: string; checkpoints?: { tag: string; at: string; note?: string; change?: string }[] | null; addrs6?: Record<string, string> | null; dataGw?: string; dataGw6?: string; dns?: string; net?: EmuCompNetSeen | null; }
export interface LabEvent { at: string; vm: string; name: string; level: 'info' | 'ok' | 'warn' | 'error'; text: string; host?: string; by?: string; }
export interface EmuTopoInfo { name: string; saved: string; hosts: string[] | null; switches: number; open: boolean; running?: number; runOn?: string[]; }
export interface LabLag { name: string; mode: string; source: string; }
/** A computer's network as its console showed it. */
export interface EmuCompNetSeen { addrs?: Record<string, string> | null; linkLocal?: Record<string, string> | null; gw?: string; gw6?: string; dns?: string; }
/** A computer's IP configuration for one port, with its gateways and DNS server. */
export interface EmuCompNet { port: number; ipv4: string; ipv6: string; gw: string; gw6: string; dns: string; }
export interface LabLink { a: string; aPort: string; b: string; bPort: string; up?: boolean; down?: boolean; lagA?: LabLag | null; lagB?: LabLag | null; lldp?: string; }
export interface LabView { exists: boolean; running: boolean; folder: string; mgmtNet: string; dns?: string[] | null; dnsSet?: boolean; autoCheckpoints?: boolean; notes?: LabNote[] | null; hubs?: LabHub[] | null; cloudPorts?: Record<string, number> | null; devices: LabDevice[] | null; links: LabLink[] | null; sessions: Record<string, string> | null; models: LabModel[] | null; outputs: LabOutput[] | null; pack?: LabPack | null; waiting?: number; os: Record<string, LabOS> | null;
  /** Emulated lab only. */
  kind?: 'emu'; topology?: string; cloudGw?: string; lanFixed?: boolean; events?: LabEvent[] | null; downloadDir?: string; downloadQueue?: string[] | null; mgmt?: string; consoles?: Record<string, string> | null; host?: string; hostInfo?: EmuHostInfo | null; hosts?: EmuHost[] | null; images?: EmuImage[] | null; kvm?: boolean; templates?: EmuTemplate[] | null; usage?: EmuUsage; deviceTypes?: EmuDeviceType[] | null; installs?: EmuInstall[] | null; hostUse?: EmuHostUse[] | null; fast?: boolean; iconStyle?: string; }
/** An install of an ISO file on a KVM host: running, waiting for the user, or finished. */
export interface EmuInstall { id: string; name: string; host: string; mode: 'alpine' | 'manual'; isoName: string; gb: number; mb?: number; ram: number; cpus: number; port: number; stage: 'copying' | 'starting' | 'installing' | 'waiting' | 'saving' | 'fetching' | 'verifying' | 'done' | 'failed' | 'cancelled'; msg: string; err?: string; image?: string; console?: string; started: string; step?: string; marks?: { step: string; at: string }[]; file?: string; tail?: string; disk?: number; part?: number; pct?: number; idle?: number; noVerify?: boolean; verifyPort?: number; verified?: string; verifyErr?: string; }
export interface EmuInstallRequest { name: string; iso: string; mode: 'alpine' | 'manual'; gb: number; mb?: number; ram: number; cpus: number; host: string; password: string; noVerify?: boolean; }
export interface EmuHostUse { host: string; name: string; addr: string; mgmt: string; mgmtNet: string; cloudGw?: string; lanFixed?: boolean; cpus: number; memTotal: number; memFree: number; load: number; cores: number; vcpus: number; ram: number; switches: number; running: number; main: boolean; }
export interface EmuPersonaDef { name: string; file: string; template: string; models: { name: string; modules: string[] | null }[] | null; values: { key: string; label: string; default: string }[] | null; stack: boolean; modules: string; osName?: string; check?: string; }
export interface EmuSlot { num: number; model: string; serial: string; module: string; master: boolean; ports: { port: number; vif: number }[] | null; }
export interface EmuPersona { slots: EmuSlot[]; values: Record<string, string> | null; applied?: string; }
export interface EmuTemplate { id: string; name: string; image: string; cpus: number; ram: number; diskGb: number; model: string; module: string; values: Record<string, string> | null; serialStart: string; macStart: string; }
export interface EmuDeviceType { key: string; model: string; kind: string; osName: string; modules: string[] | null; modName: string; images: string[]; cpus: number; ram: number; values: Record<string, string> | null; }
export interface EmuUsage { hostCpus: number; hostMemTotal: number; hostMemFree: number; load: number; running: number; vcpus: number; ram: number; cores: number; diskGb: number; saved?: number; }
export interface EmuHostInfo { sudoAll?: boolean; user: string; os: string; kernel: string; arch: string; kvm: string; vt: boolean; qemu: string; qemuImg: boolean; cpus: number; load: number; memTotal: number; memFree: number; diskFree: number; sudo: boolean; dir: string; problems: string[] | null; notes: string[] | null; osId: string; pkg: string; plan: string[] | null; canSetup: boolean; uplink: string; bridges: string[] | null; wifi: boolean; lanIf: string; lanKind: string; lanAddr?: string; gateway?: string; virt?: string; guestfs?: boolean; nat?: string; libvirt?: boolean; }
export interface EmuSetupLine { run: string; kind: string; text: string; }
export interface EmuSetupDone { run: string; error: string; host: EmuHost | null; }
export interface EmuHost { session: string; name: string; info: EmuHostInfo; checked: string; }
export interface EmuAnswer { match: string; send: string; }
export interface EmuImage { id: string; name: string; file: string; fileName: string; size: number; sha256: string; deviceType: string; cpus: number; ram: number; nics: number; nic: string; portMacs?: string; disk: string; portName: string; ready: string; login: string; password?: string; prompt: string; day0: string[] | null; answers: EmuAnswer[] | null; extra: string; source: string; serial: string; addrCmd: string; dhcpOn?: string[] | null; portAddr?: string[] | null; addrSet?: string[] | null; persona?: EmuPersonaDef | null; version?: string;
  hasPassword?: boolean; onHost?: boolean; upload?: string; missing?: boolean;  url?: string; md5?: string; page?: string; download?: string; disks?: { slot: string; fileName: string; file: string; sha256: string; size: number; url?: string; md5?: string; page?: string; bus?: string }[] | null; }
export interface EmuGroup { template?: string; diskGb?: number; model?: string; module?: string; serialStart?: string; macStart?: string; image: string; count: number; names: string; cpus?: number; ram?: number; role?: string; }
export interface EmuRequest { host: string; groups: EmuGroup[]; cabling: string; mgmt: string; mgmtNet: string; start: boolean; perPair?: number; lagMode?: string; }
export interface LabOutput { os: string; command: string; text: string; }
export interface LabGroup { os?: string; model: string; count: number; names: string; }
export interface LabRequest { groups: LabGroup[]; mgmtNet: string; cabling: string; password: string; start: boolean; }

/** What Name After Devices found for one session (state: read, nologin, norule, notfound, failed, team). */
export interface DeviceName { path: string; name: string; host: string; hostname: string; model: string; team: boolean; state: string; detail: string; }

export interface Button { label: string; send: string; toAll: boolean; action: string; script: string; }

export interface Settings {
  vaultAddr?: string; keyPassphrases?: string[];
  sessionsDir: string; logDir: string; downloadDir: string; fontFamily: string; fontSize: number; lineHeight?: number; theme: string; uiFont?: string; uiFontSize?: number;
  terminalScheme: string; cursorStyle: 'block' | 'underline' | 'bar'; cursorBlink: boolean;
  scrollback: number; copyOnSelect: boolean; rightClickPaste: boolean; confirmClose: boolean;
  language?: string; layout?: string; style?: string; iconFinish?: '' | 'glacier' | 'flat'; compactRows?: boolean; highlightStyle?: string; welcomeSeen?: string; iconStyle?: string; quickButtons?: '' | 'keycap' | 'glow'; folderColors?: '' | 'vivid' | 'steel'; nameAfterDevice?: '' | 'auto' | 'off'; nameFormat?: '' | 'model' | 'octet'; deviceFacts?: Record<string, Record<string, string>> | null; showButtonBar: boolean; hideTerminalBar?: boolean; showCommandWindow: boolean; showSessionManager: boolean; sessionManagerRight?: boolean;
  sftpCwdAuto?: boolean; sftpFollowTerminal?: boolean; sftpFollowUser?: boolean;
  pasteLineDelayMs: number; buttons: Button[]; recentSessions: string[];
  favorites?: string[] | null; lastConnected?: Record<string, string> | null; showReachability?: boolean; reachFolder?: string;
  backupDir?: string; backupJobs?: BackupJob[] | null;
  checklists?: Checklist[] | null; checklistDir?: string; recordHistory?: boolean | null;
  recordSessions?: boolean; recordDir?: string; recordKeepDays?: number;
  smartCardLibraries?: string[] | null; smartCardPinEachTime?: boolean;
  collections?: Collection[] | null; collectDir?: string;
  keywordSets: KeywordSet[] | null; defaultKeywordSet: string; showConnectBar: boolean; keyMap: KeyBinding[] | null;
  customSchemes: { name: string; colors: Record<string, string> }[] | null; accentColor: string;
  restoreTabs: boolean; openTabs: string[] | null; inWindowMenu: boolean; forceTerminalScheme: boolean; pythonPath: string;
  deviceTypes: DeviceType[] | null; alerts: AlertRule[] | null; confirmPaste: boolean; quickConnectSave?: boolean; noMoveConfirm?: boolean; noDeleteConfirm?: boolean;
  snippets: Snippet[] | null; workspaces: Workspace[] | null; folderDefaults: FolderDefault[] | null; environments?: EnvDef[] | null;
  credentials: Credential[] | null; masterHash: string;
  teams?: TeamConn[] | null; teamDisplayName?: string;
  syncFolder?: string; syncId?: string; syncOptions?: boolean;
  ai?: AISettings;
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
export interface Credential { name: string; username: string; auth?: '' | 'key'; keyFile?: string; note?: string; changed?: string; }
/** Where an account is used (AccountsInfo). */
export interface AccountInfo { name: string; sessions: number; folders: string[]; deviceTypes: string[]; password: '' | 'keychain' | 'manager'; }

export interface TabInfo {
  id: string; title: string; state: string; info: string; session: Session; logFile: string;
}

export interface PromptField { label: string; secret: boolean; value: string; }
export interface Prompt {
  id: string; tabId: string; kind: 'credentials' | 'hostkey' | 'confirm'; title: string; message: string;
  fields: PromptField[] | null; choices: string[] | null; allowRemember: boolean; rememberLabel?: string; danger: boolean;
}
export interface PromptReply { ok: boolean; values: string[]; choice: string; remember: boolean; }

export interface ImportOptions { targetFolder: string; conflict: 'rename' | 'overwrite' | 'skip'; includeDefault: boolean; configPassphrase?: string;
  /** Parts of a Termwright export's settings to import: logins, library, options. */
  settings?: string[]; }
export interface ImportResult { imported: string[] | null; renamed: string[] | null; skipped: string[] | null; errors: string[] | null; notes?: string[] | null;
  passwordsImported?: number; passwordErrors?: string[] | null; settings?: string[] | null; }
/** What the settings file of a Termwright export holds. */
export interface SettingsSummary { app: string; deviceTypes: number; accounts: number; folderDefaults: number; library: number; options: number; }

export interface AppInfo { name: string; version: string; os: string; arch: string; dataDir: string; sessionsDir: string; initError: string; locked: boolean; hasMaster: boolean; insetTitle?: boolean; }

/** Sessions of another terminal program found on this computer. */
export interface ImportSource { app: string; path: string; what: string; }

export interface FileFilter { displayName: string; pattern: string; }

export const PROTOCOLS = ['SSH2', 'Telnet', 'Serial', 'Local Shell', 'Raw', 'Console', 'RDP', 'VNC', 'WOL'] as const;
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
  'clear-screen': 'Clear Screen', 'toggle-autoscroll': 'Auto Scroll On / Off', 'toggle-timestamps': 'Timestamps On / Off', reset: 'Reset Terminal', 'log-toggle': 'Start/Stop Log', 'save-scrollback': 'Save Scrollback', 'save-scrollback-html': 'Save Scrollback As HTML', 'copy-scrollback': 'Copy Scrollback', 'copy-scrollback-html': 'Copy Scrollback as HTML Report',
  'toggle-session-manager': 'Toggle Session Manager', 'toggle-session-manager-side': 'Move Session Manager Left / Right', 'toggle-command-window': 'Toggle Command Window',
  'toggle-button-bar': 'Toggle Button Bar', 'files-panel': 'Files Panel Beside the Terminal', 'web-page': 'Open Web Page Through This Session', wake: 'Wake Up (Wake-on-LAN)', 'sftp-tab': 'Open SFTP Tab', 'run-script': 'Run Script...',
  'record-toggle': 'Start/Stop Script Recording', 'cast-toggle': 'Start/Stop Session Recording', 'cast-marker': 'Add Marker to Recording', recordings: 'Recordings...', 'cancel-scripts': 'Cancel Scripts', lock: 'Lock Session',
  'layout-tabs': 'Tabbed View', 'layout-tile-v': 'Tile Side by Side', 'layout-tile-h': 'Tile Stacked', 'layout-grid': 'Tile Grid',
  'send-break': 'Send Break', 'send-file': 'Send Text File...', 'save-selection': 'Save Selection As...',
  'xmodem': 'Send XMODEM...', 'xmodem-receive': 'Receive XMODEM...', 'scp-upload': 'SCP Upload File...', 'scp-upload-folder': 'SCP Upload Folder...', 'scp-download': 'SCP Download...',
  'ymodem-send': 'Send YMODEM...', 'ymodem-receive': 'Receive YMODEM', passwords: 'Passwords & Accounts...', 'check-updates': 'Check for Updates...', 'key-setup': 'Set Up Key Login...', 'remote-desktop': 'Set Up Remote Desktop...', 'quick-open': 'Quick Open...', welcome: 'Welcome...', 'new-session': 'New Session...', 'capture-screen': 'Save Screen As Image...', 'teach-fact': 'Save Selection as Device Fact...', 'toggle-icon-style': 'Tile / Glyph Icons', 'command-library': 'Command Library...', 'net-check': 'Network Check...', 'sim-lab': 'Simulated Lab...', 'workspaces': 'Workspaces...',
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

/** What an export for another program wrote and left out. */
export interface ExportReport { count: number; skipped: string[]; notes: string[]; }

/** A serial port found on this computer. */
/** An environment (Settings → Environments). */
export interface EnvDef { id: string; label: string; short: string; color: string; protect?: boolean; }
/** Something that uses a session as its jump host: a session, or a folder whose default it is. */
export interface JumpUse { path: string; name: string; protocol: string; folder: boolean; jump: string; }
/** A network this computer has an address on (Discover Devices). */
export interface LocalNet { interface: string; address: string; network: string; scan: string; hosts: number; vpn: boolean; note?: string; }
export interface SerialPortInfo { name: string; label: string; usb: boolean; serial?: string; }

/** Model, OS version, serial number and base MAC address of a device (empty when unknown). */
export interface DeviceFacts { model: string; version: string; serial: string; mac: string; }

/** What the Set Up Remote Desktop dialog asks the backend to do (backend RemoteDesktopRun). */
export interface RemoteDesktopRun {
  tabId: string; rdp: boolean; vnc: boolean; rdpUser: string; vncUser: string; createUsers: boolean;
  rdpNetwork: boolean; vncNetwork: boolean; cidr: string; check: boolean; force: boolean;
  sudoSame: boolean; sudoNone: boolean; sudoPassword: string;
  accountSame: boolean; accountPassword: string; vncSame: boolean; vncPassword: string;
}

/** A team server this Termwright joined (its key is in the keychain). */
export interface TeamConn { id: string; name: string; address: string; fingerprint: string; memberId: string; role: TeamRole; host?: string; container?: string; }
export type TeamRole = 'owner' | 'co-owner' | 'member';
export interface TeamView extends TeamConn { reachable: boolean; error: string; members: number; online: number; version: string;
  /** the version to update the team server to, when it is older than this Termwright */ update?: string; newer?: boolean;
  /** single sign-on asks to sign in again */ signIn?: boolean; }
export interface TeamMember { id: string; name: string; user: string; machine: string; role: TeamRole; joined: string; lastSeen: string; online: boolean; }
export interface TeamDetails { id: string; name: string; version: string; me: string; role: TeamRole; invite?: string; inviteLink: string; address: string; members: TeamMember[]; }
export interface TeamInfo { name: string; version: string; members: number; online: number; owner: boolean;
  /** single sign-on: the company account's name ("Microsoft"); inviteToo: the invite key works as well */ sso?: string; inviteToo?: boolean; }
export interface TeamSetUpStep { run: string; step: string; state: 'run' | 'ok' | 'fail' | 'info'; text: string; }
export interface TeamSetUpResult { run: string; teamId: string; invite: string; error: string; }
export interface TeamDevice { id?: string; name: string; folder: string; host: string; port: number; protocol: string; deviceType: string; mac: string; description: string; }
export interface TeamPushItem extends TeamDevice { path: string; }
export interface TeamPushResult { status: 'new' | 'added' | 'exists' | 'pending' | 'invalid'; name: string; folder: string; msg?: string; }

/** Sync between your own computers through a shared folder. */
export interface SyncStatus { folder: string; last: string; peers: string[] | null; took: number; error: string; }

/** The AI assistant (optional): the provider and model; the API key is in the OS keychain. */
export interface AISettings { provider?: '' | 'anthropic' | 'openai' | 'ollama' | 'claudecode'; model?: string; baseUrl?: string; hideHosts?: boolean; mcp?: boolean; mcpCommands?: '' | 'never'; api?: boolean; agentPerm?: '' | 'every' | 'readonly' | 'auto'; effort?: string; agentToolsOff?: string[]; }
export interface MCPInfo { running: boolean; command: string; codeCmd: string; desktopCfg: string; inDesktop: boolean; inCode?: boolean; apiUrl?: string; keyFile?: string; }
export interface FileServersSettings { folder: string; address: string; tftpPort: number; ftpPort: number; httpPort: number; tftpWrite: boolean; ftpWrite: boolean;
  ftpUser: string; idleMinutes: number; commands?: ServerCommand[] | null; }
export interface ServerCommand { deviceType: string; protocol: string; put?: boolean; text: string; }
export interface FileServer { kind: 'tftp' | 'ftp' | 'http'; running: boolean; port: number; address: string; error?: string; }
export interface FileServersState { servers: FileServer[]; folder: string; stopped?: string; }
export interface FileTransfer { id: number; server: string; peer: string; file: string; put: boolean; state: 'running' | 'done' | 'error' | 'refused';
  bytes: number; size: number; error?: string; start: string; end?: string; }
export interface FileServersView { settings: FileServersSettings; state: FileServersState; transfers: FileTransfer[]; }
export interface FSAddress { ip: string; label: string; reaches: boolean; }
export interface ServedFile { name: string; size: number; }
export interface NCResult { target: string; address: string; state: 'ok' | 'bad' | 'warn' | 'na'; summary: string; ms: number; detail: string; }
export interface IperfTest { peer: string; seconds: number; sentBps: number; receivedBps: number; retransmits: number; time: string; }
export interface IperfInfo { path: string; running: boolean; port: number; last?: IperfTest | null; error?: string; }
export interface FirewallState { name: string; on: 'on' | 'off' | 'unknown'; app: 'allowed' | 'blocked' | 'ask' | 'unknown'; notes: string[] | null; fix: string; canFix: boolean; program: string; }
export interface AIStatus { provider: string; model: string; hasKey: boolean; ready: boolean; }
export interface AIAnswer { text: string; commands: string[]; sent: string; removed: number; model: string; usage?: { in: number; out: number; model?: string } | null; }

export interface EmuMatrixHost { session: string; name: string; addr: string; kvm: string; vt: boolean; virt: string; qemu: string; qemuImg: boolean; guestfs: boolean; sudo: boolean; canSetup: boolean; problems: string[] | null; cpus: number; load: number; memTotal: number; memFree: number; diskTotal: number; diskFree: number; imagesDir: string; error: string; }
export interface EmuMatrixImage { id: string; name: string; version: string; fileName: string; size: number; local: boolean; switches: number; }
export interface EmuReach { from: string; to: string; udp: string; tcp: string; text: string; at: string; }
export interface EmuMatrix { hosts: EmuMatrixHost[] | null; images: EmuMatrixImage[] | null; cells: Record<string, Record<string, string>> | null; reach: EmuReach[] | null; busy: boolean; checking: boolean; syncAll: boolean; keep20: boolean; }
export interface EmuRemoveInfo { switches: number; imagesBytes: number; imagesDir: string; labBytes: number; labDirs: number; sudoRule: boolean; group: boolean; pkgs: string[] | null; fullSudo: boolean; error: string; }
export interface EmuRemoveHostReq { host: string; images: boolean; labs: boolean; sudoRule: boolean; group: boolean; pkgs: boolean; password: string; forgetOff: boolean; }
export interface EmuCapture { key: string; vm: string; switch: string; port: string; peer: string; host: string; file: string; started: string; running: boolean; size: number; }
export interface EmuPacket { no: number; time: number; src: string; dst: string; proto: string; len: number; info: string; }
/** A decoded field of a packet: off and n are its bytes. */
export interface EmuField { name: string; value?: string; off: number; n: number; kids?: EmuField[] | null; }
export interface EmuPacketDetail extends EmuPacket { at: string; hex: string; fields: EmuField[] | null; total: number; }

/** A test port of the traffic generator: a sender and counter on a switch's free port. */
export interface EmuTester { id: string; name: string; vm: string; port: number; portName?: string; mac: string; ip?: string; ip6?: string; groups?: string[] | null; dhcp?: boolean; slaac?: boolean; lldp?: boolean; lease?: string; lease6?: string; gw?: string; vlan?: number; speed?: number; lag?: string; lacpFast?: boolean; lacpUp?: string; neighbor?: string; x?: number; y?: number;
  clients?: EmuClients | null; stp?: EmuSTP | null; clientState?: EmuClientState | null; stpState?: EmuSTPState | null; }
/** Many hosts behind a test port (802.1X, DHCP, multicast). */
export interface EmuClients { n: number; mac?: string; rate?: number; dhcp?: boolean; groups?: string[] | null; dot1x?: { user?: string; password?: string } | null; }
export interface EmuClientState { n: number; bound: number; auth: number; failed: number; list?: string[][] | null; }
/** Spanning tree on a test port. */
export interface EmuSTP { mode: 'stp' | 'rstp'; prio?: number; tc?: number; }
export interface EmuSTPState { sent: number; got: number; root?: string; bridge?: string; cost?: number; role?: string; rootIsUs?: boolean; tc?: boolean; }
/** The lab's RADIUS server. */
export interface EmuRadiusUser { user: string; password: string; vlan?: string; }
export interface EmuRadiusLogin { at: number; switch: string; user: string; how: string; result: string; vlan?: string; }
export interface EmuRadiusHost { host: string; address: string; running: boolean; err?: string; accept: number; reject: number; challenge: number; acct: number; logins: EmuRadiusLogin[]; }
export interface EmuRadiusView { on: boolean; port: number; secret?: string; users?: EmuRadiusUser[] | null; mab?: string[] | null; mabVlan?: string; hosts: EmuRadiusHost[]; }
/** A stream of frames between test ports. */
export interface EmuStream { id: string; name: string; from: string; to?: string; kind: 'udp' | 'icmp' | 'tcp' | 'arp' | 'raw' | 'pcap'; vlan?: number; prio?: number; dstMac?: string; srcIp?: string; dstIp?: string; dport?: number; size: number; pps: number; seconds?: number; count?: number; maxLoss: number; maxLatency?: number; off?: boolean; dscp?: number; sizeMode?: '' | 'inc' | 'random' | 'imix'; sizeMax?: number; burst?: number; mods?: EmuMod[] | null; maxJitter?: number; rateUnit?: '' | 'mbps' | 'pct'; rate?: number; rtt?: boolean; pcap?: string; pcapId?: string; pcapFrames?: number; pcapSecs?: number; pcapTiming?: boolean; loops?: number; rewriteMacs?: boolean; }
/** A saved traffic test (RFC 2544, RFC 2889 learning, convergence). */
export interface EmuTest { id: string; name: string; kind: 'throughput' | 'latency' | 'frame-loss' | 'back-to-back' | 'learning' | 'convergence' | 'iperf'; from: string; to: string; both?: boolean; monitor?: string; vlan?: number; sizes?: number[] | null; trial?: number; maxPps?: number; rate?: number; loss?: number; resolution?: number; max?: number; action?: '' | 'pull' | 'stop' | 'restart'; switch?: string; port?: string; at?: number; hold?: number; seconds?: number; server?: string; client?: string; serverIp?: string; proto?: 'tcp' | 'udp'; bandwidth?: string; parallel?: number; reverse?: boolean; }
export interface EmuTestRow { dir?: string; size?: number; pps?: number; mbps?: number; load?: number; loss: number; latMin?: number; latAvg?: number; latMax?: number; jitter?: number; burst?: number; addresses?: number; lost?: number; outageMs?: number; retrans?: number; trials?: number; note?: string; }
export interface EmuTestRun { id: string; test: string; name: string; kind: string; status: 'running' | 'done' | 'failed' | 'stopped'; step?: string; started: string; ended?: string; rows: EmuTestRow[]; log?: string[] | null; error?: string; }
/** A field of a stream's frames that changes from frame to frame. */
export interface EmuMod { field: 'srcMac' | 'dstMac' | 'srcIp' | 'dstIp' | 'vlan' | 'sport' | 'dport'; mode: 'inc' | 'random'; count: number; step?: number; }
export interface EmuStreamResult { id: string; tx: number; rx: number; expected: number; other: number; loss: number; latAvg: number; latMax: number; latMin?: number; jitter?: number; ooo?: number; dup?: number; txRate: number; rxRate: number; running: boolean; result: '' | 'pass' | 'fail'; why?: string;
  p50?: number; p95?: number; p99?: number; hist?: { lo: number; hi: number; n: number }[]; flows?: EmuFlowResult[]; flowsN?: number; flowsLossy?: number; marks?: { vlan: number; prio: number; dscp: number; n: number }[]; txMbps?: number; rxMbps?: number; }
/** One flow of a stream with changing fields: one combination of their values. */
export interface EmuFlowResult { flow: number; name: string; tx: number; rx: number; loss: number; latAvg: number; latMax: number; }
/** A lab tool the AI chat called: done (out), or to be asked first. */
export interface EmuToolStep { ask?: boolean; title?: string; text?: string; danger?: boolean; out?: string; }
export interface EmuTrafficView { testers: EmuTester[]; streams: EmuStream[]; results: EmuStreamResult[]; running: boolean; started?: string; note?: string; history?: Record<string, [number, number, number, number?, number?][]> | null; }

/** Switches running on a host, of one topology (open or in the background). */
export interface EmuRunRow { host: string; topology: string; open: boolean; switches: string[]; }

/** A device's CPU over the last minutes (millicores, as kubectl counts: 1000m = one host core). */
export interface EmuCpuStats { mins: number; avg: number; peak: number; p95: number; over: number; overN: number; thr: number; thrS: number; }

/** A change Claude or the API made to the emulated lab, kept with the lab as it was before (Revert). */
export interface EmuChange { id: string; at: string; who: string; tool: string; what: string; result: string; revertible: boolean; note?: string; reverted?: string; }

/** A ready test of the traffic generator: streams and saved tests between test ports named by role (A, B, C…). */
export interface EmuTrafficTemplate { id: string; name: string; desc?: string; builtin?: boolean; ports: number; roles?: string[] | null; streams?: EmuStream[] | null; tests?: EmuTest[] | null; saved?: string; }

/** A note or an area on the lab's canvas. */
export interface LabNote { id: string; kind: 'note' | 'area'; text: string; x: number; y: number; w: number; h: number; color?: number; }

/** A hub or an L2 switch of the emulated lab (no VM): the switch ports on it. */
export interface LabHub { id: string; name: string; kind: 'hub' | 'switch'; x: number; y: number; legs: { vm: string; port: number; at: number; name: string; portName: string }[]; ports: number; cfg?: LabHubCfg | null; console?: string; ssh?: string; conPort?: number; }
export interface EmuRouting { proto: 'ospf' | 'bgp'; port: number; routes: number; prefix: string; area?: string; as?: number; peer?: string; peerAs?: number; }
export interface EmuRoutingView extends EmuRouting { state: string; up: boolean; prefixes: number; first: string; step: number; raw: string; }
export interface LabHubPort { mode: 'access' | 'trunk'; vlan?: number; vlans?: number[] | null; }
export interface LabHubCfg { rev: number; ports?: Record<string, LabHubPort> | null; mgmtIp?: string; mgmtVlan?: number; }
export interface LabHubDetail { id: string; running: boolean; in: number[]; macs: { mac: string; vlan: number; port: number; age: number }[]; cfg?: LabHubCfg | null; sshOk: boolean; error?: string; }
