import SHORTCUTS from './shortcuts.json';

/** In-window menu bar definition; mirrors the native menu built in main.go. */
export interface MenuDef { label?: string; action?: string; keys?: string; sep?: boolean; }

/** The shortcut of a menu action as shown in menus ("⌘D", "⌘⇧K", "Ctrl+Shift+D", "Alt+Shift+K"). */
export function shortcutOf(action: string, isMac: boolean): string {
  const v = (SHORTCUTS as Record<string, string>)[action];
  if (!v || action.startsWith('_')) return '';
  if (v.startsWith('+')) return isMac ? '⌘⇧' + v.slice(1) : 'Alt+Shift+' + v.slice(1);
  return isMac ? '⌘' + v : 'Ctrl+Shift+' + v;
}

export function menuBar(isMac: boolean): { title: string; items: MenuDef[] }[] {
  const k = (c: string) => (isMac ? '⌘' + c : 'Ctrl+Shift+' + c);
  const menus = [
    { title: 'File', items: [
      { label: 'Connect...', action: 'connect', keys: k('O') }, { label: 'Quick Connect...', action: 'quick-connect', keys: k('Q') },
      { label: 'Connect Local Shell', action: 'local-shell', keys: k('L') }, { label: 'New Session...', action: 'new-session' }, { sep: true },
      { label: 'Reconnect', action: 'reconnect', keys: k('R') }, { label: 'Disconnect', action: 'disconnect' },
      { label: 'Clone Session', action: 'clone-tab' }, { sep: true },
      { label: 'Workspaces...', action: 'workspaces' }, { label: 'Save Workspace...', action: 'save-workspace' }, { sep: true },
      { label: 'Log Session...', action: 'log-toggle' }, { label: 'Record Session', action: 'cast-toggle' }, { label: 'Add Marker to Recording...', action: 'cast-marker', keys: k('J') }, { label: 'Recordings...', action: 'recordings' }, { label: 'Save Scrollback As...', action: 'save-scrollback', keys: k('S') }, { label: 'Save Scrollback As HTML...', action: 'save-scrollback-html' },
      { label: 'Save Selection As...', action: 'save-selection' }, { label: 'Save Screen As Image...', action: 'capture-screen' }, { sep: true },
      { label: 'Import Sessions...', action: 'import' }, { label: 'Discover Devices...', action: 'discover' }, { label: 'Configuration Backups...', action: 'backups' }, { label: 'Change Checklists...', action: 'checklists' }, { label: 'Run and Collect...', action: 'collect' }, { label: 'Export Sessions...', action: 'export' }, { sep: true },
      { label: 'Close Tab', action: 'close-tab', keys: k('W') },
    ] },
    { title: 'Edit', items: [
      { label: 'Copy', action: 'copy', keys: k('C') }, { label: 'Paste', action: 'paste', keys: k('V') },
      { label: 'Copy and Paste', action: 'copy-paste' }, { label: 'Select All', action: 'select-all', keys: k('A') },
      { label: 'Find...', action: 'find', keys: k('F') }, { label: 'Save Selection as Device Fact...', action: 'teach-fact' }, { sep: true },
      { label: 'Clear Scrollback', action: 'clear-scrollback' }, { label: 'Clear Screen', action: 'clear-screen' },
      { label: 'Reset Terminal', action: 'reset' }, { sep: true },
      { label: 'Timestamps On / Off', action: 'toggle-timestamps' }, { label: 'Auto Scroll On / Off', action: 'toggle-autoscroll' },
    ] },
    { title: 'View', items: [
      { label: 'Quick Open...', action: 'quick-open', keys: k('G') }, { sep: true },
      { label: 'Session Manager', action: 'toggle-session-manager', keys: k('M') }, { label: 'Session Manager Left / Right', action: 'toggle-session-manager-side' }, { label: 'Command Window', action: 'toggle-command-window', keys: k('K') }, { label: 'Files Panel', action: 'files-panel' },
      { label: 'Button Bar', action: 'toggle-button-bar', keys: k('B') }, { label: 'Connect Bar', action: 'toggle-connect-bar' },
      { label: 'Menu Bar in Window', action: 'toggle-window-menu' }, { label: 'Modern / Classic Layout', action: 'toggle-layout' }, { label: 'Tile / Glyph Icons', action: 'toggle-icon-style' }, { sep: true },
      { label: 'Tabbed', action: 'layout-tabs' }, { label: 'Tile Side by Side', action: 'layout-tile-v' },
      { label: 'Tile Stacked', action: 'layout-tile-h' }, { label: 'Tile Grid', action: 'layout-grid' }, { sep: true },
      { label: 'Zoom In', action: 'zoom-in', keys: isMac ? '⌘=' : 'Ctrl+=' }, { label: 'Zoom Out', action: 'zoom-out', keys: isMac ? '⌘-' : 'Ctrl+-' },
      { label: 'Reset Zoom', action: 'zoom-reset', keys: isMac ? '⌘0' : 'Ctrl+0' },
    ] },
    { title: 'Transfer', items: [
      { label: 'Open SFTP Tab', action: 'sftp-tab', keys: k('P') }, { label: 'Send File with XMODEM...', action: 'xmodem' },
      { label: 'Receive File with XMODEM...', action: 'xmodem-receive' },
      { label: 'Send Files with YMODEM...', action: 'ymodem-send' }, { label: 'Receive Files with YMODEM', action: 'ymodem-receive' },
      { label: 'Send Text File...', action: 'send-file' }, { sep: true },
      { label: 'Open Web Page Through This Session...', action: 'web-page' }, { sep: true },
      { label: 'SCP Upload File...', action: 'scp-upload' }, { label: 'SCP Upload Folder...', action: 'scp-upload-folder' },
      { label: 'SCP Download...', action: 'scp-download' },
    ] },
    { title: 'Script', items: [
      { label: 'Command Library...', action: 'command-library', keys: k('E') }, { label: 'Command History...', action: 'history', keys: k('Y') }, { sep: true },
      { label: 'Run Script...', action: 'run-script' }, { label: 'Cancel Running Scripts', action: 'cancel-scripts' }, { sep: true },
      { label: 'Start / Stop Script Recording', action: 'record-toggle' },
    ] },
    { title: 'Options', items: [
      { label: 'Session Options...', action: 'session-options' }, { label: 'Global Options...', action: 'global-options', keys: isMac ? '⌘,' : 'Ctrl+,' },
      { sep: true }, { label: 'Passwords & Credentials...', action: 'passwords' },
      { label: 'Set Up Key Login / Generate Key...', action: 'key-setup' },
    ] },
    { title: 'Tab', items: [
      { label: 'Compare Outputs...', action: 'compare' }, { sep: true },
      { label: 'Next Tab', action: 'next-tab', keys: 'Ctrl+Tab' }, { label: 'Previous Tab', action: 'prev-tab', keys: 'Ctrl+Shift+Tab' },
      { label: 'Send Command to All Tabs...', action: 'send-all' }, { label: 'Send Break', action: 'send-break' }, { sep: true },
      { label: 'Lock Session', action: 'lock' }, { label: 'Lock All Sessions', action: 'lock-all' },
    ] },
    { title: 'Help', items: [
      { label: 'Welcome...', action: 'welcome' }, { label: 'User Guide', action: 'user-guide' }, { label: 'Keyboard Shortcuts', action: 'shortcuts' }, { label: 'Check for Updates...', action: 'check-updates' },
      { label: 'About Termwright', action: 'about' },
    ] },
  ];
  for (const m of menus) for (const i of m.items as MenuDef[]) if (i.action) i.keys = shortcutOf(i.action, isMac) || i.keys;
  return menus;
}
