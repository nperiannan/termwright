import { filesPanelFor } from '../core/device-kind';
import { ChangeDetectorRef, Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Autofocus } from '../core/autofocus';
import { askRef, isRef, refOf } from '../core/secret-ref';
import { followJump } from '../core/jump-refs';
import { Backend, errText } from '../core/backend';
import { BAUD_RATES, CONSOLE_METHODS, EMULATIONS, FACT_EVERY, FactRule, LogonAction, PROTOCOLS, Session, SessionNode, SmartCardKey, defaultPort } from '../core/models';
import { SCHEME_NAMES } from '../core/themes';
import { Tabs } from '../core/tabs.service';
import { envInfo, envList, envOf } from '../core/env';
import { Ui } from '../core/ui.service';
import { SerialPortPicker } from '../components/serial-port-picker';
import { Icon } from '../components/icon';
import { DeviceIcon, deviceKindOf } from '../components/device-icon';
import { accountChoice, defaultLogin, defaultText, setAccountChoice, userOf } from '../core/accounts';
import { SessionIcon, SessionKind } from '../components/session-icon';
import { FolderPicker } from '../components/folder-picker';
import { FactTeach } from './fact-teach';
import { factList, factsAge } from '../core/facts';
import { testFactRegex } from '../core/fact-regex';

type Page = 'connection' | 'auth' | 'firewall' | 'forward' | 'serial' | 'local' | 'logon' | 'terminal' | 'log' | 'remote' | 'facts';

/** Saves a session (and its password when one was typed or cleared),
 *  refreshes the Session Manager, selects it and updates its open tabs. */
export async function storeSession(ui: Ui, backend: Backend, tabs: Tabs, s: Session, oldPath: string, tabId: string | undefined,
  pw: { pw: string; noPw: boolean; wasEmpty: boolean }): Promise<Session> {
  const setPw = (!s.deviceType || s.ownLogin) && (pw.pw !== '' || pw.noPw !== pw.wasEmpty);
  if (setPw) s.savePassword = true;
  const saved = await backend.saveSession(s, oldPath);
  if (setPw) await backend.setSessionPassword(saved.path, pw.noPw ? '' : pw.pw, pw.noPw);
  await ui.refreshTree();
  ui.selectedPath.set(saved.path);
  ui.selectedIsFolder.set(false);
  for (const t of tabs.tabs()) {
    if (t.session.path === oldPath || t.id === tabId) {
      await backend.refreshTabSession(t.id, saved.path).catch(() => {});
      tabs.patch(t.id, { session: saved, title: saved.name });
    }
  }
  // renamed or moved: the sessions that use it as jump host are asked whether to follow
  if (oldPath && oldPath !== saved.path) await followJump(ui, backend, [{ from: oldPath, to: saved.path }]);
  return saved;
}

