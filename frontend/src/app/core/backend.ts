import { Injectable } from '@angular/core';
import {
  AccountInfo, JumpUse, LocalNet, RemoteDesktopRun,  SerialPortInfo, DeviceFacts, AppInfo, BackupFile, ExportReport, BackupStatus, CheckRun, FileEntry, HistoryEntry, FileFilter, ImportOptions, ImportResult, MovedDevice, PromptReply, RecordingData, RecordingInfo, RemoteInfo, SmartCardInfo, Session, SessionNode, Settings, TabInfo, UpdateStatus, ImportSource, SettingsSummary, TeamConn, TeamDetails, TeamInfo, TeamPushItem, TeamPushResult, TeamView, SyncStatus, AIAnswer, AISettings, AIStatus, FileServersView, FileServersSettings, ServedFile, FSAddress, MCPInfo, FirewallState, NCResult, IperfInfo, IperfTest, DeviceName, LabView, LabRequest, LabLink, LabOutput, LabPack, EmuHost, EmuImage, EmuRequest } from './models';

/* The Wails runtime injects window.go (bound Go methods) and window.runtime. */
type GoApp = Record<string, (...args: unknown[]) => Promise<unknown>>;
interface WailsRuntime {
  EventsOn(name: string, cb: (...data: any[]) => void): () => void;
  ClipboardGetText(): Promise<string>;
  ClipboardSetText(text: string): Promise<boolean>;
  BrowserOpenURL(url: string): void;
  WindowSetTitle(title: string): void;
}
declare global {
  interface Window { go?: { main?: { App?: GoApp } }; runtime?: WailsRuntime; }
}

/** Typed wrapper around the Go backend bound by Wails. */
export type SftpMode = '' | 'jump' | 'target';

@Injectable({ providedIn: 'root' })
export class Backend {
  get available(): boolean { return !!window.go?.main?.App; }

  private call<T>(method: string, ...args: unknown[]): Promise<T> {
    const app = window.go?.main?.App;
    if (!app) return Promise.reject(new Error('Backend not available - run inside the Termwright app (wails dev / wails build).'));
    return app[method](...args) as Promise<T>;
  }

  on(event: string, cb: (...data: any[]) => void): () => void {
    return window.runtime ? window.runtime.EventsOn(event, cb) : () => {};
  }

  clipboardGet(): Promise<string> {
    return window.runtime ? window.runtime.ClipboardGetText() : navigator.clipboard.readText();
  }
  clipboardSet(text: string): Promise<unknown> {
    return window.runtime ? window.runtime.ClipboardSetText(text) : navigator.clipboard.writeText(text);
  }
  openURL(url: string) { window.runtime?.BrowserOpenURL(url); }

  getAppInfo() { return this.call<AppInfo>('GetAppInfo'); }
  titleBarDoubleClick() { return this.call<void>('TitleBarDoubleClick'); }
  windowButtons() { return this.call<{ centerY: number; right: number } | null>('WindowButtons'); }
  downloadCount() { return this.call<number>('DownloadCount'); }
  quit() { return this.call<void>('Quit'); }
  getSettings() { return this.call<Settings>('GetSettings'); }
  accountsInfo() { return this.call<AccountInfo[] | null>('AccountsInfo'); }
  accountSessions(name: string) { return this.call<string[] | null>('AccountSessions', name); }
  renameAccount(oldName: string, newName: string) { return this.call<void>('RenameAccount', oldName, newName); }
  accountCandidates() { return this.call<{ username: string; paths: string[] }[] | null>('AccountCandidates'); }
  convertToAccount(name: string, paths: string[]) { return this.call<void>('ConvertToAccount', name, paths); }
  saveSettings(s: Settings) { return this.call<void>('SaveSettings', s); }

