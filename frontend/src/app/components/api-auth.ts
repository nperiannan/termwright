import { Component, inject, input, output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HReqAuth } from '../core/models';
import { Ui } from '../core/ui.service';
import { VarInput } from './var-input';

export const AUTH_TYPES: { v: string; l: string; d: string }[] = [
  { v: '', l: 'Inherit auth from parent', d: 'Uses the authorization of its collection, else the API session\'s.' },
  { v: 'none', l: 'No Auth', d: 'Sends no authorization (a login page, a public health check).' },
  { v: 'bearer', l: 'Bearer Token', d: 'Sends Authorization: Bearer <token>.' },
  { v: 'basic', l: 'Basic Auth', d: 'Sends Authorization: Basic with a user name and password.' },
  { v: 'apikey', l: 'API Key', d: 'Sends a key and value in a header or the query.' },
  { v: 'digest', l: 'Digest Auth', d: 'Answers the server\'s 401 challenge with an MD5 or SHA-256 digest of the user name and password.' },
  { v: 'oauth2', l: 'OAuth 2.0', d: 'Gets an access token (authorization code with PKCE or implicit, signed in in your browser; password or client credentials), sends it as a Bearer token and gets a new one when it expires or is refused.' },
  { v: 'awsv4', l: 'AWS Signature', d: 'Signs the request with AWS Signature Version 4 (access key, secret key, region and service).' },
  { v: 'oauth1', l: 'OAuth 1.0', d: 'Signs the request with OAuth 1.0 (HMAC-SHA1, HMAC-SHA256, HMAC-SHA512 or PLAINTEXT), in the Authorization header, the query or the body.' },
  { v: 'hawk', l: 'Hawk Authentication', d: 'Sends a Hawk Authorization header: a MAC of the request made with the Hawk ID and key (optionally with the body\'s hash).' },
  { v: 'ntlm', l: 'NTLM Authentication', d: 'Signs in to Windows servers (IIS, Exchange, SharePoint) with NTLMv2: user name, password and domain.' },
];

