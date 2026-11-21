import { builtinUndo } from '../core/recorder.service';
import { LOOKS } from '../core/map-svg';
import { ChangeDetectorRef, Component, DestroyRef, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { askRef, isRef, refOf } from '../core/secret-ref';
import { LANGUAGES, previewLanguages, tr } from '../core/i18n';
import { EFFORTS, modelChoices, readOnlyList } from '../core/ai-chat.service';
import { Combo, ComboOption } from '../components/combo';
import { Backend, errText } from '../core/backend';
import { ACTIONS, DeviceType, KeyBinding, PROTOCOLS, Settings, SmartCardInfo, SmartCardKey, SyncStatus, AISettings, AIStatus, AISkill, MCPInfo, comboOf, FACT_NAMES, dtTunnels } from '../core/models';
import { COLOR_KEYS, SCHEME_NAMES, fullScheme } from '../core/themes';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';
import { DEFAULT_ENVIRONMENTS, ENV_COLORS, envFg } from '../core/env';
import { UI_FONTS, UI_SIZES, uiFontStack } from '../core/ui-fonts';
import { DEVICE_KINDS } from '../components/device-icon';

interface DtPw { orig: string; pw: string; empty: boolean; state: string; forget: boolean; }
type Page = 'general' | 'terminal' | 'schemes' | 'envs' | 'keywords' | 'keymap' | 'buttons' | 'updates' | 'devices' | 'alerts' | 'cards' | 'ai' | 'skills' | 'integrations';

/** What Global Options can find: page, the text shown on the page, extra words people search for. */
const SEARCH: [Page, string, string][] = [
  ['general', 'Sessions folder', 'storage location ini path'], ['general', 'Logs folder', 'log directory'],
  ['general', 'Recordings folder', 'session recording cast replay playback audit'], ['general', 'Session recording', 'record every connection replay playback audit delete keep days'],
  ['cards', 'PKCS#11 libraries', 'smart card piv cac yubikey opensc token hsm ssh key'], ['cards', 'Ask the PIN for every login', 'smart card pin'],
  ['general', 'Downloads folder', 'zmodem download save'], ['general', 'SFTP from a terminal', 'working directory detection folder cwd follow'],
  ['general', 'SFTP tabs follow the terminal folder', 'cwd'], ['general', "SFTP tabs and the Files panel browse as the terminal's user", 'root sudo files panel'],
  ['general', 'Python interpreter', 'script python3'], ['general', 'UI theme', 'dark light mode appearance'], ['general', 'Interface font', 'font typeface vscode inter plex readable'], ['general', 'Text size', 'font size bigger larger zoom readable'],
  ['general', 'Accent color', 'colour highlight'], ['general', 'Confirm before closing connected tabs', 'quit exit'],
  ['general', 'Confirm before pasting more than one line', 'paste multi-line'], ['general', 'Remember typed commands', 'history autocomplete suggestions'], ['general', 'Reopen the sessions', 'restore tabs start'],
  ['general', 'Show Session Manager', 'tree sidebar'], ['general', 'Show Command Window', 'broadcast'], ['general', 'Show Button Bar', 'buttons'],
  ['general', 'Show Connect Bar', 'toolbar quick connect'], ['general', 'Show the menu bar inside the window', 'menu'],
  ['terminal', 'Font', 'typeface monospace'], ['terminal', 'Font size', 'zoom text size'], ['terminal', 'Color scheme', 'colours theme terminal'],
  ['terminal', 'Use this scheme for all sessions', 'force override'], ['terminal', 'Cursor', 'block underline bar'], ['terminal', 'Blink', 'cursor'],
  ['terminal', 'Scrollback lines', 'history buffer'], ['terminal', 'Terminal bar', 'timestamps auto scroll clear screen'], ['terminal', 'Copy on select', 'clipboard mouse'], ['terminal', 'Paste on right-click', 'mouse clipboard'],
  ['terminal', 'Paste line delay', 'slow paste milliseconds'],
  ['schemes', 'Scheme', 'colour scheme custom colors palette'], ['envs', 'Environments', 'environment production staging test development colour tag folder'], ['keywords', 'Default set', 'keyword highlighting highlight words colour'],
  ['keymap', 'Key Map', 'shortcut keyboard binding hotkey'], ['buttons', 'Button Bar', 'buttons send command'],
  ['devices', 'Device Types', 'login profile exos voss cisco linux username password prompt'], ['devices', 'No password', 'device type'],
  ['devices', 'Send initial carriage return', 'enter'], ['devices', 'Backup commands', 'configuration backup running-config show configuration'], ['alerts', 'Alerts', 'notify pattern sound desktop notification'],
  ['updates', 'Notify me about', 'update stable nightly'], ['updates', 'Check at start-up', 'update automatic'], ['updates', 'Repository', 'github'],
];
const PAGE_NAMES: Record<Page, string> = { general: 'General', terminal: 'Terminal & Appearance', schemes: 'Color Schemes', envs: 'Environments', keywords: 'Keyword Highlighting',
  keymap: 'Key Map', buttons: 'Button Bar', devices: 'Device Types', alerts: 'Alerts', cards: 'Smart Cards', updates: 'Updates', ai: 'AI Assistant', integrations: 'Integrations', skills: 'Instructions & Skills' };

/** Global Options dialog. */
@Component({
  selector: 'app-options-dialog',
  imports: [FormsModule, Icon, Combo],
  template: `
  @if (s) {
  <div class="modal-backdrop">
    <form class="modal wide" (ngSubmit)="save()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="options" [size]="18" /></span>
        <span class="mh-t">{{ 'Settings' }}<span class="mh-s">Global Options for every session</span></span></div>
      <div class="modal-split">
        <nav class="cat">
          <input type="search" class="osearch" placeholder="Search options" [ngModel]="q()" (ngModelChange)="q.set($event)" name="osearch" autocomplete="off" />
          @if (q().trim()) {
            @for (h of hits(); track $index) {
              <a class="hit" (click)="goTo(h[0], h[1])">{{ h[1] }}<span>{{ pageName(h[0]) }}</span></a>
            } @empty { <span class="nohit">Nothing found</span> }
          } @else {
          <a [class.on]="page() === 'general'" (click)="page.set('general')"><app-icon name="eye" [size]="15" />General</a>
          <a [class.on]="page() === 'terminal'" (click)="page.set('terminal')"><app-icon name="shell" [size]="15" />{{ 'Terminal' }}</a>
          <a class="sub" [class.on]="page() === 'schemes'" (click)="page.set('schemes')"><app-icon name="palette" [size]="15" />Color Schemes</a>
          <a class="sub" [class.on]="page() === 'envs'" (click)="page.set('envs')"><app-icon name="layers" [size]="15" />Environments</a>
          <a [class.on]="page() === 'keywords'" (click)="page.set('keywords')"><app-icon name="highlight" [size]="15" />Keyword Highlighting</a>
          <div class="cat-g">Automation</div>
          <a [class.on]="page() === 'keymap'" (click)="page.set('keymap')"><app-icon name="keyboard" [size]="15" />Key Map</a>
          <a [class.on]="page() === 'buttons'" (click)="page.set('buttons')"><app-icon name="buttons" [size]="15" />Button Bar</a>
          <a [class.on]="page() === 'devices'" (click)="page.set('devices')"><app-icon name="device" [size]="15" />Device Types</a>
          <a [class.on]="page() === 'alerts'" (click)="page.set('alerts')"><app-icon name="bell" [size]="15" />Alerts</a>
          <a [class.on]="page() === 'ai'" (click)="page.set('ai'); loadAI()"><app-icon name="sparkles" [size]="15" />AI Assistant</a>
          <a class="sub" [class.on]="page() === 'skills'" (click)="page.set('skills'); loadSkills()"><app-icon name="checklist" [size]="15" />Instructions &amp; Skills</a>
          <a [class.on]="page() === 'integrations'" (click)="page.set('integrations'); loadInteg()"><app-icon name="connect" [size]="15" />Integrations</a>
          <div class="cat-g">System</div>
          <a [class.on]="page() === 'cards'" (click)="page.set('cards'); loadCards()"><app-icon name="card" [size]="15" />Smart Cards</a>
          <a [class.on]="page() === 'updates'" (click)="page.set('updates')"><app-icon name="down" [size]="15" />Updates</a>
        }
        </nav>
        <div class="modal-body">
          <h2 class="page-title">{{ pageName(page()) }}</h2>
          @switch (page()) {
            @case ('general') {
              <div class="grid">
                <h4 class="gsec">Appearance</h4>
                <label>UI theme</label>
                
                  <div class="themes">
                    @for (t of themeTiles; track t.v) {
                      <button type="button" class="tht" [class.on]="(s.theme || 'system') === t.v" (click)="s.theme = t.v"><span class="pv {{ t.v }}"></span>{{ t.label }}</button>
                    }
                  </div>
                
                
                  <label>Style</label>
                  <div class="seg w2"><button type="button" [class.on]="s.style === 'standard'" (click)="s.style = 'standard'">Standard</button><button type="button" [class.on]="s.style !== 'standard'" (click)="s.style = ''">Glacier</button></div>
                  <div class="hint">Glacier: frosted glass panels and raised cards, three per row - for the light and the dark theme.</div>
                  <label>Folder and file icons</label>
                  <div class="seg w3"><button type="button" [class.on]="!s.iconFinish" (click)="s.iconFinish = ''">As the style</button><button type="button" [class.on]="s.iconFinish === 'glacier'" (click)="s.iconFinish = 'glacier'">Glass</button><button type="button" [class.on]="s.iconFinish === 'flat'" (click)="s.iconFinish = 'flat'">Flat</button></div>
                  <div class="hint">Glass: folders with a front flap and a light edge, files as a page with a type label - in the Session Manager, the Files panel and SFTP. <i>As the style</i> = glass in Glacier, flat in Standard.</div>
                
                <label>Language</label>
                <select name="lang" [ngModel]="s.language || ''" (ngModelChange)="s.language = $event">
                  @for (l of languages; track l.code) { <option [value]="l.code" translate="no">{{ l.name }}</option> }
                </select>
                <div class="f"><label>Network map style</label>
                  <select [ngModel]="s.mapLook || 'flow'" (ngModelChange)="s.mapLook = $event">
                    @for (l of looks; track l.id) { <option [value]="l.id">{{ l.label }}</option> }
                  </select>
                  <div class="hint">How the Network map is drawn. It can also be changed on the map itself.</div>
                </div>
                <div class="pair">
                <div class="f"><label>Folder colours</label>
                <div>
                  <div class="seg w3"><button type="button" [class.on]="!s.folderColors" (click)="s.folderColors = ''">Soft</button><button type="button" [class.on]="s.folderColors === 'vivid'" (click)="s.folderColors = 'vivid'">Vivid</button><button type="button" [class.on]="s.folderColors === 'steel'" (click)="s.folderColors = 'steel'">Steel blue</button></div>
                  <div class="hint">Folders by level, and the environment tags (PROD, LAB…): soft tones with tinted tags, bright colours with solid tags, or blue-grey with outlined tags.</div>
                </div></div>
                <div class="f"><label>Session properties</label>
                <div>
                  <div class="seg w2"><button type="button" [class.on]="!s.sessionEditor" (click)="s.sessionEditor = ''">Side panel</button><button type="button" [class.on]="s.sessionEditor === 'window'" (click)="s.sessionEditor = 'window'">Full window</button></div>
                  <div class="hint">Where a session's properties open. The side panel shows the main settings, with More Options… for every page; the full window shows every page at once.</div>
                </div></div>
                <div class="f"><label>Start page buttons</label>
                <div>
                  
                    <div class="seg w3"><button type="button" [class.on]="!s.quickButtons" (click)="s.quickButtons = ''">Raised</button><button type="button" [class.on]="s.quickButtons === 'keycap'" (click)="s.quickButtons = 'keycap'">Keycaps</button><button type="button" [class.on]="s.quickButtons === 'glow'" (click)="s.quickButtons = 'glow'">Glow cards</button></div>
                  
                  <div class="hint">Quick Connect, New Session, Local Shell, Import and Discover Devices on the start page.</div>
                </div></div>
                <div class="f"><label>Icons</label>
                <div>
                  
                    <div class="seg w2"><button type="button" [class.on]="!s.iconStyle" (click)="s.iconStyle = ''">Tiles</button><button type="button" [class.on]="s.iconStyle === 'glyph'" (click)="s.iconStyle = 'glyph'">Glyphs</button></div>
                  
                  
                    <label class="inline-check" style="margin-top: 6px"><input type="checkbox" name="compact" [ngModel]="!!s.compactRows" (ngModelChange)="s.compactRows = $event" />
                      Compact Session Manager (one line per session - more sessions in view)</label>
                  
                </div></div>
                </div>
                <label>Interface font</label>
                <select name="uifont" [ngModel]="s.uiFont || ''" (ngModelChange)="s.uiFont = $event">@for (f of uiFonts; track f.v) { <option [value]="f.v">{{ f.label }}</option> }</select>
                <label>Text size</label>
                <select name="uisize" [ngModel]="s.uiFontSize || 13" (ngModelChange)="s.uiFontSize = +$event">@for (z of uiSizes; track z.v) { <option [ngValue]="z.v">{{ z.label }}</option> }</select>
                <label></label>
                <div class="ui-sample" [style.font-family]="fontStack(s.uiFont)" [style.font-size.px]="s.uiFontSize || 13">
                  Session Manager · Quick Connect · core-sw1 10.10.0.1 · 0123456789<br /><span class="hint">Applies to menus, dialogs and the Session Manager; the terminal font is under Terminal &amp; Appearance.</span></div>
                <label>Accent color</label>
                <div class="row"><input type="color" name="acc" class="swatch" [ngModel]="s.accentColor || '#4f9cf7'" (ngModelChange)="s.accentColor = $event" />
                  <button type="button" (click)="s.accentColor = ''">Default</button></div>
                <h4 class="gsec">Behaviour</h4>
                <label class="cl">Behavior</label>
                <div class="checks">
                  <label><input type="checkbox" name="cc" [(ngModel)]="s.confirmClose" />  <span class="tx">Confirm before closing connected tabs<small>and before quitting</small></span> </label>
                  <label><input type="checkbox" name="cpaste" [(ngModel)]="s.confirmPaste" />  <span class="tx">Confirm pasting more than one line<small>shows the lines first - production tabs always ask</small></span> </label>
                  <label><input type="checkbox" name="kawake" [ngModel]="s.keepAwake !== false" (ngModelChange)="s.keepAwake = $event" />
                     <span class="tx">Keep the computer awake while Termwright is open<small>it does not go to sleep on its own, so sessions, transfers and the lab keep running; the display can still turn off{{ keepAwakeErr ? ' - not possible here: ' + keepAwakeErr : '' }}</small></span> </label>
                  <label><input type="checkbox" name="rhist" [ngModel]="s.recordHistory !== false" (ngModelChange)="s.recordHistory = $event" />
                     <span class="tx">Remember typed commands<small>for Command History and suggestions - never commands with passwords, secrets or keys</small></span> </label>
                  <label><input type="checkbox" name="qcs" [ngModel]="!!s.quickConnectSave" (ngModelChange)="s.quickConnectSave = $event" />  <span class="tx">Save Quick Connect sessions<small>every connection made with Quick Connect is kept in the Session Manager (in the selected folder)</small></span> </label>
                  <label><input type="checkbox" name="nmc" [ngModel]="!s.noMoveConfirm" (ngModelChange)="s.noMoveConfirm = !$event" />  <span class="tx">Confirm before moving sessions<small>dragging or pasting sessions and folders into another folder asks first (Undo is in the Session Manager's ⋯ menu)</small></span> </label>
                  <label><input type="checkbox" name="ndc" [ngModel]="!s.noDeleteConfirm" (ngModelChange)="s.noDeleteConfirm = !$event" />  <span class="tx">Confirm before deleting sessions<small>deleting sessions and folders asks first (Undo is in the Session Manager's ⋯ menu); a jump host that other sessions use always asks</small></span> </label>
                  <label><input type="checkbox" name="rt" [(ngModel)]="s.restoreTabs" />  <span class="tx">Reopen the tabs that were open<small>when Termwright starts</small></span> </label>
                  <label><input type="checkbox" name="sm" [(ngModel)]="s.showSessionManager" />  <span class="tx">Show Session Manager<small>the session list on the left (or right)</small></span> </label>
                  <label><input type="checkbox" name="bb" [(ngModel)]="s.showButtonBar" />  <span class="tx">Show Button Bar<small>buttons that send commands, under the terminal</small></span> </label>
                  <label><input type="checkbox" name="cw" [(ngModel)]="s.showCommandWindow" />  <span class="tx">Show Command Window<small>type a command and send it to one or more tabs</small></span> </label>
                  
                </div>
                <label>Sessions named by an address</label>
                <div>
                  <div class="seg w3"><button type="button" [class.on]="!s.nameAfterDevice" (click)="s.nameAfterDevice = ''">Suggest the device’s name</button><button type="button" [class.on]="s.nameAfterDevice === 'auto'" (click)="s.nameAfterDevice = 'auto'">Rename automatically</button><button type="button" [class.on]="s.nameAfterDevice === 'off'" (click)="s.nameAfterDevice = 'off'">Off</button></div>
                  <div class="hint">After connecting to a session named like 10.1.1.1, Termwright reads the name the device gives itself (its hostname) and offers it, or renames the session with Undo. Team devices are named by the team. Right-click a folder → Name After Devices… does many at once.</div>
                </div>
                <h4 class="gsec">Folders and files</h4>
                <label>Sessions folder</label>
                <div class="row"><input name="sd" [(ngModel)]="s.sessionsDir" /><button type="button" (click)="browse('sessionsDir')">Browse...</button></div>
                <label></label>
                <div class="hint">Sessions are stored as one .ini file per session, in the layout other terminal programs use too.
                  You can point this at an existing "Config/Sessions" folder to share sessions (back it up first).</div>
                <label>Logs folder</label>
                <div class="row"><input name="ld" [(ngModel)]="s.logDir" /><button type="button" (click)="browse('logDir')">Browse...</button></div>
                <label>Recordings folder</label>
                <div class="row"><input name="rd" [(ngModel)]="s.recordDir" placeholder="Documents/Termwright Recordings" /><button type="button" (click)="browse('recordDir')">Browse...</button></div>
                <label>Session recording</label>
                <div class="checks">
                  <label><input type="checkbox" name="recall" [(ngModel)]="s.recordSessions" /> Record every connection of every session</label>
                  <label>Delete recordings after <input name="rkeep" type="number" min="0" style="width: 70px" [(ngModel)]="s.recordKeepDays" /> days (0 = keep them)</label>
                </div>
                <label>Downloads folder</label>
                <div class="row"><input name="dd" [(ngModel)]="s.downloadDir" /><button type="button" (click)="browse('downloadDir')">Browse...</button></div>
                <label>SFTP from a terminal</label>
                <div class="checks">
                  <label><input type="checkbox" name="scwd" [(ngModel)]="s.sftpCwdAuto" /> Working directory detection: turn on folder reporting in the shell (bash, zsh) when an SFTP tab is opened from it</label>
                  <label><input type="checkbox" name="sfol" [(ngModel)]="s.sftpFollowTerminal" /> SFTP tabs follow the terminal folder</label>
                  <label><input type="checkbox" name="sfus" [(ngModel)]="s.sftpFollowUser" /> SFTP tabs and the Files panel browse as the terminal's user (root via sudo after "sudo su -")</label>
                </div>
                <label>Python interpreter</label>
                <input name="py" [(ngModel)]="s.pythonPath" placeholder="(automatic: python3, or py -3 on Windows)" />
                <label></label>
                <div class="hint">Used to run Python (.py) scripts with the <code>crt</code> object. Python 3 must be installed.</div>
                <label></label><div class="hint">Files received with ZMODEM (remote "sz") are saved here.</div>
                <label>Data folder</label><div class="hint mono">{{ dataDir() }}</div>
                <label>Backup</label>
                <div>
                  <div class="row"><button type="button" [disabled]="bkBusy()" (click)="backupNow()">Back Up Now</button>
                    <button type="button" [disabled]="bkBusy()" (click)="restoreBackup()">Restore from Backup…</button></div>
                  <div class="hint">A zip in Documents/Termwright Backups with your settings, sessions, accounts, library, device types, AI chats and team links - everything in the data folder but logs. Passwords and keys stay in the OS keychain.</div>
                  @if (bkMsg()) { <div class="hint">{{ bkMsg() }}</div> }
                </div>
                <label>Sync</label>
                <div class="sync">
                  <div class="row"><input name="syncf" [(ngModel)]="s.syncFolder" placeholder="Off - choose a folder your computers share (OneDrive, iCloud Drive, Dropbox, a network share)" />
                    <button type="button" (click)="browseSync()">Choose Folder...</button>
                    @if (s.syncFolder) { <button type="button" (click)="s.syncFolder = ''">Turn Off</button> }</div>
                  <label class="inline-check"><input type="checkbox" name="synco" [(ngModel)]="s.syncOptions" [disabled]="!s.syncFolder" /> Global Options too (theme, fonts, terminal and the rest of this window)</label>
                  <div class="hint">Your sessions, accounts, device types, folder defaults and library (commands, checklists, buttons, workspaces…) go to and come from your other computers that use the same folder, every half minute. Passwords and keys never do - they stay in each computer's keychain. Team folders come from the team server. A session changed or deleted on one computer is changed or deleted on the others, unless it was changed there later.</div>
                  @if (syncSt(); as st) {
                    @if (st.folder) {
                      <div class="hint">{{ st.error ? 'Sync: ' + st.error : 'Last synced ' + ago(st.last) + (st.peers?.length ? ' · with ' + st.peers!.length + ' other computer' + (st.peers!.length === 1 ? '' : 's') : ' · no other computer in this folder yet') }}
                        <button type="button" class="link" (click)="syncNow()" [disabled]="s.syncFolder !== st.folder">Sync Now</button></div>
                    }
                  }
                </div>
              </div>
            }
            @case ('terminal') {
              <div class="grid">
                <label>Font</label><input name="ff" [(ngModel)]="s.fontFamily" />
                <label>Font size</label><input name="fs" type="number" min="6" max="48" class="short" [(ngModel)]="s.fontSize" />
                <label>Line spacing</label>
                <select name="lh" [ngModel]="s.lineHeight || 1" (ngModelChange)="s.lineHeight = +$event">
                  <option [ngValue]="1">Normal - the font's own (as most terminal programs)</option><option [ngValue]="1.1">A little more (1.1)</option>
                  <option [ngValue]="1.2">More (1.2)</option><option [ngValue]="1.35">Roomy (1.35)</option></select>
                <label>Color scheme</label>
                <select name="sch" [(ngModel)]="s.terminalScheme">
                  <option value="Auto">Auto (match the UI theme: Default / Light)</option>
                  @for (c of allSchemes(); track c) { <option [value]="c">{{ c }}</option> }
                </select>
                <label></label>
                <div class="checks">
                  <label><input type="checkbox" name="fts" [(ngModel)]="s.forceTerminalScheme" /> Use this scheme for all sessions (ignore schemes set in Session Options or imported sessions)</label>
                </div>
                <label>Cursor</label>
                <div class="row">
                  <select name="cs" [(ngModel)]="s.cursorStyle"><option value="block">Block</option><option value="underline">Underline</option><option value="bar">Vertical bar</option></select>
                  <label><input type="checkbox" name="cb" [(ngModel)]="s.cursorBlink" /> Blink</label>
                </div>
                <label>Scrollback lines</label><input name="sb" type="number" min="100" class="short" [(ngModel)]="s.scrollback" />
                <label>Terminal bar</label>
                <div class="checks"><label><input type="checkbox" name="tbar" [ngModel]="!s.hideTerminalBar" (ngModelChange)="s.hideTerminalBar = !$event" /> Show Timestamps, Auto scroll and Clear under SSH, Telnet and local tabs (serial tabs always have their send bar)</label></div>
                <label>Mouse</label>
                <div class="checks">
                  <label><input type="checkbox" name="cos" [(ngModel)]="s.copyOnSelect" /> Copy on select</label>
                  <label><input type="checkbox" name="rcp" [(ngModel)]="s.rightClickPaste" /> Paste on right-click (Shift+right-click opens the menu)</label>
                </div>
                <label>Paste line delay</label>
                <div class="row"><input name="pld" type="number" min="0" class="short" [(ngModel)]="s.pasteLineDelayMs" /> ms between lines (0 = off; useful for slow network devices)</div>
              </div>
            }
            @case ('envs') {
              <p class="hint">An environment marks sessions and folders with a colour and a short tag (on tabs, cards and in the Session Manager).
                A folder in an environment - and everything in it, subfolders and sessions - takes its colour and tag, unless a subfolder or
                session has an environment of its own. The ten environment colours are never folder colours (amber, violet, blue, teal, pink and lime
                mark the top level and the five levels of subfolders), so a folder's colour always tells which it is.</p>
              <div class="envs">
                @for (e of s.environments ?? []; track $index; let i = $index) {
                  <div class="envrow">
                    <span class="tagpv" [style.background]="e.color" [style.color]="envFg(e.color)">{{ e.short || '?' }}</span>
                    <input class="lab" [(ngModel)]="e.label" [name]="'el' + i" placeholder="Name, e.g. Pre-production" />
                    <input class="sh mono" [ngModel]="e.short" (ngModelChange)="e.short = $event.toUpperCase().slice(0, 6)" [name]="'es' + i" placeholder="TAG" maxlength="6" />
                    <div class="sw">
                      @for (c of envColors; track c.c) {
                        <button type="button" class="dotc" [class.on]="e.color === c.c" [style.background]="c.c" [title]="c.n" (click)="e.color = c.c"></button>
                      }
                    </div>
                    <label class="inline-check" title="Pastes into its tabs are confirmed and the tab shows the tag, as for production">
                      <input type="checkbox" [(ngModel)]="e.protect" [name]="'ep' + i" /> Like production</label>
                    <button type="button" class="icon-btn" [disabled]="e.id === 'production'" [title]="e.id === 'production' ? 'Production stays (you can rename and recolour it)' : 'Remove this environment'"
                      (click)="removeEnv(i)"><app-icon name="trash" [size]="14" /></button>
                  </div>
                }
              </div>
              <div class="row" style="margin-top: 10px"><button type="button" (click)="addEnv()"><app-icon name="plus" [size]="14" class="md-only" /> New Environment</button>
                <button type="button" class="link" (click)="resetEnvs()">Back to the four built-in ones</button></div>
              <div class="hint">Renaming or recolouring an environment changes it on every session and folder that has it. A removed environment no longer shows on them (folder defaults that named it are cleared).</div>
            }
            @case ('schemes') {
              <div class="grid">
                <label>Scheme</label>
                <div class="row">
                  <select name="schEdit" [ngModel]="schemeName()" (ngModelChange)="schemeName.set($event)">
                    @for (c of allSchemes(); track c) { <option [value]="c">{{ c }}{{ isCustom(c) ? ' (custom)' : '' }}</option> }
                  </select>
                  <button type="button" (click)="copyScheme()">Copy as new...</button>
                  <button type="button" [disabled]="!isCustom(schemeName())" (click)="deleteScheme()">Delete</button>
                  <button type="button" (click)="importSchemes()">Import...</button>
                  <button type="button" [disabled]="!s.customSchemes?.length" (click)="exportSchemes()">Export...</button>
                </div>
              </div>
              <div class="scheme-preview" [style.background]="colors()['background']" [style.color]="colors()['foreground']">
                <div>user&#64;router:~$ <span [style.background]="colors()['cursor']">&nbsp;</span></div>
                <div class="sw">
                  @for (k of ansi; track k) { <span [style.color]="colors()[k]">■</span> }
                </div>
                <div><span [style.background]="colors()['selectionBackground']">selected text</span>
                  <span [style.color]="colors()['red']"> ERROR</span>&nbsp;<span [style.color]="colors()['green']">OK</span>
                  <span [style.color]="colors()['yellow']"> WARN</span> <span [style.color]="colors()['blue']"> 10.0.0.1</span></div>
              </div>
              @if (isCustom(schemeName())) {
                <div class="color-grid">
                  @for (k of colorKeys; track k) {
                    <label><input type="color" [name]="'c-' + k" class="swatch" [ngModel]="colors()[k]" (ngModelChange)="setColor(k, $event)" /> {{ k }}</label>
                  }
                </div>
              } @else {
                <p class="hint">Built-in schemes can't be edited. Use <b>Copy as new</b> to make an editable copy.</p>
              }
            }
            @case ('keywords') {
              <div class="grid">
                <label>Default set</label>
                <select name="dks" [(ngModel)]="s.defaultKeywordSet">
                  <option value="">None</option>
                  @for (k of s.keywordSets ?? []; track $index) { <option [value]="k.name">{{ k.name }}</option> }
                </select>
                <label></label><div class="hint">Off (None) by default. Pick a set, e.g. <b>Network</b>, to highlight errors, up / down and addresses in every session; Session Options → Terminal → <i>Keyword highlighting</i> picks one for a single session.</div>
                <label>Style</label>
                <select name="hls" [ngModel]="s.highlightStyle || ''" (ngModelChange)="s.highlightStyle = $event">
                  <option value="">Colour the text (up in green, down in red)</option>
                  <option value="background">Colour the background behind the text</option>
                </select>
                <label>Edit set</label>
                <div class="row">
                  <select name="kse" [ngModel]="kwIndex()" (ngModelChange)="kwIndex.set(+$event)">
                    @for (k of s.keywordSets ?? []; track $index) { <option [ngValue]="$index">{{ k.name }}</option> }
                  </select>
                  <button type="button" (click)="addSet()">New...</button>
                  <button type="button" [disabled]="!curSet()" (click)="renameSet()">Rename...</button>
                  <button type="button" [disabled]="!curSet()" (click)="deleteSet()">Delete</button>
                </div>
              </div>
              @if (curSet(); as set) {
                <table class="list" style="margin-top: 12px">
                  <thead><tr><th>Text or pattern</th><th>Regex</th><th>Match case</th><th>Color</th><th></th></tr></thead>
                  <tbody>
                    @for (k of set.keywords; track $index) {
                      <tr>
                        <td><input [name]="'kt' + $index" [(ngModel)]="k.text" class="mono" /></td>
                        <td class="c"><input type="checkbox" [name]="'kr' + $index" [(ngModel)]="k.regex" /></td>
                        <td class="c"><input type="checkbox" [name]="'kc' + $index" [(ngModel)]="k.caseSensitive" /></td>
                        <td class="c"><input type="color" [name]="'kk' + $index" [(ngModel)]="k.color" class="swatch" /></td>
                        <td><button type="button" class="icon-btn" (click)="set.keywords.splice($index, 1)">✕</button></td>
                      </tr>
                    }
                  </tbody>
                </table>
                <button type="button" (click)="set.keywords.push({ text: '', regex: false, caseSensitive: false, color: '#6b5310' })">Add keyword</button>
                <p class="hint">Sessions can pick a set under Session Options → Terminal. Highlighting is skipped in full-screen apps (vi, less).</p>
              }
            }
            @case ('keymap') {
              <p class="hint">Click a <b>Keys</b> box and press the combination. Each binding can send text (\\r = Enter), run an action or run a script.
                Bindings apply while a terminal has focus and take priority over the remote application.</p>
              <table class="list">
                <thead><tr><th>Keys</th><th>Send text</th><th>Action</th><th>Script</th><th></th></tr></thead>
                <tbody>
                  @for (k of s.keyMap ?? []; track $index) {
                    <tr>
                      <td><input [name]="'kk' + $index" [value]="k.keys" readonly class="mono keycap" placeholder="press keys"
                                 (keydown)="capture($event, k)" /></td>
                      <td><input [name]="'ks' + $index" [(ngModel)]="k.send" class="mono" /></td>
                      <td><select [name]="'ka' + $index" [(ngModel)]="k.action">
                        <option value="">-</option>
                        @for (a of actions; track a[0]) { <option [value]="a[0]">{{ a[1] }}</option> }
                      </select></td>
                      <td><input [name]="'kx' + $index" [(ngModel)]="k.script" placeholder="path/to/script.js" /></td>
                      <td><button type="button" class="icon-btn" (click)="s.keyMap!.splice($index, 1)">✕</button></td>
                    </tr>
                  }
                </tbody>
              </table>
              <button type="button" (click)="(s.keyMap ??= []).push({ keys: '', send: '', action: '', script: '' })">Add binding</button>
            }
            @case ('devices') {
              <p class="hint">A device type is a login profile. Pick it in Quick Connect, the Connect Bar (<code>voss 10.1.1.1</code>) or a
                session's Connection page, and only the address is needed. <b>Login steps</b> answer username/password prompts and run for
                Telnet/Serial; <b>after-login steps</b> run for every protocol. In Send, <code>\\s</code> = username, <code>\\w</code> = password
                (from the device type, the session, or asked once).</p>
              <div class="row dt-pick">
                <select name="dtSel" [ngModel]="dtIndex()" (ngModelChange)="dtIndex.set(+$event)">
                  @for (d of s.deviceTypes ?? []; track $index) { <option [ngValue]="$index">{{ d.name }}</option> }
                </select>
                <button type="button" (click)="addDevice()">New</button>
                <button type="button" (click)="copyDevice()" [disabled]="!curDevice()">Duplicate</button>
                <button type="button" (click)="removeDevice()" [disabled]="!curDevice()">Delete</button>
              </div>
              @if (curDevice(); as d) {
                <div class="grid">
                  <label>Name</label><input name="dtn" [(ngModel)]="d.name" />
                  <label>Icon</label>
                  <select name="dti" [ngModel]="d.icon || ''" (ngModelChange)="d.icon = $event || undefined" title="The picture of this device type's sessions">
                    <option value="">From the name</option>
                    @for (k of deviceKinds; track k.v) { <option [value]="k.v">{{ k.label }}</option> }
                  </select>
                  <label>Default protocol</label>
                  <div class="row">
                    <select name="dtp" [(ngModel)]="d.protocol">@for (p of protocols; track p) { <option [value]="p">{{ p }}</option> }</select>
                    Port <input name="dtport" type="number" class="short" [(ngModel)]="d.port" placeholder="default" />
                  </div>
                  <label>Account</label>
                  <select name="dtc" [(ngModel)]="d.credential" title="Sessions of this device type that keep the Default account log in with it (a folder's account comes first)">
                    <option value="">(none - the username and password below)</option>
                    @for (c of s.credentials ?? []; track c.name) { <option [value]="c.name">{{ c.name }}{{ c.username ? ' · ' + c.username : '' }}</option> }</select>
                  @if (d.credential) {
                    <label></label><div class="hint">Sessions of this device type with the <b>Default</b> account log in with the account <b>{{ d.credential }}</b> (a folder's account comes first). Passwords &amp; Accounts changes its password.</div>
                  } @else {
                  <label>Username</label><input name="dtu" [(ngModel)]="d.username" placeholder="e.g. admin (blank = session's, or asked)" />
                  }
                  @if (!d.credential && pwOf(d); as p) {
                    <label>Password</label>
                    <div class="row wrap">
                      <input name="dtpw" [type]="isRef(p.pw) ? 'text' : 'password'" autocomplete="new-password" [(ngModel)]="p.pw" [disabled]="p.empty" [placeholder]="pwPh(p)" />
                      <button type="button" title="Read the password from 1Password, Bitwarden, HashiCorp Vault or a command" (click)="pickRef(p)">Password manager...</button>
                      <label class="inl"><input type="checkbox" name="dtnp" [(ngModel)]="p.empty" (ngModelChange)="$event && (p.pw = '')" /> No password</label>
                      @if ((p.state === 'set' || refOf(p.state)) && !p.forget && !p.pw) { <button type="button" (click)="p.forget = true">Forget</button> }
                    </div>
                    <label></label><div class="hint">Saved in the OS keychain. Sessions with this device type use this username and password
                      unless they choose another Account (or their own username and password). Tick <b>No password</b> for devices like EXOS where
                      <code>admin</code> has an empty password - you will not be asked again.</div>
                  }
                  <label>Prompt (regex)</label><input name="dtpr" class="mono" [(ngModel)]="d.prompt" placeholder="e.g. :\\d+[>#] ?$" />
                  <label>Detect (regex)</label>
                  <div class="webd">
                    <div class="row2"><input name="dtdt" class="mono" [(ngModel)]="d.detect" placeholder="e.g. (?i)huawei|\\bvrp\\b" title="Recognises this kind of device: matched against what Discover Devices sees (SSH and Telnet banners, SNMP system description, web page) and, with Identify Device Type, against the output of the command beside it" />
                      <input name="dtdc" class="mono" [(ngModel)]="d.detectCommand" placeholder="show version" title="Identify Device Type runs it after logging in and matches the pattern against its output" /></div>
                    <div class="row2"><input name="dtdx" class="mono" [(ngModel)]="detectSample" placeholder="Paste a banner or show version output to test the pattern" />
                      <span class="hint" [class.bad]="detectTest(d) === 'bad'">{{ detectTest(d) === 'bad' ? 'The pattern is not a valid regular expression' : detectTest(d) ? 'Matches: ' + detectTest(d) : detectSample.trim() ? 'No match' : '' }}</span></div>
                  </div>
                  <label></label><div class="hint">Discover Devices picks this device type when the pattern matches a device, and right-click a session → <b>Identify Device Type</b> checks a saved session. One pattern per kind of device is enough - no built-in type needed.</div>
                  <label>Backup commands</label>
                  <textarea name="dtbk" class="mono" rows="2" [(ngModel)]="d.backupCommands"
                    [placeholder]="backupDefault(d.name) || 'Commands that print the configuration, one per line, e.g. show running-config'"></textarea>
                  <label>Undo rules</label>
                  <textarea name="dtun" class="mono" rows="3" [(ngModel)]="d.undoRules" [placeholder]="undoDefault(d.name) || 'How the test recorder suggests the cleanup of a command, one rule per line: regular expression => undo, e.g. ^create vlan (\\S+).* => delete vlan $1'"></textarea>
                  <label></label><div class="hint">The test recorder suggests each recorded command's undo for the cleanup. <code>$1</code>… are the groups, <code>$0</code> the whole command; an empty undo means the command needs none; the first matching rule wins. Empty: the built-in rules for {{ d.name || 'this type' }}{{ undoDefault(d.name) ? '' : ' (none)' }}.</div>
                  <label>Test save / restore</label>
                  <div class="row2"><textarea name="dtts" class="mono" rows="2" [(ngModel)]="d.testSave" [placeholder]="testStateDefault(d.name, 0) || 'Save the configuration, e.g. checkpoint {file}'"></textarea>
                    <textarea name="dttr" class="mono" rows="2" [(ngModel)]="d.testRestore" [placeholder]="testStateDefault(d.name, 1) || 'Put it back, e.g. rollback running-config checkpoint {file}'"></textarea></div>
                  <label></label><div class="hint">For tests with <b>Afterwards put back: configuration</b>: run before the test and after its cleanup, one command per line; <code>{{ '{file}' }}</code> is a name for the saved copy.</div>
                  <label>Error patterns</label>
                  <textarea name="dter" class="mono" rows="2" [(ngModel)]="d.errorPatterns" [placeholder]="errorsDefault(d.name) || 'Error messages of the device, one regular expression per line, e.g. ^ *% *Invalid'"></textarea>
                  <label></label><div class="hint">A test step fails when the device answers a command with a line that matches (unless the step has <b>Ignore device errors</b>). Empty: the built-in patterns{{ errorsDefault(d.name) ? '' : ' (none for this type)' }}.</div>
                  <label>Upgrade</label>
                  <textarea name="dtup" class="mono" rows="2" [(ngModel)]="d.upgrade" placeholder="Commands that install a software image for a test's upgrade step; {{ '{image}' }} is the image, e.g. download image {{ '{image}' }} / reboot"></textarea>
                  <label></label><div class="hint">Termwright answers the confirmations and waits up to 25 minutes until the device logs in again. Never run on production sessions.</div>
                  <label>Factory reset</label>
                  <textarea name="dtfr" class="mono" rows="2" [(ngModel)]="d.factoryReset" [placeholder]="factoryDefault(d.name) || 'Commands that reset to factory default and reboot, e.g. write erase / reload'"></textarea>
                  <label></label><div class="hint">For test runs with <b>On a failed cleanup: Factory default</b>; Termwright answers the confirmations and waits up to 15 minutes for the device. Never run on production sessions.</div>
                  <label>Network map</label>
                  <div class="mapd">
                    <div class="hint">How the network map reads a device of this type with TextFSM templates - for any vendor. Neighbour templates give <code>LOCAL_PORT</code>, <code>NEIGHBOR</code>, <code>NEIGHBOR_PORT</code>, <code>CHASSIS_ID</code>, <code>MGMT_IP</code> (ntc-templates names work too); identity <code>HOSTNAME</code>, <code>CHASSIS_ID</code>, <code>MODEL</code>; ports <code>PORT</code>, <code>ADMIN</code>, <code>OPER</code>, <code>SPEED</code>. Only show/display commands run. {{ mapNote(d) }}</div>
                    <div class="row2"><input name="dtmp" class="mono" [ngModel]="d.map?.paging" (ngModelChange)="setMap(d, 'paging', $event)" placeholder="Paging off, e.g. screen-length 0 temporary" />
                      <button type="button" (click)="mapFromBuiltin(d)" [disabled]="!hasBuiltinMap(d.name)" title="Fill in the plan Termwright ships for this type, to change it">Start From Built-in</button></div>
                    @for (k of mapKinds; track k.key) {
                      <div class="mapk"><b>{{ k.label }}</b>
                        <input [name]="'dtmc' + k.key" class="mono" [ngModel]="mapGet(d, k.key)" (ngModelChange)="setMap(d, k.key, $event)" [placeholder]="k.ph" />
                        <textarea [name]="'dtmt' + k.key" class="mono" rows="4" [ngModel]="mapGet(d, k.key + 'Template')" (ngModelChange)="setMap(d, k.key + 'Template', $event)" placeholder="TextFSM template: Value LOCAL_PORT (\\S+) … Start …"></textarea>
                      </div>
                    }
                    <details class="maptest"><summary>Test a template on pasted output</summary>
                      <div class="row2"><select name="dtmts" [(ngModel)]="mapTestKind">@for (k of mapKinds; track k.key) {<option [value]="k.key">{{ k.label }}</option>}</select>
                        <button type="button" (click)="mapTest(d)">Test</button></div>
                      <textarea name="dtmto" class="mono" rows="5" [(ngModel)]="mapTestOut" placeholder="Paste what the device printed for the command"></textarea>
                      @if (mapTestMsg()) { <div class="hint">{{ mapTestMsg() }}</div> }
                      @if (mapTestRows().length) { <table class="maprows"><tr>@for (h of mapTestHead(); track h) {<th>{{ h }}</th>}</tr>
                        @for (r of mapTestRows(); track $index) {<tr>@for (h of mapTestHead(); track h) {<td>{{ r[h] }}</td>}</tr>}</table> }
                    </details>
                  </div>
                  <label>Web server</label>
                  <div class="webd">
                    <textarea name="dtwc" class="mono" rows="1" [(ngModel)]="d.webCheck" [placeholder]="webDefault(d.name, 'check') || 'A command that shows the web server, e.g. show web-server'" title="Open Web Interface runs it in the background to see whether HTTP and HTTPS are on"></textarea>
                    <div class="row2"><input name="dtwh" class="mono" [(ngModel)]="d.webHttp" [placeholder]="webDefault(d.name, 'http') || 'HTTP on (regex)'" title="Matches the check's output while HTTP is on" /><input name="dtws" class="mono" [(ngModel)]="d.webHttps" [placeholder]="webDefault(d.name, 'https') || 'HTTPS on (regex)'" title="Matches the check's output while HTTPS is on" /></div>
                    <textarea name="dtwo" class="mono" rows="2" [(ngModel)]="d.webOn" [placeholder]="webDefault(d.name, 'on') || 'Commands that turn HTTP and HTTPS on, one per line ({name} = the session)'" title="Turn On HTTP and HTTPS… in Open Web Interface runs these, after showing them"></textarea>
                    <input name="dtwv" class="mono" [(ngModel)]="d.webSave" [placeholder]="webDefault(d.name, 'save') || 'Command that saves the configuration'" />
                  </div>
                  <label>Tunnel</label>
                  <select name="dttn" [(ngModel)]="d.tunnel" title="Open Web Page Through This Session needs its SSH server to pass connections on (TCP forwarding); EXOS, VOSS and Cisco IOS do not">
                    <option [ngValue]="undefined">{{ tunnelDefault(d.name) }}</option><option value="on">Its SSH server forwards connections</option><option value="off">No forwarding (Open Web Page Through This Session is off)</option></select>
                  <label>AI instructions</label>
                  <textarea name="dtai" rows="2" [(ngModel)]="d.aiInstructions" placeholder="Followed by the AI on sessions of this type, e.g. save configuration after every change"
                    title="Added to the global instructions (Instructions &amp; Skills) when the AI works on a session of this device type"></textarea>
                  <label>AI agent: read-only</label>
                  <textarea name="dtro" class="mono" rows="2" [(ngModel)]="d.aiReadOnly" [placeholder]="readOnlyDefault(d.name)"
                    title="The AI agent runs commands that begin with these without asking (except on production); everything else asks first. One per line."></textarea>
                  <label></label>
                  <div class="checks"><label><input type="checkbox" name="dticr" [(ngModel)]="d.initialCr" /> Send initial carriage return</label></div>
                </div>
                <h4>Device facts</h4>
                <p class="hint">Values read after connecting, in a background login (nothing is typed in your tab): shown on the tab, the status bar,
                  the session and HTML reports. The first ( ) group of the pattern is the value. Easiest: run the command in a tab, select the value,
                  right-click → <b>Save Selection as Device Fact</b> - the pattern is made for you.</p>
                <table class="list facts">
                  <thead><tr><th>Name</th><th>Command</th><th>Pattern (regex)</th><th class="c" title="Read at every connect; otherwise once per session">Every connect</th><th></th></tr></thead>
                  <tbody>
                    @for (f of d.facts ?? []; track $index) {
                      <tr>
                        <td><input [name]="'fn' + $index" [(ngModel)]="f.name" list="tw-fact-names" style="width: 100px" /></td>
                        <td><input class="mono" [name]="'fc' + $index" [(ngModel)]="f.command" /></td>
                        <td><input class="mono" [name]="'fr' + $index" [(ngModel)]="f.regex" [class.err]="!reOk(f.regex)" [title]="reOk(f.regex) ? '' : 'Not a valid regular expression'" /></td>
                        <td class="c"><input type="checkbox" [name]="'fe' + $index" [(ngModel)]="f.every" /></td>
                        <td><button type="button" class="icon-btn" (click)="d.facts!.splice($index, 1)">✕</button></td>
                      </tr>
                    }
                  </tbody>
                </table>
                <datalist id="tw-fact-names">@for (n of factNames; track n) { <option [value]="n"></option> }</datalist>
                <div class="row">
                  <button type="button" (click)="(d.facts = d.facts ?? []).push({ name: '', command: '', regex: '', every: false })">Add fact</button>
                </div>
                @for (part of stepParts; track part.key) {
                  <h4>{{ part.title }}</h4>
                  <table class="list">
                    <thead><tr><th>Expect</th><th>Send</th><th class="c">Hide</th><th class="c">CR</th><th></th></tr></thead>
                    <tbody>
                      @for (a of steps(d, part.key); track $index) {
                        <tr>
                          <td><input [name]="part.key + 'e' + $index" [(ngModel)]="a.expect" /></td>
                          <td><input class="mono" [name]="part.key + 's' + $index" [(ngModel)]="a.send" /></td>
                          <td class="c"><input type="checkbox" [name]="part.key + 'h' + $index" [(ngModel)]="a.hide" /></td>
                          <td class="c"><input type="checkbox" [name]="part.key + 'c' + $index" [(ngModel)]="a.trailingCr" /></td>
                          <td class="nowrap">
                            <button type="button" class="icon-btn" [disabled]="$index === 0" (click)="moveStep(d, part.key, $index, -1)">↑</button>
                            <button type="button" class="icon-btn" (click)="steps(d, part.key).splice($index, 1)">✕</button>
                          </td>
                        </tr>
                      }
                    </tbody>
                  </table>
                  <button type="button" (click)="steps(d, part.key).push({ expect: '', send: '', hide: false, trailingCr: true, credential: '' })">Add step</button>
                }
              }
            }
            @case ('alerts') {
              <p class="hint">Get notified when output in <b>any</b> tab matches - even tabs you are not looking at. Each pattern fires at most
                once every 5 seconds per tab. The tab gets a red dot until you open it.</p>
              <table class="list">
                <thead><tr><th class="c">On</th><th>Pattern</th><th class="c">Regex</th><th class="c">Aa</th><th class="c">Sound</th><th class="c">Desktop</th><th></th></tr></thead>
                <tbody>
                  @for (a of s.alerts ?? []; track $index) {
                    <tr>
                      <td class="c"><input type="checkbox" [name]="'ae' + $index" [(ngModel)]="a.enabled" /></td>
                      <td><input class="mono" [name]="'ap' + $index" [(ngModel)]="a.pattern" placeholder="%LINK-3- or CRITICAL" /></td>
                      <td class="c"><input type="checkbox" [name]="'ar' + $index" [(ngModel)]="a.regex" /></td>
                      <td class="c"><input type="checkbox" [name]="'ac' + $index" [(ngModel)]="a.caseSensitive" title="Case sensitive" /></td>
                      <td class="c"><input type="checkbox" [name]="'as' + $index" [(ngModel)]="a.sound" /></td>
                      <td class="c"><input type="checkbox" [name]="'ad' + $index" [(ngModel)]="a.desktop" title="Operating-system notification when Termwright is in the background" /></td>
                      <td><button type="button" class="icon-btn" (click)="s.alerts!.splice($index, 1)">✕</button></td>
                    </tr>
                  }
                </tbody>
              </table>
              <button type="button" (click)="(s.alerts ??= []).push({ pattern: '', regex: false, caseSensitive: false, enabled: true, sound: true, desktop: true })">Add alert</button>
            }
            @case ('ai') {
              <div class="glist">
                <div class="gt">AI service</div>
                <div class="list">
                  <div class="li"><span class="l">Service</span>
                    <select name="aip" class="v" [ngModel]="s.ai?.provider || ''" (ngModelChange)="setAI({ provider: $event, model: '', baseUrl: '' }); aiModels.set([]); aiCheck.set(null); aiMsg.set('')">
                      <option value="">Off</option>
                      <option value="claudecode">Claude Code - your Claude login</option>
                      <option value="anthropic">Claude API - Anthropic API key</option>
                      <option value="openai">OpenAI-compatible - OpenAI, Azure, a gateway</option>
                      <option value="ollama">Ollama - a local model</option>
                    </select></div>
                  @if (s.ai?.provider === 'anthropic' || s.ai?.provider === 'openai') {
                    <div class="li"><span class="l">API key<small>{{ aiSt()?.hasKey ? 'Saved in the OS keychain' : 'Kept in the OS keychain, never in the settings' }}</small></span>
                      <input name="aik" class="v" type="password" [(ngModel)]="aiKey" autocomplete="off" [placeholder]="aiSt()?.hasKey ? '•••••••• (type to change)' : 'paste the key'" />
                      <button type="button" (click)="saveAIKey()" [disabled]="!aiKey.trim()">Save Key</button>
                      @if (aiSt()?.hasKey) { <button type="button" (click)="forgetAIKey()">Forget</button> }</div>
                  }
                  @if (s.ai?.provider === 'openai' || s.ai?.provider === 'ollama') {
                    <div class="li"><span class="l">Address</span>
                      <input name="aib" class="v" [ngModel]="s.ai?.baseUrl || ''" (ngModelChange)="setAI({ baseUrl: $event })" [placeholder]="s.ai?.provider === 'ollama' ? 'http://localhost:11434' : 'https://api.openai.com/v1'" /></div>
                  }
                  @if (s.ai?.provider === 'claudecode') {
                    <div class="li"><span class="l">Program<small>Empty = found on this computer</small></span>
                      <input name="aicc" class="v mono" [ngModel]="s.ai?.baseUrl || ''" (ngModelChange)="setAI({ baseUrl: $event }); aiCheck.set(null)" placeholder="~/.local/bin/claude" /></div>
                  }
                  @if (s.ai?.provider) {
                    <div class="li"><span class="l">Status<small>{{ aiMsg() || 'Test checks the service with the settings above' }}</small></span>
                      <span class="pill" [class]="'pill ' + aiStatus().cls"><i></i>{{ aiStatus().text }}</span>
                      <button type="button" (click)="loadAIModels()">Test</button></div>
                    <div class="li"><span class="l">Model</span>
                      <app-combo class="v" name="aim" label="Model" [value]="s.ai?.model || ''" (valueChange)="setAI({ model: $event })" [options]="modelOptions()" [loading]="aiLoading()"
                        [loadingText]="tr('Loading the models…')" [emptyText]="tr('The service lists no models - type a model name')" (opened)="!aiModels().length && !aiLoading() && loadAIModels()"
                        [placeholder]="tr(s.ai?.provider === 'claudecode' ? 'Default of Claude Code' : 'The newest the service offers')" /></div>
                    @if (s.ai?.provider === 'claudecode') {
                      <div class="li"><span class="l">Effort<small>How hard the model thinks: higher is slower and uses more of your allowance</small></span>
                        <select name="aie" class="v" [ngModel]="s.ai?.effort || ''" (ngModelChange)="setAI({ effort: $event })">@for (e of efforts; track e.k) { <option [value]="e.k">{{ tr(e.k ? e.name : 'Default of the model') }}</option> }</select></div>
                    }
                  }
                </div>
                <div class="gf">@switch (s.ai?.provider) {
                  @case ('claudecode') { Uses the Claude account Claude Code is signed in to - no API key. Termwright runs it without tools: it only answers. Install it from claude.com/claude-code and run <code>claude</code> once to sign in. }
                  @case ('ollama') { A model on this computer or a lab server - nothing leaves your network. }
                  @case ('') { Off: the ✦ button opens this page. }
                  @default { The device output you choose is sent to this service. }
                }</div>

                <div class="gt">Privacy</div>
                <div class="list">
                  <div class="li"><span class="l">Remove passwords, keys, SNMP communities and tokens<small>Always on</small></span>
                    <span class="sw"><input type="checkbox" checked disabled aria-label="Remove secrets (always on)" /></span></div>
                  <div class="li"><span class="l">Hide IP addresses, MAC addresses and host names<small>They become [host-1], [host-2]… before sending</small></span>
                    <span class="sw"><input type="checkbox" name="aih" [ngModel]="!!s.ai?.hideHosts" (ngModelChange)="setAI({ hideHosts: $event })" aria-label="Hide addresses and host names" /></span></div>
                </div>
                <div class="gf">Nothing is sent until you ask - from the ✦ button or right-click → Explain with AI - and the window shows what was sent. Check your company's policy before sending device output to an AI service.</div>

                <div class="gt" id="opt-mcp">AI apps (MCP connector)</div>
                <div class="list">
                  <div class="li"><span class="l">Let AI apps use Termwright<small>Claude Desktop, Claude Code and other apps that take MCP servers (Cursor, VS Code, Windsurf…) can list your tabs and sessions and read a tab</small></span>
                    @if (s.ai?.mcp) { <span class="pill" [class.ok]="mcp()?.running"><i></i>{{ mcp()?.running ? 'Running' : 'Starts on Save' }}</span> }
                    <span class="sw"><input type="checkbox" name="mcp" [ngModel]="!!s.ai?.mcp" (ngModelChange)="setAI({ mcp: $event })" aria-label="Let AI apps use Termwright" /></span></div>
                  @if (s.ai?.mcp) {
                    <div class="li used"><span class="l">Used by<small>Since Termwright started</small></span>
                      <span class="v2">@for (c of mcp()?.clients ?? []; track c.name) { <span class="cli"><i class="dot" [class.live]="isRecent(c.at)"></i><b>{{ c.name }}</b> · {{ ago(c.at) }}@if (c.calls) { · {{ c.calls === 1 ? '1 tool' : c.calls + ' tools' }}, last {{ c.tool }} }</span> }
                        @empty { <span class="muted">No AI app has used Termwright yet - add it below, then ask the app something about your tabs or lab.</span> }</span></div>
                    <div class="li"><span class="l">Commands<small>Opening a session or typing in a tab</small></span>
                      <select name="mcpc" class="v" [ngModel]="s.ai?.mcpCommands || ''" (ngModelChange)="setAI({ mcpCommands: $event })">
                        <option value="">Ask me each time</option>
                        <option value="never">Never - Claude only reads</option>
                      </select></div>
                    <div class="li"><span class="l">Claude Desktop<small>{{ mcp()?.inDesktop ? 'Restart Claude Desktop after adding' : 'Adds Termwright to its connectors' }}</small></span>
                      <span class="pill" [class.ok]="mcp()?.inDesktop" [class.warn]="!mcp()?.inDesktop"><i></i>{{ mcp()?.inDesktop ? 'Added' : 'Not added' }}</span>
                      <button type="button" [class.primary]="!mcp()?.inDesktop" (click)="mcpDesktop()">{{ mcp()?.inDesktop ? 'Add Again' : 'Add' }}</button></div>
                    <div class="li"><span class="l">Claude Code<small>claude mcp add, for your user</small></span>
                      <span class="pill" [class.ok]="codeIn()" [class.warn]="!codeIn()"><i></i>{{ codeIn() ? 'Added' : 'Not added' }}</span>
                      <button type="button" [class.primary]="!codeIn()" (click)="mcpCode()">{{ codeIn() ? 'Add Again' : 'Add' }}</button></div>
                    <details class="li man"><summary class="l">Set up by hand</summary>
                      <div class="mrow"><span>Claude Code</span><code>{{ mcp()?.codeCmd }}</code><button type="button" (click)="copyText(mcp()?.codeCmd ?? '')">Copy</button></div>
                      <div class="mrow"><span>Claude Desktop</span><code>{{ mcp()?.command }} mcp</code><button type="button" (click)="copyText(mcp()?.command ?? '')">Copy</button></div></details>
                    <div class="li other"><span class="l">Other AI apps<small>Paste this into the app's MCP settings, then restart it</small></span>
                      <select name="mcpo" class="v" [ngModel]="mcpApp()" (ngModelChange)="mcpApp.set($event)" aria-label="The AI app">
                        @for (o of mcpApps; track o.v) { <option [value]="o.v">{{ o.label }}</option> }
                      </select>
                      <button type="button" class="primary" (click)="copyText(mcpSnippet())">Copy Config</button></div>
                    <div class="li snip"><pre>{{ mcpSnippet() }}</pre><small class="muted">{{ mcpWhere() }}</small></div>
                  }
                </div>
                <div class="gf">{{ mcpMsg() || 'Every command Claude wants to type is shown in Termwright first. The connector listens on this computer only, with a random key.' }}</div>

                <div class="gt">Local API and command line</div>
                <div class="list">
                  <div class="li"><span class="l">Emulated lab for scripts<small>A REST API on this computer and <code>termwright lab …</code>: switches, cables, consoles, traffic tests, captures - without asking first</small></span>
                    @if (s.ai?.api) { <span class="pill" [class.ok]="mcp()?.running"><i></i>{{ mcp()?.running ? 'Running' : 'Starts on Save' }}</span> }
                    <span class="sw"><input type="checkbox" name="api" [ngModel]="!!s.ai?.api" (ngModelChange)="setAI({ api: $event })" aria-label="Local API" /></span></div>
                  @if (s.ai?.api && mcp()?.apiUrl) {
                    <div class="mrow li"><span>Address</span><code>{{ mcp()?.apiUrl }}</code><button type="button" (click)="copyText(mcp()?.apiUrl ?? '')">Copy</button></div>
                    <div class="mrow li"><span>Key</span><code>Authorization: Bearer ••••••••</code><button type="button" (click)="copyApiKey()">Copy Key</button></div>
                    <div class="mrow li"><span>Command line</span><code>{{ mcp()?.command }} lab help</code><button type="button" (click)="copyText((mcp()?.command ?? '') + ' lab help')">Copy</button></div>
                  }
                </div>
                <div class="gf">For test frameworks and CI on this computer: a new random key each time Termwright starts, kept with the address in {{ mcp()?.keyFile || 'mcp.json in the data folder' }}. Changes made through it are not asked first and show as API in the Events.</div>
              </div>
            }
            @case ('integrations') {
              @if (integ(); as g) {
                @for (k of integKinds; track k.k) {
                  <div class="glist">
                    <div class="gt">{{ k.t }}</div>
                    <div class="list">
                      @switch (k.k) {
                        @case ('qtest') {
                          <div class="li"><span class="l">Address<small>qTest Manager site</small></span><input name="qtu" class="v" [(ngModel)]="g.qtest.baseUrl" placeholder="https://yourcompany.qtestnet.com" /></div>
                          <div class="li"><span class="l">User name</span><input name="qtn" class="v" [(ngModel)]="g.qtest.user" autocomplete="off" /></div>
                          <div class="li"><span class="l">Project id<small>a number, see Test connection</small></span><input name="qtp" class="v" type="number" min="0" [(ngModel)]="g.qtest.project" /></div>
                          <div class="li"><span class="l">Test cycle id<small>where test runs are made; empty: the project</small></span><input name="qtc" class="v" type="number" min="0" [(ngModel)]="g.qtest.cycle" /></div>
                          <div class="li"><span class="l">Send every run<small>without asking, also scheduled runs</small></span><input name="qta" type="checkbox" [(ngModel)]="g.qtest.auto" /></div>
                        }
                        @case ('jira') {
                          <div class="li"><span class="l">Address<small>Jira Cloud or Data Center</small></span><input name="jiu" class="v" [(ngModel)]="g.jira.baseUrl" placeholder="https://yourcompany.atlassian.net" /></div>
                          <div class="li"><span class="l">E-mail / user<small>Cloud: your e-mail; Data Center with a personal access token: empty</small></span><input name="jin" class="v" [(ngModel)]="g.jira.user" autocomplete="off" /></div>
                          <div class="li"><span class="l">Project key</span><input name="jip" class="v" [(ngModel)]="g.jira.project" placeholder="NET" /></div>
                          <div class="li"><span class="l">Issue type</span><input name="jit" class="v" [(ngModel)]="g.jira.issueType" placeholder="Bug" /></div>
                          <div class="li"><span class="l">Defect for failures<small>open a defect for each failed test of a scheduled run</small></span><input name="jia" type="checkbox" [(ngModel)]="g.jira.auto" /></div>
                        }
                        @case ('git') {
                          <div class="li"><span class="l">Host<small>the tests folder's remote, over HTTPS</small></span><input name="gih" class="v" [(ngModel)]="g.git.host" placeholder="github.com" /></div>
                          <div class="li"><span class="l">User name</span><input name="giu" class="v" [(ngModel)]="g.git.user" autocomplete="off" /></div>
                        }
                      }
                      <div class="li"><span class="l">Token<small>{{ integHas()[k.k] ? 'Saved in the OS keychain' : 'Kept in the OS keychain, never in the settings' }}</small></span>
                        <input [name]="'tok' + k.k" class="v" type="password" autocomplete="off" [(ngModel)]="integTok[k.k]" [placeholder]="integHas()[k.k] ? '•••••••• (type to change)' : 'paste the token'" />
                        <button type="button" (click)="saveIntegTok(k.k)" [disabled]="!integTok[k.k].trim()">Save Token</button>
                        @if (integHas()[k.k]) { <button type="button" (click)="forgetIntegTok(k.k)">Forget</button> }</div>
                      <div class="li"><span class="l"></span><button type="button" (click)="testInteg(k.k)">Test Connection</button><span class="hint">{{ integMsg()[k.k] }}</span></div>
                    </div>
                  </div>
                }
                <div class="hint">Addresses and user names are saved with the options; tokens only in the OS keychain - they are never shown, logged or put in a command line.</div>
              }
            }
            @case ('skills') {
              <div class="skp">
                <div class="skh"><b>Instructions</b><span class="mut">followed in every AI chat - your team's rules, naming, change windows. Folders (Folder Defaults) and device types (Device Types) can add their own.</span>
                  <span class="sp"></span><button type="button" (click)="importInstructions()" title="Read an AGENTS.md or CLAUDE.md">Import AGENTS.md…</button><button type="button" (click)="exportInstructions()" [disabled]="!(s.ai?.instructions ?? '').trim()" title="Save them as an AGENTS.md, the file other AI tools read">Export…</button></div>
                <textarea name="aiins" class="mono" rows="4" [ngModel]="s.ai?.instructions ?? ''" (ngModelChange)="setAI({ instructions: $event })"
                  placeholder="e.g. VLAN names are <site>-<purpose>. Save the configuration after every change. Never change ports 1:49-1:52 (uplinks)."></textarea>
                <div class="skh"><b>Skills</b><span class="mut">{{ skills().length }} skill{{ skills().length === 1 ? '' : 's' }} · procedures the AI loads when a request matches; it sees only their names and descriptions until then. SKILL.md files - the open format other AI tools use too.</span></div>
                <div class="skbar"><span class="sp"></span>
                  <button type="button" (click)="importSkill()">Import…</button><button type="button" (click)="skillsFolder()">Open Folder</button>
                  @if (!skills().length) { <button type="button" (click)="addExampleSkills()">Add Examples</button> }
                  <button type="button" (click)="aiSkillOpen.set(!aiSkillOpen())">✦ Write with AI</button>
                  <button type="button" class="primary" (click)="newSkill()">+ New Skill</button></div>
                @if (aiSkillOpen()) {
                  <div class="skai"><input name="skai" [(ngModel)]="aiSkillText" (keydown.enter)="writeSkill()" [disabled]="aiSkillBusy()" placeholder="Describe the procedure, e.g. upgrade a VOSS switch: check the image, copy it from the file server, activate, reboot, verify" />
                    <button type="button" class="primary" (click)="writeSkill()" [disabled]="aiSkillBusy() || !aiSkillText.trim()">{{ aiSkillBusy() ? 'Writing…' : 'Write' }}</button></div>
                }
                <div class="skg">
                  <div class="skl">
                    <input name="skq" [(ngModel)]="skillQ" placeholder="Filter skills" />
                    <div class="skls">
                      @for (k of shownSkills(); track k.name) {
                        <div class="ski" [class.on]="skillEdit()?.name === k.name && !skillNew()" (click)="openSkill(k.name)" [class.off]="!skillOn(k.name)">
                          <b>{{ k.name }}@for (t of k.deviceTypes ?? []; track t) { <span class="tg">{{ t }}</span> }@if (!k.deviceTypes?.length) { <span class="tg any">any</span> }@if (!skillOn(k.name)) { <span class="tg offt">off</span> }</b>
                          <div class="d">{{ k.description }}</div>
                        </div>
                      } @empty { <div class="mut pad">No skills yet. <b>Add Examples</b> adds three to start from, or write your own with <b>+ New Skill</b>.</div> }
                    </div>
                  </div>
                  <div class="ske">
                    @if (skillEdit(); as k) {
                      <div class="skeh"><b>{{ skillNew() ? 'New skill' : k.name }}</b>
                        <span class="sp"></span>
                        @if (!skillNew()) { <label class="inl"><input type="checkbox" name="skon" [ngModel]="skillOn(k.name)" (ngModelChange)="setSkillOn(k.name, $event)" /> On</label> }</div>
                      <textarea name="sktext" class="mono sktext" [(ngModel)]="skillText" (ngModelChange)="skillDirty.set(true)" spellcheck="false"></textarea>
                      @if (skillErr()) { <div class="bad small">{{ skillErr() }}</div> }
                      @if (k.files?.length) { <div class="mut small">Other files: {{ k.files!.join(', ') }}</div> }
                      <div class="skbar"><span class="mut small ell" [title]="k.path">{{ skillNew() ? 'name, description (when to use it) and device-types, then the steps' : k.path }}</span><span class="sp"></span>
                        @if (!skillNew()) { <button type="button" (click)="exportSkill(k.name)">Export…</button><button type="button" (click)="removeSkill(k.name)">Remove</button> }
                        <button type="button" class="primary" (click)="saveSkill()" [disabled]="!skillDirty()">Save</button></div>
                    } @else { <div class="mut pad">Choose a skill, or make a new one.</div> }
                  </div>
                </div>
              </div>
            }
            @case ('cards') {
              <p class="hint">SSH keys on smart cards and tokens - PIV and CAC cards, YubiKey (PIV), SafeNet eToken, HSMs - through the card's PKCS#11 library.
                The private key never leaves the card. Choose the key in Session Options → SSH2 Authentication → Smart card.</p>
              @if (cards() && !cards()!.available) { <p class="err">Smart cards are not supported by this build of Termwright.</p> }
              <div class="grid">
                <label>PKCS#11 libraries</label>
                <div>
                  <textarea name="cardlibs" rows="3" spellcheck="false" style="width: 100%" [ngModel]="(s.smartCardLibraries ?? []).join('\n')" (ngModelChange)="s.smartCardLibraries = splitLines($event)"
                    placeholder="One per line. Empty = the usual places, e.g. OpenSC opensc-pkcs11 or YubiKey libykcs11"></textarea>
                  <div class="row">
                    <button type="button" (click)="browseCardLib()">Add Library…</button>
                    @for (f of cards()?.found ?? []; track f) {
                      @if (!(s.smartCardLibraries ?? []).includes(f)) { <button type="button" (click)="s.smartCardLibraries = [...(s.smartCardLibraries ?? []), f]" [title]="f">+ {{ libName(f) }}</button> }
                    }
                  </div>
                </div>
                <label>PIN</label>
                <div class="checks">
                  <label><input type="checkbox" name="pinEach" [(ngModel)]="s.smartCardPinEachTime" /> Ask the PIN for every login (otherwise once per card until Termwright quits)</label>
                  <div class="row"><button type="button" (click)="forgetPins()" [disabled]="!cards()?.loggedIn">Forget Entered PINs</button>
                    <span class="hint">{{ cards()?.loggedIn ? cards()!.loggedIn + ' card(s) with the PIN entered' : 'No PIN entered' }}</span></div>
                </div>
                <label>Cards</label>
                <div>
                  <button type="button" (click)="loadCards(true)">{{ cardsBusy() ? 'Reading…' : 'Test / Read Cards' }}</button>
                  @for (r of cards()?.reports ?? []; track r.library) {
                    <div class="card-lib"><b>{{ libName(r.library) }}</b><span class="hint"> · {{ r.library }}</span>
                      @if (r.error) { <div class="err">{{ r.error }}</div> }
                      @else if (!r.tokens.length) { <div class="hint">No card inserted.</div> }
                      @for (k of r.keys; track k.fingerprint) {
                        <div class="card-key">
                          <div><b>{{ k.token }}</b> · {{ k.label || 'key ' + k.id }} · {{ k.type }} {{ k.bits }}{{ k.subject ? ' · ' + k.subject : '' }}</div>
                          <div class="mono hint">{{ k.fingerprint }}</div>
                          <button type="button" (click)="copyKey(k)">Copy Public Key</button>
                        </div>
                      }
                      @if (!r.error && r.tokens.length && !r.keys.length) { <div class="hint">{{ r.tokens.join(', ') }}: no key for SSH (RSA or ECDSA) found.</div> }
                    </div>
                  } @empty { @if (cards()) { <div class="hint">No PKCS#11 library found. Install your card's middleware (for most cards: OpenSC) or add its library above.</div> } }
                  <div class="hint">Copy Public Key gives the line for the server's <code>~/.ssh/authorized_keys</code>.</div>
                </div>
              </div>
            }
            @case ('updates') {
              <p class="hint">Help → <b>Check for Updates…</b> shows the installed version, the latest stable release with its changes, and any newer nightly build. These settings decide when you are told about them.</p>
              <div class="grid">
                <label>Notify me about</label>
                <select name="uch" [(ngModel)]="s.updateChannel">
                  <option value="stable">Stable releases (recommended)</option>
                  <option value="nightly">Stable releases and nightly builds</option>
                  <option value="off">Nothing - I check myself</option>
                </select>
                <label></label>
                <div class="checks"><label><input type="checkbox" name="uos" [(ngModel)]="s.updateOnStart" [disabled]="s.updateChannel === 'off'" /> Check at start-up (once a day)</label></div>
                @if (s.updateChannel === 'nightly') {
                  <label></label>
                  <div class="warn">Nightly builds are untested builds of the latest code. They can crash or damage your settings and saved sessions - back up first (the update window has a <b>Back Up Now</b> button).</div>
                }
              </div>
              <button type="button" (click)="checkNow()">Check for Updates…</button>
            }
            @case ('buttons') {
              <p class="hint">Text is sent as typed; use \\r for Enter, \\t Tab, \\x03 Ctrl+C, \\e Escape.</p>
              <table class="list">
                <thead><tr><th>Label</th><th>Send</th><th>All tabs</th><th>Action</th><th>Script</th><th></th></tr></thead>
                <tbody>
                  @for (b of s.buttons; track $index) {
                    <tr>
                      <td><input [name]="'bl' + $index" [(ngModel)]="b.label" /></td>
                      <td><input [name]="'bs' + $index" [(ngModel)]="b.send" class="mono" /></td>
                      <td class="c"><input type="checkbox" [name]="'ba' + $index" [(ngModel)]="b.toAll" /></td>
                      <td><select [name]="'bx' + $index" [(ngModel)]="b.action"><option value="">-</option>
                        @for (a of actions; track a[0]) { <option [value]="a[0]">{{ a[1] }}</option> }</select></td>
                      <td><input [name]="'bj' + $index" [(ngModel)]="b.script" placeholder="script.js" /></td>
                      <td class="nowrap">
                        <button type="button" class="icon-btn" [disabled]="$index === 0" (click)="move($index, -1)">↑</button>
                        <button type="button" class="icon-btn" [disabled]="$index === s.buttons.length - 1" (click)="move($index, 1)">↓</button>
                        <button type="button" class="icon-btn" (click)="s.buttons.splice($index, 1)">✕</button>
                      </td>
                    </tr>
                  }
                </tbody>
              </table>
              <button type="button" (click)="s.buttons.push({ label: 'New', send: '\\\\r', toAll: false, action: '', script: '' })">Add button</button>
            }
          }
        </div>
      </div>
      <div class="modal-foot">
         <span class="hint">Changes apply when you press Save</span> 
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Cancel</button>
        <button type="submit" class="primary">{{ 'Save' }}</button>
      </div>
    </form>
  </div>
  }`,
  styles: [`
    .skp { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
    .skh { display: flex; gap: 10px; align-items: baseline; margin-top: 6px; } .skh .sp { flex: 1; } .skh button { flex: none; } .skh .mut { font-size: 12px; }
    .skp textarea[name=aiins] { width: 100%; box-sizing: border-box; font-size: 12px; }
    .skbar { display: flex; gap: 8px; align-items: center; } .skbar .sp, .skeh .sp { flex: 1; }
    .skai { display: flex; gap: 8px; } .skai input { flex: 1; }
    .skg { display: grid; grid-template-columns: 250px 1fr; gap: 12px; height: 330px; }
    .skl, .ske { border: 1px solid var(--border); border-radius: 9px; display: flex; flex-direction: column; min-height: 0; overflow: hidden; }
    .skl > input { margin: 8px; } .skls { overflow: auto; flex: 1; }
    .ski { padding: 8px 11px; border-top: 1px solid var(--line); cursor: pointer; } .ski:hover { background: var(--hover); } .ski.on { background: var(--selected); } .ski.off { opacity: .6; }
    .ski b { font-size: 12.5px; } .ski .d { color: var(--muted); font-size: 11.5px; margin-top: 2px; line-height: 1.35; display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
    .tg { font-size: 10px; border-radius: 4px; padding: 0 5px; margin-left: 6px; background: color-mix(in srgb, var(--ok) 18%, transparent); color: var(--ok); font-weight: 600; }
    .tg.any { background: color-mix(in srgb, var(--accent) 18%, transparent); color: var(--accent); } .tg.offt { background: var(--surface-2); color: var(--muted); }
    .ske { padding: 0 10px 10px; } .skeh { display: flex; gap: 10px; align-items: center; padding: 9px 2px; }
    .sktext { flex: 1; width: 100%; box-sizing: border-box; font-size: 12px; line-height: 1.55; resize: none; min-height: 0; margin-bottom: 8px; }
    .ske .small, .skp .small { font-size: 11.5px; } .ske .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; flex: 1; } .ske .bad { color: var(--danger); margin-bottom: 6px; }

    .mapd { display: flex; flex-direction: column; gap: 6px; min-width: 0; .row2 { display: grid; grid-template-columns: 1fr auto; gap: 6px; } .mapk { display: grid; grid-template-columns: 90px 1fr; gap: 4px 8px; align-items: start; b { font-weight: 500; padding-top: 6px; } textarea { grid-column: 2; } }
      .maptest summary { cursor: pointer; color: var(--muted); } .maptest { display: flex; flex-direction: column; gap: 6px; } .maprows { border-collapse: collapse; font: 11.5px var(--mono); th, td { border: 1px solid var(--border); padding: 2px 6px; text-align: left; } } }
    .webd { display: flex; flex-direction: column; gap: 6px; min-width: 0; .row2 { display: grid; grid-template-columns: 1fr 1fr; gap: 6px; } }
    .glist { max-width: 760px;
      .gt { font-size: calc(11px * var(--ui-scale, 1)); letter-spacing: .06em; text-transform: uppercase; color: var(--muted); font-weight: 600; margin: 4px 0 6px 4px; }
      .list { border: 1px solid var(--line); border-radius: var(--r-lg); background: var(--surface); }
      .li { display: flex; align-items: center; gap: 10px; padding: 9px 14px; min-height: 30px; }
      .li + .li { border-top: 1px solid var(--line); }
      .l { flex: 1; min-width: 0; small { display: block; color: var(--muted); font-size: calc(11.5px * var(--ui-scale, 1)); margin-top: 1px; overflow-wrap: anywhere; } }
      .v { width: 300px; max-width: 48%; flex: none; }
      .mono { font-family: var(--mono); }
      .gf { font-size: calc(11.5px * var(--ui-scale, 1)); color: var(--muted); margin: 6px 4px 18px; line-height: 1.5; code { font-size: inherit; } }
      .pill { display: inline-flex; align-items: center; gap: 6px; padding: 2px 9px; border-radius: 999px; font-size: calc(11.5px * var(--ui-scale, 1)); white-space: nowrap; border: 1px solid var(--line); color: var(--muted);
        i { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
        &.ok { color: var(--ok); border-color: color-mix(in srgb, var(--ok) 45%, transparent); background: color-mix(in srgb, var(--ok) 10%, transparent); }
        &.warn { color: var(--warn); border-color: color-mix(in srgb, var(--warn) 45%, transparent); background: color-mix(in srgb, var(--warn) 10%, transparent); }
        &.bad { color: var(--danger); border-color: color-mix(in srgb, var(--danger) 45%, transparent); background: color-mix(in srgb, var(--danger) 10%, transparent); } }
      .man { display: block; summary { cursor: pointer; color: var(--muted); } }
      .li.used { align-items: flex-start; .v2 { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; align-items: flex-end; font-size: calc(12px * var(--ui-scale, 1)); text-align: right; } .cli { display: block; max-width: 100%; b { white-space: nowrap; } } .dot { display: inline-block; vertical-align: middle; margin-right: 6px; width: 7px; height: 7px; border-radius: 50%; background: var(--muted); } .dot.live { background: var(--success, #1fa45a); } }
      .li.snip { flex-direction: column; align-items: stretch; gap: 6px; padding-top: 0; border-top: none; pre { margin: 0; padding: 8px 10px; border-radius: 8px; background: var(--surface-2); border: 1px solid var(--line); font: calc(11.5px * var(--ui-scale, 1)) var(--mono); white-space: pre-wrap; word-break: break-all; user-select: text; } small { font-size: calc(11px * var(--ui-scale, 1)); } }
      .mrow { display: flex; align-items: center; gap: 8px; margin-top: 8px; span { width: 110px; flex: none; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); }
        code { flex: 1; min-width: 0; overflow-x: auto; white-space: nowrap; font-size: calc(11.5px * var(--ui-scale, 1)); padding: 5px 8px; border-radius: 6px; background: var(--bg); border: 1px solid var(--line); } }
      @media (max-width: 760px) { .li { flex-wrap: wrap; } .v { width: 100%; max-width: none; } } }
    .envs { display: flex; flex-direction: column; gap: 8px; margin-top: 10px; }
    .envrow { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 8px 10px; border: 1px solid var(--line); border-radius: var(--r-lg); background: var(--surface);
      .tagpv { min-width: 52px; text-align: center; font-size: calc(10px * var(--ui-scale, 1)); font-weight: 700; letter-spacing: .03em; padding: 2px 6px; border-radius: 4px; }
      .lab { flex: 1 1 160px; min-width: 120px; } .sh { width: 80px; }
      .sw { display: flex; gap: 5px; }
      .dotc { width: 20px; height: 20px; padding: 0; border-radius: 50%; border: 2px solid transparent; box-shadow: 0 0 0 1px var(--line);
        &.on { border-color: var(--text); } }
      .inline-check { white-space: nowrap; } }
    .gsec { display: none; }
    :host-context(.modern) .gsec { display: block; grid-column: 1 / -1; margin: 14px 0 2px; font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600;
      letter-spacing: .06em; text-transform: uppercase; color: var(--faint); &:first-child { margin-top: 0; } }
    .themes { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; max-width: 420px; }
    .tht { height: auto; display: flex; flex-direction: column; align-items: stretch; gap: 6px; padding: 6px 6px 7px; border: 1px solid var(--line, var(--border));
      border-radius: var(--r, 6px); background: var(--surface-2, var(--tab)); font-weight: 500; color: var(--muted);
      &.on { border-color: var(--accent); color: var(--text); box-shadow: 0 0 0 1px var(--accent); } }
    .pv { display: block; height: 44px; border-radius: var(--r-sm, 5px); border: 1px solid rgba(127, 127, 127, .25); position: relative; overflow: hidden;
      &::before { content: ''; position: absolute; left: 0; top: 0; bottom: 0; width: 30%; }
      &::after { content: ''; position: absolute; left: 38%; right: 10%; top: 10px; height: 5px; border-radius: 3px; box-shadow: 0 10px 0 -1px currentColor; } }
    .pv.light { background: #f6f7f9; color: #c9ced6; &::before { background: #e9ecf0; } &::after { background: #9aa3af; } }
    .pv.dark { background: #16181d; color: #3a3f48; &::before { background: #1e2127; } &::after { background: #6b7280; } }
    .pv.system { background: linear-gradient(110deg, #f6f7f9 50%, #16181d 50%); color: #8a919c; &::before { background: transparent; } &::after { background: #8a919c; } }
    .seg.w2 { max-width: 300px; } .seg.w3 { max-width: 380px; }
    .pair .seg.w2 { max-width: none; }
  `],
})
export class OptionsDialog implements OnInit {
  protected readonly looks = LOOKS;
  protected ui = inject(Ui);
  readonly uiFonts = UI_FONTS;
  protected readonly tr = tr;
  readonly deviceKinds = Object.entries(DEVICE_KINDS).map(([v, k]) => ({ v, label: k.label }));
  readonly uiSizes = UI_SIZES;
  readonly factNames = FACT_NAMES;
  readonly languages = LANGUAGES.filter(l => !l.preview || previewLanguages());
  readonly themeTiles = [{ v: 'light', label: 'Light' }, { v: 'dark', label: 'Dark' }, { v: 'system', label: 'System' }];
  reOk(re: string) { if (!re) return true; try { new RegExp(re); return true; } catch { return false; } }
  readonly isMac = navigator.platform.toLowerCase().includes('mac') || navigator.userAgent.includes('Mac OS');
  fontStack(v: string | undefined) { return uiFontStack(v); }
  private backend = inject(Backend);
  private cdr = inject(ChangeDetectorRef);
  readonly page = signal<Page>((this.ui.optionsPage() || 'general') as Page);
  /** Built-in backup command shown when a device type has none of its own. */
  readOnlyDefault(name: string) { return 'Runs without asking (one per line; empty = ' + readOnlyList(name, '').join(', ') + ')'; }
  /** The built-in web server commands of EXOS and VOSS, shown as placeholders (an empty field uses them). */
  tunnelDefault(name: string) { return dtTunnels([], name) ? 'Default: forwards connections' : 'Default: no forwarding (' + name + ')'; }
  webDefault(name: string, k: 'check' | 'http' | 'https' | 'on' | 'save') {
    const d: Record<string, Record<string, string>> = {
      exos: { check: 'show management', http: 'web access : enabled (tcp', https: 'enabled (ssl port', on: 'enable web http · ssl certificate · enable web https', save: 'save configuration' },
      voss: { check: 'show web-server', http: 'status enabled, secure-only disabled', https: 'status : enabled', on: 'web-server enable · no web-server secure-only', save: 'save config' },
    };
    const c = d[name.toLowerCase()]?.[k];
    return c ? c + '  (built-in)' : '';
  }
  detectSample = '';
  /** What the device type's Detect pattern matches in the sample ('bad': not a valid pattern). Go's (?i) at the start becomes the i flag. */
  detectTest(d: { detect?: string }): string {
    const p = (d.detect ?? '').trim(), t = this.detectSample;
    if (!p || !t.trim()) return '';
    try {
      const ci = /^\(\?i\)/.test(p);
      const m = new RegExp(ci ? p.slice(4) : p, ci ? 'im' : 'm').exec(t);
      return m ? m[0] : '';
    } catch { return 'bad'; }
  }
  undoDefault(name: string) { const r = builtinUndo(name); return r ? r + '\n(built-in rules)' : ''; }
  testStateDefault(name: string, i: 0 | 1) {
    const d: Record<string, [string, string]> = { 'cisco nx-os': ['checkpoint {file}', 'rollback running-config checkpoint {file}\nno checkpoint {file}'],
      'cisco ios': ['copy running-config flash:{file}.cfg', 'configure replace flash:{file}.cfg force'], exos: ['save configuration {file}', 'use configuration {file}\nreboot'] };
    const c = d[name.toLowerCase()]?.[i];
    return c ? c + '  (built-in)' : '';
  }
  /** Copies of the built-ins in internal/session/devicetype.go. */
  errorsDefault(name: string) {
    const d: Record<string, string> = { exos: '^\\s*Error:\n%% (Invalid|Incomplete|Ambiguous|Unrecognized)', voss: '^\\s*% *(Invalid|Unrecognized|Incomplete|Ambiguous|Error|Cannot)',
      'cisco ios': '^\\s*% *(Invalid|Incomplete|Ambiguous|Unknown|Error|Bad)', 'cisco nx-os': '^\\s*% *(Invalid|Incomplete|Ambiguous)\n^\\s*ERROR:', 'huawei vrp': '^\\s*Error:',
      'eltex mes': '^\\s*% *(Unrecognized|Invalid|Incomplete|Ambiguous|Error)', 'ruckus icx': 'Invalid input\n^\\s*Error' };
    const c = d[name.toLowerCase()];
    return c ? c + '\n(built-in)' : '';
  }
  readonly mapKinds = [
    { key: 'neighbors', label: 'Neighbours', ph: 'e.g. display lldp neighbor' },
    { key: 'identity', label: 'Identity', ph: 'e.g. display lldp local (optional)' },
    { key: 'ports', label: 'Ports', ph: 'e.g. display interface brief (optional)' },
  ] as const;
  mapTestKind = 'neighbors'; mapTestOut = '';
  readonly mapTestMsg = signal(''); readonly mapTestRows = signal<Record<string, string>[]>([]);
  mapTestHead() { return Object.keys(this.mapTestRows()[0] ?? {}); }
  mapGet(d: DeviceType, k: string) { return (d.map as Record<string, string> | null | undefined)?.[k] ?? ''; }
  setMap(d: DeviceType, k: string, v: string) { d.map = { ...(d.map ?? {}), [k]: v }; }
  hasBuiltinMap(name: string) { return /huawei|vrp|icx|ruckus|fastiron|eltex|^mes/i.test(name || ''); }
  mapNote(d: DeviceType) {
    if (d.map?.neighbors?.trim() && d.map?.neighborsTemplate?.trim()) return 'This plan is used for the type.';
    if (this.hasBuiltinMap(d.name)) return 'Empty: the built-in templates for ' + d.name + '.';
    return /exos|voss|fabric|switch engine|ios|nx-os|nexus|arista|eos|junos|juniper/i.test(d.name || '') ? 'Empty: the built-in reading for ' + d.name + '.' : 'Empty: the map reads this type by SNMP only.';
  }
  async mapFromBuiltin(d: DeviceType) {
    try { const m = await this.backend.topoBuiltinMap(d.name); if (m) d.map = { ...m }; } catch (e) { this.mapTestMsg.set(String(e)); }
  }
  async mapTest(d: DeviceType) {
    this.mapTestRows.set([]);
    const tpl = this.mapGet(d, this.mapTestKind + 'Template');
    if (!tpl.trim()) { this.mapTestMsg.set('Write the template first.'); return; }
    try { const r = (await this.backend.topoTestMap(tpl, this.mapTestOut)) ?? []; this.mapTestRows.set(r); this.mapTestMsg.set(r.length ? `${r.length} rows` : 'No rows - check the patterns against the output.'); }
    catch (e) { this.mapTestMsg.set(String(e)); }
  }
  factoryDefault(name: string) {
    const d: Record<string, string> = { exos: 'unconfigure switch all', 'cisco ios': 'write erase\nreload', 'cisco nx-os': 'write erase\nreload' };
    const c = d[name.toLowerCase()];
    return c ? c + '\n(built-in)' : '';
  }
  backupDefault(name: string) {
    const d: Record<string, string> = { exos: 'show configuration', voss: 'show running-config', 'cisco ios': 'show running-config' };
    const c = d[name.toLowerCase()];
    return c ? c + '  (built-in default)' : '';
  }
  readonly q = signal('');
  readonly hits = computed(() => {
    const w = this.q().toLowerCase().split(/\s+/).filter(Boolean);
    return SEARCH.filter(([p, label, extra]) => { const t = (label + ' ' + extra + ' ' + PAGE_NAMES[p]).toLowerCase(); return w.every(x => t.includes(x)); });
  });
  pageName(p: Page) { return PAGE_NAMES[p]; }

  readonly envColors = ENV_COLORS;
  readonly envFg = envFg;
  addEnv() {
    if (!this.s) return;
    const list = (this.s.environments ??= []);
    const used = new Set(list.map(e => e.color));
    list.push({ id: '', label: '', short: '', color: (ENV_COLORS.find(c => !used.has(c.c)) ?? ENV_COLORS[0]).c });
  }
  removeEnv(i: number) { this.s?.environments?.splice(i, 1); }
  resetEnvs() { if (this.s) this.s.environments = structuredClone(DEFAULT_ENVIRONMENTS); }
  /** New environments get an id from their name (what sessions and folders store); empty rows are dropped. */
  private normEnvs() {
    if (!this.s?.environments) return;
    const ids = new Set<string>();
    this.s.environments = this.s.environments.filter(e => e.label.trim()).map(e => {
      let id = e.id || e.label.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'env';
      const base = id; let n = 2;
      while (ids.has(id)) id = base + '-' + n++;
      ids.add(id);
      return { ...e, id, label: e.label.trim(), short: (e.short || e.label).trim().toUpperCase().slice(0, 6) };
    });
    const known = new Set(this.s.environments.map(e => e.id));
    for (const d of this.s.folderDefaults ?? []) if (d.environment && !known.has(d.environment)) d.environment = '';
  }

  /** Opens the page and highlights the option. */
  goTo(p: Page, label: string) {
    this.page.set(p);
    this.q.set('');
    setTimeout(() => {
      const body = document.querySelector('.modal.wide .modal-body');
      const has = (e: Element) => (e.textContent ?? '').replace(/\s+/g, ' ').includes(label);
      const el = [...(body?.querySelectorAll('label, h3, h4') ?? [])].find(has) ?? [...(body?.querySelectorAll('.grid > *') ?? [])].find(has);
      if (!el) return;
      el.scrollIntoView({ block: 'center' });
      el.classList.add('flash');
      setTimeout(() => el.classList.remove('flash'), 1800);
    }, 50);
  }
  readonly dataDir = signal('');
  s: Settings | null = null;
  readonly kwIndex = signal(0);
  readonly actions = Object.entries(ACTIONS);
  readonly colorKeys = COLOR_KEYS;
  readonly ansi = COLOR_KEYS.slice(4);
  readonly schemeName = signal('Default');
  private schemeRev = signal(0);
  readonly allSchemes = computed(() => {
    this.schemeRev();
    return [...SCHEME_NAMES, ...(this.s?.customSchemes ?? []).map(c => c.name)];
  });
  readonly colors = computed(() => {
    this.schemeRev();
    return fullScheme(this.schemeName(), this.s?.customSchemes ?? []);
  });

  readonly protocols = PROTOCOLS;
  readonly stepParts = [{ key: 'loginSteps' as const, title: 'Login steps (Telnet / Serial / Raw)' }, { key: 'afterLogin' as const, title: 'After-login steps (all protocols)' }];
  readonly dtIndex = signal(0);
  curDevice() { return this.s?.deviceTypes?.[this.dtIndex()] ?? null; }
  /** Password edits per device type, saved to the keychain on OK. */
  private readonly dtPw = new Map<DeviceType, DtPw>();
  private readonly dtRemoved: string[] = [];
  pwOf(d: DeviceType): DtPw {
    let x = this.dtPw.get(d);
    if (!x) {
      const p: DtPw = { orig: d.name.trim(), pw: '', empty: false, state: '', forget: false };
      this.dtPw.set(d, (x = p));
      if (p.orig) this.backend.deviceTypePasswordState(p.orig).then(st => { p.state = st; p.empty = st === 'empty'; this.cdr.markForCheck(); }).catch(() => {});
    }
    return x;
  }
  readonly isRef = isRef;
  readonly refOf = refOf;
  async pickRef(p: DtPw) {
    const r = await askRef(this.ui, this.backend, isRef(p.pw) ? p.pw : refOf(p.state));
    if (r) { p.pw = r; p.empty = false; p.forget = false; this.cdr.markForCheck(); }
  }
  pwPh(p: DtPw) {
    if (refOf(p.state) && !p.forget) return 'from ' + refOf(p.state);
    if (p.empty) return 'no password';
    if (p.forget) return 'will be removed';
    return p.state === 'set' ? '•••••• saved (type to change)' : 'not saved (asked when connecting)';
  }
  steps(d: DeviceType, k: 'loginSteps' | 'afterLogin') { return (d[k] ??= []); }
  moveStep(d: DeviceType, k: 'loginSteps' | 'afterLogin', i: number, dir: number) {
    const l = this.steps(d, k);
    [l[i], l[i + dir]] = [l[i + dir], l[i]];
  }
  addDevice() {
    if (!this.s) return;
    (this.s.deviceTypes ??= []).push({ name: 'New device', protocol: 'SSH2', port: 0, username: '', credential: '', initialCr: false,
      loginSteps: [{ expect: 'login:', send: '\\s', hide: false, trailingCr: true, credential: '' }, { expect: 'assword:', send: '\\w', hide: true, trailingCr: true, credential: '' }],
      afterLogin: [], prompt: '' });
    this.dtIndex.set(this.s.deviceTypes.length - 1);
  }
  copyDevice() {
    const d = this.curDevice();
    if (!d || !this.s) return;
    this.s.deviceTypes!.push({ ...structuredClone(d), name: d.name + ' (copy)' });
    this.dtIndex.set(this.s.deviceTypes!.length - 1);
  }
  removeDevice() {
    if (!this.s?.deviceTypes) return;
    const gone = this.s.deviceTypes[this.dtIndex()];
    if (gone) { const p = this.pwOf(gone); if (p.orig && p.state) this.dtRemoved.push(p.orig); }
    this.s.deviceTypes.splice(this.dtIndex(), 1);
    this.dtIndex.set(0);
  }

  async checkNow() {
    await this.save();
    this.ui.open('update');
  }

  isCustom(n: string) { return !!this.s?.customSchemes?.some(c => c.name === n); }

  async copyScheme() {
    if (!this.s) return;
    const name = await this.ui.input('New Color Scheme', 'Name', this.schemeName() + ' (copy)');
    if (!name || this.allSchemes().includes(name)) return;
    this.s.customSchemes = [...(this.s.customSchemes ?? []), { name, colors: fullScheme(this.schemeName(), this.s.customSchemes ?? []) }];
    this.schemeName.set(name);
    this.schemeRev.update(v => v + 1);
  }

  deleteScheme() {
    if (!this.s) return;
    const n = this.schemeName();
    this.s.customSchemes = (this.s.customSchemes ?? []).filter(c => c.name !== n);
    if (this.s.terminalScheme === n) this.s.terminalScheme = 'Default';
    this.schemeName.set('Default');
    this.schemeRev.update(v => v + 1);
  }

  setColor(k: string, v: string) {
    const c = this.s?.customSchemes?.find(x => x.name === this.schemeName());
    if (c) { c.colors = { ...c.colors, [k]: v }; this.schemeRev.update(x => x + 1); }
  }

  async exportSchemes() {
    const p = await this.backend.chooseSaveFile('Export Color Schemes', 'termwright-schemes.json', [{ displayName: 'JSON', pattern: '*.json' }]).catch(() => '');
    if (p) await this.backend.saveTextFile(p, JSON.stringify(this.s?.customSchemes ?? [], null, 2)).then(() => this.ui.toast('Exported to ' + p, 'success'), e => this.ui.error(e));
  }

  async importSchemes() {
    const p = await this.backend.chooseOpenFile('Import Color Schemes', [{ displayName: 'JSON', pattern: '*.json' }]).catch(() => '');
    if (!p || !this.s) return;
    try {
      const list = JSON.parse(await this.backend.readTextFile(p)) as { name: string; colors: Record<string, string> }[];
      const ok = (Array.isArray(list) ? list : [list]).filter(x => x && typeof x.name === 'string' && x.colors && typeof x.colors === 'object');
      const existing = new Set(this.allSchemes());
      for (const sc of ok) {
        let name = sc.name;
        while (existing.has(name)) name += ' (imported)';
        existing.add(name);
        const colors: Record<string, string> = {};
        for (const k of COLOR_KEYS) if (typeof sc.colors[k] === 'string' && /^#[0-9a-f]{6}$/i.test(sc.colors[k])) colors[k] = sc.colors[k];
        this.s.customSchemes = [...(this.s.customSchemes ?? []), { name, colors }];
      }
      this.schemeRev.update(v => v + 1);
      this.ui.toast(`Imported ${ok.length} scheme(s)`, 'success');
    } catch (e) { this.ui.error(e); }
  }

  capture(ev: KeyboardEvent, k: KeyBinding) {
    if (ev.key === 'Tab' && !ev.ctrlKey && !ev.altKey && !ev.metaKey) return; // keep keyboard navigation
    ev.preventDefault();
    ev.stopPropagation();
    if (ev.key === 'Backspace' && !ev.ctrlKey && !ev.altKey && !ev.metaKey && !ev.shiftKey) { k.keys = ''; return; }
    const c = comboOf(ev);
    if (c) k.keys = c;
  }

  curSet() { return this.s?.keywordSets?.[this.kwIndex()] ?? null; }

  async addSet() {
    const name = await this.ui.input('New Keyword Set', 'Name');
    if (!name || !this.s) return;
    this.s.keywordSets = [...(this.s.keywordSets ?? []), { name, keywords: [] }];
    this.kwIndex.set(this.s.keywordSets.length - 1);
    this.cdr.markForCheck();
  }

  async renameSet() {
    const set = this.curSet();
    if (!set || !this.s) return;
    const name = await this.ui.input('Rename Keyword Set', 'Name', set.name);
    if (!name) return;
    if (this.s.defaultKeywordSet === set.name) this.s.defaultKeywordSet = name;
    set.name = name;
    this.cdr.markForCheck();
  }

  deleteSet() {
    if (!this.s?.keywordSets) return;
    const set = this.curSet();
    this.s.keywordSets = this.s.keywordSets.filter(k => k !== set);
    if (set && this.s.defaultKeywordSet === set.name) this.s.defaultKeywordSet = '';
    this.kwIndex.set(0);
  }

  keepAwakeErr = '';
  ngOnInit() {
    void this.backend.syncState().then(x => this.syncSt.set(x)).catch(() => {});
    void this.backend.keepAwakeStatus().then(x => this.keepAwakeErr = x.error ?? '').catch(() => {});
    if (this.page() === 'ai') this.loadAI();
    if (this.page() === 'skills') void this.loadSkills();
    this.ui.optionsPage.set('');
    const focus = this.ui.optionsFocus(); // Help → Connect an AI App…: the connector's part of the page
    if (focus) { this.ui.optionsFocus.set(''); setTimeout(() => document.getElementById('opt-' + focus)?.scrollIntoView({ block: 'start', behavior: 'smooth' }), 150); }
    const cur = this.ui.settings();
    if (cur) this.s = structuredClone(cur);
    if (this.s && !this.s.environments?.length) this.s.environments = structuredClone(DEFAULT_ENVIRONMENTS);
    if (this.s) this.schemeName.set(!this.s.terminalScheme || this.s.terminalScheme === 'Auto' ? 'Default' : this.s.terminalScheme);
    this.schemeRev.update(v => v + 1);
    this.backend.getAppInfo().then(i => this.dataDir.set(i.dataDir)).catch(() => {});
  }

  readonly cards = signal<SmartCardInfo | null>(null);
  readonly cardsBusy = signal(false);
  async loadCards(again = false) {
    if (this.cards() && !again) return;
    this.cardsBusy.set(true);
    try { this.cards.set(await this.backend.smartCards(this.s?.smartCardLibraries ?? [])); } catch (e) { this.ui.error(e); }
    this.cardsBusy.set(false);
    this.cdr.markForCheck();
  }
  splitLines(v: string) { return v.split('\n').map(x => x.trim()).filter(Boolean); }
  libName(p: string) { return p.split(/[\\/]/).pop() ?? p; }
  async browseCardLib() {
    const f = await this.backend.chooseOpenFile('PKCS#11 Library', []).catch(() => '');
    if (f && this.s) { this.s.smartCardLibraries = [...(this.s.smartCardLibraries ?? []), f]; this.cdr.markForCheck(); }
  }
  async forgetPins() { await this.backend.forgetSmartCardPins(); this.ui.toast('The PINs will be asked again'); await this.loadCards(true); }
  async copyKey(k: SmartCardKey) { await this.backend.clipboardSet(k.authorizedKey); this.ui.toast('Public key copied', 'success'); }

  readonly syncSt = signal<SyncStatus | null>(null);
  // AI assistant
  readonly aiSt = signal<AIStatus | null>(null);
  readonly aiModels = signal<string[]>([]);
  readonly aiLoading = signal(false);
  protected readonly efforts = EFFORTS;
  /** The model list: the service's default first, then what it offers (Claude names made readable). */
  modelOptions(): ComboOption[] {
    const cc = this.s?.ai?.provider === 'claudecode';
    return [{ value: '', label: cc ? tr('Default of Claude Code') : tr('The newest the service offers'), group: cc ? tr('Newest') : '' }, ...modelChoices(this.s?.ai?.provider ?? '', this.aiModels())];
  }
  readonly aiMsg = signal('');
  aiKey = '';
  readonly integKinds = [{ k: 'qtest', t: 'qTest - test results' }, { k: 'jira', t: 'Jira / Atlassian - defects' }, { k: 'git', t: 'Git host - the tests folder' }];
  readonly integHas = signal<Record<string, boolean>>({});
  readonly integMsg = signal<Record<string, string>>({});
  integTok: Record<string, string> = { qtest: '', jira: '', git: '' };
  /** The settings' integrations (created when missing, so the fields can bind). */
  integ() {
    if (!this.s) return null;
    const g = (this.s.integrations ??= { qtest: {}, jira: {}, git: {} });
    g.qtest ??= {}; g.jira ??= {}; g.git ??= {};
    return g;
  }
  loadInteg() { void this.backend.integrationTokens().then(m => this.integHas.set(m ?? {})).catch(() => {}); }
  async saveIntegTok(k: string) {
    try { await this.backend.integrationSetToken(k, this.integTok[k]); this.integTok[k] = ''; this.integMsg.update(m => ({ ...m, [k]: tr('The token is saved in the OS keychain.') })); this.loadInteg(); }
    catch (e) { this.integMsg.update(m => ({ ...m, [k]: errText(e) })); }
  }
  async forgetIntegTok(k: string) { await this.backend.integrationSetToken(k, '').catch(() => {}); this.loadInteg(); }
  async testInteg(k: string) {
    this.integMsg.update(m => ({ ...m, [k]: tr('Checking…') }));
    try { const r = await this.backend.integrationTest(k, this.integ() ?? undefined); this.integMsg.update(m => ({ ...m, [k]: '✓ ' + r })); }
    catch (e) { this.integMsg.update(m => ({ ...m, [k]: '✗ ' + errText(e) })); }
  }
  loadAI() {
    void this.backend.aiState().then(x => this.aiSt.set(x)).catch(() => {});
    void this.backend.mcpState().then(x => this.mcp.set(x)).catch(() => {});
  }
  /** Other AI apps: the same command (this program, mcp) in each app's own settings format. */
  readonly mcpApps = [
    { v: 'json', label: 'Cursor, Windsurf, Gemini CLI…', where: 'Cursor: ~/.cursor/mcp.json · Windsurf: ~/.codeium/windsurf/mcp_config.json · Gemini CLI: ~/.gemini/settings.json - most other apps take the same mcpServers setting' },
    { v: 'vscode', label: 'VS Code', where: 'VS Code: Command Palette → MCP: Open User Configuration (or .vscode/mcp.json in a project)' },
    { v: 'codex', label: 'Codex CLI', where: 'Codex: ~/.codex/config.toml' },
  ];
  readonly mcpApp = signal('json');
  mcpSnippet() {
    const cmd = this.mcp()?.command || 'termwright';
    if (this.mcpApp() === 'codex') return `[mcp_servers.termwright]\ncommand = ${JSON.stringify(cmd)}\nargs = ["mcp"]\n`;
    if (this.mcpApp() === 'vscode') return JSON.stringify({ servers: { termwright: { type: 'stdio', command: cmd, args: ['mcp'] } } }, null, 2);
    return JSON.stringify({ mcpServers: { termwright: { command: cmd, args: ['mcp'] } } }, null, 2);
  }
  mcpWhere() { return this.mcpApps.find(o => o.v === this.mcpApp())?.where ?? ''; }
  isRecent(at: string) { return Date.now() - Date.parse(at) < 5 * 60e3; }
  // while the AI page is open: who used the connector, kept fresh
  private mcpTimer = setInterval(() => { if (this.page() === 'ai' && this.s?.ai?.mcp) void this.backend.mcpState().then(x => this.mcp.set(x)).catch(() => {}); }, 5000);
  private mcpTimerEnd = inject(DestroyRef).onDestroy(() => clearInterval(this.mcpTimer));
  readonly mcp = signal<MCPInfo | null>(null);
  readonly aiCheck = signal<boolean | null>(null); // the last Test: worked / failed
  readonly codeAdded = signal(false);
  /** Claude Code has Termwright: what its settings file says, or what was just added. */
  codeIn() { return this.codeAdded() || !!this.mcp()?.inCode; }
  /** The status pill: the last Test, else what the saved settings say. */
  aiStatus(): { cls: string; text: string } {
    const p = this.s?.ai?.provider ?? '';
    if (!p) return { cls: '', text: tr('Off') };
    if (this.aiCheck() === true) return { cls: 'ok', text: tr('Ready') };
    if (this.aiCheck() === false) return { cls: 'bad', text: tr('Not working') };
    const st = this.aiSt();
    if ((p === 'anthropic' || p === 'openai') && !st?.hasKey) return { cls: 'warn', text: tr('Needs a key') };
    if (st?.provider === p && st.ready) return { cls: 'ok', text: tr('Ready') };
    return { cls: '', text: tr('Not tested') };
  }
  readonly mcpMsg = signal('');
  async copyApiKey() { try { const k = await this.backend.apiKey(); if (k) { await navigator.clipboard.writeText(k); this.ui.toast('Key copied', 'success'); } } catch (e) { this.ui.error(e); } }
  async mcpDesktop() {
    try { await this.backend.mcpAddToDesktop(); this.mcpMsg.set(tr('Added to Claude Desktop - restart Claude Desktop, then look for Termwright under its connectors.')); this.loadAI(); }
    catch (e) { this.mcpMsg.set(errText(e)); }
  }
  async mcpCode() {
    this.mcpMsg.set(tr('Adding…'));
    try { await this.backend.mcpAddToClaudeCode(); this.codeAdded.set(true); this.loadAI(); this.mcpMsg.set(tr('Added to Claude Code - start a new claude session and type /mcp to see it.')); }
    catch (e) { this.mcpMsg.set(errText(e)); }
  }
  async copyText(t: string) { if (t) { await this.backend.clipboardSet(t); this.ui.toast(tr('Copied'), 'success'); } }
  // ---- AI instructions and skills ----
  readonly skills = signal<AISkill[]>([]);
  readonly skillEdit = signal<AISkill | null>(null);
  readonly skillNew = signal(false);
  readonly skillDirty = signal(false);
  readonly skillErr = signal('');
  readonly aiSkillOpen = signal(false);
  readonly aiSkillBusy = signal(false);
  skillText = ''; skillQ = ''; aiSkillText = '';
  async loadSkills() { this.skills.set(await this.backend.aiSkills().catch(() => [])); if (!this.skillEdit() && this.skills().length) void this.openSkill(this.skills()[0].name); this.cdr.markForCheck(); }
  shownSkills() { const q = this.skillQ.trim().toLowerCase(); return this.skills().filter(k => !q || (k.name + ' ' + k.description + ' ' + (k.deviceTypes ?? []).join(' ')).toLowerCase().includes(q)); }
  skillOn(n: string) { return !(this.s?.ai?.skillsOff ?? []).includes(n); }
  setSkillOn(n: string, on: boolean) { const off = (this.s?.ai?.skillsOff ?? []).filter(x => x !== n); if (!on) off.push(n); this.setAI({ skillsOff: off }); }
  private async keepEdits(): Promise<boolean> {
    return !this.skillDirty() || await this.ui.confirm('Unsaved Skill', 'The skill has changes that are not saved. Leave them?', 'Leave');
  }
  async openSkill(name: string) {
    if (!(await this.keepEdits())) return;
    try { const k = await this.backend.aiSkill(name); this.skillEdit.set(k); this.skillText = k.text ?? ''; this.skillNew.set(false); this.skillDirty.set(false); this.skillErr.set(''); }
    catch (e) { this.skillErr.set(errText(e)); }
    this.cdr.markForCheck();
  }
  async importInstructions() {
    try {
      const t = await this.backend.aiImportInstructions(); if (!t) return;
      const cur = (this.s?.ai?.instructions ?? '').trim();
      const replace = !cur || await this.ui.confirm('Import Instructions', 'Replace the instructions with the file\'s? Cancel adds the file\'s after them.', 'Replace');
      this.setAI({ instructions: replace ? t.trim() : cur + '\n\n' + t.trim() });
      this.ui.toast('Imported - press Save to keep them', 'info');
    } catch (e) { this.ui.error(e); }
  }
  async exportInstructions() { try { const p = await this.backend.aiExportInstructions(this.s?.ai?.instructions ?? ''); if (p) this.ui.toast('Saved ' + p, 'success'); } catch (e) { this.ui.error(e); } }
  async newSkill(text?: string) {
    if (!(await this.keepEdits())) return;
    this.skillEdit.set({ name: '', description: '', path: '', modified: '', on: true });
    this.skillText = text ?? '---\nname: my-procedure\ndescription: What it does and when to use it, e.g. "use for upgrade or new firmware".\ndevice-types: EXOS\n---\n# My procedure\n\n1. First step - the command to run and what to check.\n2. Ask before anything that changes the device or interrupts traffic.\n3. Check the result and say what was done.\n';
    this.skillNew.set(true); this.skillDirty.set(true); this.skillErr.set(''); this.cdr.markForCheck();
  }
  async saveSkill() {
    const k = this.skillEdit(); if (!k) return;
    try {
      const saved = await this.backend.aiSaveSkill(this.skillNew() ? '' : k.name, this.skillText);
      this.skillDirty.set(false); this.skillNew.set(false); this.skillErr.set('');
      await this.loadSkills(); this.skillEdit.set(saved); this.skillText = saved.text ?? this.skillText;
      this.ui.toast('Saved the skill ' + saved.name, 'success');
    } catch (e) { this.skillErr.set(errText(e)); }
    this.cdr.markForCheck();
  }
  async removeSkill(n: string) {
    if (!(await this.ui.confirm('Remove Skill', 'Remove the skill ' + n + '? Its folder is moved to .removed in the skills folder, so it can be put back.', 'Remove', true))) return;
    try { await this.backend.aiRemoveSkill(n); this.skillEdit.set(null); this.skillDirty.set(false); await this.loadSkills(); } catch (e) { this.ui.error(e); }
  }
  async importSkill() {
    try { const k = await this.backend.aiImportSkill(false); if (k?.name) { this.skillDirty.set(false); await this.loadSkills(); await this.openSkill(k.name); this.ui.toast('Imported the skill ' + k.name, 'success'); } }
    catch (e) {
      if (/already exists/.test(errText(e)) && await this.ui.confirm('Replace Skill', errText(e) + '. Replace it? The old one is moved to .removed.', 'Replace')) {
        try { const k = await this.backend.aiImportSkill(true); if (k?.name) { await this.loadSkills(); await this.openSkill(k.name); } } catch (e2) { this.ui.error(e2); }
      } else this.ui.error(e);
    }
  }
  async exportSkill(n: string) { try { const p = await this.backend.aiExportSkill(n); if (p) this.ui.toast('Saved ' + p, 'success'); } catch (e) { this.ui.error(e); } }
  async skillsFolder() { try { await this.backend.showInFolder(await this.backend.aiSkillsFolder()); } catch (e) { this.ui.error(e); } }
  async addExampleSkills() { try { const l = await this.backend.aiAddExampleSkills(); await this.loadSkills(); this.ui.toast('Added ' + (l.join(', ') || 'nothing new'), 'success'); } catch (e) { this.ui.error(e); } }
  async writeSkill() {
    const q = this.aiSkillText.trim(); if (!q || this.aiSkillBusy()) return;
    this.aiSkillBusy.set(true); this.cdr.markForCheck();
    try {
      const a = await this.backend.aiAsk({ kind: 'skill', text: '', question: q, deviceType: '', protocol: '' });
      const m = /```(?:markdown|md)?\s*\n([\s\S]*?)```/.exec(a.text);
      await this.newSkill((m ? m[1] : a.text).trim() + '\n');
      this.aiSkillText = ''; this.aiSkillOpen.set(false);
      this.ui.toast('Written by the AI assistant - check it, then Save', 'info');
    } catch (e) { this.ui.error(e); }
    this.aiSkillBusy.set(false); this.cdr.markForCheck();
  }
  setAI(p: Partial<AISettings>) { if (this.s) this.s.ai = { ...(this.s.ai ?? {}), ...p }; this.cdr.markForCheck(); }
  async saveAIKey() {
    const p = this.s?.ai?.provider ?? '';
    try { await this.backend.aiSetKey(p, this.aiKey); this.aiKey = ''; this.aiMsg.set(tr('The key is saved in the OS keychain.')); this.loadAI(); }
    catch (e) { this.aiMsg.set(errText(e)); }
  }
  async forgetAIKey() { await this.backend.aiSetKey(this.s?.ai?.provider ?? '', '').catch(() => {}); this.loadAI(); }
  async loadAIModels() {
    this.aiLoading.set(true);
    try { await this.loadAIModelsNow(); } finally { this.aiLoading.set(false); this.cdr.markForCheck(); }
  }
  private async loadAIModelsNow() {
    this.aiMsg.set('');
    if (this.s?.ai?.provider === 'claudecode') {
      try {
        const p = await this.backend.claudeCodeFind(this.s.ai.baseUrl ?? '');
        this.aiModels.set((await this.backend.aiModels(this.s.ai).catch(() => null)) ?? ['opus', 'sonnet', 'haiku']);
        this.aiMsg.set(tr('Claude Code found: {0}. Choose the model and the effort below, or keep the defaults.', p));
        this.aiCheck.set(true);
      } catch (e) { this.aiMsg.set(errText(e)); this.aiCheck.set(false); }
      return;
    }
    try {
      const m = await this.backend.aiModels(this.s?.ai ?? {});
      this.aiModels.set(m ?? []);
      this.aiCheck.set(true);
      this.aiMsg.set(m?.length ? tr('{0} models - pick one, or leave it empty for the first: {1}', m.length, m[0]) : tr('The service lists no models.'));
    } catch (e) { this.aiMsg.set(errText(e)); this.aiCheck.set(false); }
  }
  readonly bkBusy = signal(false);
  readonly bkMsg = signal('');
  async backupNow() {
    this.bkBusy.set(true); this.bkMsg.set('');
    try { this.bkMsg.set(tr('Backup saved to {0}', await this.backend.backupConfig())); } catch (e) { this.bkMsg.set(errText(e)); }
    this.bkBusy.set(false);
  }
  /** Restores a backup: the current settings are backed up first, Termwright closes and the backup is applied when it starts again. */
  async restoreBackup() {
    const p = await this.backend.chooseOpenFile(tr('Restore from Backup'), [{ displayName: tr('Termwright backup'), pattern: '*.zip' }]).catch(() => '');
    if (!p) return;
    if (!(await this.ui.confirm(tr('Restore from Backup'), tr('Replace your settings, sessions, accounts, library and AI chats with the ones in {0}? Your current ones are backed up first. Termwright closes - start it again to finish. Open tabs are closed.', p.split(/[\\/]/).pop() ?? p), tr('Restore and Close'), true))) return;
    this.bkBusy.set(true); this.bkMsg.set('');
    try {
      const r = await this.backend.restoreConfig(p);
      if (await this.ui.confirm(tr('Start Termwright Again'), tr('{0} files are ready to restore. Your settings from before are in {1}. Close Termwright now and start it again to finish.', r.files, r.before), tr('Close Termwright'))) await this.backend.quit();
      else this.bkMsg.set(tr('The backup is restored the next time Termwright starts. Your settings from before: {0}', r.before));
    } catch (e) { this.bkMsg.set(errText(e)); }
    this.bkBusy.set(false);
  }
  async browseSync() {
    const d = await this.backend.chooseDirectory('Choose the Sync Folder').catch(() => '');
    if (d && this.s) this.s.syncFolder = d;
    this.cdr.markForCheck();
  }
  async syncNow() { this.syncSt.set(await this.backend.syncNow()); }
  ago(iso: string) { const m = Math.round((Date.now() - Date.parse(iso)) / 60000); return m < 1 ? 'just now' : m < 60 ? m + ' min ago' : Math.round(m / 60) + ' h ago'; }

  async browse(field: 'sessionsDir' | 'logDir' | 'downloadDir' | 'recordDir') {
    const d = await this.backend.chooseDirectory('Choose Folder').catch(() => '');
    if (d && this.s) this.s[field] = d;
    this.cdr.markForCheck();
  }

  move(i: number, d: number) {
    const b = this.s!.buttons;
    [b[i], b[i + d]] = [b[i + d], b[i]];
  }

  private async saveDevicePasswords() {
    const list = (this.s?.deviceTypes ?? []).filter(d => d.name.trim());
    const kept = new Set(list.map(d => d.name.trim().toLowerCase()));
    for (const n of this.dtRemoved) if (!kept.has(n.toLowerCase())) await this.backend.setDeviceTypePassword(n, '', false);
    for (const d of list) {
      const p = this.dtPw.get(d);
      if (!p) continue;
      const name = d.name.trim();
      if (p.orig && p.state && p.orig.toLowerCase() !== name.toLowerCase()) await this.backend.renameDeviceTypePassword(p.orig, name);
      if (p.pw) { await this.backend.setDeviceTypePassword(name, p.pw, false); d.credential = ''; }
      else if (p.empty !== (p.state === 'empty')) { await this.backend.setDeviceTypePassword(name, '', p.empty); if (p.empty) d.credential = ''; }
      else if (p.forget) await this.backend.setDeviceTypePassword(name, '', false);
    }
  }

  async save() {
    if (!this.s) return;
    this.normEnvs();
    try { if ((this.s.mapLook || 'flow') !== (localStorage.getItem('map.look2') || this.s.mapLook || 'flow')) localStorage.removeItem('map.look2'); } catch { /* ignore */ }
    this.s.fontSize = Number(this.s.fontSize) || 14;
    this.s.scrollback = Number(this.s.scrollback) || 10000;
    this.s.pasteLineDelayMs = Number(this.s.pasteLineDelayMs) || 0;
    this.s.keyMap = (this.s.keyMap ?? []).filter(k => k.keys && (k.send || k.action || k.script));
    await this.saveDevicePasswords().catch(e => this.ui.error(e));
    this.s.deviceTypes = (this.s.deviceTypes ?? []).filter(d => d.name.trim()).map(d => ({ ...d, name: d.name.trim(), port: Number(d.port) || 0,
      loginSteps: (d.loginSteps ?? []).filter(x => x.expect || x.send), afterLogin: (d.afterLogin ?? []).filter(x => x.expect || x.send) }));
    this.s.alerts = (this.s.alerts ?? []).filter(a => a.pattern.trim());
    await this.ui.saveSettings(this.s);
    this.ui.close();
  }
}