  getSessionTree() { return this.call<SessionNode>('GetSessionTree'); }
  getSession(path: string) { return this.call<Session>('GetSession', path); }
  newSessionDefaults() { return this.call<Session>('NewSessionDefaults'); }
  saveSession(s: Session, oldPath: string) { return this.call<Session>('SaveSession', s, oldPath); }
  setNoCursorReport(path: string, on: boolean) { return this.call<void>('SetNoCursorReport', path, on); }
  deleteSession(path: string) { return this.call<void>('DeleteSession', path); }
  labState() { return this.call<LabView>('LabState'); }
  labPackInfo(path: string) { return this.call<LabPack>('LabPackInfo', path); }
  labPackAccept(path: string, hash: string) { return this.call<LabView>('LabPackAccept', path, hash); }
  labPackRemove() { return this.call<LabView>('LabPackRemove'); }
  emuState() { return this.call<LabView>('EmuState'); }
  emuCheckHost(session: string) { return this.call<EmuHost>('EmuCheckHost', session); }
  emuRemoveHost(session: string) { return this.call<LabView>('EmuRemoveHost', session); }
  emuAddImage(path: string) { return this.call<LabView>('EmuAddImage', path); }
  emuImportAppliance(path: string) { return this.call<LabView>('EmuImportAppliance', path); }
  emuSetImageFile(id: string, path: string) { return this.call<LabView>('EmuSetImageFile', id, path); }
  emuSaveImage(im: EmuImage, clearPassword: boolean) { return this.call<LabView>('EmuSaveImage', im, clearPassword); }
  emuRemoveImage(id: string) { return this.call<LabView>('EmuRemoveImage', id); }
  emuUpload(id: string, host: string) { return this.call<LabView>('EmuUpload', id, host); }
  emuAdd(r: EmuRequest) { return this.call<LabView>('EmuAdd', r); }
  emuStart(ids: string[]) { return this.call<LabView>('EmuStart', ids); }
  emuStop(ids: string[]) { return this.call<LabView>('EmuStop', ids); }
  emuRestart(ids: string[]) { return this.call<LabView>('EmuRestart', ids); }
  emuReset(ids: string[]) { return this.call<LabView>('EmuReset', ids); }
  emuRemove(ids: string[], sessions: boolean) { return this.call<LabView>('EmuRemove', ids, sessions); }
  emuConnect(l: LabLink) { return this.call<LabView>('EmuConnect', l); }
  emuDisconnect(id: string, port: string) { return this.call<LabView>('EmuDisconnect', id, port); }
  emuSetLink(id: string, port: string, up: boolean) { return this.call<LabView>('EmuSetLink', id, port, up); }
  emuPlace(id: string, x: number, y: number) { return this.call<void>('EmuPlace', id, x, y); }
  emuFreePorts(id: string) { return this.call<string[] | null>('EmuFreePorts', id); }
  emuConsoleTail(id: string) { return this.call<string>('EmuConsoleTail', id); }
  labAdd(r: LabRequest) { return this.call<LabView>('LabAdd', r); }
  labStart(ids: string[]) { return this.call<LabView>('LabStart', ids); }
  labStop(ids: string[]) { return this.call<LabView>('LabStop', ids); }
  labRemove(ids: string[], sessions: boolean) { return this.call<LabView>('LabRemove', ids, sessions); }
  labConnect(l: LabLink) { return this.call<LabView>('LabConnect', l); }
  labDisconnect(id: string, port: string) { return this.call<LabView>('LabDisconnect', id, port); }
  labPlace(id: string, x: number, y: number) { return this.call<void>('LabPlace', id, x, y); }
  labFreePorts(id: string) { return this.call<string[] | null>('LabFreePorts', id); }
  labSetIdentity(id: string, serial: string, mac: string) { return this.call<LabView>('LabSetIdentity', id, serial, mac); }
  labSetSerials(ids: string[], serials: string[]) { return this.call<LabView>('LabSetSerials', ids, serials); }
  labSetOutputs(o: LabOutput[]) { return this.call<LabView>('LabSetOutputs', o); }
  labShow(id: string, cmd: string) { return this.call<string>('LabShow', id, cmd); }
  readDeviceNames(paths: string[]) { return this.call<DeviceName[]>('ReadDeviceNames', paths); }
  renameSessions(items: { path: string; name: string }[]) { return this.call<{ old: string; path: string; error?: string }[]>('RenameSessions', items); }
  refreshFacts(path: string, all: boolean) { return this.call<Record<string, string> | null>('RefreshFacts', path, all); }
  runFactCommand(path: string, deviceType: string, command: string) { return this.call<string>('RunFactCommand', path, deviceType, command); }
  setSessionFacts(path: string, f: Record<string, string>) { return this.call<Record<string, string> | null>('SetSessionFacts', path, f); }
  clearRecent(path: string) { return this.call<Settings>('ClearRecent', path); }
  copySessionAs(path: string, openAs: string) { return this.call<Session>('CopySessionAs', path, openAs); }
  duplicateSession(path: string) { return this.call<string>('DuplicateSession', path); }
  createFolder(path: string) { return this.call<void>('CreateFolder', path); }
  renameFolder(oldPath: string, newPath: string) { return this.call<void>('RenameFolder', oldPath, newPath); }
  deleteFolder(path: string) { return this.call<void>('DeleteFolder', path); }
  forgetPassword(path: string) { return this.call<void>('ForgetPassword', path); }
  findImportSources() { return this.call<ImportSource[] | null>('FindImportSources'); }
  importSessions(src: string, opt: ImportOptions) { return this.call<ImportResult>('ImportSessions', src, opt); }
  importNetBox(url: string, token: string, filter: string, opt: ImportOptions) { return this.call<ImportResult>('ImportNetBox', url, token, filter, opt); }
  importAWS(profile: string, region: string, usePublic: boolean, opt: ImportOptions) { return this.call<ImportResult>('ImportAWS', profile, region, usePublic, opt); }
  exportSessions(paths: string[], dest: string, format: string, settings: boolean) { return this.call<number>('ExportSessions', paths, dest, format, settings); }
  exportSettingsIn(src: string) { return this.call<SettingsSummary | null>('ExportSettingsIn', src); }
  exportSessionsFor(paths: string[], dest: string, format: string) { return this.call<ExportReport>('ExportSessionsFor', paths, dest, format); }
  listSerialPorts() { return this.call<string[]>('ListSerialPorts'); }
  deviceFacts(path: string, deviceType: string, text: string, fetch: boolean) { return this.call<DeviceFacts>('DeviceFacts', path, deviceType, text, fetch); }
  serialPortList() { return this.call<SerialPortInfo[]>('SerialPortList'); }
  detectSerialBaud(s: Session) { return this.call<{ baud: number; tried: { baud: number; bytes: number; score: number }[] }>('DetectSerialBaud', s); }
  defaultSSHConfigPath() { return this.call<string>('DefaultSSHConfigPath'); }

