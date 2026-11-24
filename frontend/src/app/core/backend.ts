import { Injectable } from '@angular/core';
import type { TestbedMeta, TestSuiteDefaults, TestWeb, TestWebAction, WebResult, WebPick, TestbedDevice, TestExpect, WebStatus, TestBooking, TestTrends, TestProc, TestProcInfo, TemplateSpec, SNMPRow, HMockInfo, APIMonitor, APIMonitorState, SNMPDashboard, SNMPDashPoll, SNMPSimState, TestApproval, TopoPlan, TopoRequest, TopoResult, LayoutRequest, LayoutResult, TestCase, TestCaseInfo, TestRunReport, TestsTree, TestsGitInfo, TestSchedule, TestScheduleView, IntegrationsConfig, TestTable, TestHistory } from './models';
import { EmuEngines, EmuHostNIC, EmuTrex,
  EmuServer, EmuServerView,
  AccountInfo, EmuBoard, EmuTrafficTemplate, LabNote, EmuCapture, EmuCompNet, EmuPacket, EmuMatrix, EmuRemoveInfo, EmuRemoveHostReq, JumpUse, LocalNet, RemoteDesktopRun,  SerialPortInfo, DeviceFacts, AppInfo, BackupFile, ExportReport, BackupStatus, CheckRun, FileEntry, HistoryEntry, FileFilter, ImportOptions, ImportResult, MovedDevice, PromptReply, RecordingData, RecordingInfo, RemoteInfo, SmartCardInfo, Session, SessionNode, Settings, TabInfo, UpdateStatus, ImportSource, SettingsSummary, TeamConn, TeamDetails, TeamInfo, TeamPushItem, TeamPushResult, TeamView, SyncStatus, AIAnswer, AISettings, AIStatus, FileServersView, FileServersSettings, ServedFile, FSAddress, MCPInfo, FirewallState, NCResult, IperfInfo, IperfTest, DeviceName, LabView, LabRequest, LabLink, LabOutput, LabPack, EmuHost, EmuImage, EmuInstallRequest, EmuRequest, EmuTemplate, EmuPersona, EmuTopoInfo, EmuTester, EmuStream, EmuTrafficView, LabHubDetail, EmuRouting, EmuRoutingView, EmuRadiusView, EmuRunRow, EmuToolStep, EmuPacketDetail, EmuTest, EmuTestRun, EmuChange, SNMPRequest, SNMPResult, SNMPMibNode, SNMPModule, SNMPSecretState, SNMPTrapState, SNMPTrapsView, HSendRequest, HResponse, HCollection, HCollectionInfo, HRunRequest, HRunResult, HHistoryItem, HRequest, AISkill, HEnv, HVar, HHostCookies, SyslogSettings, SyslogState, SyslogMsg, SyslogRule, SyslogQuery, SyslogSendIn } from './models';

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