/** Session Options dialog (create / edit a saved session). */
@Component({
  selector: 'app-session-editor',
  imports: [FormsModule, Autofocus, SerialPortPicker, Icon, DeviceIcon, SessionIcon, FolderPicker, FactTeach],
  template: `
  @if (s) {
  <div class="modal-backdrop">
    <form class="modal wide" (ngSubmit)="save(false)" autocomplete="off">
      <div class="modal-head"><span class="mh-ic dev">@if (devKind(); as k) { <app-device-icon [kind]="k" [size]="34" /> } @else { <app-session-icon [kind]="protoKind()" [size]="34" /> }</span>
        <span class="mh-t">{{ modern() ? (s.name || 'New Session') : (oldPath ? 'Session Options - ' + oldPath : 'New Session') }}<span class="mh-s">{{ subtitle() }}</span></span></div>
      <div class="modal-split">
        <nav class="cat so">
          <a [class.on]="page() === 'connection'" (click)="page.set('connection')" style="order: 1"><app-icon name="connect" [size]="15" />Connection</a>
          @if (s.protocol === 'SSH2') {
            <a class="sub" [class.on]="page() === 'auth'" (click)="page.set('auth')" style="order: 2"><app-icon name="key" [size]="15" />{{ modern() ? 'Authentication' : 'SSH2 Authentication' }}</a>
          }
          @if (!noTerm()) { <a class="sub" [class.on]="page() === 'forward'" [class.dim]="s.protocol !== 'SSH2'" (click)="page.set('forward')" style="order: 5"><app-icon name="forward" [size]="15" />Port Forwarding</a> }
          @if (remote()) { <a class="sub" [class.on]="page() === 'remote'" (click)="page.set('remote')" style="order: 3"><app-icon name="monitor" [size]="15" />{{ s.protocol === 'RDP' ? 'Remote Desktop' : 'VNC' }}</a> }
          @if (s.protocol === 'SSH2' || s.protocol === 'Telnet' || s.protocol === 'Raw' || remote() || s.protocol === 'WOL') {
            <a class="sub" [class.on]="page() === 'firewall'" (click)="page.set('firewall')" style="order: 4"><app-icon name="route" [size]="15" />Jump Host / Proxy</a>
          }
          @if (s.protocol === 'SSH2' || s.protocol === 'Telnet') {
            <a class="sub" [class.on]="page() === 'facts'" (click)="page.set('facts')" style="order: 7"><app-icon name="device" [size]="15" />Device Facts</a>
          }
          @if (s.protocol === 'Serial') { <a class="sub" [class.on]="page() === 'serial'" (click)="page.set('serial')" style="order: 6"><app-icon name="serial" [size]="15" />Serial</a> }
          @if (s.protocol === 'Local Shell') { <a class="sub" [class.on]="page() === 'local'" (click)="page.set('local')" style="order: 7"><app-icon name="shell" [size]="15" />Local Shell</a> }
          @if (!noTerm()) {
            <div class="cat-g" style="order: 8">Terminal</div>
            <a class="sub" [class.on]="page() === 'logon'" (click)="page.set('logon')" style="order: 10"><app-icon name="script" [size]="15" />{{ modern() ? 'Logon Actions' : 'Logon Actions & Triggers' }}</a>
            <a [class.on]="page() === 'terminal'" (click)="page.set('terminal')" style="order: 9"><app-icon name="shell" [size]="15" />{{ modern() ? 'Appearance' : 'Terminal' }}</a>
            <a class="sub" [class.on]="page() === 'log'" (click)="page.set('log')" style="order: 11"><app-icon name="log" [size]="15" />{{ modern() ? 'Logging' : 'Log File' }}</a>
          }
        </nav>
        <div class="modal-body">
          <h2 class="page-title">{{ pageName() }}</h2>
          @switch (page()) {
            @case ('facts') {
              <div class="facts">
                @if (!oldPath) {
                  <div class="hint">Save the session first - device facts are kept for saved sessions.</div>
                } @else {
                  <div class="fnow">
                    @for (r of factTable(); track r.name) {
                      @if (factEdit()?.orig === r.name) {
                        <div class="fed">
                          <div class="pair2"><label>Name<input name="fen" [(ngModel)]="factEdit()!.name" /></label>
                            <label>Value for this session<input name="fev" class="mono" [(ngModel)]="factEdit()!.value" placeholder="(not read yet)" /></label></div>
                          @if (s.deviceType) {
                            <div class="pair2"><label>Command<input name="fec" class="mono" [(ngModel)]="factEdit()!.command" placeholder="e.g. show sys-info" spellcheck="false" /></label>
                              <label>Read it<div class="seg"><button type="button" [class.on]="factEdit()!.every" (click)="factEdit()!.every = true">Every connect</button><button type="button" [class.on]="!factEdit()!.every" (click)="factEdit()!.every = false">Once</button></div></label></div>
                            <label>Pattern (regular expression - the first ( ) group is the value)<input name="fer" class="mono" [(ngModel)]="factEdit()!.regex" spellcheck="false" /></label>
                            @if (factOut() && factEdit()!.regex) {
                              <div class="hint">In the output above: {{ testFact(factEdit()!.regex) ?? 'not found' }}</div>
                            }
                          }
                          <div class="row"><span class="hint">{{ s.deviceType ? 'Name, command and pattern are kept with ' + s.deviceType + ' (all its sessions); the value only for this session.' : 'Without a device type only the value is kept.' }}</span><span class="spacer"></span>
                            <button type="button" (click)="cancelFactEdit()">Cancel</button><button type="button" class="primary" (click)="saveFactEdit()">Save</button></div>
                        </div>
                      } @else {
                        <div class="fr">
                          <span class="fk">{{ r.name }}</span>
                          <span class="fv">{{ r.value || '-' }}</span>
                          <span class="fc" [title]="r.rule ? r.rule.regex : 'No rule: the value was saved by hand or the rule was removed'">{{ r.rule ? r.rule.command + (r.rule.every ? ' · every connect' : ' · once') : 'no rule' }}</span>
                          <span class="fx"><button type="button" class="icon-btn" title="Edit" (click)="editFact(r)"><app-icon name="edit" [size]="14" /></button>
                            <button type="button" class="icon-btn" title="Remove" (click)="removeFact(r)"><app-icon name="trash" [size]="14" /></button></span>
                        </div>
                      }
                    } @empty { <div class="hint">Nothing read yet{{ s.deviceType ? '' : ' - choose the device type on the Connection page first' }}.</div> }
                    <div class="row fa"><span class="hint">{{ factAge() }}</span><span class="spacer"></span>
                      <button type="button" (click)="newFact()">Add by Hand…</button>
                      <button type="button" (click)="refreshFacts()" [disabled]="!!factBusy() || !s.deviceType">{{ factBusy() === 'refresh' ? 'Reading…' : 'Read Now' }}</button></div>
                  </div>
                  <h4>Teach a new fact</h4>
                  @if (!s.deviceType) {
                    <div class="hint">Choose the device type on the Connection page first - a fact is read for every session of that type.</div>
                  } @else {
                    <div class="hint">Run a command (in a background login - nothing is typed in your tabs), then select the value in its output with the mouse, for example the version, and press Save Selection as Fact.</div>
                    <div class="row"><input name="fcmd" class="mono" [(ngModel)]="factCmd" placeholder="e.g. show sys-info" spellcheck="false" (keydown.enter)="$event.preventDefault(); runFact()" />
                      <button type="button" (click)="runFact()" [disabled]="!!factBusy() || !factCmd.trim()">{{ factBusy() === 'run' ? 'Running…' : 'Run' }}</button></div>
                    @if (factOut(); as out) {
                      <pre class="fout" (mouseup)="pickFact($event)">{{ out }}</pre>
                      <div class="row"><span class="hint">{{ factPick() ? 'Selected: ' + factPick()!.value : 'Select the value in the output' }}</span><span class="spacer"></span>
                        <button type="button" class="primary" [disabled]="!factPick()" (click)="teachFromOutput()">Save Selection as Fact…</button></div>
                    }
                  }
                }
              </div>
            }
            @case ('connection') {
              <div class="grid ord">
                <div class="pair o1">
                  <div class="f"><label>Name</label><input name="name" [(ngModel)]="s.name" required placeholder="e.g. core-switch-01" /></div>
                  <div class="f"><label>Folder</label>
                    @if (modern()) { <app-folder-picker [(value)]="s.folder" /> }
                    @else { <input name="folder" [(ngModel)]="s.folder" placeholder="e.g. Datacenter/Rack 4 (blank = top level)" /> }</div>
                </div>
                <div class="f o2"><label>Protocol</label>
                @if (modern()) {
                  <div class="seg">
                    @for (p of mainProtos; track p) { <button type="button" [class.on]="s.protocol === p" (click)="s.protocol = p; protocolChanged()">{{ p === 'SSH2' ? 'SSH' : p }}</button> }
                    <select name="otherp" [ngModel]="mainProtos.includes(s.protocol) ? '' : s.protocol" (ngModelChange)="$event && (s.protocol = $event) && protocolChanged()">
                      <option value="">More…</option>@for (p of otherProtos; track p) { <option [value]="p">{{ p === 'WOL' ? 'Wake-on-LAN' : p }}</option> }</select>
                  </div>
                } @else {
                <select name="protocol" [(ngModel)]="s.protocol" (ngModelChange)="protocolChanged()">
                  @for (p of protocols; track p) { <option [value]="p">{{ p === 'WOL' ? 'Wake-on-LAN' : p }}</option> }
                </select>
                }
                </div>
                <div class="pair o4">
                @if (!remote() && s.protocol !== 'WOL') {
                <div class="f"><label>Device type</label>
                <select name="devtype" [ngModel]="s.deviceType" (ngModelChange)="s.deviceType = $event; onDeviceType()">
                  <option value="">(none)</option>
                  @for (d of deviceTypes(); track d) { <option [value]="d">{{ d }}</option> }
                </select></div>
                }
                <div class="f"><label>Environment</label>
                <div class="row">
                  <select name="env" [(ngModel)]="s.environment" class="envsel" title="Colours the tab; production asks before a paste or a send to several tabs.">
                    <option value="">(none{{ folderEnv() ? ' - folder: ' + folderEnv() : '' }})</option>
                    @for (v of envs(); track v.id) { <option [value]="v.id">{{ v.label }}</option> }
                  </select>
                  <span class="hint envhint">Colours the tab; production asks before a paste or a send to several tabs.</span>
                </div></div>
                </div>
                @if (s.deviceType && !remote()) {
                  <label></label>
                  <div class="hint o4">Login and after-login steps come from the device type (Global Options → Device Types) and run before
                    this session's own Logon Actions.</div>
                }
                @if (s.protocol === 'SSH2') {
                  <label>Connect as</label>
                  <select name="openAs" [(ngModel)]="s.openAs">
                    <option value="">Terminal (SSH2)</option>
                    <option value="sftp">SFTP file browser only</option>
                  </select>
                  @if (s.openAs === 'sftp') {
                    <label></label>
                    <div class="hint">Opening this session logs in with its own SSH connection that only starts SFTP (no shell) -
                      for devices such as VOSS that allow one session per connection. Right-click a session or tab for the other mode.</div>
                  }
                }
                @if (s.protocol === 'Console') {
                  <label>Console server</label>
                  <select name="cserver" [(ngModel)]="s.consoleServer">
                    <option value="">(choose the console server's session)</option>
                    @for (p of consoleServers(); track p) { <option [value]="p">{{ p }}</option> }
                  </select>
                  <label>Port</label>
                  <div class="row"><input name="cport" type="number" min="1" max="9999" class="short" [(ngModel)]="s.consolePort" />
                    <span class="hint">the console server's port (line) number the device is cabled to</span></div>
                  <label>Access</label>
                  <select name="cmethod" [(ngModel)]="s.consoleMethod">
                    @for (m of consoleMethods; track m.id) { <option [value]="m.id">{{ m.label }}</option> }
                  </select>
                  @if (s.consoleMethod === 'custom') {
                    <label>Custom</label>
                    <div class="row">
                      <select name="cproto" [(ngModel)]="s.consoleProtocol"><option value="SSH2">SSH2</option><option value="Telnet">Telnet</option></select>
                      <span>TCP port</span><input name="cbase" type="number" class="short" [(ngModel)]="s.consoleBase" placeholder="0" title="0 = the console server session's port" /><span>+ port</span>
                      @if (s.consoleProtocol !== 'Telnet') { <span>user</span><input name="cuser" [(ngModel)]="s.consoleUser" placeholder="{user}:{port}" title="{user} = the console server's user, {port} = port number, {nn} = two digits" /> }
                    </div>
                  }
                  <label></label>
                  <div class="hint">{{ consoleHint() }} The console server's session gives the address, jump host and SSH login; the device type and the
                    username / password below log in to the device at the console.</div>
                }
                @if (s.protocol === 'WOL') {
                  <label>MAC address</label>
                  <div class="row"><input name="wmac" class="mono" [(ngModel)]="s.mac" placeholder="00:11:22:33:44:55" spellcheck="false" style="max-width: 200px" required />
                    <span class="hint">of the wired port that is set to wake (BIOS / UEFI and the network card)</span></div>
                  <label>Network</label>
                  <div class="row"><input name="wnet" class="mono" [(ngModel)]="s.wolNetwork" placeholder="192.168.1.0/24" spellcheck="false" style="max-width: 200px" />
                    <span class="hint">the magic packet goes to its broadcast address - filled in from the IP address, change the mask if needed; without an IP address it is searched for the MAC address</span></div>
                  <label>IP address</label>
                  <div class="row"><input name="hostname" class="mono" [ngModel]="s.hostname" (ngModelChange)="wolIp($event)" placeholder="or the network above" spellcheck="false" style="max-width: 200px" />
                    <span class="hint">pinged to show up (green) or down (orange); fills in the network above</span></div>
                  <label>UDP port</label>
                  <div class="row"><input name="wport" type="number" min="1" max="65535" class="short" [ngModel]="s.wolPort || 9" (ngModelChange)="s.wolPort = +$event === 9 ? 0 : +$event" />
                    <label>Broadcast</label><input name="wbc" class="mono" [(ngModel)]="s.wolBroadcast" placeholder="(the network's)" spellcheck="false" style="max-width: 160px"
                      title="Only when it differs from the network's broadcast address, e.g. 255.255.255.255" /></div>
                  <label>After waking</label>
                  <div class="row">
                    <span>search</span><input name="wscans" type="number" min="1" max="20" class="short" [ngModel]="s.wolScans || 4" (ngModelChange)="s.wolScans = +$event === 4 ? 0 : +$event" />
                    <span>times, every</span><input name="wsecs" type="number" min="5" max="600" class="short" [ngModel]="s.wolScanSecs || 30" (ngModelChange)="s.wolScanSecs = +$event === 30 ? 0 : +$event" />
                    <span>seconds, then mark it not present / down</span></div>
                  <label>Ping</label>
                  <div class="row">
                    <select name="wping" [ngModel]="s.wolPingSecs || 120" (ngModelChange)="s.wolPingSecs = +$event === 120 ? 0 : +$event" style="width: auto">
                      <option [ngValue]="-1">never</option><option [ngValue]="30">every 30 seconds</option><option [ngValue]="60">every minute</option>
                      <option [ngValue]="120">every 2 minutes</option><option [ngValue]="300">every 5 minutes</option><option [ngValue]="600">every 10 minutes</option><option [ngValue]="1800">every 30 minutes</option>
                    </select>
                    <span class="hint">when it has an IP address: the dot in the Session Manager is green (answers) or orange (no answer); its NetBIOS name is read too</span></div>
                  @if (s.jumpSession) {
                    <label>Send from</label>
                    <select name="wfrom" [ngModel]="s.wolFrom || ''" (ngModelChange)="s.wolFrom = $event" style="width: auto">
                      <option value="">automatic (this computer when it is on the device's network)</option><option value="local">this computer</option><option value="jump">the jump host ({{ s.jumpSession }})</option>
                    </select>
                  }
                  <label></label>
                  <div class="row"><button type="button" (click)="wakeNow()" [disabled]="!s.mac">Wake Now</button>
                    <span class="hint">Double-click the session (or Connect) sends the magic packet and then pings the device, or searches the network for its MAC address and saves the address it finds.</span></div>
                }
                @if (s.protocol === 'RDP') {
                  <label></label><div class="hint">Opens in this computer's Remote Desktop client (Remote Desktop Connection on Windows, Windows App on macOS,
                    FreeRDP or Remmina on Linux). With a jump host or proxy, Termwright opens a local tunnel for it.</div>
                }
                @if (s.protocol === 'VNC') {
                  <label></label><div class="hint">Opens in Termwright's VNC viewer tab (VNC servers, macOS Screen Sharing, Raspberry Pi, Linux desktops), also through a jump host or proxy.</div>
                }
                @if (s.protocol === 'SSH2' || s.protocol === 'Telnet' || s.protocol === 'Raw' || remote()) {
                  <div class="pair hp o3">
                    <div class="f"><label>{{ modern() ? 'Address' : 'Hostname' }}</label><input name="hostname" [(ngModel)]="s.hostname" required placeholder="host name or IP address" /></div>
                    <div class="f"><label>Port</label><input name="port" type="number" min="1" max="65535" [(ngModel)]="s.port" class="short" /></div>
                  </div>
                  <label>MAC address</label>
                  <div class="row"><input name="mac" [(ngModel)]="s.mac" placeholder="(not saved)" spellcheck="false" style="max-width: 220px" />
                    <span class="hint">saved by Discover Devices: finds the device again when DHCP gives it a new address, and warns when another device answers at this address</span></div>
                  @if (s.mac) {
                    <label>Wake-on-LAN</label>
                    <div class="row wol">
                      <input name="wolb" [(ngModel)]="s.wolBroadcast" placeholder="broadcast: its network" spellcheck="false" style="max-width: 190px"
                        title="Leave empty for the broadcast address of the network the device's address is on; enter another one only if needed, e.g. 192.168.20.255" />
                      <input name="wolp" type="number" min="1" max="65535" [ngModel]="s.wolPort || null" (ngModelChange)="s.wolPort = $event || 0" placeholder="port 9" class="short" title="UDP port (9 by default, sometimes 7)" />
                      @if (s.jumpSession || (s.useRemoteCommand && isHop(s.remoteCommand))) {
                        <select name="wolf" [ngModel]="s.wolFrom || ''" (ngModelChange)="s.wolFrom = $event" style="width: auto">
                          <option value="">automatic</option>
                          <option value="local">from this computer</option>
                          <option value="jump">from {{ s.jumpSession ? 'the jump host' : 'the jump station' }}</option>
                        </select>
                      }
                      <button type="button" (click)="wakeNow()" [disabled]="!s.mac">Wake Now</button>
                    </div>
                  }
                }
                @if (canLogin()) {
                  <label class="o5">Account</label>
                  <select class="o5" name="acct" [ngModel]="acctChoice()" (ngModelChange)="setAcct($event)"
                    title="Who logs in: Default = the folder's account, else the device type's account or login; or a saved account; or this session's own username and password">
                    <option value="">Default · {{ defText() }}</option>
                    @for (a of accounts(); track a.name) { <option [value]="a.name">{{ a.name }}{{ a.username ? ' · ' + a.username : '' }}</option> }
                    <option value="-">Own username and password</option>
                    <option value="+">+ New Account…</option>
                  </select>
                  @if (acctUser(); as au) {
                    <label class="o5"></label>
                    <div class="acct-card o5"><span class="av">{{ initials(au.account || s.deviceType || '?') }}</span>
                      <span class="tx"><span>Logs in as <b class="mono">{{ au.user || '(asked when connecting)' }}</b> {{ au.how }}</span><small>Device type = how it logs in · Account = who logs in. Also used for SFTP, jump hosts and background logins.</small></span>
                      <button type="button" (click)="ui.open('passwords')">{{ au.account ? 'Edit Account…' : 'Accounts…' }}</button></div>
                  }
                }
                @if (s.protocol !== 'WOL' && (s.protocol === 'SSH2' || (modern() && !remote() && s.protocol !== 'Local Shell'))) {
                  <label class="o5">{{ modern() ? '' : 'Options' }}</label>
                  <div class="checks o5">
                    @if (modern() && !remote() && s.protocol !== 'Local Shell') {
                      <label><input type="checkbox" name="arc2" [(ngModel)]="s.autoReconnect" /><span class="tx">Reconnect automatically<small>when the connection drops</small></span></label>
                    }
                    @if (s.protocol === 'SSH2') {
                      <label title="A file browser beside the terminal, opposite the Session Manager: it follows the terminal's folder and opens files on this computer (View → Files Panel shows or hides it for a tab)">
                        <input type="checkbox" name="filesPanel" [(ngModel)]="s.filesPanel" />
                        @if (modern()) { <span class="tx">Files panel beside the terminal<small>follows the terminal's folder; double-click opens a file here</small></span> } @else { Show the Files panel beside the terminal }</label>
                    }
                  </div>
                }
                @if (ownFields()) {
                  <label class="o6">Username</label><input class="o6" name="username" [(ngModel)]="s.username" placeholder="(prompt when connecting)" />
                  <label class="o6">Password</label>
                  <div class="row o6">
                    <input name="sesspw" [type]="isRef(pw) ? 'text' : 'password'" [(ngModel)]="pw" [disabled]="noPw" autocomplete="new-password"
                      [placeholder]="refOf(pwState()) ? 'from ' + refOf(pwState()) : pwState() === 'set' ? '(saved - type to change)' : pwState() === 'empty' ? '(saved: no password)' : '(ask when connecting)'" />
                    <label class="nowrap"><input type="checkbox" name="nopw" [(ngModel)]="noPw" /> No password</label>
                    <button type="button" class="nowrap" title="Read the password from 1Password, Bitwarden, HashiCorp Vault or a command when connecting" (click)="pickRef()">Password manager...</button>
                  </div>
                }
                <label>Description</label>
                <textarea name="description" rows="4" [ngModel]="s.description.join('\\n')" (ngModelChange)="s.description = $event ? $event.split('\\n') : []"></textarea>
              </div>
            }
            @case ('remote') {
              <div class="grid">
                @if (s.protocol === 'RDP') {
                  <label>Domain</label><input name="rdpdom" [(ngModel)]="s.rdpDomain" placeholder="(none) e.g. CORP" />
                  <label>Window</label>
                  <div class="checks"><label><input type="checkbox" name="rdpfull" [(ngModel)]="s.rdpFullScreen" /> Full screen</label></div>
                  @if (!s.rdpFullScreen) {
                    <label>Size</label><div class="row"><input name="rdpsize" [(ngModel)]="s.rdpSize" class="short" placeholder="1600x900" /><span class="hint">blank = the client's default</span></div>
                  }
                  <label></label>
                  <div class="hint">On Windows the saved password is handed to Remote Desktop Connection; Windows App on macOS asks for it
                    (the tab has <i>Copy Password</i>); FreeRDP receives it on its input.</div>
                } @else {
                  <label>Control</label>
                  <div class="checks"><label><input type="checkbox" name="vncview" [(ngModel)]="s.vncViewOnly" /> View only (do not send keyboard and mouse)</label></div>
                  <label></label>
                  <div class="hint">Username is needed for macOS Screen Sharing (your Mac account) and VeNCrypt servers; classic VNC uses the password only.</div>
                }
              </div>
            }
            @case ('auth') {
              <div class="grid">
                <label>Methods</label>
                <div class="checks">
                  @for (m of authAll; track m) {
                    <label><input type="checkbox" [checked]="hasAuth(m)" (change)="toggleAuth(m)" /> {{ authLabel[m] || m }}</label>
                  }
                  <div class="hint">Tried in the order shown. Public keys come from the identity file below, a running SSH agent, or ~/.ssh/id_*.</div>
                </div>
                @if (hasAuth('gssapi')) {
                  <label>Kerberos principal</label>
                  <input name="krbp" [(ngModel)]="s.kerberosPrincipal" placeholder="e.g. alice@CORP.EXAMPLE.COM (blank = kinit ticket, else login user)" />
                  <label>Service name</label>
                  <input name="krbs" [(ngModel)]="s.kerberosSpn" placeholder="host@%H (default; %H = hostname)" />
                  <label></label>
                  <div class="hint">Uses your ticket from <code>kinit</code> (file cache). Without one, Termwright logs in to Kerberos itself and asks
                    for the principal's password once per run. The realm comes from krb5.conf, or from DNS when the principal includes it.</div>
                }
                <label>Smart card</label>
                <div class="row">
                  <select name="card" [(ngModel)]="s.smartCard" (focus)="loadCards()">
                    <option value="">(not used)</option>
                    <option value="any">Any key on the inserted cards</option>
                    @for (k of cardKeys(); track k.fingerprint) { <option [value]="k.fingerprint">{{ k.token }} · {{ k.label || k.id }} ({{ k.type }})</option> }
                    @if (s.smartCard && s.smartCard !== 'any' && !hasCardKey(s.smartCard)) { <option [value]="s.smartCard">{{ s.smartCard }} (card not inserted)</option> }
                  </select>
                  <button type="button" (click)="loadCards(true)" title="Read the inserted cards again">Refresh</button>
                </div>
                @if (s.smartCard) {
                  <label></label>
                  <div class="hint">PIV, CAC and other PKCS#11 cards (libraries: Global Options → Smart Cards). The PIN is asked at the first login and kept
                    in memory only until Termwright quits. Needs the Public Key method above.{{ cardNote() }}</div>
                }
                <label>Identity file</label>
                <div class="row"><input name="identity" [(ngModel)]="s.identityFile" placeholder="(default keys / agent)" />
                  <button type="button" (click)="browseIdentity()">Browse...</button></div>
                <label>Certificate</label>
                <div class="row"><input name="certf" [(ngModel)]="s.certificateFile" placeholder="(automatic: identity file + -cert.pub)" />
                  <button type="button" (click)="browseCert()">Browse...</button></div>
                <label></label>
                <div class="hint">OpenSSH user certificates (signed by your CA) are used automatically when <code>&lt;key&gt;-cert.pub</code> sits next
                  to the key, or from the file set here. FIDO2 security keys (<code>sk-ed25519</code>, <code>sk-ecdsa</code>): set the key as identity
                  file and load it once with <code>ssh-add</code>; Termwright then signs through ssh-agent (touch the key when it blinks).</div>
                <label>Password</label>
                <div class="checks">
                  <label><input type="checkbox" name="savepw" [(ngModel)]="s.savePassword" /> Save password in the OS keychain</label>
                  <div class="hint">You will be asked for the password on the next connect and can choose to remember it.</div>
                </div>
                <label>Forwarding</label>
                <div class="checks">
                  <label><input type="checkbox" name="agentfwd" [(ngModel)]="s.agentForward" /> Forward SSH agent</label>
                  <label><input type="checkbox" name="x11" [(ngModel)]="s.x11Forward" /> Forward X11 (needs a local X server: XQuartz / VcXsrv)</label>
                </div>
                <label>Keep-alive</label>
                <div class="row"><label><input type="checkbox" name="ka" [(ngModel)]="s.keepalive" /> Send keep-alive every</label>
                  <input name="kas" type="number" min="5" class="short" [(ngModel)]="s.keepaliveSec" /> seconds</div>
                <label>Algorithms</label>
                <div class="checks">
                  <label><input type="checkbox" name="legacyalgo" [(ngModel)]="s.legacyAlgorithms" /> Allow legacy algorithms (for old devices)</label>
                  <div class="hint">For a device that fails with <i>no common algorithm for key exchange</i> (for example one that only offers
                    <code>diffie-hellman-group1-sha1</code>, <code>aes128-cbc</code> or <code>hmac-sha1</code>). The weak algorithms are added at the
                    end of each list, so modern ones are still used whenever the device offers them. Use it only on networks you trust.</div>
                </div>
                <label>Key exchange</label>
                <input name="kexalgo" [(ngModel)]="s.kexAlgorithms" class="mono" spellcheck="false" placeholder="(default)  e.g. +diffie-hellman-group1-sha1" />
                <label>Ciphers</label>
                <input name="cipheralgo" [(ngModel)]="s.ciphers" class="mono" spellcheck="false" placeholder="(default)  e.g. +aes128-cbc,3des-cbc" />
                <label>MACs</label>
                <input name="macalgo" [(ngModel)]="s.macs" class="mono" spellcheck="false" placeholder="(default)  e.g. +hmac-sha1" />
                <label>Host key</label>
                <input name="hkalgo" [(ngModel)]="s.hostKeyAlgorithms" class="mono" spellcheck="false" placeholder="(default)  e.g. +ssh-rsa" />
                <label></label>
                <div class="hint">Optional lists in the OpenSSH way: <code>a,b</code> exactly these, <code>+a</code> add to the end, <code>^a</code> put first,
                  <code>-a*</code> remove. Leave empty for the defaults.</div>
              </div>
            }
            @case ('firewall') {
              <div class="grid">
                <label>Jump host</label>
                <select name="jump" [(ngModel)]="s.jumpSession">
                  <option value="">(none - connect directly)</option>
                  @for (p of jumpChoices(); track p) { <option [value]="p">{{ p }}</option> }
                </select>
                <label></label>
                <div class="hint">Connect through another saved SSH2 session (like ProxyJump). Jump hosts can be chained.</div>
                <label>Proxy</label>
                <select name="ptype" [(ngModel)]="s.proxyType" [disabled]="!!s.jumpSession">
                  <option value="">None</option><option value="socks5">SOCKS5</option><option value="http">HTTP CONNECT</option>
                </select>
                @if (s.proxyType && !s.jumpSession) {
                  <label>Proxy host</label>
                  <div class="row"><input name="phost" [(ngModel)]="s.proxyHost" /> Port <input name="pport" type="number" class="short" [(ngModel)]="s.proxyPort" /></div>
                  <label>Proxy user</label><input name="puser" [(ngModel)]="s.proxyUser" placeholder="(no authentication)" />
                }
              </div>
            }
            @case ('forward') {
              @if (s.protocol !== 'SSH2') {
                <p class="hint">Port forwarding (tunnels) works over <b>SSH2</b> only. This session uses {{ s.protocol }}.
                  Change the protocol on the Connection page to SSH2 to add local, remote or SOCKS forwards.
                  @if (s.portForwards.length) { The {{ s.portForwards.length }} forward(s) saved with this session are kept but not used. }</p>
              } @else {
              <p class="hint"><b>Local</b>: listen on this computer and forward to a host reachable from the server (ssh -L).
                <b>Remote</b>: listen on the server and forward back to a host reachable from here (ssh -R).
                <b>Dynamic</b>: local SOCKS5 proxy through the server (ssh -D).</p>
              <table class="list">
                <thead><tr><th>Name</th><th>Type</th><th>Listen address</th><th>Port</th><th>Destination host</th><th>Port</th><th></th></tr></thead>
                <tbody>
                  @for (f of s.portForwards; track $index) {
                    <tr>
                      <td><input [name]="'fn' + $index" [(ngModel)]="f.name" /></td>
                      <td><select [name]="'ft' + $index" [(ngModel)]="f.type">
                        <option value="local">Local</option><option value="remote">Remote</option><option value="dynamic">Dynamic</option></select></td>
                      <td><input [name]="'fa' + $index" [(ngModel)]="f.bindAddr" placeholder="127.0.0.1" /></td>
                      <td><input [name]="'fp' + $index" type="number" [(ngModel)]="f.bindPort" /></td>
                      <td><input [name]="'fh' + $index" [(ngModel)]="f.destHost" [disabled]="f.type === 'dynamic'" /></td>
                      <td><input [name]="'fd' + $index" type="number" [(ngModel)]="f.destPort" [disabled]="f.type === 'dynamic'" /></td>
                      <td><button type="button" class="icon-btn" (click)="s.portForwards.splice($index, 1)">✕</button></td>
                    </tr>
                  }
                </tbody>
              </table>
              <button type="button" (click)="s.portForwards.push({ type: 'local', bindAddr: '', bindPort: 0, destHost: '', destPort: 0, name: '' })">Add forward</button>
              }
            }
            @case ('serial') {
              <div class="grid">
                <label>Port</label>
                <app-serial-port-picker [(value)]="s.comPort" />
                <label>Baud rate</label>
                <div>
                  <div class="row">
                    <select name="baud" class="baud" [(ngModel)]="s.baudRate" [disabled]="!!s.serialAutoBaud">@for (b of bauds; track b) { <option [ngValue]="b">{{ b }}</option> }</select>
                    <button type="button" (click)="detectBaud()" [disabled]="!s.comPort || detecting()" title="Send Enter at each common speed and keep the one where the device answers with readable text">{{ detecting() ? 'Detecting…' : 'Detect Speed' }}</button>
                  </div>
                  <div class="checks" style="margin-top: 6px"><label><input type="checkbox" name="autobaud" [(ngModel)]="s.serialAutoBaud" /> Detect the speed each time it connects</label></div>
                  @if (baudMsg()) { <div class="hint">{{ baudMsg() }}</div> }
                </div>
                <label>Data bits</label>
                <select name="databits" [(ngModel)]="s.dataBits">@for (b of [5, 6, 7, 8]; track b) { <option [ngValue]="b">{{ b }}</option> }</select>
                <label>Parity</label>
                <select name="parity" [(ngModel)]="s.parity">
                  @for (p of parities; track $index) { <option [ngValue]="$index">{{ p }}</option> }
                </select>
                <label>Stop bits</label>
                <select name="stopbits" [(ngModel)]="s.stopBits">
                  <option [ngValue]="0">1</option><option [ngValue]="1">1.5</option><option [ngValue]="2">2</option>
                </select>
                <label>Modem lines</label>
                <div class="checks">
                  <label><input type="checkbox" name="dtr" [(ngModel)]="s.serialDtr" /> Assert DTR on open</label>
                  <label><input type="checkbox" name="rts" [(ngModel)]="s.serialRts" /> Assert RTS on open</label>
                  <div class="hint">Use Tab → Send Break for a serial break.</div>
                </div>
                <label>Flow control</label>
                <select name="sflow" [(ngModel)]="s.serialFlow">
                  <option value="">None</option><option value="rtscts">Hardware (RTS/CTS)</option><option value="xonxoff">Software (XON/XOFF)</option>
                </select>
              </div>
            }
            @case ('local') {
              <div class="grid">
                <label>Command</label><input name="lcmd" [(ngModel)]="s.localCommand" placeholder="(default shell)" />
                <label>Start directory</label><input name="ldir" [(ngModel)]="s.localDir" placeholder="(home directory)" />
              </div>
            }
            @case ('logon') {
              <div class="checks">
                <label><input type="checkbox" name="autolog" [(ngModel)]="s.automateLogon" /> Automate logon</label>
                <label><input type="checkbox" name="initcr" [(ngModel)]="s.initialCr" /> Send initial carriage return</label>
              </div>
              <table class="list logon" [class.dim]="!s.automateLogon">
                <thead><tr><th>Expect</th><th>Send</th><th class="c">Hide</th><th class="c">Send trailing CR</th><th>Credentials</th></tr></thead>
                <tbody>
                  @for (a of s.logonActions; track $index) {
                    <tr [class.sel]="logonSel === $index" (click)="logonSel = $index" (dblclick)="editLogon($index)">
                      <td>{{ a.expect || '(none)' }}</td>
                      <td>{{ a.hide ? '********' : (a.send || '(none)') }}</td>
                      <td class="c">{{ a.hide ? '✓' : '' }}</td>
                      <td class="c">{{ a.trailingCr ? '✓' : '' }}</td>
                      <td>{{ a.credential }}</td>
                    </tr>
                  } @empty {
                    <tr><td colspan="5" class="hint">No expect/send steps. Click Add… to wait for a prompt (e.g. "login:") and send a reply.</td></tr>
                  }
                </tbody>
              </table>
              <div class="row">
                <button type="button" (click)="editLogon(-1)" [disabled]="!s.automateLogon">Add…</button>
                <button type="button" (click)="editLogon(logonSel)" [disabled]="!s.automateLogon || logonSel < 0">Edit…</button>
                <button type="button" (click)="deleteLogon()" [disabled]="!s.automateLogon || logonSel < 0">Delete</button>
                <span class="spacer"></span>
                <button type="button" title="Move up" (click)="moveLogon(-1)" [disabled]="logonSel <= 0">▲</button>
                <button type="button" title="Move down" (click)="moveLogon(1)" [disabled]="logonSel < 0 || logonSel >= s.logonActions.length - 1">▼</button>
              </div>
              <h4>Logon script</h4>
              <div class="grid">
                <label><input type="checkbox" name="uselscript" [(ngModel)]="s.useLogonScript" /> Logon script</label>
                <div class="row"><input name="lscript" [(ngModel)]="s.logonScript" [disabled]="!s.useLogonScript" placeholder="Python (.py), VBScript (.vbs, Windows) or JavaScript (.js) file run after connecting" />
                  <button type="button" (click)="browseScript()" [disabled]="!s.useLogonScript">…</button></div>
                <label>Arguments</label>
                <input name="largs" [(ngModel)]="s.logonArgs" [disabled]="!s.useLogonScript" placeholder="available to the script as scriptArgs" />
              </div>
              @if (s.protocol === 'SSH2') {
                <h4>SSH2</h4>
                <div class="grid">
                  <label><input type="checkbox" name="userc" [(ngModel)]="s.useRemoteCommand" /> Remote command</label>
                  <input name="rcmd" [(ngModel)]="s.remoteCommand" [disabled]="!s.useRemoteCommand"
                    placeholder="e.g. ssh -t -i ~/.ssh/aws_key ec2-user@10.0.0.5" />
                  <label></label>
                  <div class="hint">Runs this command on the server instead of a login shell (a terminal is still allocated).
                    From a jump station, <code>ssh -t -i &lt;key on the jump host&gt; user&#64;target</code> logs you straight into the target
                    using the key stored on the jump station. The tab ends when the command exits.</div>
                  @if (s.useRemoteCommand && isHop(s.remoteCommand)) {
                    <label>SFTP opens on</label>
                    <div class="checks">
                      <label><input type="radio" name="sftpt" value="" [ngModel]="sftpOn(s)" (ngModelChange)="setSftpOn(s, $event)" /> Where the terminal is (the target; the jump station after <code>exit</code>)</label>
                      <label><input type="radio" name="sftpt" value="jump" [ngModel]="sftpOn(s)" (ngModelChange)="setSftpOn(s, $event)" /> Always {{ s.hostname || 'the jump station' }} (the host Termwright logs in to)</label>
                      <label><input type="radio" name="sftpt" value="target" [ngModel]="sftpOn(s)" (ngModelChange)="setSftpOn(s, $event)" /> Always {{ hopTarget(s.remoteCommand) }} (the command's target, through the jump station, using its key)</label>
                    </div>
                    <label></label>
                    <div class="hint">Applies to the SFTP button (Open SFTP Tab), Connect SFTP Session and "Connect as SFTP" (without a terminal,
                      "where the terminal is" opens on the jump station). The tab and Session Manager menus always offer both. SCP from the SFTP tab goes to the same host.</div>
                  }
                </div>
              }
              <h4>Triggers</h4>
              <p class="hint">Whenever the output contains <b>Pattern</b> (plain text or regular expression), send <b>Send</b>.
                Useful for paging prompts such as --More-- or confirmations. Limited to 20 per 10 seconds.</p>
              <table class="list">
                <thead><tr><th>Pattern</th><th>Regex</th><th>Send</th><th></th></tr></thead>
                <tbody>
                  @for (tr of s.triggers; track $index) {
                    <tr>
                      <td><input [name]="'tp' + $index" [(ngModel)]="tr.pattern" placeholder="--More--" /></td>
                      <td class="c"><input type="checkbox" [name]="'tre' + $index" [(ngModel)]="tr.regex" /></td>
                      <td><input [name]="'ts' + $index" [(ngModel)]="tr.send" placeholder="e.g. a space" /></td>
                      <td><button type="button" class="icon-btn" (click)="s.triggers.splice($index, 1)">✕</button></td>
                    </tr>
                  }
                </tbody>
              </table>
              <button type="button" (click)="s.triggers.push({ pattern: '', regex: false, send: '' })">Add trigger</button>
            }
            @case ('terminal') {
              <div class="grid">
                <label>Emulation</label>
                <select name="emu" [(ngModel)]="s.emulation">@for (e of emulations; track e) { <option [value]="e">{{ e }}</option> }</select>
                <label>TERM override</label><input name="termType" [(ngModel)]="s.termType" placeholder="(from emulation, e.g. xterm-256color)" />
                <label>Scrollback lines</label><input name="sb" type="number" min="100" class="short" [(ngModel)]="s.scrollback" />
                <label>Font</label>
                <div class="row"><input name="ffam" [(ngModel)]="s.fontFamily" placeholder="(global font)" />
                  Size <input name="fsz" type="number" min="0" max="48" class="short" [(ngModel)]="s.fontSize" /></div>
                <label>Backspace sends</label>
                <select name="bksp" [(ngModel)]="s.backspace"><option value="">Delete (^?)</option><option value="bs">Backspace (^H)</option></select>
                <label>Keyword highlighting</label>
                <select name="kws" [(ngModel)]="s.keywordSet">
                  <option value="">(global default)</option><option value="None">None</option>
                  @for (k of keywordSets(); track k) { <option [value]="k">{{ k }}</option> }
                </select>
                <label>Connection</label>
                <div class="checks"><label><input type="checkbox" name="arc" [(ngModel)]="s.autoReconnect" /> Auto reconnect when the connection drops</label></div>
                <label>Display</label>
                <div class="checks">
                  <label><input type="checkbox" name="tstamps" [(ngModel)]="s.timestamps" /> Show the local time at the start of each line (display only)</label>
                  <label><input type="checkbox" name="noscroll" [ngModel]="!s.noAutoScroll" (ngModelChange)="s.noAutoScroll = !$event" /> Auto scroll to new output</label>
                </div>
                <label>Color scheme</label>
                <select name="scheme" [(ngModel)]="s.colorScheme">
                  <option value="">(global default)</option>
                  @for (c of schemeChoices(); track c) { <option [value]="c">{{ c }}</option> }
                </select>
              </div>
            }
            @case ('log') {
              <div class="grid">
                <label>Log file name</label>
                <div class="row"><input name="logfile" [(ngModel)]="s.logFile" placeholder="(Logs folder)/%S-%Y%M%D-%h%m%s.log" />
                  <button type="button" (click)="browseLog()">Browse...</button></div>
                <label></label>
                <div class="hint">Substitutions: %H host, %S session, %P port, %U user, %Y %M %D date, %h %m %s time, %t ms, %% percent.</div>
                <label>Options</label>
                <div class="checks">
                  <label><input type="checkbox" name="loc" [(ngModel)]="s.logOnConnect" /> Start log upon connect</label>
                  <label><input type="checkbox" name="lap" [(ngModel)]="s.logAppend" /> Append to file</label>
                  <label><input type="checkbox" name="lraw" [(ngModel)]="s.logRaw" /> Raw log (keep escape sequences)</label>
                </div>
                <label>Recording</label>
                <div class="checks">
                  <label><input type="checkbox" name="lrec" [(ngModel)]="s.recordSession" /> Record every connection (replay it later with its timing: File → Recordings…)</label>
                </div>
                <label>Each line prefix</label><input name="lpre" [(ngModel)]="s.logEachLine" placeholder="e.g. [%h:%m:%s] " />
              </div>
            }
          }
        </div>
      </div>
      <div class="modal-foot">
        @if (error()) { <span class="err">{{ error() }}</span> }
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Cancel</button>
        <button type="button" (click)="save(true)">{{ s.protocol === 'WOL' ? 'Save &amp; Wake' : 'Save &amp; Connect' }}</button>
        <button type="submit" class="primary">Save</button>
      </div>
    </form>
    @if (teaching()) { <app-fact-teach [embedded]="true" (done)="teachDone()" /> }
    @if (logonEdit; as e) {
      <div class="modal-backdrop nested">
        <form class="modal small" (ngSubmit)="saveLogon()">
          <div class="modal-head">Expect/Send Properties</div>
          <div class="modal-body">
            <div class="grid">
              <label>Expect</label><input name="le-exp" [(ngModel)]="e.expect" placeholder="e.g. login: (empty = send immediately)" appAutofocus />
              <label>Send</label><input name="le-snd" [type]="e.hide ? 'password' : 'text'" [(ngModel)]="e.send" placeholder="e.g. admin or \\s" />
              <label></label>
              <div class="checks">
                <label><input type="checkbox" name="le-hide" [(ngModel)]="e.hide" /> Hide (do not show the Send text)</label>
                <label><input type="checkbox" name="le-cr" [(ngModel)]="e.trailingCr" /> Send trailing carriage return</label>
              </div>
              <label>Credentials</label>
              <select name="le-cred" [(ngModel)]="e.credential">
                <option value="">(optional - session username/password)</option>
                @for (c of credentialNames(); track c) { <option [value]="c">{{ c }}</option> }
              </select>
            </div>
            <table class="list help">
              <tbody>
                <tr><td>\\r</td><td>Carriage return</td><td>\\s</td><td>Username from credentials</td></tr>
                <tr><td>\\n</td><td>Line feed</td><td>\\w</td><td>Password from credentials</td></tr>
                <tr><td>\\b</td><td>Backspace</td><td>\\e</td><td>Escape</td></tr>
                <tr><td>\\v</td><td>Paste clipboard</td><td>\\t</td><td>Tab</td></tr>
                <tr><td>\\p</td><td>Pause 1 second</td><td>\\\\</td><td>Backslash</td></tr>
              </tbody>
            </table>
            <div class="hint">Credentials are managed in Options → Global Options → Credentials. Without one, \\s and \\w use this session's
              username and saved password.</div>
          </div>
          <div class="modal-foot">
            <span class="spacer"></span>
            <button type="button" (click)="logonEdit = null">Cancel</button>
            <button type="submit" class="primary">OK</button>
          </div>
        </form>
      </div>
    }
  </div>
  }
  `,
  styles: [`
    .acct-card { display: flex; gap: 10px; align-items: center; padding: 8px 12px; border: 1px solid var(--line, var(--border)); border-radius: 10px; background: var(--surface-2, var(--input-bg));
      .av { width: 28px; height: 28px; border-radius: 50%; flex: none; display: grid; place-items: center; font-weight: 700; font-size: calc(11px * var(--ui-scale, 1)); background: var(--accent-soft, #23314a); color: var(--accent); }
      .tx { flex: 1; display: flex; flex-direction: column; line-height: 1.4; small { color: var(--muted); } }
      button { white-space: nowrap; } }
    .facts { display: flex; flex-direction: column; gap: 10px; max-width: 680px;
      h4 { margin: 10px 0 0; } .spacer { flex: 1; } }
    .fnow { border: 1px solid var(--line, var(--border)); border-radius: var(--r-lg, 8px); padding: 6px 12px; }
    .fr { display: grid; grid-template-columns: 100px minmax(0, 1fr) minmax(0, 1fr) auto; gap: 10px; align-items: center; min-height: 34px; border-bottom: 1px solid var(--line, var(--border));
      .fk { color: var(--muted); } .fv { font-family: var(--mono); overflow-wrap: anywhere; }
      .fc { color: var(--faint); font-family: var(--mono); font-size: calc(11.5px * var(--ui-scale, 1)); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
      .fx { display: flex; gap: 2px; } .icon-btn { width: 26px; height: 26px; color: var(--muted); } }
    .fed { display: flex; flex-direction: column; gap: 8px; padding: 10px 0; border-bottom: 1px solid var(--line, var(--border));
      label { display: flex; flex-direction: column; gap: 4px; font-size: calc(12px * var(--ui-scale, 1)); color: var(--muted); min-width: 0; }
      input { min-height: 30px; } input.mono { font-family: var(--mono); }
      .seg button { white-space: nowrap; } .spacer { flex: 1; } }
    .pair2 { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
    .fa { padding: 6px 0 2px; }
    .row input.mono { flex: 1; font-family: var(--mono); }
    .fout { margin: 0; max-height: 260px; overflow: auto; padding: 9px 12px; border: 1px solid var(--line, var(--border)); border-radius: var(--r, 6px);
      background: var(--term-bg, var(--bg)); font: calc(12px * var(--ui-scale, 1))/1.55 var(--mono); white-space: pre; user-select: text; cursor: text;
      &::selection, & ::selection { background: var(--accent); color: #fff; } }
  `],
})
export class SessionEditor implements OnInit {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  private cdr = inject(ChangeDetectorRef);