  openTab(path: string) { return this.call<TabInfo>('OpenTab', path); }
  openAdHoc(s: Session) { return this.call<TabInfo>('OpenAdHoc', s); }
  openRemote(path: string, s: Session | null) { return this.call<RemoteInfo>('OpenRemote', path, s ?? {}); }
  reopenRemote(id: string) { return this.call<void>('ReopenRemote', id); }
  remoteCredentials(id: string) { return this.call<{ username: string; password: string }>('RemoteCredentials', id); }
  copyRemotePassword(id: string) { return this.call<void>('CopyRemotePassword', id); }
  remoteStatus(id: string) { return this.call<number>('RemoteStatus', id); }
  closeRemote(id: string) { return this.call<void>('CloseRemote', id); }
  backupDir() { return this.call<string>('BackupDir'); }
  backupSessions(targets: string[]) { return this.call<string[]>('BackupSessions', targets); }
  backupRun(job: string, targets: string[]) { return this.call<string>('BackupRun', job, targets); }
  backupCancel(runId: string) { return this.call<void>('BackupCancel', runId); }
  backupStatus() { return this.call<Record<string, BackupStatus>>('BackupStatus'); }
  backupFiles(path: string) { return this.call<BackupFile[]>('BackupFiles', path); }
  backupJobRuns() { return this.call<Record<string, string>>('BackupJobRuns'); }
  showInFolder(path: string) { return this.call<void>('ShowInFolder', path); }
  openPath(path: string) { return this.call<void>('OpenUpdate', path); }
  checklistDir() { return this.call<string>('ChecklistDir'); }
  checklistRuns(name: string) { return this.call<CheckRun[]>('ChecklistRuns', name); }
  checklistRun(name: string, runId: string, phase: 'before' | 'after', note: string) { return this.call<string>('ChecklistRun', name, runId, phase, note); }
  checklistCancel(name: string, runId: string) { return this.call<void>('ChecklistCancel', name, runId); }
  showChecklistFolder() { return this.call<void>('ShowChecklistFolder'); }
  collectDir() { return this.call<string>('CollectDir'); }
  collectRuns(name: string) { return this.call<CheckRun[]>('CollectRuns', name); }
  collectRun(name: string, note: string) { return this.call<string>('CollectRun', name, note); }
  collectCancel(name: string, runId: string) { return this.call<void>('CollectCancel', name, runId); }
  showCollectFolder() { return this.call<void>('ShowCollectFolder'); }
  recordingDir() { return this.call<string>('RecordingDir'); }
  recordings() { return this.call<RecordingInfo[]>('Recordings'); }
  loadRecording(path: string) { return this.call<RecordingData>('LoadRecording', path); }
  chooseRecording() { return this.call<string>('ChooseRecording'); }
  startSessionRecording(tabId: string) { return this.call<string>('StartSessionRecording', tabId); }
  stopSessionRecording(tabId: string) { return this.call<void>('StopSessionRecording', tabId); }
  addRecordingMarker(tabId: string, note: string) { return this.call<void>('AddRecordingMarker', tabId, note); }
  showRecordingFolder() { return this.call<void>('ShowRecordingFolder'); }
  smartCards(libs: string[]) { return this.call<SmartCardInfo>('SmartCards', libs); }
  forgetSmartCardPins() { return this.call<void>('ForgetSmartCardPins'); }
  checkMovedDevices(moves: MovedDevice[]) { return this.call<MovedDevice[]>('CheckMovedDevices', moves); }
  findByMac(path: string) { return this.call<MovedDevice>('FindByMAC', path); }
  moveSession(path: string, host: string) { return this.call<Session>('MoveSession', path, host); }
  rememberMacs(macs: Record<string, string>) { return this.call<number>('RememberMACs', macs); }
  replaceMac(path: string, mac: string) { return this.call<void>('ReplaceMAC', path, mac); }
  teams() { return this.call<TeamView[]>('Teams'); }
  teamIdentity() { return this.call<{ name: string; user: string; machine: string }>('TeamIdentity'); }
  teamDetails(id: string) { return this.call<TeamDetails>('TeamDetails', id); }
  teamInviteInfo(invite: string) { return this.call<TeamInfo>('TeamInviteInfo', invite); }
  teamJoin(invite: string) { return this.call<TeamConn>('TeamJoin', invite); }
  teamRotateInvite(id: string) { return this.call<string>('TeamRotateInvite', id); }
  teamSetRole(id: string, memberId: string, role: string) { return this.call<void>('TeamSetRole', id, memberId, role); }
  teamRemoveMember(id: string, memberId: string) { return this.call<void>('TeamRemoveMember', id, memberId); }
  teamLeave(id: string, force: boolean) { return this.call<void>('TeamLeave', id, force); }
  teamSetUp(r: { sessionPath: string; name: string; port: number; image: string; hostNetwork: boolean }) { return this.call<string>('TeamSetUp', r); }
  teamSetUpCancel(run: string) { return this.call<void>('TeamSetUpCancel', run); }
  teamUpdateServer(r: { teamId: string; sessionPath: string; image: string }) { return this.call<string>('TeamUpdateServer', r); }
  teamPushDevices(paths: string[], base: string) { return this.call<TeamPushItem[]>('TeamPushDevices', paths, base); }
  teamCheck(id: string, items: TeamPushItem[]) { return this.call<TeamPushResult[]>('TeamCheck', id, items); }
  teamPush(id: string, items: TeamPushItem[]) { return this.call<TeamPushResult[]>('TeamPush', id, items); }
  teamOpenAdmin(id: string) { return this.call<void>('TeamOpenAdmin', id); }
  teamPresence(devices: { host: string; port: number; protocol: string; mac?: string; name?: string; since?: string }[]) { return this.call<void>('TeamPresence', devices); }
  teamStatuses() { return this.call<{ teamId: string; team: string; status: any }[]>('TeamStatuses'); }
  teamReserve(d: { host: string; port: number; protocol: string; mac?: string; name?: string }, until: string, note: string) { return this.call<unknown>('TeamReserve', d, until, note); }
  teamRelease(teamId: string, id: string) { return this.call<void>('TeamRelease', teamId, id); }
  fileServers() { return this.call<FileServersView>('FileServers'); }
  fileServersSave(s: FileServersSettings) { return this.call<void>('FileServersSave', s); }
  fileServersStart(kind: string) { return this.call<void>('FileServersStart', kind); }
  fileServersStop(kind: string) { return this.call<void>('FileServersStop', kind); }
  fileServersClear() { return this.call<void>('FileServersClear'); }
  fileServersFiles() { return this.call<ServedFile[]>('FileServersFiles'); }
  fileServersTestFile() { return this.call<string>('FileServersTestFile'); }
  netCheckLocal(targets: string[]) { return this.call<NCResult[]>('NetCheckLocal', targets); }
  netCheckTrace(target: string) { return this.call<string>('NetCheckTrace', target); }
  iperfState() { return this.call<IperfInfo>('IperfState'); }
  iperfServerStart(port: number) { return this.call<IperfInfo>('IperfServerStart', port); }
  iperfServerStop() { return this.call<void>('IperfServerStop'); }
  iperfClient(host: string, seconds: number, both: boolean) { return this.call<IperfTest[]>('IperfClient', host, seconds, both); }
  firewallCheck() { return this.call<FirewallState>('FirewallCheck'); }
  firewallAllow() { return this.call<FirewallState>('FirewallAllow'); }
  fileServersPassword() { return this.call<string>('FileServersPassword'); }
  fileServersNewPassword() { return this.call<string>('FileServersNewPassword'); }
  fileServersChooseFolder() { return this.call<string>('FileServersChooseFolder'); }
  fileServersShowFolder() { return this.call<void>('FileServersShowFolder'); }
  fileServersAddresses(host: string) { return this.call<FSAddress[]>('FileServersAddresses', host); }
  textReply(id: string, text: string, total: number) { return this.call<void>('TextReply', id, text, total); }
  claudeCodeFind(path: string) { return this.call<string>('ClaudeCodeFind', path); }
  mcpState() { return this.call<MCPInfo>('MCPState'); }
  mcpAddToDesktop() { return this.call<void>('MCPAddToDesktop'); }
  mcpAddToClaudeCode() { return this.call<string>('MCPAddToClaudeCode'); }
  aiState() { return this.call<AIStatus>('AIState'); }
  aiSetKey(provider: string, key: string) { return this.call<void>('AISetKey', provider, key); }
  aiModels(s: AISettings) { return this.call<string[]>('AIModels', s); }
  aiChat(r: { messages: { role: string; text: string; images?: { name: string; mediaType: string; data: string }[] }[]; deviceType: string; protocol: string; agent: boolean; tabs: string[]; tools: string[]; folders: string[]; perm: string }) { return this.call<AIAnswer>('AIChat', r); }
  aiAsk(r: { kind: string; text: string; deviceType: string; protocol: string; question?: string }) { return this.call<AIAnswer>('AIAsk', r); }
  syncNow() { return this.call<SyncStatus>('SyncNow'); }
  syncState() { return this.call<SyncStatus>('SyncState'); }
  teamJoinSSO(invite: string) { return this.call<TeamConn>('TeamJoinSSO', invite); }
  teamSSOAgain(id: string) { return this.call<void>('TeamSSOAgain', id); }
  teamSSOCancel() { return this.call<void>('TeamSSOCancel'); }
  teamWatch(teamId: string, d: { host: string; port: number; protocol: string; mac?: string; name?: string }, on: boolean) { return this.call<boolean>('TeamWatch', teamId, d, on); }
  teamConnected(d: { host: string; port: number; protocol: string; mac?: string }) { return this.call<void>('TeamConnected', d); }
  importTemplate() { return this.call<string>('ImportTemplate'); }
  saveTextAs(title: string, name: string, text: string) { return this.call<string>('SaveTextAs', title, name, text); }
  historyAdd(e: Partial<HistoryEntry>) { return this.call<void>('HistoryAdd', e); }
  historyList() { return this.call<HistoryEntry[]>('HistoryList'); }
  historyClear() { return this.call<void>('HistoryClear'); }
  historyRemove(cmd: string, deviceType: string) { return this.call<void>('HistoryRemove', cmd, deviceType); }
  historyClearTypes(deviceTypes: string[]) { return this.call<void>('HistoryClearTypes', deviceTypes); }
  startTab(id: string, cols: number, rows: number) { return this.call<void>('StartTab', id, cols, rows); }
  sendInput(id: string, data: string) { return this.call<void>('SendInput', id, data); }
  sendBinary(id: string, b64: string) { return this.call<void>('SendBinary', id, b64); }
  resizeTab(id: string, cols: number, rows: number) { return this.call<void>('ResizeTab', id, cols, rows); }
  disconnectTab(id: string) { return this.call<void>('DisconnectTab', id); }
  closeTab(id: string) { return this.call<void>('CloseTab', id); }
  refreshTabSession(id: string, path = '') { return this.call<void>('RefreshTabSession', id, path); }
  startLog(id: string, path: string, append: boolean, raw: boolean) { return this.call<string>('StartLog', id, path, append, raw); }
  stopLog(id: string) { return this.call<void>('StopLog', id); }
  answerPrompt(id: string, r: PromptReply) { return this.call<void>('AnswerPrompt', id, r); }