/** A device's own web server, read by its device type. */
export interface WebServerState { known: boolean; http: boolean; https: boolean; output?: string; on?: string[] | null; save?: string[] | null; }

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

  /** A picture on the system clipboard (base64 PNG), or null: the app's Paste reaches the page as text only. */
  clipboardImage() { return this.call<{ data: string; type: string } | null>('ClipboardImage'); }
  /** What to type in a terminal to sign Claude Code in on this computer (its path when the terminal would not find it). */
  claudeCodeSignIn() { return this.call<string>('ClaudeCodeSignIn'); }
  clipboardGet(): Promise<string> {
    return window.runtime ? window.runtime.ClipboardGetText() : navigator.clipboard.readText();
  }
  clipboardSet(text: string): Promise<unknown> {
    return window.runtime ? window.runtime.ClipboardSetText(text) : navigator.clipboard.writeText(text);
  }
  openURL(url: string) { window.runtime?.BrowserOpenURL(url); }

  getAppInfo() { return this.call<AppInfo>('GetAppInfo'); }
  titleBarDoubleClick() { return this.call<void>('TitleBarDoubleClick'); }
  windowButton(b: 'min' | 'max' | 'close') { return this.call<void>('WindowButton', b); }
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
  emuCatalog() { return this.call<{ file: string; name: string; group: string; note: string; added: boolean; count: number }[]>('EmuCatalog'); }
  emuAddFromCatalog(file: string) { return this.call<LabView>('EmuAddFromCatalog', file); }
  emuSetDiskFile(id: string, slot: string, path: string) { return this.call<LabView>('EmuSetDiskFile', id, slot, path); }
  emuDownloadStop(id: string) { return this.call<LabView>('EmuDownloadStop', id); }
  emuDownloadMove(id: string, where: string) { return this.call<LabView>('EmuDownloadMove', id, where); }
  emuRemoveFromCatalog(file: string, files: boolean) { return this.call<LabView>('EmuRemoveFromCatalog', file, files); }
  emuClearEvents() { return this.call<LabView>('EmuClearEvents'); }
  emuDownload(ids: string[]) { return this.call<LabView>('EmuDownload', ids); }
  emuPersonaPreview(id: string, p: EmuPersona) { return this.call<string>('EmuPersonaPreview', id, p); }
  emuSuggest(model: string) { return this.call<{ serial: string; mac: string }>('EmuSuggest', model); }
  emuSaveTemplate(t: EmuTemplate) { return this.call<LabView>('EmuSaveTemplate', t); }
  emuRemoveTemplate(id: string) { return this.call<LabView>('EmuRemoveTemplate', id); }
  emuImportProfiles(path: string) { return this.call<LabView>('EmuImportProfiles', path); }
  emuSetPersona(id: string, p: EmuPersona) { return this.call<LabView>('EmuSetPersona', id, p); }
  emuApplyPersona(ids: string[]) { return this.call<LabView>('EmuApplyPersona', ids); }
  emuSetResources(id: string, cpus: number, ram: number, diskGb: number) { return this.call<LabView>('EmuSetResources', id, cpus, ram, diskGb); }
  emuSetMACs(ids: string[], start: string) { return this.call<LabView>('EmuSetMACs', ids, start); }
  labSetMACs(ids: string[], start: string) { return this.call<LabView>('LabSetMACs', ids, start); }
  emuSetIdentity(id: string, serial: string, mac: string) { return this.call<LabView>('EmuSetIdentity', id, serial, mac); }
  emuSetSerials(ids: string[], serials: string[], fromFormat: boolean) { return this.call<LabView>('EmuSetSerials', ids, serials, fromFormat); }
  labNewSerials(ids: string[]) { return this.call<LabView>('LabNewSerials', ids); }
  emuSetupHost(session: string, password: string) { return this.call<string>('EmuSetupHost', session, password); }
  emuCheckHost(session: string) { return this.call<EmuHost>('EmuCheckHost', session); }
  emuRemoveHost(session: string) { return this.call<LabView>('EmuRemoveHost', session); }
  emuAddImage(path: string) { return this.call<LabView>('EmuAddImage', path); }
  /** The microcontrollers there are, and a firmware for one: a flash image (.bin) or an ESP-IDF build folder. */
  emuBoards() { return this.call<EmuBoard[]>('EmuBoards'); }
  emuAddFirmware(board: string, path: string, name: string, flashMb: number) { return this.call<LabView>('EmuAddFirmware', board, path, name, flashMb); }
  emuSampleFirmware(board: string) { return this.call<LabView>('EmuSampleFirmware', board); }
  emuAddServer(host: string, start: boolean) { return this.call<LabView>('EmuAddServer', host, start); }
  emuServerState(id: string) { return this.call<EmuServerView>('EmuServerState', id); }
  emuSetServer(id: string, s: EmuServer) { return this.call<LabView>('EmuSetServer', id, s); }
  emuServerPassword(id: string) { return this.call<string>('EmuServerPassword', id); }
  emuServerUpload(id: string, files: string[], dir: string) { return this.call<EmuServerView>('EmuServerUpload', id, files, dir); }
  emuServerDownload(id: string, name: string, to: string) { return this.call<void>('EmuServerDownload', id, name, to); }
  emuServerDelete(id: string, names: string[]) { return this.call<EmuServerView>('EmuServerDelete', id, names); }
  emuServerMkdir(id: string, name: string) { return this.call<EmuServerView>('EmuServerMkdir', id, name); }
  /** A microcontroller gets another firmware of its board (its flash is made again) and starts again. */
  emuFlashFirmware(id: string, imageId: string) { return this.call<LabView>('EmuFlashFirmware', id, imageId); }
  emuSetGdb(id: string, on: boolean) { return this.call<LabView>('EmuSetGDB', id, on); }
  emuInstallStart(r: EmuInstallRequest) { return this.call<LabView>('EmuInstallStart', r); }
  emuInstallFinish(id: string) { return this.call<LabView>('EmuInstallFinish', id); }
  /** An automatic install becomes one by hand, keeping what the installer did. */
  emuInstallTakeOver(id: string) { return this.call<LabView>('EmuInstallTakeOver', id); }
  emuInstallCancel(id: string) { return this.call<LabView>('EmuInstallCancel', id); }
  emuInstallRemove(id: string) { return this.call<LabView>('EmuInstallRemove', id); }
  emuImportAppliance(path: string) { return this.call<LabView>('EmuImportAppliance', path); }
  emuSetImageFile(id: string, path: string) { return this.call<LabView>('EmuSetImageFile', id, path); }
  emuSaveImage(im: EmuImage, clearPassword: boolean) { return this.call<LabView>('EmuSaveImage', im, clearPassword); }
  emuRemoveImage(id: string) { return this.call<LabView>('EmuRemoveImage', id); }
  emuUpload(id: string, host: string) { return this.call<LabView>('EmuUpload', id, host); }
  emuAdd(r: EmuRequest) { return this.call<LabView>('EmuAdd', r); }
  emuStart(ids: string[]) { return this.call<LabView>('EmuStart', ids); }
  emuStop(ids: string[]) { return this.call<LabView>('EmuStop', ids); }
  emuShutDown(ids: string[]) { return this.call<LabView>('EmuShutDown', ids); }
  emuSetFast(on: boolean) { return this.call<LabView>('EmuSetFast', on); }
  emuSetMgmt(mgmt: string, mgmtNet: string) { return this.call<LabView>('EmuSetMgmt', mgmt, mgmtNet); }
  emuSetDNS(servers: string[]) { return this.call<LabView>('EmuSetDNS', servers); }
  emuCheckpoint(id: string, note: string) { return this.call<LabView>('EmuCheckpoint', id, note); }
  emuCheckpointRestore(id: string, tag: string) { return this.call<LabView>('EmuCheckpointRestore', id, tag); }
  emuCheckpointRemove(id: string, tag: string) { return this.call<LabView>('EmuCheckpointRemove', id, tag); }
  emuSetCheckpoints(on: boolean) { return this.call<LabView>('EmuSetCheckpoints', on); }
  emuNoteSave(n: LabNote) { return this.call<LabView>('EmuNoteSave', n); }
  emuNoteRemove(id: string) { return this.call<LabView>('EmuNoteRemove', id); }
  emuHubAdd(kind: 'hub' | 'switch', name: string, x: number, y: number) { return this.call<LabView>('EmuHubAdd', kind, name, x, y); }
  emuHubSet(id: string, name: string, x: number, y: number) { return this.call<LabView>('EmuHubSet', id, name, x, y); }
  emuHubConnect(id: string, vm: string, port: string) { return this.call<LabView>('EmuHubConnect', id, vm, port); }
  emuHubDisconnect(id: string, vm: string, port: number) { return this.call<LabView>('EmuHubDisconnect', id, vm, port); }
  emuHubRemove(id: string) { return this.call<LabView>('EmuHubRemove', id); }
  emuHubSetPorts(id: string, n: number) { return this.call<LabView>('EmuHubSetPorts', id, n); }
  emuHubPortSet(id: string, port: number, mode: string, vlan: number, vlans: number[]) { return this.call<LabView>('EmuHubPortSet', id, port, mode, vlan, vlans); }
  emuHubSetMgmt(id: string, ip: string, vlan: number) { return this.call<LabView>('EmuHubSetMgmt', id, ip, vlan); }
  emuHubDetail(id: string) { return this.call<LabHubDetail>('EmuHubDetail', id); }
  emuSetCloudPorts(host: string, n: number) { return this.call<LabView>('EmuSetCloudPorts', host, n); }
  emuScreen(kind: 'vm' | 'install', id: string) { return this.call<{ png: string; w: number; h: number; at: string }>('EmuScreen', kind, id); }
  emuScreenKeys(kind: 'vm' | 'install', id: string, text: string, enter: boolean, keys: string[]) { return this.call<void>('EmuScreenKeys', kind, id, text, enter, keys); }
  emuInstallType(id: string, line: string, enter: boolean) { return this.call<void>('EmuInstallType', id, line, enter); }
  emuSetHostMgmt(host: string, mgmt: string, mgmtNet: string) { return this.call<LabView>('EmuSetHostMgmt', host, mgmt, mgmtNet); }
  emuConsoleLog(id: string) { return this.call<string>('EmuConsoleLog', id); }
  emuDay0Again(id: string) { return this.call<LabView>('EmuDay0Again', id); }
  emuMigrate(id: string, host: string, live: boolean) { return this.call<LabView>('EmuMigrate', id, host, live); }
  emuRemoveHostInfo(host: string) { return this.call<EmuRemoveInfo>('EmuRemoveHostInfo', host); }
  emuRemoveHostWith(r: EmuRemoveHostReq) { return this.call<LabView>('EmuRemoveHostWith', r); }
  emuHostKVM(host: string) { return this.call<boolean>('EmuHostKVM', host); }
  emuLog(lines: number) { return this.call<{ level: string; path: string; tail: string }>('EmuLog', lines); }
  emuSetLogLevel(level: string) { return this.call<{ level: string; path: string; tail: string }>('EmuSetLogLevel', level); }
  emuCaptureStart(id: string, port: string) { return this.call<EmuCapture[]>('EmuCaptureStart', id, port); }
  emuCaptureStop(key: string) { return this.call<EmuCapture[]>('EmuCaptureStop', key); }
  emuCaptureDelete(files: string[]) { return this.call<EmuCapture[]>('EmuCaptureDelete', files); }
  emuRunning() { return this.call<EmuRunRow[]>('EmuRunning'); }
  /** Switches off a lab's switches that run in the background on a host (a topology not open, or none any more). */
  emuStopBackground(host: string, lab: string) { return this.call<number>('EmuStopBackground', host, lab); }
  emuQemuCommand(id: string) { return this.call<string>('EmuQemuCommand', id); }
  emuLinkCheck(id: string, seconds: number) { return this.call<string>('EmuLinkCheck', id, seconds); }
  emuTests() { return this.call<EmuTest[]>('EmuTests'); }
  emuTestSave(t: EmuTest) { return this.call<EmuTest[]>('EmuTestSave', t); }
  emuTestRemove(id: string) { return this.call<EmuTest[]>('EmuTestRemove', id); }
  emuTestRun(id: string) { return this.call<EmuTestRun>('EmuTestRun', id); }
  emuTestStop() { return this.call<void>('EmuTestStop'); }
  emuTestRuns() { return this.call<EmuTestRun[]>('EmuTestRuns'); }
  emuTestReportSave(id: string, path: string) { return this.call<void>('EmuTestReportSave', id, path); }
  emuTraffic() { return this.call<EmuTrafficView>('EmuTraffic'); }
  emuTesterAdd(vm: string, port: string, ip: string, gw: string, vlan: number) { return this.call<EmuTrafficView>('EmuTesterAdd', vm, port, ip, gw, vlan); }
  emuTesterSet(t: EmuTester) { return this.call<EmuTrafficView>('EmuTesterSet', t); }
  emuRadius() { return this.call<EmuRadiusView>('EmuRadius'); }
  emuRadiusSet(r: Omit<EmuRadiusView, 'hosts'>) { return this.call<EmuRadiusView>('EmuRadiusSet', r); }
  emuTesterRemove(id: string) { return this.call<EmuTrafficView>('EmuTesterRemove', id); }
  emuStreamSave(s: EmuStream) { return this.call<EmuTrafficView>('EmuStreamSave', s); }
  emuTrafficTemplates() { return this.call<EmuTrafficTemplate[]>('EmuTrafficTemplates'); }
  emuTrafficTemplateApply(id: string, ports: string[]) { return this.call<EmuTrafficView>('EmuTrafficTemplateApply', id, ports); }
  emuTrafficTemplateSave(name: string, desc: string, ids: string[]) { return this.call<EmuTrafficTemplate>('EmuTrafficTemplateSave', name, desc, ids); }
  emuTrafficTemplateRemove(id: string) { return this.call<void>('EmuTrafficTemplateRemove', id); }
  emuStreamRemove(id: string) { return this.call<EmuTrafficView>('EmuStreamRemove', id); }
  emuTrafficStart(ids: string[]) { return this.call<EmuTrafficView>('EmuTrafficStart', ids); }
  emuTrafficStop() { return this.call<EmuTrafficView>('EmuTrafficStop'); }
  emuTrafficReport(path: string) { return this.call<void>('EmuTrafficReport', path); }
  emuHostNICs(host: string) { return this.call<EmuHostNIC[]>('EmuHostNICs', host); }
  emuTesterAddNIC(host: string, nic: string, cabled: string, ip: string, gw: string, vlan: number) { return this.call<EmuTrafficView>('EmuTesterAddNIC', host, nic, cabled, ip, gw, vlan); }
  emuEngines() { return this.call<EmuEngines>('EmuEngines'); }
  emuEngineSet(engine: string) { return this.call<EmuEngines>('EmuEngineSet', engine); }
  emuTrexSave(t: EmuTrex) { return this.call<EmuEngines>('EmuTrexSave', t); }
  emuTrexRemove(id: string) { return this.call<EmuEngines>('EmuTrexRemove', id); }
  emuTrexPort(id: string, port: number, use: boolean, cabled: string) { return this.call<EmuEngines>('EmuTrexPort', id, port, use, cabled); }
  emuCaptures() { return this.call<EmuCapture[]>('EmuCaptures'); }
  emuCapturePackets(key: string, last: number) { return this.call<EmuPacket[]>('EmuCapturePackets', key, last); }
  emuCapturePacket(key: string, no: number) { return this.call<EmuPacketDetail>('EmuCapturePacket', key, no); }
  emuCaptureSavePackets(key: string, nos: number[], path: string) { return this.call<void>('EmuCaptureSavePackets', key, nos, path); }
  emuCaptureSave(key: string, path: string) { return this.call<void>('EmuCaptureSave', key, path); }
  emuCaptureWireshark(key: string) { return this.call<void>('EmuCaptureWireshark', key); }
  emuImageMatrix() { return this.call<EmuMatrix>('EmuImageMatrix'); }
  emuSyncImages(hosts: string[], images: string[], force: boolean) { return this.call<EmuMatrix>('EmuSyncImages', hosts, images, force); }
  emuRemoveImageFrom(host: string, id: string) { return this.call<EmuMatrix>('EmuRemoveImageFrom', host, id); }
  emuSyncSettings(syncAll: boolean, keep20: boolean) { return this.call<EmuMatrix>('EmuSyncSettings', syncAll, keep20); }
  emuCheckReach() { return this.call<EmuMatrix>('EmuCheckReach'); }
  emuSetImagesDir(host: string, dir: string) { return this.call<EmuMatrix>('EmuSetImagesDir', host, dir); }
  emuOpenPortsText(host: string) { return this.call<string>('EmuOpenPortsText', host); }
  emuMoveVM(id: string, host: string) { return this.call<LabView>('EmuMoveVM', id, host); }
  emuSetLag(vm: string, name: string, mode: string, ports: string[]) { return this.call<LabView>('EmuSetLag', vm, name, mode, ports); }
  emuLagCommands(vm: string, name: string, remove: boolean) { return this.call<string[]>('EmuLagCommands', vm, name, remove); }
  emuApplyLag(vm: string, name: string, remove: boolean) { return this.call<LabView>('EmuApplyLag', vm, name, remove); }
  emuCheckCabling() { return this.call<LabView>('EmuCheckCabling'); }
  emuReadLags(ids: string[]) { return this.call<LabView>('EmuReadLags', ids); }
  emuTopologies() { return this.call<EmuTopoInfo[]>('EmuTopologies'); }
  emuSaveTopology(name: string) { return this.call<LabView>('EmuSaveTopology', name); }
  emuOpenTopology(name: string, start: boolean) { return this.call<LabView>('EmuOpenTopology', name, start); }
  emuNewTopology(name: string) { return this.call<LabView>('EmuNewTopology', name); }
  emuDeleteTopology(name: string) { return this.call<void>('EmuDeleteTopology', name); }
  emuRestart(ids: string[]) { return this.call<LabView>('EmuRestart', ids); }
  emuReset(ids: string[]) { return this.call<LabView>('EmuReset', ids); }
  emuRemove(ids: string[], sessions: boolean) { return this.call<LabView>('EmuRemove', ids, sessions); }
  emuConnect(l: LabLink) { return this.call<LabView>('EmuConnect', l); }
  emuDisconnect(id: string, port: string) { return this.call<LabView>('EmuDisconnect', id, port); }
  emuSetLink(id: string, port: string, up: boolean) { return this.call<LabView>('EmuSetLink', id, port, up); }
  emuPlace(id: string, x: number, y: number) { return this.call<void>('EmuPlace', id, x, y); }
  emuFreePorts(id: string) { return this.call<string[] | null>('EmuFreePorts', id); }
  emuSetRole(id: string, role: string) { return this.call<LabView>('EmuSetRole', id, role); }
  emuSetComputerNet(id: string, n: EmuCompNet) { return this.call<LabView>('EmuSetComputerNet', id, n); }
  emuComputerRun(id: string, app: string, target: string) { return this.call<string>('EmuComputerRun', id, app, target); }
  emuSetDesktop(id: string, on: boolean, home: string) { return this.call<LabView>('EmuSetDesktop', id, on, home); }
  emuScreenClick(kind: 'vm' | 'install', id: string, x: number, y: number, button: string, double: boolean) { return this.call<void>('EmuScreenClick', kind, id, x, y, button, double); }
  emuSetRouting(id: string, r: EmuRouting | { proto: '' }) { return this.call<EmuRoutingView>('EmuSetRouting', id, r); }
  emuRouting(id: string) { return this.call<EmuRoutingView>('EmuRouting', id); }
  emuRoutingStream(id: string, from: string, pps: number) { return this.call<EmuTrafficView>('EmuRoutingStream', id, from, pps); }
  emuSetCPU(id: string, maxCores: number, weight: number) { return this.call<LabView>('EmuSetCPU', id, maxCores, weight); }
  /** A device's CPU request and limit in millicores (0 = none); they apply at once. */
  /** The lab's change history (newest first), what Revert would do, and Revert. */
  emuChanges() { return this.call<EmuChange[]>('EmuChanges'); }
  /** The AI panel's own lab commands: the lab is kept before one runs, and the change goes to the history after. */
  emuAgentChangeBegin(what: string, verb: string) { return this.call<string>('EmuAgentChangeBegin', what, verb); }
  emuAgentChangeEnd(id: string, result: string) { return this.call<void>('EmuAgentChangeEnd', id, result); }
  emuRevertPlan(id: string) { return this.call<string[]>('EmuRevertPlan', id); }
  emuRevert(id: string) { return this.call<string[]>('EmuRevert', id); }
  emuSetCPUReq(id: string, requestM: number, limitM: number) { return this.call<LabView>('EmuSetCPUReq', id, requestM, limitM); }
  emuAddComputer(host: string, start: boolean) { return this.call<LabView>('EmuAddComputer', host, start); }
  emuSetPortAddr(id: string, port: number, addr: string) { return this.call<LabView>('EmuSetPortAddr', id, port, addr); }
  emuOpenTopologyOn(name: string, start: boolean, hosts: Record<string, string>) { return this.call<LabView>('EmuOpenTopologyOn', name, start, hosts); }
  emuDuplicateTopology(name: string, newName: string) { return this.call<EmuTopoInfo[]>('EmuDuplicateTopology', name, newName); }
  emuRenameTopology(name: string, newName: string) { return this.call<EmuTopoInfo[]>('EmuRenameTopology', name, newName); }
  emuExportTopology(name: string, path: string) { return this.call<void>('EmuExportTopology', name, path); }
  emuImportTopology(path: string) { return this.call<EmuTopoInfo[]>('EmuImportTopology', path); }
  emuCheckTunnels() { return this.call<{ from: string; to: string; ok: boolean; text: string }[]>('EmuCheckTunnels'); }
  emuSetUplink(id: string, port: string, on: boolean) { return this.call<LabView>('EmuSetUplink', id, port, on); }
  emuSetIconStyle(id: string, style: string) { return this.call<LabView>('EmuSetIconStyle', id, style); }
  emuAgentText() { return this.call<string>('EmuAgentText'); }
  // ---- syslog ----
  syslogSettings() { return this.call<SyslogSettings>('SyslogSettingsGet'); }
  syslogStart(s: SyslogSettings) { return this.call<SyslogState>('SyslogStart', s); }
  syslogStop() { return this.call<void>('SyslogStop'); }
  syslogState() { return this.call<SyslogState>('SyslogState'); }
  syslogMessages(q: SyslogQuery) { return this.call<SyslogMsg[]>('SyslogMessages', q); }
  syslogClear() { return this.call<void>('SyslogClear'); }
  syslogRules() { return this.call<SyslogRule[]>('SyslogRules'); }
  syslogSetRules(r: SyslogRule[]) { return this.call<void>('SyslogSetRules', r); }
  syslogSend(m: SyslogSendIn) { return this.call<string>('SyslogSend', m); }
  syslogExport(q: SyslogQuery) { return this.call<string>('SyslogExport', q); }
  // ---- API client ----
  httpSend(r: HSendRequest) { return this.call<HResponse>('HTTPSend', r); }
  httpTest(session: Session, path: string, secret: string, url = '/') { return this.call<HResponse>('HTTPTest', { session, path, secret, url }); }
  httpSecrets(path: string) { return this.call<{ secret: boolean; vars: string[] }>('HTTPSecrets', path); }
  /** null keeps a secret, '' removes it; keep: the secret variables the session still has. */
  httpSetSecrets(path: string, secret: string | null, vars: { name: string; value: string | null; prod: string | null }[], keep: string[]) { return this.call<void>('HTTPSetSecrets', { path, secret, vars, keep }); }
  httpToken(path: string, env: string) { return this.call<{ held: boolean; expires?: string }>('HTTPToken', path, env); }
  httpCollections() { return this.call<HCollectionInfo[]>('HTTPCollections'); }
  httpCollection(id: string) { return this.call<HCollection>('HTTPCollection', id); }
  httpSaveCollection(c: HCollection) { return this.call<HCollection>('HTTPSaveCollection', c); }
  httpDeleteCollection(id: string) { return this.call<void>('HTTPDeleteCollection', id); }
  httpImport(text: string, name: string, session: string) { return this.call<HCollection>('HTTPImport', text, name, session); }
  httpImportFile(session: string) { return this.call<HCollection>('HTTPImportFile', session); }
  httpExportFile(id: string, format: string) { return this.call<string>('HTTPExportFile', id, format); }
  httpCode(r: HSendRequest, lang: string) { return this.call<string>('HTTPCode', r, lang); }
  httpRun(r: HRunRequest) { return this.call<HRunResult>('HTTPRun', r); }
  httpHistory() { return this.call<HHistoryItem[]>('HTTPHistory'); }
  httpClearHistory() { return this.call<void>('HTTPClearHistory'); }
  testsFolder() { return this.call<string>('TestsFolder'); }
  testCases() { return this.call<TestCaseInfo[]>('TestCases'); }
  testCase(file: string) { return this.call<TestCase>('TestCase', file); }
  testSave(file: string, c: TestCase) { return this.call<string>('TestSave', file, c); }
  testsTree() { return this.call<TestsTree>('TestsTree'); }
  testsSignature() { return this.call<string>('TestsSignature'); }
  testFile(rel: string) { return this.call<string>('TestFile', rel); }
  testSaveYAML(rel: string, text: string) { return this.call<string>('TestSaveYAML', rel, text); }
  testsOpenFolder() { return this.call<string>('TestsOpenFolder'); }
  testsClone(repo: string) { return this.call<string>('TestsClone', repo); }
  testsNewTestbed(name: string, folder: string, sessions: string[] = []) { return this.call<string>('TestsNewTestbed', name, folder, sessions); }
  testbedDevices(file: string) { return this.call<TestbedDevice[]>('TestbedDevices', file); }
  testbedChange(file: string, add: string[], removeRoles: string[]) { return this.call<void>('TestbedChange', file, add, removeRoles); }
  testsDeviceModels(paths: string[]) { return this.call<Record<string, string>>('TestsDeviceModels', paths); }
  testsGitInit() { return this.call<string>('TestsGitInit'); }
  testsGitPull() { return this.call<string>('TestsGitPull'); }
  testsGitCommit(message: string) { return this.call<string>('TestsGitCommit', message); }
  testsGitPush() { return this.call<string>('TestsGitPush'); }
  testSchedules() { return this.call<TestScheduleView[]>('TestSchedules'); }
  testSaveSchedule(s: TestSchedule) { return this.call<TestSchedule>('TestSaveSchedule', s); }
  testRemoveSchedule(id: string) { return this.call<void>('TestRemoveSchedule', id); }
  testRunScheduleNow(id: string) { return this.call<void>('TestRunScheduleNow', id); }
  testsGit() { return this.call<TestsGitInfo>('TestsGit'); }
  testsOpen(rel: string) { return this.call<void>('TestsOpen', rel); }
  testsOpenInEditor(rel: string) { return this.call<void>('TestsOpenInEditor', rel); }
  runTests(r: { rerun?: number; caseTimeout?: number; runTimeout?: number; breaks?: number[]; stepMode?: boolean; from?: number; only?: number; pauseOnFail?: boolean; files?: string[]; vars?: Record<string, string>; testbed?: string; confirmed?: boolean; onCleanupFail?: string; cleanup?: string; tags?: string[]; forceLock?: boolean }) { return this.call<TestRunReport>('RunTests', r); }
  runTestsParallel(r: { files?: string[]; testbeds: string[]; confirmed?: boolean; onCleanupFail?: string; cleanup?: string; forceLock?: boolean }) { return this.call<TestRunReport[]>('RunTestsParallel', r); }
  httpMockStart(collection: string, port = 0) { return this.call<HMockInfo>('HTTPMockStart', collection, port); }
  httpMockStop(collection: string) { return this.call<void>('HTTPMockStop', collection); }
  httpMocks() { return this.call<HMockInfo[]>('HTTPMocks'); }
  apiMonitors() { return this.call<APIMonitorState[]>('APIMonitors'); }
  apiSaveMonitor(m: Partial<APIMonitor>) { return this.call<APIMonitor>('APISaveMonitor', m); }
  apiDeleteMonitor(id: string) { return this.call<void>('APIDeleteMonitor', id); }
  apiRunMonitor(id: string) { return this.call<void>('APIRunMonitor', id); }
  snmpExportXLSX(title: string, headers: string[], rows: string[][]) { return this.call<string>('SNMPExportXLSX', title, headers, rows); }
  snmpSaveWalk(name: string, rows: SNMPRow[]) { return this.call<string>('SNMPSaveWalk', name, rows); }
  snmpOpenWalk() { return this.call<{ file: string; rows: SNMPRow[] } | null>('SNMPOpenWalk'); }
  snmpSimStart(i: { file?: string; rows?: SNMPRow[]; port?: number; community?: string; all?: boolean; writable?: boolean }) { return this.call<SNMPSimState>('SNMPSimStart', i); }
  snmpSimStop() { return this.call<void>('SNMPSimStop'); }
  snmpSim() { return this.call<SNMPSimState>('SNMPSim'); }
  snmpDashboards() { return this.call<SNMPDashboard[]>('SNMPDashboards'); }
  snmpSaveDashboard(d: SNMPDashboard) { return this.call<SNMPDashboard>('SNMPSaveDashboard', d); }
  snmpDeleteDashboard(id: string) { return this.call<void>('SNMPDeleteDashboard', id); }
  snmpPollDashboard(id: string) { return this.call<SNMPDashPoll>('SNMPPollDashboard', id); }
  testApprovals() { return this.call<TestApproval[]>('TestApprovals'); }
  testRequestApproval(files: string[], testbed: string, reason: string) { return this.call<TestApproval>('TestRequestApproval', files, testbed, reason); }
  testDecide(id: string, approve: boolean, note: string) { return this.call<TestApproval>('TestDecide', id, approve, note); }
  testReleaseLock(testbed: string) { return this.call<void>('TestReleaseLock', testbed); }
  testTryChecks(expects: TestExpect[], output: string, status: number) { return this.call<{ what: string; passed: boolean; got?: string }[]>('TestTryChecks', expects, output, status); }
  testParse(session: string, command: string, template: string, output: string) { return this.call<TestTable>('TestParse', session, command, template, output); }
  testSaveFile(rel: string, text: string) { return this.call<void>('TestSaveFile', rel, text); }
  testSaveSuite(rel: string, s: { name?: string; description?: string; tests?: string[]; tags?: string[] }) { return this.call<string>('TestSaveSuite', rel, s); }
  testHistory(file: string, limit = 50) { return this.call<TestHistory[]>('TestHistory', file, limit); }
  testSendQTest(dir: string, files: string[] = []) { return this.call<string>('TestSendQTest', dir, files); }
  testCreateDefect(dir: string, file: string) { return this.call<string[]>('TestCreateDefect', dir, file); }
  testsExport(o: { files: string[]; lang: string; os: string; testbed: string; name: string; standalone?: boolean; noFailShots?: boolean; stepShots?: boolean }) { return this.call<string>('TestsExport', o); }
  testProcs() { return this.call<TestProcInfo[]>('TestProcs'); }
  testProc(name: string) { return this.call<TestProc>('TestProc', name); }
  testSaveProc(name: string, p: TestProc) { return this.call<string>('TestSaveProc', name, p); }
  testMakeTemplate(name: string, session: string, command: string, spec: TemplateSpec) { return this.call<string>('TestMakeTemplate', name, session, command, spec); }
  testPreviewTemplate(spec: TemplateSpec, output: string) { return this.call<TestTable | null>('TestPreviewTemplate', spec, output); }
  testPickData() { return this.call<string>('TestPickData'); }
  testTrends(files: string[], testbed: string, limit: number) { return this.call<TestTrends>('TestTrends', files, testbed, limit); }
  testBookings() { return this.call<TestBooking[]>('TestBookings'); }
  testSaveBooking(b: TestBooking) { return this.call<TestBooking>('TestSaveBooking', b); }
  testRemoveBooking(id: string) { return this.call<void>('TestRemoveBooking', id); }
  webStatus() { return this.call<WebStatus>('WebStatus'); }
  webSetup() { return this.call<void>('WebSetup'); }
  webRecord(w: TestWeb) { return this.call<TestWebAction[]>('WebRecord', w); }
  webRecordLogin(path: string, url: string, user: string) { return this.call<TestWebAction[]>('WebRecordLogin', path, url, user); }
  webTestLogin(path: string) { return this.call<string>('WebTestLogin', path); }
  webOpenApp(path: string) { return this.call<void>('WebOpenApp', path); }
  webDebugOpen(w: TestWeb, vars: Record<string, string>) { return this.call<void>('WebDebugOpen', w, vars); }
  webDebugRun(acts: TestWebAction[], from: number, vars: Record<string, string>) { return this.call<WebResult[]>('WebDebugRun', acts, from, vars); }
  webDebugPick() { return this.call<WebPick | null>('WebDebugPick'); }
  webDebugRecord(on: boolean) { return this.call<void>('WebDebugRecord', on); }
  webDebugRestart() { return this.call<void>('WebDebugRestart'); }
  webDebugClose() { return this.call<void>('WebDebugClose'); }
  testsSaveWebBlock(name: string, description: string, acts: TestWebAction[]) { return this.call<string>('TestsSaveWebBlock', name, description, acts); }
  testbedSetRole(file: string, role: string, session: string) { return this.call<void>('TestbedSetRole', file, role, session); }
  testbedSetValue(file: string, role: string, key: string, value: string) { return this.call<void>('TestbedSetValue', file, role, key, value); }
  testbedRenameRole(file: string, from: string, to: string) { return this.call<void>('TestbedRenameRole', file, from, to); }
  testsRoles(files: string[]) { return this.call<{ role: string; session: string; deviceType?: string }[]>('TestsRoles', files); }
  testsUsePortVars(c: TestCase) { return this.call<{ case: TestCase; changed: number }>('TestsUsePortVars', c); }
  testsPorts(session: string) { return this.call<string[]>('TestsPorts', session); }
  testbedInfo(file: string) { return this.call<TestbedMeta>('TestbedInfo', file); }
  testbedSetInfo(file: string, m: TestbedMeta) { return this.call<void>('TestbedSetInfo', file, m); }
  testbedSetPortProp(file: string, role: string, port: string, key: string, value: string) { return this.call<void>('TestbedSetPortProp', file, role, port, key, value); }
  testsDefaultsView(dir: string) { return this.call<{ own: TestSuiteDefaults; inherited: TestSuiteDefaults }>('TestsDefaultsView', dir); }
  testsSaveSuiteDefaults(dir: string, d: TestSuiteDefaults) { return this.call<void>('TestsSaveSuiteDefaults', dir, d); }
  testsWillSkip(files: string[], testbed: string) { return this.call<{ file: string; case: string; why: string }[]>('TestsWillSkip', files, testbed); }
  testsImage(p: string) { return this.call<string>('TestsImage', p); }
  testsWebBlocks() { return this.call<TestProcInfo[]>('TestsWebBlocks'); }
  testStop() { return this.call<void>('TestStop'); }
  testResume(action: string, vars?: Record<string, string>) { return this.call<void>('TestResume', action, vars ?? null); }
  testManualAnswer(id: string, result: string, note: string) { return this.call<void>('TestManualAnswer', id, result, note); }
  testImportManual(device: string) { return this.call<string[]>('TestImportManual', device); }
  testsFetchTemplates() { return this.call<string>('TestsFetchTemplates'); }
  testRemove(file: string) { return this.call<string>('TestRemove', file); }
  testRename(file: string, to: string, copy: boolean) { return this.call<string>('TestRename', file, to, copy); }
  testGenerateGo(file: string) { return this.call<string>('TestGenerateGo', file); }
  testLastRun() { return this.call<TestRunReport | null>('TestLastRun'); }
  keepAwakeStatus() { return this.call<{ active: boolean; error?: string }>('KeepAwakeStatus'); }
  aiImportInstructions() { return this.call<string>('AIImportInstructions'); }
  aiExportInstructions(text: string) { return this.call<string>('AIExportInstructions', text); }
  aiSkills() { return this.call<AISkill[]>('AISkills'); }
  aiSkill(name: string) { return this.call<AISkill>('AISkill', name); }
  aiSaveSkill(old: string, text: string) { return this.call<AISkill>('AISaveSkill', old, text); }
  aiRemoveSkill(name: string) { return this.call<void>('AIRemoveSkill', name); }
  aiImportSkill(replace: boolean) { return this.call<AISkill>('AIImportSkill', replace); }
  aiExportSkill(name: string) { return this.call<string>('AIExportSkill', name); }
  aiSkillsFolder() { return this.call<string>('AISkillsFolder'); }
  aiAddExampleSkills() { return this.call<string[]>('AIAddExampleSkills'); }
  aiSkillForChat(name: string) { return this.call<string>('AISkillForChat', name); }
  identifyDeviceType(path: string) { return this.call<{ type: string; current: string; command?: string; matched?: string; tried: string[] }>('IdentifyDeviceType', path); }
  httpParseCurl(text: string) { return this.call<HRequest>('HTTPParseCurl', text); }
  httpEnvironments() { return this.call<HEnv[]>('HTTPEnvironments'); }
  httpSaveEnvironment(e: HEnv) { return this.call<HEnv>('HTTPSaveEnvironment', e); }
  httpDeleteEnvironment(id: string) { return this.call<void>('HTTPDeleteEnvironment', id); }
  httpImportEnvironment(text: string, name: string) { return this.call<HEnv>('HTTPImportEnvironment', text, name); }
  httpImportEnvironmentFile() { return this.call<HEnv>('HTTPImportEnvironmentFile'); }
  httpExportEnvironmentFile(id: string) { return this.call<string>('HTTPExportEnvironmentFile', id); }
  httpVariables(r: HSendRequest) { return this.call<HVar[]>('HTTPVariables', r); }
  httpCookies(session: string) { return this.call<HHostCookies[]>('HTTPCookies', session); }
  httpDeleteCookie(session: string, host: string, name: string) { return this.call<void>('HTTPDeleteCookie', session, host, name); }
  httpSaveResponse(body: string, base64: boolean, name: string) { return this.call<string>('HTTPSaveResponse', body, base64, name); }
  // ---- SNMP ----
  snmpRun(r: SNMPRequest) { return this.call<SNMPResult>('SNMPRun', r); }
  snmpSecrets(path: string) { return this.call<SNMPSecretState>('SNMPSecrets', path); }
  /** null keeps a secret, '' removes it. */
  snmpSetSecrets(path: string, community: string | null, authPass: string | null, privPass: string | null, writeCommunity: string | null = null) { return this.call<void>('SNMPSetSecrets', { path, community, authPass, privPass, writeCommunity }); }
  snmpMibChildren(oid: string) { return this.call<SNMPMibNode[]>('SNMPMibChildren', oid); }
  snmpMibInfo(name: string) { return this.call<SNMPMibNode>('SNMPMibInfo', name); }
  snmpMibSearch(q: string) { return this.call<SNMPMibNode[]>('SNMPMibSearch', q); }
  snmpMibModules() { return this.call<SNMPModule[]>('SNMPMibModules'); }
  snmpLoadMibs() { return this.call<string[]>('SNMPLoadMIBs'); }
  snmpRemoveMib(module: string) { return this.call<void>('SNMPRemoveMIB', module); }
  snmpTrapsStart(port: number) { return this.call<SNMPTrapState>('SNMPTrapsStart', port); }
  snmpTrapsStop() { return this.call<void>('SNMPTrapsStop'); }
  snmpTraps() { return this.call<SNMPTrapsView>('SNMPTraps'); }
  snmpMibFolder() { return this.call<string>('SNMPMibFolder'); }
  snmpSendTrap(t: { address: string; version?: string; community?: string; user?: string; auth?: string; authPass?: string; priv?: string; privPass?: string; trapOid: string; values?: { oid: string; type?: string; value: string }[] }) { return this.call<void>('SNMPSendTrap', t); }
  snmpTrapsClear() { return this.call<void>('SNMPTrapsClear'); }
  /** The in-app AI's network tools (snmp): mode is the agent permission; ask = the step needs the user's yes. */
  netAgentTool(name: string, args: Record<string, unknown>, mode: string, approved: boolean) { return this.call<EmuToolStep>('NetAgentTool', name, args, mode, approved); }
  emuAgentTool(name: string, args: Record<string, unknown>, approved: boolean) { return this.call<EmuToolStep>('EmuAgentTool', name, args, approved); }
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
  topoRead(req: TopoRequest) { return this.call<TopoResult>('TopoRead', req); }
  topoForget(scope: string) { return this.call<void>('TopoForget', scope); }
  layoutGraph(req: LayoutRequest) { return this.call<LayoutResult>('LayoutGraph', req); }
  saveMapPDF(title: string, name: string, jpegBase64: string) { return this.call<string>('SaveMapPDF', title, name, jpegBase64); }
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
  teamUpdateServer(r: { teamId: string; sessionPath: string; image: string; channel?: string }) { return this.call<string>('TeamUpdateServer', r); }
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
  /** The firewall for the ports typed (saved or not; 0 = the saved one). */
  firewallCheck(tftp = 0, ftp = 0, http = 0) { return this.call<FirewallState>('FirewallCheckPorts', tftp, ftp, http); }
  /** Allows them: with a sudo password through sudo, without one through the system's own password prompt. */
  firewallAllow(tftp = 0, ftp = 0, http = 0, sudoPw = '') { return this.call<FirewallState>('FirewallAllowWith', tftp, ftp, http, sudoPw); }
  fileServersPassword() { return this.call<string>('FileServersPassword'); }
  fileServersNewPassword() { return this.call<string>('FileServersNewPassword'); }
  fileServersChooseFolder() { return this.call<string>('FileServersChooseFolder'); }
  fileServersShowFolder() { return this.call<void>('FileServersShowFolder'); }
  fileServersAddresses(host: string) { return this.call<FSAddress[]>('FileServersAddresses', host); }
  textReply(id: string, text: string, total: number) { return this.call<void>('TextReply', id, text, total); }
  claudeCodeFind(path: string) { return this.call<string>('ClaudeCodeFind', path); }
  mcpState() { return this.call<MCPInfo>('MCPState'); }
  apiKey() { return this.call<string>('APIKey'); }
  mcpAddToDesktop() { return this.call<void>('MCPAddToDesktop'); }
  mcpAddToClaudeCode() { return this.call<string>('MCPAddToClaudeCode'); }
  aiState() { return this.call<AIStatus>('AIState'); }
  integrationTokens() { return this.call<Record<string, boolean>>('IntegrationTokens'); }
  integrationSetToken(kind: string, token: string) { return this.call<void>('IntegrationSetToken', kind, token); }
  topoTestMap(template: string, output: string) { return this.call<Record<string, string>[] | null>('TopoTestMap', template, output); }
  topoBuiltinMap(deviceType: string) { return this.call<TopoPlan | null>('TopoBuiltinMap', deviceType); }
  integrationTest(kind: string, cfg?: IntegrationsConfig) { return this.call<string>('IntegrationTest', kind, cfg ?? null); }
  aiSetKey(provider: string, key: string) { return this.call<void>('AISetKey', provider, key); }
  aiModels(s: AISettings) { return this.call<string[]>('AIModels', s); }
  /** The sections of this version's guides that match (for the AI assistant). */
  aiHelp(query: string) { return this.call<string>('AIHelp', query); }
  /** Termwright's own state now: version, KVM hosts and their VMs, labs, teams, file servers, tabs. */
  aiStatus() { return this.call<string>('AIStatus'); }
  aiChat(r: { messages: { role: string; text: string; images?: { name: string; mediaType: string; data: string }[] }[]; deviceType: string; protocol: string; agent: boolean; tabs: string[]; tools: string[]; folders: string[]; perm: string; plan?: string; session?: string }) { return this.call<AIAnswer>('AIChat', r); }
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
  sftpOpen(tabId: string, mode: SftpMode = '') { return this.call<{ id: string; home: string; target: string; local?: string }>('SFTPOpen', tabId, mode); }
  sftpStat(id: string, p: string) { return this.call<{ size: number; mod: number }>('SFTPStat', id, p); }
  sftpReadTail(id: string, p: string) { return this.call<{ text: string; size: number }>('SFTPReadTail', id, p); }
  sftpSaveText(id: string, p: string, text: string, expect: number) { return this.call<number>('SFTPSaveText', id, p, text, expect); }
  localShowInFolder(id: string, p: string) { return this.call<void>('LocalShowInFolder', id, p); }
  localTrashFiles(id: string, paths: string[]) { return this.call<void>('LocalTrashFiles', id, paths); }
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
  /** The device's own web server (its device type says how to read it): HTTP and HTTPS on or off. */
  webServerCheck(s: Session) { return this.call<WebServerState>('WebServerCheck', s); }
  /** Turns the device's web server on for HTTP and HTTPS (and saves the configuration when save is set). */
  webServerEnable(s: Session, save: boolean) { return this.call<WebServerState>('WebServerEnable', s, save); }
  openWebPage(tabId: string, s: Session, through: 'session' | 'device', url: string, browser: string) {
    return this.call<{ browser: string; via: string; url?: string; note?: string }>('OpenWebPage', tabId, s, through, url, browser);
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
  /** The system's dictation in the focused field: '' when it started, else a hint. */
  startDictation() { return this.call<string>('StartDictation'); }
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