  readonly protocols = PROTOCOLS;
  readonly emulations = EMULATIONS;
  readonly bauds = BAUD_RATES;
  /** Linux device types turn the Files panel on (it stays a choice). */
  onDeviceType() { if (this.s) filesPanelFor(this.s); }
  readonly envs = envList;
  readonly folderEnv = computed(() => {
    const e = this.ui.editing();
    const s = e?.session;
    if (!s) return '';
    return envInfo(envOf((s.folder ? s.folder + '/' : '') + 'x', '', this.ui.settings()?.folderDefaults))?.label ?? '';
  });
  readonly schemeChoices = computed(() => [...SCHEME_NAMES, ...(this.ui.settings()?.customSchemes ?? []).map(c => c.name)]);
  readonly parities = ['None', 'Odd', 'Even', 'Mark', 'Space'];
  readonly authAll = ['gssapi', 'publickey', 'keyboard-interactive', 'password'];
  readonly authLabel: Record<string, string> = { gssapi: 'gssapi (Kerberos)' };
  readonly page = signal<Page>('connection');
  readonly error = signal('');
  readonly keywordSets = computed(() => (this.ui.settings()?.keywordSets ?? []).map(k => k.name));
  readonly deviceTypes = computed(() => (this.ui.settings()?.deviceTypes ?? []).map(d => d.name));
  /** Light check for "ssh … host" (the backend parses it fully). */
  isHop(cmd: string) { return /^\s*(exec\s+)?\S*\bssh\s+\S/.test(cmd ?? ''); }
  /** Sends Wake-on-LAN with the values in the dialog (saved or not). */
  private autoNet = '';
  /** The IP address of a Wake-on-LAN session: its network is filled in (the mask can still be changed) unless it was typed. */
  wolIp(v: string) {
    const s = this.s;
    if (!s) return;
    s.hostname = v;
    const ip = (v || '').trim();
    if (!/^\d+\.\d+\.\d+\.\d+$/.test(ip) || (s.wolNetwork && s.wolNetwork !== this.autoNet)) return;
    void this.backend.wakeNetworkFor(ip).then(n => {
      if (n && (!s.wolNetwork || s.wolNetwork === this.autoNet)) { s.wolNetwork = n; this.autoNet = n; }
    }).catch(() => {});
  }