  /** mode: '' = as the session says, 'jump' = the host logged in to, 'target' = host of its "ssh … host" remote command. */
  discoverNetworks() { return this.call<LocalNet[]>('DiscoverNetworks'); }
  discoverStart(spec: string, community: string) { return this.call<{ id: string; total: number }>('DiscoverStart', spec, community); }
  discoverStop(id: string) { return this.call<void>('DiscoverStop', id); }
  discoverOuiStatus() { return this.call<string>('DiscoverOUIStatus'); }
  discoverDownloadOui() { return this.call<number>('DiscoverDownloadOUI'); }
  sftpLoginShell(id: string) { return this.call<string>('SFTPLoginShell', id); }
  sftpOpen(tabId: string, mode: SftpMode = '') { return this.call<{ id: string; home: string; target: string }>('SFTPOpen', tabId, mode); }
  diagnoseRemoteDesktop(path: string, sudo: string) { return this.call<unknown>('DiagnoseRemoteDesktop', path, sudo); }
  restartRemoteDesktopService(path: string, which: 'RDP' | 'VNC', sudo: string) { return this.call<string>('RestartRemoteDesktopService', path, which, sudo); }
  remoteDesktopInfo(tabId: string) { return this.call<{ knownPassword: boolean; user: string }>('RemoteDesktopInfo', tabId); }
  startRemoteDesktop(run: RemoteDesktopRun) { return this.call<string>('StartRemoteDesktop', run); }
  remoteDesktopSessionPassword(id: string, path: string, kind: 'RDP' | 'VNC') { return this.call<boolean>('RemoteDesktopSessionPassword', id, path, kind); }
  rememberSessionPassword(path: string, password: string) { return this.call<void>('RememberSessionPassword', path, password); }
  forgetRemoteDesktop(id: string) { return this.call<void>('ForgetRemoteDesktop', id); }
  cancelRemoteDesktop(id: string) { return this.call<void>('CancelRemoteDesktop', id); }
  sftpConnect(path: string, mode: SftpMode = '') { return this.call<{ id: string; home: string; info: string; target: string }>('SFTPConnect', path, mode); }
  sftpSetRoot(id: string, root: boolean) { return this.call<string>('SFTPSetRoot', id, root); }
  sftpRunAs(id: string, command: string) { return this.call<{ home: string; user: string }>('SFTPRunAs', id, command); }
  screenReply(id: string, snap: { lines: string[]; row: number; col: number; rows: number; cols: number } | null) { return this.call<void>('ScreenReply', id, snap); }
  testSecretRef(ref: string) { return this.call<string>('TestSecretRef', ref); }
  forgetCachedSecrets() { return this.call<void>('ForgetCachedSecrets'); }
  setVaultToken(token: string) { return this.call<void>('SetVaultToken', token); }
  hasVaultToken() { return this.call<boolean>('HasVaultToken'); }
  sftpHopTarget(s: Session) { return this.call<string>('SFTPHopTarget', s); }
  sftpList(id: string, dir: string) { return this.call<FileEntry[] | null>('SFTPList', id, dir); }
  sftpRealPath(id: string, p: string) { return this.call<string>('SFTPRealPath', id, p); }
  sftpMkdir(id: string, p: string) { return this.call<void>('SFTPMkdir', id, p); }
  sftpRename(id: string, from: string, to: string) { return this.call<void>('SFTPRename', id, from, to); }
  sftpReadText(id: string, p: string) { return this.call<string>('SFTPReadText', id, p); }
  sftpCreateFile(id: string, p: string) { return this.call<void>('SFTPCreateFile', id, p); }
  sftpRemove(id: string, p: string) { return this.call<void>('SFTPRemove', id, p); }
  sftpClose(id: string) { return this.call<void>('SFTPClose', id); }
  /** Browsers a web page can be opened in through a session (Chrome, Edge, Chromium, Brave, Firefox). */
  /** Wake-on-LAN to the session's MAC address; with wait, watches its port for up to 3 minutes. */
  /** Wake-on-LAN session: sends the packet, then reports "wol:status" while it pings or searches the network. */
  jumpUsers(paths: string[], deep: boolean, except: string[]) { return this.call<JumpUse[]>('JumpUsers', paths, deep, except); }
  repointJumps(moves: Record<string, string>) { return this.call<number>('RepointJumps', moves); }
  clearJumpDefaults(paths: string[]) { return this.call<number>('ClearJumpDefaults', paths); }
  wakeNetworkFor(ip: string) { return this.call<string>('WakeNetworkFor', ip); }
  wakeDevice(s: Session) { return this.call<{ sent: string[]; via: string }>('WakeDevice', s); }
  wakeCancel(path: string) { return this.call<void>('WakeCancel', path); }
  pingHosts(targets: { key: string; host: string; port: number }[]) { return this.call<Record<string, { up: boolean; name?: string }>>('PingHosts', targets); }
  wakeSession(tabId: string, s: Session, wait: boolean) {
    return this.call<{ sent: string[]; via: string; target: string; awake: boolean; waited: number }>('WakeSession', tabId, s, wait);
  }
  webBrowsers() { return this.call<{ id: string; name: string }[] | null>('WebBrowsers'); }
  openWebPage(tabId: string, s: Session, through: 'session' | 'device', url: string, browser: string) {
    return this.call<{ browser: string; via: string }>('OpenWebPage', tabId, s, through, url, browser);
  }
  /** Files panel: copy a remote file to a temporary folder and open it with the system's program. */
  sftpOpenFile(id: string, remote: string) { return this.call<{ local: string; mod: number }>('SFTPOpenFile', id, remote); }
  openedFileMod(local: string) { return this.call<number>('OpenedFileMod', local); }
  sftpUploadOpened(id: string, local: string, remote: string) { return this.call<void>('SFTPUploadOpened', id, local, remote); }
  localList(dir: string) { return this.call<FileEntry[] | null>('LocalList', dir); }
  localHome() { return this.call<string>('LocalHome'); }
  localParent(p: string) { return this.call<string>('LocalParent', p); }
  localJoin(dir: string, name: string) { return this.call<string>('LocalJoin', dir, name); }
  localMkdir(p: string) { return this.call<void>('LocalMkdir', p); }
  localCreateFile(p: string) { return this.call<void>('LocalCreateFile', p); }
  localTrash(paths: string[]) { return this.call<void>('LocalTrash', paths); }
  startTransfer(sftpId: string, dir: 'up' | 'down', sources: string[], dest: string) { return this.call<string>('StartTransfer', sftpId, dir, sources, dest); }
  cancelTransfer(id: string) { return this.call<void>('CancelTransfer', id); }
  resumeTransfer(id: string) { return this.call<string>('ResumeTransfer', id); }
  startRemoteCopy(srcId: string, dstId: string, sources: string[], dest: string) { return this.call<string>('StartRemoteCopy', srcId, dstId, sources, dest); }
  setUpdateToken(token: string) { return this.call<void>('SetUpdateToken', token); }
  hasUpdateToken() { return this.call<boolean>('HasUpdateToken'); }
  keySetupDefaults(host: string) { return this.call<{ defaultPath: string; comment: string; existing: string[] }>('KeySetupDefaults', host); }
  generateKeyPair(kind: string, path: string, comment: string, pass: string) { return this.call<string>('GenerateKeyPair', kind, path, comment, pass); }
  publicKeyOf(path: string, pass: string) { return this.call<string>('PublicKeyOf', path, pass); }
  installPublicKey(tabId: string, line: string) { return this.call<string>('InstallPublicKey', tabId, line); }
  testKeyLogin(tabId: string, path: string, pass: string) { return this.call<void>('TestKeyLogin', tabId, path, pass); }
  useKeyForSession(sessionPath: string, keyPath: string, pass: string, save: boolean) { return this.call<void>('UseKeyForSession', sessionPath, keyPath, pass, save); }
  notify(title: string, body: string) { return this.call<void>('Notify', title, body); }
  setDeviceTypePassword(name: string, pw: string, empty: boolean) { return this.call<void>('SetDeviceTypePassword', name, pw, empty); }
  deviceTypePasswordState(name: string) { return this.call<string>('DeviceTypePasswordState', name); }
  renameDeviceTypePassword(oldName: string, newName: string) { return this.call<void>('RenameDeviceTypePassword', oldName, newName); }
  keyPassphrases() { return this.call<{ path: string; exists: boolean; sessions: string[] | null }[]>('KeyPassphrases'); }
  setKeyPassphrase(path: string, pw: string) { return this.call<void>('SetKeyPassphrase', path, pw); }
  forgetKeyPassphrase(path: string) { return this.call<void>('ForgetKeyPassphrase', path); }
  setSessionPassword(path: string, pw: string, empty: boolean) { return this.call<void>('SetSessionPassword', path, pw, empty); }
  checkReachable(targets: { key: string; host: string; port: number }[]) { return this.call<Record<string, boolean>>('CheckReachable', targets); }
  sessionPasswordState(path: string) { return this.call<string>('SessionPasswordState', path); }
  scpTransferSftp(sftpId: string, dir: 'up' | 'down', src: string, dest: string) { return this.call<string>('SCPTransferSFTP', sftpId, dir, src, dest); }
  checkForUpdate() { return this.call<UpdateStatus>('CheckForUpdate'); }
  backupConfig() { return this.call<string>('BackupConfig'); }
  restoreConfig(path: string) { return this.call<{ files: number; sessions: number; before: string }>('RestoreConfig', path); }
  aiChatsLoad() { return this.call<string>('AIChatsLoad'); }
  aiChatsSave(data: string) { return this.call<void>('AIChatsSave', data); }
  downloadUpdate(url: string, name: string) { return this.call<string>('DownloadUpdate', url, name); }
  openUpdate(path: string) { return this.call<void>('OpenUpdate', path); }
  sendYmodem(tabId: string, files: string[]) { return this.call<string>('SendYmodem', tabId, files); }
  receiveYmodem(tabId: string) { return this.call<string>('ReceiveYmodem', tabId); }
  setCredentialPassword(name: string, password: string) { return this.call<void>('SetCredentialPassword', name, password); }
  hasCredentialPassword(name: string) { return this.call<boolean>('HasCredentialPassword', name); }
  lockPasswords() { return this.call<boolean>('LockPasswords'); }
  unlockPasswords(password: string) { return this.call<boolean>('UnlockPasswords', password); }
  setMasterPassword(current: string, next: string) { return this.call<void>('SetMasterPassword', current, next); }
  exportVault(dest: string, passphrase: string) { return this.call<number>('ExportVault', dest, passphrase); }
  importVault(src: string, passphrase: string) { return this.call<number>('ImportVault', src, passphrase); }
  sendXmodem(tabId: string, file: string, blockSize: number) { return this.call<string>('SendXmodem', tabId, file, blockSize); }
  sendBreak(tabId: string) { return this.call<void>('SendBreak', tabId); }
  zmodemSend(tabId: string, files: string[]) { return this.call<string>('ZmodemSend', tabId, files); }
  receiveXmodem(tabId: string, out: string) { return this.call<string>('ReceiveXmodem', tabId, out); }
  scpTransfer(tabId: string, dir: 'up' | 'down', src: string, dest: string) { return this.call<string>('SCPTransfer', tabId, dir, src, dest); }
  startSync(sftpId: string, dir: 'up' | 'down', localDir: string, remoteDir: string) { return this.call<string>('StartSync', sftpId, dir, localDir, remoteDir); }
  chooseOpenFiles(title: string) { return this.call<string[] | null>('ChooseOpenFiles', title); }
  runScriptFile(tabId: string, file: string) { return this.call<string>('RunScriptFile', tabId, file); }
  cancelScript(runId: string) { return this.call<void>('CancelScript', runId); }
  startRecording(tabId: string) { return this.call<void>('StartRecording', tabId); }
  stopRecording(tabId: string) { return this.call<string>('StopRecording', tabId); }

  chooseOpenFile(title: string, filters: FileFilter[] = []) {
    // Windows shows only the first filter's files: always offer "All files" too, and match upper-case extensions
    if (filters.length && /win/i.test(navigator.platform)) {
      filters = [...filters.map(f => ({ ...f, pattern: f.pattern.split(';').flatMap(p => [p, p.toUpperCase()]).filter((v, i, a) => a.indexOf(v) === i).join(';') })),
        { displayName: 'All files (*.*)', pattern: '*.*' }];
    }
    return this.call<string>('ChooseOpenFile', title, filters);
  }
  chooseDirectory(title: string) { return this.call<string>('ChooseDirectory', title); }
  chooseSaveFile(title: string, name: string, filters: FileFilter[] = []) { return this.call<string>('ChooseSaveFile', title, name, filters); }
  saveBase64File(path: string, b64: string) { return this.call<void>('SaveBase64File', path, b64); }
  saveTextFile(path: string, content: string) { return this.call<void>('SaveTextFile', path, content); }
  readTextFile(path: string) { return this.call<string>('ReadTextFile', path); }
}

export function errText(e: unknown): string {
  if (e instanceof Error) return e.message;
  return typeof e === 'string' ? e : JSON.stringify(e);
}
