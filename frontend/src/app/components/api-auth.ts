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
        }
        @if (typed()) { <div class="warn">A typed {{ auth() === 'basic' ? 'password' : auth() === 'apikey' ? 'key value' : 'token' }} is used to send but is not saved in the collection. Put it in a variable instead, e.g. <code>{{ '{{Bearer}}' }}</code>, as a secret one in an environment (Environments) or in the API session, where it is kept in the computer's credential store.</div> }
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
    return (this.auth() === 'bearer' && lit(d.token)) || (this.auth() === 'basic' && lit(d.password)) || (this.auth() === 'apikey' && lit(d.value));
  }
}