  wakeNow() { if (this.s) void this.tabs.wake(structuredClone(this.s), '', 'none'); }
  /** Who logs in (Account). */
  canLogin() { return !!this.s && !['Local Shell', 'VNC', 'WOL'].includes(this.s.protocol); }
  ownFields() {
    if (!this.s || this.s.protocol === 'WOL') return false;
    if (!this.canLogin()) return true;
    const c = accountChoice(this.s);
    return c === '-' || (c === '' && defaultLogin(this.ui.settings(), this.s).kind === 'own');
  }
  accounts() { return this.ui.settings()?.credentials ?? []; }
  acctChoice() { return this.s ? accountChoice(this.s) : ''; }
  setAcct(v: string) {
    if (!this.s) return;
    if (v === '+') { const keep = accountChoice(this.s); setAccountChoice(this.s, keep + ' '); setTimeout(() => setAccountChoice(this.s!, keep)); this.ui.open('passwords'); return; }
    setAccountChoice(this.s, v);
  }
  defText() { return this.s ? defaultText(defaultLogin(this.ui.settings(), this.s)) : ''; }
  acctUser(): { user: string; how: string; account?: string } | null {
    if (!this.s || !this.canLogin()) return null;
    const c = accountChoice(this.s);
    if (c && c !== '-') return { user: userOf(this.ui.settings(), c), how: 'with the account ' + c, account: c };
    if (c === '-') return null;
    const d = defaultLogin(this.ui.settings(), this.s);
    if (d.kind === 'account') return { user: d.user ?? '', how: `with the account ${d.name} (from the ${d.from})`, account: d.name };
    if (d.kind === 'device') return { user: d.user ?? '', how: `with the ${d.from}'s login` };
    return null;
  }
  initials(n: string) { return n.split(/[-_ .]+/).filter(Boolean).slice(0, 2).map(x => x[0].toUpperCase()).join('') || '?'; }