/** The Authorization editor of a request or a collection (Postman's Auth type list and its fields). */
@Component({
  selector: 'app-api-auth',
  imports: [FormsModule, VarInput],
  template: `
    <div class="au">
      <div class="lft">
        <div class="lb">Auth type</div>
        <button type="button" class="pick" (click)="typeMenu($event)">{{ typeLabel() }}<span class="cv">▾</span></button>
        <p class="mut">{{ typeDesc() }}</p>
        <p class="mut">The authorization header is made when the request is sent.</p>
      </div>
      <div class="rgt">
        @switch (auth()) {
          @case ('') { <div class="inh"><b>{{ forCollection() ? 'The API session\\'s authentication' : 'Inherited' }}</b><div class="mut">{{ inherit() }}</div></div> }
          @case ('none') { <div class="mut">This {{ forCollection() ? 'collection\\'s requests send' : 'request sends' }} no authorization.</div> }
          @case ('bearer') {
            <label><span>Token</span><app-var-input class="vin" [value]="d().token ?? ''" (valueChange)="set({ token: $event })" [known]="known()" placeholder="{{ '{{token}}' }}" label="Token" /></label>
          }
          @case ('basic') {
            <label><span>Username</span><app-var-input class="vin" [value]="d().user ?? ''" (valueChange)="set({ user: $event })" [known]="known()" label="Username" /></label>
            <label><span>Password</span><app-var-input class="vin" [value]="d().password ?? ''" (valueChange)="set({ password: $event })" [known]="known()" placeholder="{{ '{{password}}' }}" label="Password" /></label>
          }
          @case ('apikey') {
            <label><span>Key</span><app-var-input class="vin" [value]="d().key ?? ''" (valueChange)="set({ key: $event })" [known]="known()" placeholder="X-API-Key" label="Key" /></label>
            <label><span>Value</span><app-var-input class="vin" [value]="d().value ?? ''" (valueChange)="set({ value: $event })" [known]="known()" placeholder="{{ '{{apiKey}}' }}" label="Value" /></label>
            <label><span>Add to</span><select class="fs" [ngModel]="d().in || 'header'" (ngModelChange)="set({ in: $event })"><option value="header">Header</option><option value="query">Query Params</option></select></label>
          }
          @case ('digest') {
            <label><span>Username</span><app-var-input class="vin" [value]="d().user ?? ''" (valueChange)="set({ user: $event })" [known]="known()" label="Username" /></label>
            <label><span>Password</span><app-var-input class="vin" [value]="d().password ?? ''" (valueChange)="set({ password: $event })" [known]="known()" placeholder="{{ '{{password}}' }}" label="Password" /></label>
          }
          @case ('oauth2') {
            <label><span>Grant type</span><select [ngModel]="d().grant || 'client_credentials'" (ngModelChange)="set({ grant: $event })"><option value="authorization_code">Authorization Code</option><option value="implicit">Implicit</option><option value="password">Password Credentials</option><option value="client_credentials">Client Credentials</option></select></label>
            @if (browserGrant()) {
              <label><span>Auth URL</span><app-var-input class="vin" [value]="d().authUrl ?? ''" (valueChange)="set({ authUrl: $event })" [known]="known()" placeholder="https://login.example.com/oauth/authorize" label="Auth URL" /></label>
              <label><span>Callback URL</span><app-var-input class="vin" [value]="d().redirectUri ?? ''" (valueChange)="set({ redirectUri: $event })" [known]="known()" placeholder="default: http://127.0.0.1:<free port>/callback" label="Callback URL" /></label>
              @if (d().grant === 'authorization_code') {
                <label><span>PKCE</span><select [ngModel]="d().pkce || ''" (ngModelChange)="set({ pkce: $event || undefined })"><option value="">Off</option><option value="S256">SHA-256 (S256)</option><option value="plain">Plain</option></select></label>
              }
              <label><span>State</span><app-var-input class="vin" [value]="d().state ?? ''" (valueChange)="set({ state: $event })" [known]="known()" placeholder="default: a random one" label="State" /></label>
              <p class="mut note">The sign-in opens in your browser; Termwright listens on the callback URL for the answer. Register the callback URL with the provider: a fixed one such as <code>http://localhost:8765/callback</code>, or <code>http://127.0.0.1/callback</code> if it accepts any port.</p>
            }
            <label><span>Access token URL</span><app-var-input class="vin" [value]="d().tokenUrl ?? ''" (valueChange)="set({ tokenUrl: $event })" [known]="known()" placeholder="https://login.example.com/oauth/token" label="Access token URL" /></label>
            <label><span>Client ID</span><app-var-input class="vin" [value]="d().clientId ?? ''" (valueChange)="set({ clientId: $event })" [known]="known()" label="Client ID" /></label>
            <label><span>Client secret</span><app-var-input class="vin" [value]="d().clientSecret ?? ''" (valueChange)="set({ clientSecret: $event })" [known]="known()" placeholder="{{ '{{clientSecret}}' }}" label="Client secret" /></label>
            @if (d().grant === 'password') {
              <label><span>Username</span><app-var-input class="vin" [value]="d().user ?? ''" (valueChange)="set({ user: $event })" [known]="known()" label="Username" /></label>
              <label><span>Password</span><app-var-input class="vin" [value]="d().password ?? ''" (valueChange)="set({ password: $event })" [known]="known()" placeholder="{{ '{{password}}' }}" label="Password" /></label>
            }
            <label><span>Scope</span><app-var-input class="vin" [value]="d().scope ?? ''" (valueChange)="set({ scope: $event })" [known]="known()" placeholder="read write" label="Scope" /></label>
            <label><span>Client auth</span><select [ngModel]="d().clientAuth || 'header'" (ngModelChange)="set({ clientAuth: $event === 'header' ? undefined : $event })"><option value="header">Send as Basic Auth header</option><option value="body">Send client credentials in body</option></select></label>
            <label><span>Access token</span><app-var-input class="vin" [value]="d().token ?? ''" (valueChange)="set({ token: $event })" [known]="known()" placeholder="optional: {{ '{{accessToken}}' }} skips the token URL" label="Access token" /></label>
          }
          @case ('oauth1') {
            <label><span>Signature method</span><select [ngModel]="d().signatureMethod || 'HMAC-SHA1'" (ngModelChange)="set({ signatureMethod: $event })"><option>HMAC-SHA1</option><option>HMAC-SHA256</option><option>HMAC-SHA512</option><option>PLAINTEXT</option></select></label>
            <label><span>Consumer key</span><app-var-input class="vin" [value]="d().consumerKey ?? ''" (valueChange)="set({ consumerKey: $event })" [known]="known()" label="Consumer key" /></label>
            <label><span>Consumer secret</span><app-var-input class="vin" [value]="d().consumerSecret ?? ''" (valueChange)="set({ consumerSecret: $event })" [known]="known()" placeholder="{{ '{{consumerSecret}}' }}" label="Consumer secret" /></label>
            <label><span>Access token</span><app-var-input class="vin" [value]="d().token ?? ''" (valueChange)="set({ token: $event })" [known]="known()" label="Access token" /></label>
            <label><span>Token secret</span><app-var-input class="vin" [value]="d().tokenSecret ?? ''" (valueChange)="set({ tokenSecret: $event })" [known]="known()" placeholder="{{ '{{tokenSecret}}' }}" label="Token secret" /></label>
            <label><span>Realm</span><app-var-input class="vin" [value]="d().realm ?? ''" (valueChange)="set({ realm: $event })" [known]="known()" placeholder="optional" label="Realm" /></label>
            <label><span>Add to</span><select [ngModel]="d().in || 'header'" (ngModelChange)="set({ in: $event === 'header' ? undefined : $event })"><option value="header">Request Headers</option><option value="query">Query Params</option><option value="body">Request Body (x-www-form-urlencoded)</option></select></label>
          }
          @case ('hawk') {
            <label><span>Hawk Auth ID</span><app-var-input class="vin" [value]="d().hawkId ?? ''" (valueChange)="set({ hawkId: $event })" [known]="known()" label="Hawk Auth ID" /></label>
            <label><span>Hawk Auth Key</span><app-var-input class="vin" [value]="d().hawkKey ?? ''" (valueChange)="set({ hawkKey: $event })" [known]="known()" placeholder="{{ '{{hawkKey}}' }}" label="Hawk Auth Key" /></label>
            <label><span>Algorithm</span><select [ngModel]="d().algorithm || 'sha256'" (ngModelChange)="set({ algorithm: $event })"><option value="sha256">SHA256</option><option value="sha1">SHA1</option></select></label>
            <label><span>ext</span><app-var-input class="vin" [value]="d().ext ?? ''" (valueChange)="set({ ext: $event })" [known]="known()" placeholder="optional" label="ext" /></label>
            <label><span>app</span><app-var-input class="vin" [value]="d().app ?? ''" (valueChange)="set({ app: $event })" [known]="known()" placeholder="optional" label="app" /></label>
            <label><span>dlg</span><app-var-input class="vin" [value]="d().dlg ?? ''" (valueChange)="set({ dlg: $event })" [known]="known()" placeholder="optional" label="dlg" /></label>
            <label><span>Payload hash</span><span><input type="checkbox" [ngModel]="!!d().payload" (ngModelChange)="set({ payload: $event || undefined })" /> Include the body's hash</span></label>
          }
          @case ('ntlm') {
            <label><span>Username</span><app-var-input class="vin" [value]="d().user ?? ''" (valueChange)="set({ user: $event })" [known]="known()" placeholder="user, DOMAIN\\user or user@domain" label="Username" /></label>
            <label><span>Password</span><app-var-input class="vin" [value]="d().password ?? ''" (valueChange)="set({ password: $event })" [known]="known()" placeholder="{{ '{{password}}' }}" label="Password" /></label>
            <label><span>Domain</span><app-var-input class="vin" [value]="d().domain ?? ''" (valueChange)="set({ domain: $event })" [known]="known()" placeholder="optional" label="Domain" /></label>
            <label><span>Workstation</span><app-var-input class="vin" [value]="d().workstation ?? ''" (valueChange)="set({ workstation: $event })" [known]="known()" placeholder="optional" label="Workstation" /></label>
          }
          @case ('awsv4') {
            <label><span>Access key</span><app-var-input class="vin" [value]="d().accessKey ?? ''" (valueChange)="set({ accessKey: $event })" [known]="known()" placeholder="{{ '{{awsAccessKey}}' }}" label="Access key" /></label>
            <label><span>Secret key</span><app-var-input class="vin" [value]="d().secretKey ?? ''" (valueChange)="set({ secretKey: $event })" [known]="known()" placeholder="{{ '{{awsSecretKey}}' }}" label="Secret key" /></label>
            <label><span>Region</span><app-var-input class="vin" [value]="d().region ?? ''" (valueChange)="set({ region: $event })" [known]="known()" placeholder="us-east-1" label="Region" /></label>
            <label><span>Service name</span><app-var-input class="vin" [value]="d().service ?? ''" (valueChange)="set({ service: $event })" [known]="known()" placeholder="execute-api" label="Service name" /></label>
            <label><span>Session token</span><app-var-input class="vin" [value]="d().sessionToken ?? ''" (valueChange)="set({ sessionToken: $event })" [known]="known()" placeholder="optional, for temporary credentials" label="Session token" /></label>
          }
        }
        @if (typed()) { <div class="warn">A typed {{ typedWhat() }} is used to send but is not saved in the collection. Put it in a variable instead, e.g. <code>{{ '{{Bearer}}' }}</code>, as a secret one in an environment (Environments) or in the API session, where it is kept in the computer's credential store.</div> }
      </div>
    </div>`,
  styles: [`
    .au { display: grid; grid-template-columns: 260px 1fr; gap: 18px; padding: 12px 14px; }
    .lft { border-right: 1px solid var(--line); padding-right: 14px; } .lb { color: var(--muted); font-size: 12px; margin-bottom: 4px; }
    .pick { width: 100%; height: 30px; display: flex; align-items: center; justify-content: space-between; } .cv { color: var(--muted); font-size: 10px; }
    .mut { color: var(--muted); font-size: 12px; line-height: 1.5; margin: 8px 0 0; }
    .rgt { display: flex; flex-direction: column; gap: 10px; }
    label { display: grid; grid-template-columns: 110px minmax(0, 440px); align-items: center; gap: 10px; } label span { color: var(--muted); font-size: 12px; }
    .vin { height: 30px; } select { height: 30px; }
    .inh b { font-weight: 600; } .inh .mut { margin-top: 4px; }
    .note { margin: 0 0 0 120px; max-width: 440px; } .note code { font-size: 11px; }
    .warn { font-size: 12px; line-height: 1.5; color: var(--warn); border: 1px solid color-mix(in srgb, var(--warn) 45%, transparent); border-radius: 6px; padding: 7px 9px; max-width: 560px; }
  `],
})
export class ApiAuth {
  readonly auth = input('');
  readonly data = input<HReqAuth | undefined>(undefined);
  readonly inherit = input('');
  readonly forCollection = input(false);
  readonly known = input<Set<string>>(new Set());
  readonly changed = output<{ auth: string; authData?: HReqAuth }>();
  private readonly ui = inject(Ui);

