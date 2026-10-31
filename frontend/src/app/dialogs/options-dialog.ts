import { ChangeDetectorRef, Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { askRef, isRef, refOf } from '../core/secret-ref';
import { LANGUAGES, previewLanguages } from '../core/i18n';
import { Backend } from '../core/backend';
import { ACTIONS, DeviceType, KeyBinding, PROTOCOLS, Settings, SmartCardInfo, SmartCardKey, comboOf, FACT_NAMES } from '../core/models';
import { COLOR_KEYS, SCHEME_NAMES, fullScheme } from '../core/themes';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';
import { UI_FONTS, UI_SIZES, uiFontStack } from '../core/ui-fonts';

interface DtPw { orig: string; pw: string; empty: boolean; state: string; forget: boolean; }
type Page = 'general' | 'terminal' | 'schemes' | 'keywords' | 'keymap' | 'buttons' | 'updates' | 'devices' | 'alerts' | 'cards';

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
  ['schemes', 'Scheme', 'colour scheme custom colors palette'], ['keywords', 'Default set', 'keyword highlighting highlight words colour'],
  ['keymap', 'Key Map', 'shortcut keyboard binding hotkey'], ['buttons', 'Button Bar', 'buttons send command'],
  ['devices', 'Device Types', 'login profile exos voss cisco linux username password prompt'], ['devices', 'No password', 'device type'],
  ['devices', 'Send initial carriage return', 'enter'], ['devices', 'Backup commands', 'configuration backup running-config show configuration'], ['alerts', 'Alerts', 'notify pattern sound desktop notification'],
  ['updates', 'Notify me about', 'update stable nightly'], ['updates', 'Check at start-up', 'update automatic'], ['updates', 'Repository', 'github'],
];
const PAGE_NAMES: Record<Page, string> = { general: 'General', terminal: 'Terminal & Appearance', schemes: 'Color Schemes', keywords: 'Keyword Highlighting',
  keymap: 'Key Map', buttons: 'Button Bar', devices: 'Device Types', alerts: 'Alerts', cards: 'Smart Cards', updates: 'Updates' };