  /** SFTP opens on: '' where the terminal is, 'jump' or 'target' (older sessions: the target flag). */
  sftpOn(s: Session) { return s.sftpOn || (s.sftpToTarget ? 'target' : ''); }
  setSftpOn(s: Session, v: '' | 'jump' | 'target') { s.sftpOn = v; s.sftpToTarget = v === 'target'; }
  hopTarget(cmd: string) {
    const w = (cmd ?? '').trim().split(/\s+/).filter(x => x !== 'exec');
    for (let i = 1; i < w.length; i++) {
      if (!w[i].startsWith('-')) return w[i];
      const f = w[i].slice(1);
      const k = [...f].findIndex(c => 'BbcDEeFIiJLlmOoPpQRSWw'.includes(c));
      if (k >= 0 && k === f.length - 1) i++;
    }
    return 'the target';
  }

  /** Saved-password state of each device type: "set", "empty", "" (asked), looked up once per dialog. */
  readonly pwStates = signal<Record<string, string>>({});
  private pwAsked = new Set<string>();
  /** " (admin, no password)" etc. for the "Use the device type's username and password" box. */
  loginNote(name: string | undefined, user: string): string {
    if (!name) return '';
    const st = this.pwStates()[name];
    if (st === undefined && !this.pwAsked.has(name)) {
      this.pwAsked.add(name); // not a signal: no writes while the template renders
      this.backend.deviceTypePasswordState(name).then(v => this.pwStates.update(m => ({ ...m, [name]: v }))).catch(() => {});
    }
    const pw = st === 'set' ? 'password saved' : st === 'empty' ? 'no password' : st === '' ? 'password asked when connecting' : '';
    const parts = [user, pw].filter(Boolean);
    return parts.length ? ' (' + parts.join(', ') + ')' : '';
  }

