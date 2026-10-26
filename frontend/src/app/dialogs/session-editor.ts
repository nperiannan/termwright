import { ChangeDetectorRef, Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Autofocus } from '../core/autofocus';
import { askRef, isRef, refOf } from '../core/secret-ref';
import { Backend } from '../core/backend';
import { BAUD_RATES, EMULATIONS, LogonAction, PROTOCOLS, Session, SessionNode, defaultPort } from '../core/models';
import { SCHEME_NAMES } from '../core/themes';
import { Tabs } from '../core/tabs.service';
import { ENVIRONMENTS, envInfo, envOf } from '../core/env';
import { Ui } from '../core/ui.service';

type Page = 'connection' | 'auth' | 'firewall' | 'forward' | 'serial' | 'local' | 'logon' | 'terminal' | 'log';

/** Session Options dialog (create / edit a saved session). */
@Component({
  selector: 'app-session-editor',
  imports: [FormsModule, Autofocus],
  template: `
  @if (s) {
  <div class="modal-backdrop">
    <form class="modal wide" (ngSubmit)="save(false)" autocomplete="off">
      <div class="modal-head">{{ oldPath ? 'Session Options - ' + oldPath : 'New Session' }}</div>
      <div class="modal-split">
        <nav class="cat">
          <a [class.on]="page() === 'connection'" (click)="page.set('connection')">Connection</a>
          @if (s.protocol === 'SSH2') {
            <a class="sub" [class.on]="page() === 'auth'" (click)="page.set('auth')">SSH2 Authentication</a>
          }
          <a class="sub" [class.on]="page() === 'forward'" [class.dim]="s.protocol !== 'SSH2'" (click)="page.set('forward')">Port Forwarding</a>
          @if (s.protocol === 'SSH2' || s.protocol === 'Telnet' || s.protocol === 'Raw') {
            <a class="sub" [class.on]="page() === 'firewall'" (click)="page.set('firewall')">Jump Host / Proxy</a>
          }
          @if (s.protocol === 'Serial') { <a class="sub" [class.on]="page() === 'serial'" (click)="page.set('serial')">Serial</a> }
          @if (s.protocol === 'Local Shell') { <a class="sub" [class.on]="page() === 'local'" (click)="page.set('local')">Local Shell</a> }
          <a class="sub" [class.on]="page() === 'logon'" (click)="page.set('logon')">Logon Actions &amp; Triggers</a>
          <a [class.on]="page() === 'terminal'" (click)="page.set('terminal')">Terminal</a>
          <a class="sub" [class.on]="page() === 'log'" (click)="page.set('log')">Log File</a>
        </nav>
        <div class="modal-body">
          @switch (page()) {
            @case ('connection') {
              <div class="grid">
                <label>Name</label><input name="name" [(ngModel)]="s.name" required placeholder="e.g. core-switch-01" />
                <label>Folder</label><input name="folder" [(ngModel)]="s.folder" placeholder="e.g. Datacenter/Rack 4 (blank = top level)" />
                <label>Protocol</label>
                <select name="protocol" [(ngModel)]="s.protocol" (ngModelChange)="protocolChanged()">
                  @for (p of protocols; track p) { <option [value]="p">{{ p }}</option> }
                </select>
                <label>Environment</label>
                <div class="row">
                  <select name="env" [(ngModel)]="s.environment" class="envsel">
                    <option value="">(none{{ folderEnv() ? ' - folder: ' + folderEnv() : '' }})</option>
                    @for (v of envs; track v.id) { <option [value]="v.id">{{ v.label }}</option> }
                  </select>
                  <span class="hint">Colours the tab; production asks before a paste or a send to several tabs.</span>
                </div>
                <label>Device type</label>
                <select name="devtype" [(ngModel)]="s.deviceType">
                  <option value="">(none)</option>
                  @for (d of deviceTypes(); track d) { <option [value]="d">{{ d }}</option> }
                </select>
                @if (s.deviceType) {
                  <label></label>
                  <div class="hint">Login and after-login steps come from the device type (Global Options → Device Types) and run before
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
                @if (s.protocol === 'SSH2' || s.protocol === 'Telnet' || s.protocol === 'Raw') {
                  <label>Hostname</label><input name="hostname" [(ngModel)]="s.hostname" required placeholder="host name or IP address" />
                  <label>Port</label><input name="port" type="number" min="1" max="65535" [(ngModel)]="s.port" class="short" />
                }
                @if (s.deviceType) {
                  <label>Login</label>
                  <div class="checks">
                    <label><input type="checkbox" name="inherit" [ngModel]="!s.ownLogin" (ngModelChange)="s.ownLogin = !$event" />
                      Use the device type's username and password{{ loginNote(s.deviceType, deviceUser()) }}</label>
                  </div>
                }
                @if (!s.deviceType || s.ownLogin) {
                  <label>Username</label><input name="username" [(ngModel)]="s.username" placeholder="(prompt when connecting)" />
                  <label>Password</label>
                  <div class="row">
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
                <div class="row">
                  <input name="comPort" [(ngModel)]="s.comPort" list="serialPorts" placeholder="COM1, /dev/ttyUSB0, /dev/cu.usbserial..." />
                  <datalist id="serialPorts">@for (p of ports(); track p) { <option [value]="p"></option> }</datalist>
                  <button type="button" (click)="loadPorts()">Scan</button>
                </div>
                <label>Baud rate</label>
                <select name="baud" [(ngModel)]="s.baudRate">@for (b of bauds; track b) { <option [ngValue]="b">{{ b }}</option> }</select>
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
                      <label><input type="radio" name="sftpt" [value]="false" [ngModel]="!!s.sftpToTarget" (ngModelChange)="s.sftpToTarget = $event" /> {{ s.hostname || 'the jump station' }} (the host Termwright logs in to)</label>
                      <label><input type="radio" name="sftpt" [value]="true" [ngModel]="!!s.sftpToTarget" (ngModelChange)="s.sftpToTarget = $event" /> {{ hopTarget(s.remoteCommand) }} (the command's target, through the jump station, using its key)</label>
                    </div>
                    <label></label>
                    <div class="hint">Applies to the SFTP toolbar button, Connect SFTP Session and "Connect as SFTP". The tab and Session Manager
                      menus always offer both. SCP from the SFTP tab goes to the same host.</div>
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
        <button type="button" (click)="save(true)">Save &amp; Connect</button>
        <button type="submit" class="primary">Save</button>
      </div>
    </form>
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
})
export class SessionEditor implements OnInit {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  private cdr = inject(ChangeDetectorRef);

  readonly protocols = PROTOCOLS;
  readonly emulations = EMULATIONS;
  readonly bauds = BAUD_RATES;
  readonly envs = ENVIRONMENTS;
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
  readonly ports = signal<string[]>([]);
  readonly error = signal('');
  readonly keywordSets = computed(() => (this.ui.settings()?.keywordSets ?? []).map(k => k.name));
  readonly deviceTypes = computed(() => (this.ui.settings()?.deviceTypes ?? []).map(d => d.name));
  /** Light check for "ssh … host" (the backend parses it fully). */
  isHop(cmd: string) { return /^\s*(exec\s+)?\S*\bssh\s+\S/.test(cmd ?? ''); }
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
  /** " (admin, no password)" etc. for the "Use the device type's username and password" box. */
  loginNote(name: string | undefined, user: string): string {
    if (!name) return '';
    const st = this.pwStates()[name];
    if (st === undefined) {
      this.pwStates.update(m => ({ ...m, [name]: '?' }));
      this.backend.deviceTypePasswordState(name).then(v => this.pwStates.update(m => ({ ...m, [name]: v }))).catch(() => {});
    }
    const pw = st === 'set' ? 'password saved' : st === 'empty' ? 'no password' : st === '' ? 'password asked when connecting' : '';
    const parts = [user, pw].filter(Boolean);
    return parts.length ? ' (' + parts.join(', ') + ')' : '';
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
    this.oldPath = e.oldPath;
    if (e.oldPath) {
      this.backend.sessionPasswordState(e.oldPath).then(st => { this.pwState.set(st); this.noPw = st === 'empty'; this.cdr.markForCheck(); }).catch(() => {});
    }
    this.tabId = e.tabId;
  }

  protocolChanged() {
    if (!this.s) return;
    this.s.port = defaultPort(this.s.protocol) || this.s.port;
    if (this.s.protocol === 'Serial') void this.loadPorts();
  }

  async loadPorts() {
    try { this.ports.set(await this.backend.listSerialPorts()); } catch (e) { this.ui.error(e); }
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
    s.port = Number(s.port) || 0;
    s.logonActions = s.logonActions.filter(a => a.expect || a.send);
    s.triggers = s.triggers.filter(t => t.pattern);
    s.portForwards = s.portForwards.filter(f => f.bindPort > 0).map(f => ({ ...f, bindPort: Number(f.bindPort), destPort: Number(f.destPort) || 0 }));
    s.proxyPort = Number(s.proxyPort) || 0;
    s.fontSize = Number(s.fontSize) || 0;
    if (s.jumpSession && s.jumpSession === this.oldPath) { this.page.set('firewall'); this.error.set('A session cannot be its own jump host.'); return; }
    try {
      const setPw = (!s.deviceType || s.ownLogin) && (this.pw !== '' || this.noPw !== (this.pwState() === 'empty'));
      if (setPw) s.savePassword = true;
      const saved = await this.backend.saveSession(s, this.oldPath);
      if (setPw) await this.backend.setSessionPassword(saved.path, this.noPw ? '' : this.pw, this.noPw);
      await this.ui.refreshTree();
      this.ui.selectedPath.set(saved.path);
      this.ui.selectedIsFolder.set(false);
      for (const t of this.tabs.tabs()) {
        if (t.session.path === this.oldPath || t.id === this.tabId) {
          await this.backend.refreshTabSession(t.id, saved.path).catch(() => {});
          this.tabs.patch(t.id, { session: saved, title: saved.name });
        }
      }
      this.ui.close();
      if (connect) await this.tabs.openSession(saved.path);
    } catch (e) {
      this.error.set(e instanceof Error ? e.message : String(e));
    }
  }
}