  d(): HReqAuth { return this.data() ?? {}; }
  types() { return this.forCollection() ? AUTH_TYPES.map(t => t.v === '' ? { ...t, l: 'The API session\'s', d: 'Requests that inherit use the API session\'s authentication (its secrets are in the credential store).' } : t) : AUTH_TYPES; }
  typeLabel() { return this.types().find(t => t.v === this.auth())?.l ?? this.auth(); }
  typeDesc() { return this.types().find(t => t.v === this.auth())?.d ?? ''; }
  typeMenu(ev: MouseEvent) {
    this.ui.showMenu(ev, this.types().map(t => ({ label: (t.v === this.auth() ? '✓ ' : '') + t.l, icon: '', action: () => this.changed.emit({ auth: t.v, authData: t.v === '' || t.v === 'none' ? undefined : { ...this.d() } }) })));
  }
  set(p: Partial<HReqAuth>) { this.changed.emit({ auth: this.auth(), authData: { ...this.d(), ...p } }); }
  typed() {
    const d = this.d(), lit = (v?: string) => !!v?.trim() && !v.includes('{{');
    return (this.auth() === 'bearer' && lit(d.token)) || ((this.auth() === 'basic' || this.auth() === 'digest') && lit(d.password)) || (this.auth() === 'apikey' && lit(d.value))
      || (this.auth() === 'oauth2' && (lit(d.clientSecret) || lit(d.token) || (d.grant === 'password' && lit(d.password)))) || (this.auth() === 'awsv4' && (lit(d.secretKey) || lit(d.sessionToken)))
      || (this.auth() === 'oauth1' && (lit(d.consumerSecret) || lit(d.tokenSecret))) || (this.auth() === 'hawk' && lit(d.hawkKey)) || (this.auth() === 'ntlm' && lit(d.password));
  }
  browserGrant() { return this.d().grant === 'authorization_code' || this.d().grant === 'implicit'; }
  typedWhat() {
    const a = this.auth(), d = this.d(), lit = (v?: string) => !!v?.trim() && !v.includes('{{');
    if (a === 'basic' || a === 'digest' || a === 'ntlm') return 'password';
    if (a === 'oauth1') return lit(d.consumerSecret) ? 'consumer secret' : 'token secret';
    if (a === 'hawk') return 'Hawk key';
    if (a === 'apikey') return 'key value';
    if (a === 'oauth2') return lit(d.clientSecret) ? 'client secret' : lit(d.token) ? 'access token' : 'password';
    if (a === 'awsv4') return lit(d.secretKey) ? 'secret key' : 'session token';
    return 'token';
  }
}