  // ---- Device Facts page ----
  factCmd = '';
  readonly factOut = signal('');
  readonly factBusy = signal<'' | 'run' | 'refresh'>('');
  readonly factPick = signal<{ line: string; start: number; length: number; value: string } | null>(null);
  readonly teaching = signal(false);
  factRows() { return factList(this.ui.factsOf(this.oldPath)); }
  factAge() { return factsAge(this.ui.factsOf(this.oldPath)); }
  deviceRules() { return (this.ui.settings()?.deviceTypes ?? []).find(d => d.name === this.s?.deviceType)?.facts ?? []; }
  async refreshFacts() {
    this.factBusy.set('refresh');
    try { await this.ui.refreshFacts(this.oldPath, true); } finally { this.factBusy.set(''); }
  }
  async runFact() {
    if (!this.s || !this.factCmd.trim() || this.factBusy()) return;
    this.factBusy.set('run'); this.factPick.set(null);
    try { this.factOut.set((await this.backend.runFactCommand(this.oldPath, this.s.deviceType, this.factCmd)).replace(/\r/g, '') || '(no output)'); }
    catch (e) { this.ui.error(e); }
    finally { this.factBusy.set(''); }
  }
  /** The value selected in the output: its line and where it is on the line. */
  pickFact(ev: MouseEvent) {
    const pre = ev.currentTarget as HTMLElement;
    const sel = window.getSelection();
    if (!sel || sel.isCollapsed || !sel.rangeCount || !pre.contains(sel.anchorNode)) { this.factPick.set(null); return; }
    const r = sel.getRangeAt(0);
    const before = document.createRange();
    before.setStart(pre, 0); before.setEnd(r.startContainer, r.startOffset);
    const out = this.factOut();
    const at = before.toString().length;
    const value = sel.toString().split('\n')[0].replace(/\s+$/, '');
    if (!value.trim()) { this.factPick.set(null); return; }
    const ls = out.lastIndexOf('\n', at - 1) + 1;
    const le = out.indexOf('\n', at);
    this.factPick.set({ line: out.slice(ls, le < 0 ? undefined : le), start: at - ls, length: value.length, value: value.trim() });
  }
  teachFromOutput() {
    const p = this.factPick();
    if (!p || !this.s) return;
    this.ui.factTeach.set({ path: this.oldPath, deviceType: this.s.deviceType, line: p.line, start: p.start, length: p.length, command: this.factCmd.trim(), output: this.factOut() });
    this.teaching.set(true);
  }
  teachDone() { this.teaching.set(false); this.factPick.set(null); }
  /** One row per fact: the device type's rules, then values without a rule. */
  factTable(): { name: string; value: string; rule: FactRule | null }[] {
    const vals = this.ui.factsOf(this.oldPath) ?? {};
    const rules = this.deviceRules();
    const rows: { name: string; value: string; rule: FactRule | null }[] = rules.map(r => ({ name: r.name, value: vals[r.name] ?? '', rule: r }));
    for (const [k, v] of factList(vals)) if (!rules.some(r => r.name === k)) rows.push({ name: k, value: v, rule: null });
    const add = this.adding();
    if (add && !rows.some(r => r.name === add)) rows.push({ name: add, value: '', rule: null });
    return rows;
  }
  /** A fact being added by hand (shown as a row until saved or cancelled). */
  readonly adding = signal('');
  readonly factEdit = signal<{ orig: string; name: string; value: string; command: string; regex: string; every: boolean } | null>(null);
  testFact(re: string) { return testFactRegex(re, this.factOut()); }
  editFact(r: { name: string; value: string; rule: FactRule | null }) {
    this.factEdit.set({ orig: r.name, name: r.name, value: r.value, command: r.rule?.command ?? this.factCmd, regex: r.rule?.regex ?? '', every: r.rule?.every ?? FACT_EVERY.includes(r.name) });
  }
  async newFact() {
    if (!this.oldPath) return;
    const name = await this.ui.input('Add Device Fact', 'Name (e.g. Location, Boot ROM)');
    if (!name?.trim()) return;
    this.adding.set(name.trim());
    this.factEdit.set({ orig: name.trim(), name: name.trim(), value: '', command: this.factCmd, regex: '', every: false });
  }
  private async saveRules(update: (facts: FactRule[]) => FactRule[]) {
    const cur = this.ui.settings();
    if (!cur || !this.s?.deviceType) return;
    const types = structuredClone(cur.deviceTypes ?? []);
    const d = types.find(t => t.name === this.s!.deviceType);
    if (!d) return;
    d.facts = update(d.facts ?? []);
    await this.ui.saveSettings({ ...cur, deviceTypes: types });
  }
  async saveFactEdit() {
    const e = this.factEdit();
    if (!e || !this.oldPath) return;
    const name = e.name.trim();
    if (!name) { this.ui.toast('Give the fact a name.', 'info'); return; }
    if (e.regex && !this.reOk(e.regex)) { this.ui.toast('The pattern is not a valid regular expression.', 'error'); return; }
    try {
      if (this.s?.deviceType && e.command.trim() && e.regex) {
        const rule: FactRule = { name, command: e.command.trim(), regex: e.regex, every: e.every };
        await this.saveRules(f => { const i = f.findIndex(x => x.name === e.orig); const out = f.filter(x => x.name !== e.orig && x.name !== name); out.splice(i < 0 ? out.length : Math.min(i, out.length), 0, rule); return out; });
      } else if (this.s?.deviceType && e.orig !== name) {
        await this.saveRules(f => f.map(x => x.name === e.orig ? { ...x, name } : x));
      }
      const v: Record<string, string> = { [name]: e.value.trim() };
      if (e.orig !== name) v[e.orig] = '';
      const f = await this.backend.setSessionFacts(this.oldPath, v);
      if (f) this.ui.setFactsLocal(this.oldPath, f);
      this.factEdit.set(null); this.adding.set('');
    } catch (err) { this.ui.error(err); }
  }
  cancelFactEdit() { this.factEdit.set(null); this.adding.set(''); }
  async removeFact(r: { name: string; rule: FactRule | null }) {
    if (!this.oldPath) return;
    const dt = this.s?.deviceType;
    const msg = r.rule && dt
      ? `Stop reading ${r.name} for every ${dt} session, and remove the value saved for this session?`
      : `Remove ${r.name} from this session?`;
    if (!(await this.ui.confirm('Remove Device Fact', msg, 'Remove', true))) return;
    try {
      if (r.rule && dt) await this.saveRules(f => f.filter(x => x.name !== r.name));
      const f = await this.backend.setSessionFacts(this.oldPath, { [r.name]: '' });
      if (f) this.ui.setFactsLocal(this.oldPath, f);
    } catch (err) { this.ui.error(err); }
  }
  reOk(re: string) { try { new RegExp(re, 'm'); return true; } catch { return false; } }