/** Global Options dialog. */
@Component({
  selector: 'app-options-dialog',
  imports: [FormsModule, Icon],
  template: `
  @if (s) {
  <div class="modal-backdrop">
    <form class="modal wide" (ngSubmit)="save()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="options" [size]="18" /></span>
        <span class="mh-t">{{ modern() ? 'Settings' : 'Global Options' }}<span class="mh-s">Global Options for every session</span></span></div>
      <div class="modal-split">
        <nav class="cat">
          <input type="search" class="osearch" placeholder="Search options" [ngModel]="q()" (ngModelChange)="q.set($event)" name="osearch" autocomplete="off" />
          @if (q().trim()) {
            @for (h of hits(); track $index) {
              <a class="hit" (click)="goTo(h[0], h[1])">{{ h[1] }}<span>{{ pageName(h[0]) }}</span></a>
            } @empty { <span class="nohit">Nothing found</span> }
          } @else {
          <a [class.on]="page() === 'general'" (click)="page.set('general')"><app-icon name="eye" [size]="15" />General</a>
          <a [class.on]="page() === 'terminal'" (click)="page.set('terminal')"><app-icon name="shell" [size]="15" />{{ modern() ? 'Terminal' : 'Terminal & Appearance' }}</a>
          <a class="sub" [class.on]="page() === 'schemes'" (click)="page.set('schemes')"><app-icon name="palette" [size]="15" />Color Schemes</a>
          <a [class.on]="page() === 'keywords'" (click)="page.set('keywords')"><app-icon name="highlight" [size]="15" />Keyword Highlighting</a>
          <div class="cat-g">Automation</div>
          <a [class.on]="page() === 'keymap'" (click)="page.set('keymap')"><app-icon name="keyboard" [size]="15" />Key Map</a>
          <a [class.on]="page() === 'buttons'" (click)="page.set('buttons')"><app-icon name="buttons" [size]="15" />Button Bar</a>
          <a [class.on]="page() === 'devices'" (click)="page.set('devices')"><app-icon name="device" [size]="15" />Device Types</a>
          <a [class.on]="page() === 'alerts'" (click)="page.set('alerts')"><app-icon name="bell" [size]="15" />Alerts</a>
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
                @if (modern()) {
                  <div class="themes">
                    @for (t of themeTiles; track t.v) {
                      <button type="button" class="tht" [class.on]="(s.theme || 'system') === t.v" (click)="s.theme = t.v"><span class="pv {{ t.v }}"></span>{{ t.label }}</button>
                    }
                  </div>
                } @else {
                <select name="theme" [(ngModel)]="s.theme">
                  <option value="system">Follow system</option><option value="dark">Dark</option><option value="light">Light</option>
                </select>
                }
                @if (modern() && s.layout !== 'classic') {
                  <label>Style</label>
                  <div class="seg w2"><button type="button" [class.on]="s.style === 'standard'" (click)="s.style = 'standard'">Standard</button><button type="button" [class.on]="s.style !== 'standard'" (click)="s.style = ''">Glacier</button></div>
                  <div class="hint">Glacier: frosted glass panels and raised cards, three per row - for the light and the dark theme.</div>
                }
                <label>Language</label>
                <select name="lang" [ngModel]="s.language || ''" (ngModelChange)="s.language = $event">
                  @for (l of languages; track l.code) { <option [value]="l.code" translate="no">{{ l.name }}</option> }
                </select>
                <div class="pair">
                <div class="f"><label>Layout</label>
                <div>
                  @if (modern()) {
                    <div class="seg w2"><button type="button" [class.on]="!s.layout" (click)="s.layout = ''">Modern</button><button type="button" [class.on]="s.layout === 'classic'" (click)="s.layout = 'classic'">Classic</button></div>
                  } @else {
                  <select name="layout" [ngModel]="s.layout || ''" (ngModelChange)="s.layout = $event">
                    <option value="">Modern - tabs and search in the title bar</option><option value="classic">Classic - toolbar, tab strip and menu bar</option>
                  </select>
                  }
                  @if (isMac) { <div class="hint">On macOS the window's own title bar changes the next time Termwright starts.</div> }
                </div></div>
                <div class="f"><label>Icons</label>
                <div>
                  @if (modern()) {
                    <div class="seg w2"><button type="button" [class.on]="!s.iconStyle" (click)="s.iconStyle = ''">Tiles</button><button type="button" [class.on]="s.iconStyle === 'glyph'" (click)="s.iconStyle = 'glyph'">Glyphs</button></div>
                  } @else {
                  <select name="icons" [ngModel]="s.iconStyle || ''" (ngModelChange)="s.iconStyle = $event">
                    <option value="">Coloured tiles - the device in white on its colour</option><option value="glyph">Glyphs - the device in its colour, no tile</option>
                  </select>
                  }
                  @if (s.layout !== 'classic') {
                    <label class="inline-check" style="margin-top: 6px"><input type="checkbox" name="compact" [ngModel]="!!s.compactRows" (ngModelChange)="s.compactRows = $event" />
                      Compact Session Manager (one line per session - more sessions in view)</label>
                  }
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
                  <label><input type="checkbox" name="cc" [(ngModel)]="s.confirmClose" /> @if (modern()) { <span class="tx">Confirm before closing connected tabs<small>and before quitting</small></span> } @else { Confirm before closing connected tabs / quitting }</label>
                  <label><input type="checkbox" name="cpaste" [(ngModel)]="s.confirmPaste" /> @if (modern()) { <span class="tx">Confirm pasting more than one line<small>shows the lines first - production tabs always ask</small></span> } @else { Confirm before pasting more than one line }</label>
                  <label><input type="checkbox" name="rhist" [ngModel]="s.recordHistory !== false" (ngModelChange)="s.recordHistory = $event" />
                    @if (modern()) { <span class="tx">Remember typed commands<small>for Command History and suggestions - never commands with passwords, secrets or keys</small></span> }
                    @else { Remember typed commands for Command History and suggestions (never commands with passwords, secrets or keys) }</label>
                  <label><input type="checkbox" name="qcs" [ngModel]="!!s.quickConnectSave" (ngModelChange)="s.quickConnectSave = $event" /> @if (modern()) { <span class="tx">Save Quick Connect sessions<small>every connection made with Quick Connect is kept in the Session Manager (in the selected folder)</small></span> } @else { Save Quick Connect sessions in the Session Manager }</label>
                  <label><input type="checkbox" name="nmc" [ngModel]="!s.noMoveConfirm" (ngModelChange)="s.noMoveConfirm = !$event" /> @if (modern()) { <span class="tx">Confirm before moving sessions<small>dragging or pasting sessions and folders into another folder asks first (Undo is in the Session Manager's ⋯ menu)</small></span> } @else { Confirm before moving sessions and folders in the Session Manager }</label>
                  <label><input type="checkbox" name="ndc" [ngModel]="!s.noDeleteConfirm" (ngModelChange)="s.noDeleteConfirm = !$event" /> @if (modern()) { <span class="tx">Confirm before deleting sessions<small>deleting sessions and folders asks first (Undo is in the Session Manager's ⋯ menu); a jump host that other sessions use always asks</small></span> } @else { Confirm before deleting sessions and folders in the Session Manager }</label>
                  <label><input type="checkbox" name="rt" [(ngModel)]="s.restoreTabs" /> @if (modern()) { <span class="tx">Reopen the tabs that were open<small>when Termwright starts</small></span> } @else { Reopen the sessions that were open when Termwright was closed }</label>
                  <label><input type="checkbox" name="sm" [(ngModel)]="s.showSessionManager" /> @if (modern()) { <span class="tx">Show Session Manager<small>the session list on the left (or right)</small></span> } @else { Show Session Manager }</label>
                  <label><input type="checkbox" name="bb" [(ngModel)]="s.showButtonBar" /> @if (modern()) { <span class="tx">Show Button Bar<small>buttons that send commands, under the terminal</small></span> } @else { Show Button Bar }</label>
                  <label><input type="checkbox" name="cw" [(ngModel)]="s.showCommandWindow" /> @if (modern()) { <span class="tx">Show Command Window<small>type a command and send it to one or more tabs</small></span> } @else { Show Command Window }</label>
                  @if (s.layout === 'classic') {
                    <label><input type="checkbox" name="cbar" [(ngModel)]="s.showConnectBar" /> Show Connect Bar in the toolbar</label>
                    <label><input type="checkbox" name="iwm" [(ngModel)]="s.inWindowMenu" /> Show the menu bar inside the window (macOS also keeps the top-of-screen menu)</label>
                  }
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
                  <label>Backup commands</label>
                  <textarea name="dtbk" class="mono" rows="2" [(ngModel)]="d.backupCommands"
                    [placeholder]="backupDefault(d.name) || 'Commands that print the configuration, one per line, e.g. show running-config'"></textarea>
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
              <details class="adv">
                <summary>Advanced - only for your own fork</summary>
                <div class="grid">
                  <label>Repository</label><input name="urepo" [(ngModel)]="s.updateRepo" placeholder="owner/name" />
                  <label>GitHub token</label>
                  <div class="row"><input type="password" name="utok" [(ngModel)]="token" [placeholder]="hasToken() ? '(saved in the OS keychain - type to replace)' : 'only for a private fork'" autocomplete="off" />
                    <button type="button" (click)="saveToken()">Save</button>
                    @if (hasToken()) { <button type="button" (click)="clearToken()">Remove</button> }</div>
                  <label></label>
                  <div class="hint">Leave these alone to get the official Termwright releases. For a private fork use a fine-grained token with read-only <b>Contents</b> access to that repository.</div>
                </div>
              </details>
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
        @if (modern()) { <span class="hint">Changes apply when you press Save</span> }
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Cancel</button>
        <button type="submit" class="primary">{{ modern() ? 'Save' : 'OK' }}</button>
      </div>
    </form>
  </div>
  }`,
  styles: [`
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
    .seg.w2 { max-width: 300px; }
    .pair .seg.w2 { max-width: none; }
  `],
})
export class OptionsDialog implements OnInit {
  protected ui = inject(Ui);
  readonly uiFonts = UI_FONTS;
  readonly uiSizes = UI_SIZES;
  readonly factNames = FACT_NAMES;
  readonly modern = () => this.ui.settings()?.layout !== 'classic';
  readonly languages = LANGUAGES.filter(l => !l.preview || previewLanguages());
  readonly themeTiles = [{ v: 'light', label: 'Light' }, { v: 'dark', label: 'Dark' }, { v: 'system', label: 'System' }];
  reOk(re: string) { if (!re) return true; try { new RegExp(re); return true; } catch { return false; } }
  readonly isMac = navigator.platform.toLowerCase().includes('mac') || navigator.userAgent.includes('Mac OS');
  fontStack(v: string | undefined) { return uiFontStack(v); }
  private backend = inject(Backend);
  private cdr = inject(ChangeDetectorRef);
  readonly page = signal<Page>((this.ui.optionsPage() || 'general') as Page);
  /** Built-in backup command shown when a device type has none of its own. */
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

  token = '';
  readonly hasToken = signal(false);

  async saveToken() {
    if (!this.token.trim()) return;
    try { await this.backend.setUpdateToken(this.token); this.token = ''; this.hasToken.set(true); this.ui.toast('Token saved', 'success'); }
    catch (e) { this.ui.error(e); }
  }

  async clearToken() {
    try { await this.backend.setUpdateToken(''); this.hasToken.set(false); } catch (e) { this.ui.error(e); }
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

  ngOnInit() {
    this.ui.optionsPage.set('');
    const cur = this.ui.settings();
    if (cur) this.s = structuredClone(cur);
    if (this.s) this.schemeName.set(!this.s.terminalScheme || this.s.terminalScheme === 'Auto' ? 'Default' : this.s.terminalScheme);
    this.schemeRev.update(v => v + 1);
    this.backend.getAppInfo().then(i => this.dataDir.set(i.dataDir)).catch(() => {});
    this.backend.hasUpdateToken().then(v => this.hasToken.set(v)).catch(() => {});
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