  /** Under "Use the VOSS login": "admin, password saved". */
  loginSub() {
    const n = this.loginNote(this.s?.deviceType, this.deviceUser()).trim();
    return n ? n.slice(1, -1) : 'the device type\'s username and password';
  }
  deviceUser() { return (this.ui.settings()?.deviceTypes ?? []).find(d => d.name === this.s?.deviceType)?.username ?? ''; }
  pw = '';
  noPw = false;
  readonly isRef = isRef;
  readonly refOf = refOf;
  async pickRef() {
    const r = await askRef(this.ui, this.backend, isRef(this.pw) ? this.pw : refOf(this.pwState()));
    if (r) { this.pw = r; this.noPw = false; this.cdr.markForCheck(); }
  }
  readonly pwState = signal('');
  /** Saved SSH2 sessions that can be used as a jump host. */
  readonly jumpChoices = computed(() => {
    const out: string[] = [];
    const walk = (n: SessionNode) => (n.children ?? []).forEach(c => {
      if (c.isFolder) walk(c);
      else if (c.protocol === 'SSH2' && c.path !== this.oldPath) out.push(c.path);
    });
    const root = this.ui.tree();
    if (root) walk(root);
    return out;
  });
  s: Session | null = null;
  oldPath = '';
  logonSel = -1;
  logonEdit: (LogonAction & { idx: number }) | null = null;
  readonly credentialNames = computed(() => (this.ui.settings()?.credentials ?? []).map(c => c.name));
  private tabId?: string;

  ngOnInit() {
    const e = this.ui.editing();
    if (!e) return;
    this.s = structuredClone(e.session);
    this.s.description ??= [];
    this.s.logonActions ??= [];
    this.s.triggers ??= [];
    this.s.portForwards ??= [];
    if (this.s.smartCard) void this.loadCards();
    this.oldPath = e.oldPath;
    if (e.oldPath) {
      this.backend.sessionPasswordState(e.oldPath).then(st => { this.pwState.set(st); this.noPw = st === 'empty'; this.cdr.markForCheck(); }).catch(() => {});
    }
    this.tabId = e.tabId;
    const pg = this.ui.editorPage();
    if (pg) { this.page.set(pg as Page); this.ui.editorPage.set(''); }
    if (e.pw) { this.pw = e.pw; }
  }

  remote() { return this.s?.protocol === 'RDP' || this.s?.protocol === 'VNC'; }
  /** Sessions without a terminal (remote desktops, Wake-on-LAN). */
  noTerm() { return this.remote() || this.s?.protocol === 'WOL'; }
  readonly modern = () => this.ui.settings()?.layout !== 'classic';
  readonly mainProtos = ['SSH2', 'Telnet', 'Serial', 'RDP', 'VNC'];
  readonly otherProtos = PROTOCOLS.filter(p => !['SSH2', 'Telnet', 'Serial', 'RDP', 'VNC'].includes(p));
  devKind() { return this.s?.protocol === 'WOL' ? null : deviceKindOf(this.s?.deviceType); }
  protoKind(): SessionKind {
    const m: Record<string, SessionKind> = { Serial: 'serial', Console: 'console', 'Local Shell': 'shell', Telnet: 'telnet', Raw: 'raw', RDP: 'rdp', VNC: 'vnc', WOL: 'wol' };
    return m[this.s?.protocol ?? ''] ?? (this.s?.openAs === 'sftp' ? 'sftp' : 'ssh');
  }
  /** Under the title (modern layout): folder, protocol and where it connects. */
  subtitle() {
    const s = this.s;
    if (!s) return '';
    const where = s.protocol === 'Serial' ? [s.comPort, s.baudRate].filter(Boolean).join(' ') : s.protocol === 'Local Shell' ? '' : (s.username ? s.username + '@' : '') + (s.hostname || '');
    return [s.folder ? s.folder.split('/').join(' / ') : (this.oldPath ? 'Top level' : 'New session'), s.protocol, where].filter(Boolean).join(' · ');
  }
  pageName() {
    const p = this.page();
    const m: Record<string, string> = { connection: 'Connection', auth: 'Authentication', firewall: 'Jump Host / Proxy', forward: 'Port Forwarding', serial: 'Serial', local: 'Local Shell', logon: 'Logon Actions & Triggers', terminal: 'Terminal', log: 'Log File', facts: 'Device Facts', remote: this.s?.protocol === 'RDP' ? 'Remote Desktop' : 'VNC' };
    return m[p] ?? '';
  }
  readonly consoleMethods = CONSOLE_METHODS;
  /** SSH2 and Telnet sessions that can be console servers. */
  readonly consoleServers = computed(() => {
    const out: string[] = [];
    const walk = (n: SessionNode) => (n.children ?? []).forEach(c => {
      if (c.isFolder) walk(c);
      else if ((c.protocol === 'SSH2' || c.protocol === 'Telnet') && c.path !== this.oldPath) out.push(c.path);
    });
    const r = this.ui.tree();
    if (r) walk(r);
    return out;
  });
  readonly cardKeys = signal<SmartCardKey[]>([]);
  readonly cardNote = signal('');
  private cardsLoaded = false;
  async loadCards(again = false) {
    if (this.cardsLoaded && !again) return;
    this.cardsLoaded = true;
    try {
      const r = await this.backend.smartCards([]);
      if (!r.available) { this.cardNote.set(' Smart cards are not supported by this build.'); return; }
      const reps = r.reports ?? [];
      this.cardKeys.set(reps.flatMap(x => x.keys));
      const errs = reps.filter(x => x.error).map(x => x.error);
      this.cardNote.set(!reps.length ? ' No PKCS#11 library found: add it in Global Options → Smart Cards.' :
        !this.cardKeys().length ? ' No card with a key is inserted.' + (errs.length ? ' ' + errs.join('; ') : '') : '');
    } catch (e) { this.cardNote.set(' ' + errText(e)); }
  }
  hasCardKey(f: string) { return this.cardKeys().some(k => k.fingerprint === f); }

  consoleHint() {
    const s = this.s;
    if (!s?.consoleServer) return '';
    const cs = this.ui.findNode(s.consoleServer);
    const n = s.consolePort || 0, nn = String(n).padStart(2, '0');
    const host = cs?.hostname ?? '?';
    const user = cs?.username ?? 'user';
    switch (s.consoleMethod) {
      case 'opengear-ssh': return `Connects with: ssh ${user}:port${nn}@${host}.`;
      case 'avocent-ssh': return `Connects with: ssh ${user}:ttyS${n}@${host}.`;
      case 'ssh-3000': return `Connects with: ssh -p ${3000 + n} ${user}@${host}.`;
      case 'telnet-2000': return `Connects with: telnet ${host} ${2000 + n}.`;
      case 'telnet-7000': return `Connects with: telnet ${host} ${7000 + n}.`;
      default: return '';
    }
  }

  protocolChanged() {
    if (!this.s) return;
    if (this.remote() && ['logon', 'terminal', 'log', 'forward', 'auth', 'serial', 'local'].includes(this.page())) this.page.set('connection');
    this.s.port = defaultPort(this.s.protocol) || this.s.port;
    if (this.s.protocol === 'WOL') { this.s.deviceType = ''; if (this.page() !== 'firewall') this.page.set('connection'); if (!this.s.wolNetwork && /^\d+\.\d+\.\d+\.\d+$/.test(this.s.hostname)) this.s.wolNetwork = this.s.hostname.replace(/\.\d+$/, '.0/24'); }
    if (this.s.protocol === 'Console' && !this.s.consoleMethod) { this.s.consoleMethod = 'opengear-ssh'; this.s.initialCr = true; }
  }

  readonly detecting = signal(false);
  readonly baudMsg = signal('');

  /** Tries the common speeds on the port and takes the one the device answers at. */
  async detectBaud() {
    if (!this.s?.comPort) return;
    this.detecting.set(true); this.baudMsg.set('');
    try {
      const r = await this.backend.detectSerialBaud(this.s);
      if (r.baud) { this.s.baudRate = r.baud; this.baudMsg.set(`The device answers at ${r.baud} baud.`); }
      else this.baudMsg.set('No readable answer at ' + r.tried.map(t => t.baud).join(', ') + ' baud. Check the cable and that the port is not open in a tab, then try again.');
    } catch (e) { this.baudMsg.set(errText(e)); }
    finally { this.detecting.set(false); this.cdr.markForCheck(); }
  }

  hasAuth(m: string) { return (this.s?.authMethods ?? '').split(',').includes(m); }

  toggleAuth(m: string) {
    if (!this.s) return;
    const cur = this.s.authMethods.split(',').filter(Boolean);
    const next = cur.includes(m) ? cur.filter(x => x !== m) : [...cur, m];
    this.s.authMethods = this.authAll.filter(a => next.includes(a)).join(',');
  }

  async browseCert() {
    const p = await this.backend.chooseOpenFile('SSH Certificate', [{ displayName: 'Certificates', pattern: '*-cert.pub;*.pub;*' }]).catch(() => '');
    if (p && this.s) { this.s.certificateFile = p; this.cdr.markForCheck(); }
  }

  async browseIdentity() {
    const p = await this.backend.chooseOpenFile('Select Private Key').catch(() => '');
    if (p && this.s) this.s.identityFile = p;
    this.cdr.markForCheck();
  }

  async browseScript() {
    const p = await this.backend.chooseOpenFile('Logon Script', [{ displayName: 'Scripts (Python, VBScript, JavaScript)', pattern: '*.py;*.vbs;*.js' }]).catch(() => '');
    if (p && this.s) this.s.logonScript = p;
    this.cdr.markForCheck();
  }

  editLogon(idx: number) {
    if (!this.s) return;
    const a = idx >= 0 ? this.s.logonActions[idx] : null;
    this.logonEdit = { expect: a?.expect ?? '', send: a?.send ?? '', hide: a?.hide ?? false,
      trailingCr: a ? a.trailingCr : true, credential: a?.credential ?? '', idx };
  }

  saveLogon() {
    const e = this.logonEdit;
    if (!e || !this.s) return;
    const { idx, ...a } = e;
    if (idx >= 0) this.s.logonActions[idx] = a;
    else { this.s.logonActions.push(a); this.logonSel = this.s.logonActions.length - 1; }
    this.logonEdit = null;
  }

  deleteLogon() {
    if (!this.s || this.logonSel < 0) return;
    this.s.logonActions.splice(this.logonSel, 1);
    this.logonSel = Math.min(this.logonSel, this.s.logonActions.length - 1);
  }

  moveLogon(d: number) {
    const l = this.s?.logonActions;
    const i = this.logonSel, j = i + d;
    if (!l || i < 0 || j < 0 || j >= l.length) return;
    [l[i], l[j]] = [l[j], l[i]];
    this.logonSel = j;
  }

  async browseLog() {
    const p = await this.backend.chooseSaveFile('Log File', (this.s?.name || 'session') + '.log').catch(() => '');
    if (p && this.s) this.s.logFile = p;
    this.cdr.markForCheck();
  }

  async save(connect: boolean) {
    const s = this.s;
    if (!s) return;
    this.error.set('');
    if (!s.name.trim()) { this.page.set('connection'); this.error.set('Please enter a session name.'); return; }
    if (['SSH2', 'Telnet', 'Raw'].includes(s.protocol) && !s.hostname.trim()) {
      this.page.set('connection'); this.error.set('Please enter a hostname.'); return;
    }
    if (s.protocol === 'Serial' && !s.comPort.trim()) { this.page.set('serial'); this.error.set('Please choose a serial port.'); return; }
    if (s.protocol === 'WOL' && !s.mac?.trim()) { this.page.set('connection'); this.error.set('Please enter the MAC address of the device to wake.'); return; }
    if (s.protocol === 'WOL' && !s.hostname?.trim() && !s.wolNetwork?.trim()) { this.page.set('connection'); this.error.set('Please enter the network (e.g. 192.168.1.0) or the IP address of the device - the magic packet goes to that network.'); return; }
    if (s.protocol === 'Console') {
      s.consolePort = Number(s.consolePort) || 0;
      if (!s.consoleServer || s.consolePort < 1) { this.page.set('connection'); this.error.set('Choose the console server and the port number.'); return; }
      if (s.consoleServer === this.oldPath) { this.page.set('connection'); this.error.set('A session cannot be its own console server.'); return; }
    }
    s.port = Number(s.port) || 0;
    s.logonActions = s.logonActions.filter(a => a.expect || a.send);
    s.triggers = s.triggers.filter(t => t.pattern);
    s.portForwards = s.portForwards.filter(f => f.bindPort > 0).map(f => ({ ...f, bindPort: Number(f.bindPort), destPort: Number(f.destPort) || 0 }));
    s.proxyPort = Number(s.proxyPort) || 0;
    s.fontSize = Number(s.fontSize) || 0;
    if (s.jumpSession && s.jumpSession === this.oldPath) { this.page.set('firewall'); this.error.set('A session cannot be its own jump host.'); return; }
    try {
      const saved = await storeSession(this.ui, this.backend, this.tabs, s, this.oldPath, this.tabId,
        { pw: this.pw, noPw: this.noPw, wasEmpty: this.pwState() === 'empty' });
      this.ui.close();
      if (connect) await this.tabs.openSession(saved.path);
    } catch (e) {
      this.error.set(e instanceof Error ? e.message : String(e));
    }
  }
}
