import { Component, OnDestroy, OnInit, computed, effect, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { APIMonitorState, HMockInfo, HCollection, HEnv, HHistoryItem, HHostCookies, HKV, HRequest, HResponse, HRunResult, HRunStep, HSendRequest, HSettings, HVar, Session, SessionNode } from '../core/models';
import { TabView } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { SessionSelect } from './session-select';
import { Combo, ComboOption } from './combo';
import { HTTP_HEADERS, headerValues } from '../core/http-headers';
import { CodeArea } from './code-area';
import { VarInput } from './var-input';
import { Recorder } from '../core/recorder.service';
import { ApiAuth, AUTH_TYPES } from './api-auth';

const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'];
const MODES = [{ v: '', l: 'None' }, { v: 'json', l: 'JSON' }, { v: 'text', l: 'Text' }, { v: 'xml', l: 'XML' }, { v: 'urlencoded', l: 'Form URL-encoded' }, { v: 'form', l: 'Multipart form' }, { v: 'graphql', l: 'GraphQL' }];
const LANGS = [{ v: 'curl', l: 'cURL' }, { v: 'python', l: 'Python (requests)' }, { v: 'javascript', l: 'JavaScript (fetch)' }, { v: 'powershell', l: 'PowerShell' }];
const AUTH_NAMES: Record<string, string> = { '': 'none', apikey: 'API key', bearer: 'bearer token', login: 'login', basic: 'basic', oauth2: 'OAuth 2.0' };
const RAW_LANGS = [{ v: 'json', l: 'JSON' }, { v: 'text', l: 'Text' }, { v: 'javascript', l: 'JavaScript' }, { v: 'html', l: 'HTML' }, { v: 'xml', l: 'XML' }];
const PRE_SNIPPETS: { l: string; code: string }[] = [
  { l: 'Set a variable', code: `pm.variables.set("id", "10");` },
  { l: 'Set an environment variable', code: `pm.environment.set("name", "value");` },
  { l: 'Set a collection variable', code: `pm.collectionVariables.set("name", "value");` },
  { l: 'Count up a collection variable', code: `const n = parseInt(pm.collectionVariables.get("counter") || "0") + 1;\npm.collectionVariables.set("counter", n);` },
  { l: 'Get a token with another request', code: `pm.sendRequest({\n  url: pm.variables.replaceIn("{{baseUrl}}/auth/token"),\n  method: "POST",\n  header: [{ key: "Content-Type", value: "application/json" }],\n  body: { mode: "raw", raw: JSON.stringify({ username: pm.variables.get("user"), password: pm.variables.get("password") }) },\n}, (err, res) => {\n  if (!err) pm.variables.set("token", res.json().token);\n});` },
  { l: 'Skip this request', code: `if (!pm.variables.get("id")) pm.execution.skipRequest();` },
  { l: 'Log to the console', code: `console.log(pm.info.requestName, pm.variables.get("id"));` },
];
const SNIPPETS: { l: string; code: string }[] = [
  { l: 'Status is 200', code: `pm.test("Status is 200", () => pm.response.to.have.status(200));` },
  { l: 'Answer under 500 ms', code: `pm.test("Answer under 500 ms", () => pm.expect(pm.response.responseTime).to.be.below(500));` },
  { l: 'Body has a value', code: `pm.test("Body has a value", () => {\n  const j = pm.response.json();\n  pm.expect(j).to.have.property("name");\n});` },
  { l: 'Keep a value for the next requests', code: `pm.variables.set("id", pm.response.json().id);` },
  { l: 'Header is present', code: `pm.test("JSON answer", () => pm.response.to.have.header("Content-Type"));` },
  { l: 'Keep a value in the environment', code: `pm.environment.set("objectId", pm.response.json().id);` },
  { l: 'Run another request next (collection run)', code: `postman.setNextRequest("Request name");` },
  { l: 'Repeat this request up to N times', code: `const n = parseInt(pm.collectionVariables.get("counter") || "1");\nif (n < 40) {\n  pm.collectionVariables.set("counter", n + 1);\n  postman.setNextRequest(pm.info.requestName);\n} else {\n  pm.collectionVariables.set("counter", 1);\n  postman.setNextRequest(null);\n}` },
  { l: 'Stop the run', code: `postman.setNextRequest(null);` },
];

interface Draft { req: HRequest; coll: string; id: string; dirty: boolean; key: number; res?: HResponse | null; }
interface ConsoleEntry { at: string; method: string; url: string; status: number; ms: number; error?: string; logs: string[]; reqHeaders: HKV[]; reqBody?: string; resHeaders: HKV[]; open?: boolean; }
let draftKey = 0;
function mkDraft(req: HRequest, coll = '', id = '', dirty = false): Draft { return { req, coll, id, dirty, key: ++draftKey }; }
interface Row { kind: 'folder' | 'req'; depth: number; folder?: string; name?: string; open?: boolean; req?: HRequest; coll: string; }

/** The API client: requests with an API session's base URL, login and variables; collections, history, tests, runner, import, code. */
@Component({
  selector: 'app-api-client',
  imports: [FormsModule, SessionSelect, Combo, CodeArea, VarInput, ApiAuth],
  template: `
  <div class="ac" [class.withvars]="varsOpen()">
    <div class="side">
      <div class="card colls">
        <div class="ch"><b>Collections</b><span class="sp"></span>
          <a class="lk" (click)="importOpen.set(true)" title="Postman collection, OpenAPI / Swagger document or cURL">Import</a>
          <a class="lk" (click)="newCollection()">+ New</a></div>
        <input class="q" [(ngModel)]="filter" placeholder="Filter requests" spellcheck="false" />
        <div class="tl">
          @for (c of colls(); track c.id) {
            <div class="cn" (click)="toggle('c:' + c.id)" (contextmenu)="collMenu($event, c)">
              <span class="tw">{{ isOpen('c:' + c.id) ? '▾' : '▸' }}</span>
              @if (renaming() === 'c:' + c.id) { <input class="ren" [(ngModel)]="renameText" (keydown.enter)="renameColl(c)" (blur)="renameColl(c)" (click)="$event.stopPropagation()" /> }
              @else { <span class="nm" translate="no">{{ c.name }}</span> }
              <span class="ct">{{ c.requests.length }}</span>
              <a class="more" (click)="collMenu($event, c); $event.stopPropagation()" title="More">⋯</a>
            </div>
            @if (isOpen('c:' + c.id)) {
              @for (r of rows(c); track $index) {
                @if (r.kind === 'folder') {
                  <div class="fn" [style.padding-left.px]="18 + r.depth * 12" (click)="toggle('f:' + c.id + ':' + r.folder)" translate="no"><span class="tw">{{ r.open ? '▾' : '▸' }}</span>{{ r.name }}</div>
                } @else {
                  <div class="rn" [class.on]="draft().coll === c.id && draft().id === r.req!.id" [style.padding-left.px]="18 + r.depth * 12" (click)="openReq(c, r.req!)" (contextmenu)="reqMenu($event, c, r.req!)">
                    <span class="m" [class]="'m ' + r.req!.method">{{ r.req!.method === 'DELETE' ? 'DEL' : r.req!.method }}</span>
                    @if (renaming() === 'r:' + c.id + ':' + r.req!.id) { <input class="ren" [(ngModel)]="renameText" (keydown.enter)="renameReq(c, r.req!)" (blur)="renameReq(c, r.req!)" (click)="$event.stopPropagation()" /> }
                    @else { <span class="nm" translate="no">{{ r.req!.name || r.req!.url }}</span> }
                  </div>
                }
              }
            }
          } @empty { <div class="mut pad">No collections yet. Import a Postman collection or an OpenAPI document, or send a request and Save it.</div> }
        </div>
      </div>
      @if (monitors().length || mocks().length) {
        <div class="card mons">
          <div class="ch"><b>Mocks &amp; Monitors</b><span class="sp"></span><a class="lk" (click)="loadExtras()">Refresh</a></div>
          <div class="tl">
            @for (m of mocks(); track m.collection) {
              <div class="rn" [title]="'Answers with the saved examples of ' + m.name"><span class="m MOCK">MOCK</span><span class="nm">{{ m.name }}<span class="mut"> :{{ m.port }} · {{ m.hits }} answered</span></span><a class="more" (click)="mockMenu($event, m); $event.stopPropagation()">⋯</a></div>
            }
            @for (m of monitors(); track m.id) {
              <div class="rn" (contextmenu)="monMenu($event, m)" [title]="m.error || (m.lastRun ? 'Last run ' + m.lastRun.slice(11, 16) + ': ' + m.passed + ' passed, ' + m.failed + ' failed' : 'Not run yet')">
                <span class="mdot" [class.ok]="m.lastRun && !m.failed && !m.error" [class.bad]="m.failed || m.error" [class.off]="!m.enabled"></span>
                <span class="nm">{{ m.name }} <span class="mut">every {{ m.every }} min</span></span>
                <span class="hist">@for (h of (m.history ?? []).slice(-12); track $index) { <i [class.bad]="!h"></i> }</span>
                <a class="more" (click)="monMenu($event, m); $event.stopPropagation()">⋯</a></div>
            }
          </div>
        </div>
      }
      <div class="card hist">
        <div class="ch"><b>History</b><span class="sp"></span>@if (history().length) { <a class="lk" (click)="clearHistory()">Clear</a> }</div>
        <div class="tl">
          @for (h of history().slice(0, 60); track $index) {
            <div class="rn" (click)="openHist(h)" [title]="h.url"><span [class]="'m ' + h.request.method">{{ h.request.method === 'DELETE' ? 'DEL' : h.request.method }}</span>
              <span class="nm" translate="no">{{ shortUrl(h.url || h.request.url) }}</span><span class="st" [class.bad]="!h.status || h.status >= 400">{{ h.status || '✕' }}</span></div>
          } @empty { <div class="mut pad">Requests you send appear here.</div> }
        </div>
      </div>
    </div>

    <div class="main">
      <div class="rtabs" role="tablist">
        @for (t of openTabs(); track t.key; let i = $index) {
          @let d = i === active() ? draft() : t;
          <div class="rt" role="tab" [class.on]="i === active()" [attr.aria-selected]="i === active()" (click)="switchTab(i)" (auxclick)="$event.button === 1 && closeTab(i, $event)" [title]="d.req.url">
            <span [class]="'m ' + d.req.method">{{ d.req.method === 'DELETE' ? 'DEL' : d.req.method }}</span>
            <span class="nm" translate="no">{{ d.req.name || shortUrl(d.req.url) || 'Untitled Request' }}</span>
            @if (d.dirty) { <span class="dot" title="Not saved"></span> }
            <a class="cx" (click)="closeTab(i, $event)" title="Close">✕</a>
          </div>
        }
        <a class="nt" (click)="newTab()" title="New request">+</a>
        <span class="sp"></span>
        <button type="button" class="envb" (click)="envMenu($event)" [class.on]="!!envId()" title="Environment: its variables win over the session's">
          <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true"><path d="M2 4h12M2 8h12M2 12h7" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round"/></svg>
          <span translate="no">{{ envName() || 'No Environment' }}</span><span class="cv">▾</span></button>
        <button type="button" class="ib" [class.on]="varsOpen()" (click)="varsOpen.set(!varsOpen()); varsOpen() && refreshVars()" title="Variables in this request and where their values come from">{{ '{ }' }}</button>
      </div>
      <div class="crumb">
        @if (draft().coll) { <span class="cc" translate="no">{{ collName(draft().coll) }}{{ draft().req.folder ? ' / ' + draft().req.folder : '' }} /</span> }
        <input class="rn0" [ngModel]="draft().req.name" (ngModelChange)="setReq({ name: $event })" placeholder="Untitled Request" spellcheck="false" translate="no" aria-label="Request name" />
        <span class="sp"></span>
        <app-session-select class="sess" [value]="path()" (changed)="choose($event)" [filter]="apiFilter" [allowNone]="true" noneLabel="No session (send the URL as it is)"
          placeholder="No session" title="Choose an API session" hint="Its base URL, login and variables are used." emptyText="No API session yet: API → New API Session." />
        <select class="fs env" [ngModel]="env()" (ngModelChange)="env.set($event); loadToken(); refreshVars()" title="Which values of the session's variables">
          <option value="">Values: {{ sessEnvName() }}</option><option value="production">Values: Production</option></select>
        <div class="split-btn"><button type="button" (click)="save()" [title]="draft().coll ? 'Save to the collection' : 'Save to a collection'">Save{{ draft().dirty ? ' •' : '' }}</button><button type="button" class="dd" (click)="saveMenu($event)" title="More">▾</button></div>
      </div>
      <div class="bar">
        <app-combo class="meth" [class]="'meth m-' + draft().req.method" [value]="draft().req.method" (valueChange)="setReq({ method: $event.toUpperCase().trim() || 'GET' })" [options]="methodOpts" placeholder="GET" emptyText="Type a new method" name="method" label="Method" />
        <app-var-input class="url" [value]="draft().req.url" (valueChange)="setUrl($event)" (enter)="send()" [known]="known()" placeholder="Enter URL, a path after the session's base URL, or paste a cURL command" label="URL" />
        <div class="split-btn send"><button type="button" class="pri" (click)="send()" [disabled]="busy() || !draft().req.url.trim()">{{ busy() ? 'Sending…' : 'Send' }}</button><button type="button" class="pri dd" (click)="sendMenu($event)" [disabled]="busy() || !draft().req.url.trim()" title="Send and Download">▾</button></div>
      </div>
      <div class="chips">
        @if (sess(); as s) {
          <span class="chip s" translate="no">Session: {{ s.name }} · {{ s.apiBaseUrl }}</span>
          @if (s.apiAuth) { <span class="chip ok">🔒 Session auth: {{ authName(s.apiAuth) }}{{ token()?.held ? ' · token held, renewed by itself' : '' }}</span> }
          @if (s.apiInsecure) { <span class="chip warn">TLS: any certificate</span> } @else if (s.apiCaFile) { <span class="chip">TLS: own CA trusted</span> }
          @if (prod()) { <span class="chip bad">Production: changes (POST, PUT, PATCH, DELETE) ask first</span> }
          <button type="button" class="setb" (click)="editSession()" title="Base URL, authentication, variables and TLS of this API session">⚙ Session Settings</button>
        } @else {
          <span class="chip">No session: the URL is sent as it is. Choose an API session for its base URL and login, or an environment for {{ '{{variables}}' }}.</span>
        }
        @if (unresolved().length) { <span class="chip bad" translate="no">No value: {{ unresolved().join(', ') }}</span> }
      </div>
      <div class="split">
        <div class="card req">
          <div class="tabs">
            @for (t of reqTabs; track t.k) { <a [class.on]="reqTab() === t.k" (click)="reqTab.set(t.k); t.k === 'code' && loadCode()">{{ t.l }}@if (count(t.k); as n) { <i>{{ n }}</i> }@if (dot(t.k)) { <b class="gd"></b> }</a> }
            <span class="sp"></span><a class="lk" (click)="openCookies()">Cookies</a>
          </div>
          <div class="body">
            @switch (reqTab()) {
              @case ('docs') {
                <div class="docs"><div class="sh"><b>Documentation</b><span class="mut">what this request does, its parameters, examples (kept with the request; exported to Postman)</span></div>
                  <app-code-area class="ce" [value]="draft().req.description ?? ''" (valueChange)="setReq({ description: $event })" placeholder="Describe the request…" [minHeight]="140" />
                  <div class="sh"><b>Examples</b><span class="mut">saved answers - documentation, and what the collection's mock server answers (Response ⋯ → Save as Example)</span></div>
                  @for (e of draft().req.examples ?? []; track $index) {
                    <div class="exr"><span class="st" [class.bad]="e.status >= 400">{{ e.status }}</span><b>{{ e.name }}</b><span class="mut ell">{{ (e.body ?? '').slice(0, 90) }}</span><span class="sp"></span><a class="lk" (click)="delExample($index)">Remove</a></div>
                  } @empty { <div class="mut">No examples yet.</div> }
                  <div class="sh"><b>Response schema</b><span class="mut">a JSON Schema the answer's body must fit - checked after every send and in runs (Tests tab)</span></div>
                  <app-code-area class="ce" [value]="draft().req.schema ?? ''" (valueChange)="setReq({ schema: $event })" placeholder='{"type": "object", "required": ["id"], "properties": {"id": {"type": "integer"}}}' [minHeight]="90" /></div>
              }
              @case ('params') {
                <div class="sh"><b>Query Params</b><span class="sp"></span><a class="lk" (click)="toggleBulk('params')">{{ bulk() === 'params' ? 'Key-Value Edit' : 'Bulk Edit' }}</a></div>
                @if (bulk() === 'params') { <textarea class="code" [(ngModel)]="bulkText" (ngModelChange)="bulkApply('params')" placeholder="key:value, one per line; // at the start turns it off" spellcheck="false"></textarea> }
                @else {
                  <table class="kv"><thead><tr><th></th><th>Key</th><th>Value</th><th>Description</th><th></th></tr></thead><tbody>
                    @for (p of draft().req.params ?? []; track $index) {
                      <tr><td><input type="checkbox" [ngModel]="!p.off" (ngModelChange)="p.off = !$event; urlFromParams()" /></td>
                        <td><input [(ngModel)]="p.key" (ngModelChange)="urlFromParams()" spellcheck="false" placeholder="Key" /></td><td><input [(ngModel)]="p.value" (ngModelChange)="urlFromParams()" spellcheck="false" placeholder="Value" /></td>
                        <td><input [(ngModel)]="p.desc" (ngModelChange)="touch()" placeholder="Description" /></td><td><a class="x" (click)="del(draft().req.params!, $index); urlFromParams()">✕</a></td></tr>
                    }
                  </tbody></table>
                  <button type="button" class="add" (click)="add('params')">+ Add Parameter</button>
                }
                @if (draft().req.pathVars?.length) {
                  <div class="sh"><b>Path Variables</b><span class="mut">the :name parts of the URL</span></div>
                  <table class="kv"><thead><tr><th></th><th>Key</th><th>Value</th><th>Description</th><th></th></tr></thead><tbody>
                    @for (p of draft().req.pathVars!; track p.key) {
                      <tr><td></td><td class="ro" translate="no">{{ p.key }}</td><td><input [(ngModel)]="p.value" (ngModelChange)="touch()" spellcheck="false" placeholder="Value" /></td>
                        <td><input [(ngModel)]="p.desc" (ngModelChange)="touch()" placeholder="Description" /></td><td></td></tr>
                    }
                  </tbody></table>
                }
              }
              @case ('auth') {
                <app-api-auth [auth]="draft().req.auth ?? ''" [data]="draft().req.authData" [inherit]="inheritText()" [known]="known()" (changed)="setReq({ auth: $event.auth, authData: $event.authData })" />
              }
              @case ('headers') {
                <div class="sh"><b>Headers</b>
                  <a class="hid" (click)="showHidden.set(!showHidden())" [title]="showHidden() ? 'Hide the headers Termwright adds' : 'Show the headers Termwright adds'">
                    <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"><path d="M1.5 8s2.4-4.5 6.5-4.5S14.5 8 14.5 8 12.1 12.5 8 12.5 1.5 8 1.5 8z" fill="none" stroke="currentColor" stroke-width="1.3"/><circle cx="8" cy="8" r="2" fill="currentColor"/>@if (!showHidden()) { <path d="M2.5 13.5l11-11" stroke="currentColor" stroke-width="1.3"/> }</svg>
                    {{ showHidden() ? 'Hide auto-generated headers' : autoHeaders().length + ' hidden' }}</a>
                  <span class="sp"></span><a class="lk" (click)="toggleBulk('headers')">{{ bulk() === 'headers' ? 'Key-Value Edit' : 'Bulk Edit' }}</a><a class="lk" (click)="presetMenu($event)">Presets ▾</a></div>
                @if (bulk() === 'headers') { <textarea class="code" [(ngModel)]="bulkText" (ngModelChange)="bulkApply('headers')" placeholder="Header-Name: value, one per line; // at the start turns it off" spellcheck="false"></textarea> }
                @else {
                  <table class="kv"><thead><tr><th></th><th>Key</th><th>Value</th><th>Description</th><th></th></tr></thead><tbody>
                    @if (showHidden()) { @for (h of autoHeaders(); track h.key) { <tr class="auto"><td></td><td>{{ h.key }}</td><td>{{ h.value }}</td><td>{{ h.desc }}</td><td></td></tr> } }
                    @for (p of draft().req.headers ?? []; track $index) {
                      <tr><td><input type="checkbox" [ngModel]="!p.off" (ngModelChange)="p.off = !$event; touch()" /></td>
                        <td><app-combo class="hc" [value]="p.key" (valueChange)="p.key = $event; touch()" [options]="keyOpts(p.key)" placeholder="Key" emptyText="All common headers are used - type any name" [name]="'hk' + $index" /></td>
                        <td><app-combo class="hc" [value]="p.value" (valueChange)="p.value = $event; touch()" [options]="valueOpts(p.key)" placeholder="Value" emptyText="Type the value" [name]="'hv' + $index" /></td>
                        <td><input [(ngModel)]="p.desc" (ngModelChange)="touch()" placeholder="Description" /></td><td><a class="x" (click)="del(draft().req.headers!, $index)">✕</a></td></tr>
                    }
                  </tbody></table>
                  <button type="button" class="add" (click)="add('headers')">+ Add Header</button>
                }
              }
              @case ('body') {
                <div class="modes">
                  @for (m of bodyKinds; track m.v) { <label><input type="radio" name="bm" [checked]="bodyKind() === m.v" (change)="setKind(m.v)" /> {{ m.l }}</label> }
                  @if (bodyKind() === 'raw') { <a class="lk lang" (click)="langMenu($event)">{{ rawLabel() }} ▾</a> }
                  <span class="sp"></span>
                  @if (draft().req.body?.mode === 'json') { <a class="lk" (click)="beautify()">Beautify</a> }
                </div>
                @switch (bodyKind()) {
                  @case ('') { <div class="mut pad">This request does not have a body.</div> }
                  @case ('urlencoded') {
                    <table class="kv"><thead><tr><th></th><th>Key</th><th>Value</th><th>Description</th><th></th></tr></thead><tbody>@for (p of draft().req.body!.form ?? []; track $index) {
                      <tr><td><input type="checkbox" [ngModel]="!p.off" (ngModelChange)="p.off = !$event; touch()" /></td><td><input [(ngModel)]="p.key" (ngModelChange)="touch()" placeholder="Key" /></td><td><input [(ngModel)]="p.value" (ngModelChange)="touch()" placeholder="Value" /></td>
                        <td><input [(ngModel)]="p.desc" (ngModelChange)="touch()" placeholder="Description" /></td><td><a class="x" (click)="del(draft().req.body!.form!, $index)">✕</a></td></tr>
                    }</tbody></table><button type="button" class="add" (click)="addForm()">+ Add Field</button>
                  }
                  @case ('form') {
                    <table class="kv"><thead><tr><th></th><th>Key</th><th></th><th>Value</th><th>Description</th><th></th></tr></thead><tbody>@for (p of draft().req.body!.form ?? []; track $index) {
                      <tr><td><input type="checkbox" [ngModel]="!p.off" (ngModelChange)="p.off = !$event; touch()" /></td><td><input [(ngModel)]="p.key" (ngModelChange)="touch()" placeholder="Key" /></td>
                        <td class="ty"><a class="tyb" (click)="p.type = p.type === 'file' ? '' : 'file'; touch()" title="Text or File">{{ p.type === 'file' ? 'File' : 'Text' }} ▾</a></td>
                        <td>@if (p.type === 'file') { <div class="fr"><input [(ngModel)]="p.value" (ngModelChange)="touch()" placeholder="/path/to/file" /><button type="button" (click)="pickFile(p)">Choose…</button></div> }
                          @else { <input [(ngModel)]="p.value" (ngModelChange)="touch()" placeholder="Value" /> }</td>
                        <td><input [(ngModel)]="p.desc" (ngModelChange)="touch()" placeholder="Description" /></td><td><a class="x" (click)="del(draft().req.body!.form!, $index)">✕</a></td></tr>
                    }</tbody></table><button type="button" class="add" (click)="addForm()">+ Add Field</button>
                  }
                  @case ('binary') {
                    <div class="bin"><input [ngModel]="draft().req.body?.file" (ngModelChange)="setBody({ file: $event })" placeholder="/path/to/file (sent as it is)" spellcheck="false" />
                      <button type="button" (click)="pickBinary()">Choose File…</button></div>
                  }
                  @case ('graphql') {
                    <div class="gq"><div><div class="sub0">Query</div><app-code-area class="ce" [value]="draft().req.body!.raw ?? ''" (valueChange)="setBody({ raw: $event })" placeholder="query { … }" /></div>
                      <div><div class="sub0">GraphQL Variables</div><app-code-area class="ce" [value]="draft().req.body!.graphqlVars ?? ''" (valueChange)="setBody({ graphqlVars: $event })" placeholder="{ }" /></div></div>
                  }
                  @default { <app-code-area class="ce pad3" [value]="draft().req.body?.raw ?? ''" (valueChange)="setBody({ raw: $event })" [minHeight]="140" /> }
                }
              }
              @case ('scripts') {
                <div class="scr">
                  <div class="sl">
                    <a [class.on]="scriptTab() === 'pre'" (click)="scriptTab.set('pre')">Before request @if (draft().req.preScript?.trim()) { <b class="gd"></b> }</a>
                    <a [class.on]="scriptTab() === 'post'" (click)="scriptTab.set('post')">After response @if (draft().req.tests?.trim()) { <b class="gd"></b> }</a>
                  </div>
                  <div class="se">
                    <div class="aiw"><span class="ai-ic">✦</span>
                      <input [(ngModel)]="aiAskText" (keydown.enter)="writeWithAI()" [disabled]="aiBusy()" [placeholder]="scriptTab() === 'pre' ? 'Describe the script, e.g. log in and keep the token in {{token}}' : 'Describe the tests, e.g. every port of 1:1-1:4 is UP and the answer is under 300 ms'" />
                      <button type="button" (click)="writeWithAI()" [disabled]="aiBusy() || !aiAskText.trim()">{{ aiBusy() ? 'Writing…' : 'Write with AI' }}</button></div>
                    @if (aiNote()) { <div class="mut pad0">{{ aiNote() }}</div> }
                    <div class="sh"><span class="mut">{{ scriptTab() === 'pre' ? 'Runs before the request is sent (pm.variables.set, pm.sendRequest, pm.execution.skipRequest).' : 'Runs on the answer (pm.test, pm.expect, pm.response, postman.setNextRequest).' }}</span>
                      <span class="sp"></span><a class="lk" (click)="snippetMenu($event)">Snippets ▾</a></div>
                    @if (scriptTab() === 'pre') { <app-code-area class="ce" [value]="draft().req.preScript ?? ''" (valueChange)="setReq({ preScript: $event })" placeholder="pm.variables.set('id', 10);" [minHeight]="160" /> }
                    @else { <app-code-area class="ce" [value]="draft().req.tests ?? ''" (valueChange)="setReq({ tests: $event })" placeholder="pm.test('ok', () => pm.response.to.have.status(200));" [minHeight]="160" /> }
                    @if (collScript()) { <div class="mut pad0">The collection's {{ scriptTab() === 'pre' ? 'pre-request script' : 'tests' }} run first (Edit Collection → Scripts).</div> }
                  </div>
                </div>
              }
              @case ('settings') {
                <div class="sets">
                  <div class="st"><div><b>HTTP version</b><div class="mut">The HTTP version to send the request with.</div></div>
                    <select class="fs" [ngModel]="st().httpVersion || ''" (ngModelChange)="setSt({ httpVersion: $event })"><option value="">Auto</option><option value="1.1">HTTP/1.x</option><option value="2">HTTP/2</option></select></div>
                  <div class="st"><div><b>SSL certificate verification</b><div class="mut">Check the server's certificate; when it fails the request is stopped. Default: the session's choice ({{ sess()?.apiInsecure ? 'off' : 'on' }}).</div></div>
                    <select class="fs" [ngModel]="st().verify || ''" (ngModelChange)="setSt({ verify: $event })"><option value="">Session's</option><option value="on">On</option><option value="off">Off</option></select></div>
                  @for (o of settingToggles; track o.k) {
                    <div class="st"><div><b>{{ o.l }}</b><div class="mut">{{ o.d }}</div></div>
                      <label class="sw"><input type="checkbox" [ngModel]="isOn(o)" (ngModelChange)="flip(o, $event)" /><span></span>{{ isOn(o) ? 'ON' : 'OFF' }}</label></div>
                  }
                  <div class="st"><div><b>Maximum number of redirects</b><div class="mut">Redirects followed before the request stops (default 10).</div></div>
                    <input type="number" min="0" class="num" [ngModel]="st().maxRedirects || ''" (ngModelChange)="setSt({ maxRedirects: +$event || undefined })" placeholder="10" /></div>
                  <div class="st"><div><b>Request timeout (ms)</b><div class="mut">How long to wait for the answer (default 30000).</div></div>
                    <input type="number" min="0" class="num" [ngModel]="st().timeoutMs || ''" (ngModelChange)="setSt({ timeoutMs: +$event || undefined })" placeholder="30000" /></div>
                </div>
              }
              @case ('code') {
                <div class="modes"><select class="fs" [ngModel]="lang()" (ngModelChange)="lang.set($event); loadCode()">@for (l of langs; track l.v) { <option [value]="l.v">{{ l.l }}</option> }</select>
                  <a class="lk" (click)="copy(code())">Copy</a><span class="mut">Secrets are environment variables in the code, never written out.</span></div>
                <pre class="out" translate="no">{{ code() }}</pre>
              }
            }
          </div>
        </div>
        <div class="card res">
          <div class="tabs">
            <b class="rh">Response</b>
            @for (t of resTabs; track t.k) { <a [class.on]="resTab() === t.k" (click)="resTab.set(t.k)">{{ t.l }}@if (resCount(t.k); as n) { <i [class.bad]="t.k === 'tests' && failed() > 0">{{ n }}</i> }</a> }
            <span class="sp"></span>
            @if (res(); as r) {
              @if (r.status) { <span class="pill" [class.ok]="r.status < 300" [class.wn]="r.status >= 300 && r.status < 400" [class.bad]="r.status >= 400">{{ r.statusText }}</span> }
              <span class="mut">{{ r.ms }} ms · {{ size(r.size) }}</span>
              @if (resTab() === 'body') {
                <button type="button" class="mono-btn" [class.on]="mono()" (click)="toggleMono()" [attr.aria-pressed]="mono()"
                  [title]="mono() ? 'Monochrome on: plain black text (light theme) or white (dark theme). Click for colours.' : 'Monochrome: show the body in plain black (light theme) or white (dark theme) instead of colours'">
                  <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true"><circle cx="8" cy="8" r="6.2" fill="none" stroke="currentColor" stroke-width="1.4" /><path d="M8 1.8a6.2 6.2 0 0 1 0 12.4z" fill="currentColor" /></svg>
                </button>
                <select class="fs pretty" [ngModel]="pretty() ? 'p' : 'r'" (ngModelChange)="pretty.set($event === 'p')"><option value="p">Pretty</option><option value="r">Raw</option></select>
              }
              <button type="button" class="ib" (click)="resMenu($event)" title="More">⋯</button>
            }
          </div>
          <div class="body">
            @if (res(); as r) {
              @if (r.error) { <div class="err">{{ r.error }}</div> }
              @if (r.skipped) { <div class="mut pad">The pre-request script skipped this request (pm.execution.skipRequest).</div> }
              @switch (resTab()) {
                @case ('body') {
                  @if (r.base64) { <div class="mut pad">Binary answer ({{ size(r.size) }}, {{ r.contentType }}). Use ⋯ → Save Response to File.</div> }
                  @else if (pretty() && isJson(r)) { <pre class="out json" [class.mono]="mono()" translate="no" [innerHTML]="jsonHtml()"></pre> }
                  @else { <pre class="out" [class.mono]="mono()" translate="no">{{ r.body }}</pre> }
                  @if (r.truncated) { <div class="mut pad">Only the first 5 MB are shown.</div> }
                }
                @case ('headers') { <table class="kv ro" translate="no"><tbody>@for (h of r.headers; track $index) { <tr><td>{{ h.key }}</td><td>{{ h.value }}</td></tr> }</tbody></table> }
                @case ('cookies') {
                  <table class="kv ro" translate="no"><thead><tr><th>Name</th><th>Value</th><th>Domain</th><th>Path</th><th>Expires</th></tr></thead><tbody>
                    @for (c of r.cookies ?? []; track $index) { <tr><td>{{ c.name }}</td><td>{{ c.value }}</td><td>{{ c.domain }}</td><td>{{ c.path }}</td><td>{{ c.expires }}</td></tr> }</tbody></table>
                  @if (!r.cookies?.length) { <div class="mut pad">No cookies set. Cookies are kept per session for the next requests (Cookies above the request).</div> }
                }
                @case ('tests') {
                  @for (t of r.tests ?? []; track $index) { <div class="tr" [class.bad]="!t.pass"><b>{{ t.pass ? '✓ PASS' : '✕ FAIL' }}</b> {{ t.name }}@if (t.error) { <span class="mut"> — {{ t.error }}</span> }</div> }
                  @empty { <div class="mut pad">No tests. Add some in the request's Scripts tab (After response).</div> }
                  @if (r.vars && keys(r.vars).length) { <div class="sub">Variables set: @for (k of keys(r.vars); track k) { <code>{{ k }}</code> } </div> }
                  @if (r.envSet && keys(r.envSet).length) { <div class="sub">Kept in the environment: @for (k of keys(r.envSet); track k) { <code>{{ k }}</code> } </div> }
                  @if (r.logs?.length) { <div class="sub">Console</div><pre class="out small">{{ r.logs!.join('\\n') }}</pre> }
                }
                @case ('timeline') {
                  <div class="tl2">
                    @for (p of phases(r); track p.l) { <div class="ph"><span>{{ p.l }}</span><span class="bar2"><i [style.width.%]="p.w"></i></span><span class="mut">{{ p.v.toFixed(1) }} ms</span></div> }
                  </div>
                  <div class="sub" translate="no">{{ r.method }} {{ r.url }}{{ r.proto ? ' · ' + r.proto : '' }}</div>
                  <table class="kv ro" translate="no"><tbody>@for (h of r.reqHeaders; track $index) { <tr><td>{{ h.key }}</td><td>{{ h.value }}</td></tr> }</tbody></table>
                  @if (r.reqBody) { <pre class="out small" translate="no">{{ r.reqBody }}</pre> }
                }
              }
            } @else { <div class="empty">Choose an API session or an environment, type a URL and press Send (or Enter).<br />Import a Postman collection or environment, an OpenAPI / Swagger document or cURL commands with Import.</div> }
          </div>
        </div>
      </div>
      <div class="con" [class.open]="consoleOpen()">
        <div class="conh" (click)="consoleOpen.set(!consoleOpen())"><b>Console</b><span class="mut">{{ consoleLog().length }} requests</span><span class="sp"></span>
          @if (consoleOpen() && consoleLog().length) { <a class="lk" (click)="consoleLog.set([]); $event.stopPropagation()">Clear</a> }<span class="mut">{{ consoleOpen() ? '▾' : '▴' }}</span></div>
        @if (consoleOpen()) {
          <div class="conb">
            @for (c of consoleLog(); track $index) {
              <div class="cl" (click)="c.open = !c.open" [class.bad]="!c.status || c.status >= 400">
                <span class="mut">{{ c.at }}</span><span [class]="'m ' + c.method">{{ c.method }}</span><span class="u" translate="no">{{ c.url }}</span><span class="stt">{{ c.status || c.error || '✕' }}</span><span class="mut">{{ c.ms }} ms</span></div>
              @if (c.open) {
                <div class="cd" translate="no">
                  @for (l of c.logs; track $index) { <div class="lg">› {{ l }}</div> }
                  <div class="sub">Request headers</div>@for (h of c.reqHeaders; track $index) { <div><b>{{ h.key }}:</b> {{ h.value }}</div> }
                  @if (c.reqBody) { <div class="sub">Request body</div><pre>{{ c.reqBody }}</pre> }
                  <div class="sub">Response headers</div>@for (h of c.resHeaders; track $index) { <div><b>{{ h.key }}:</b> {{ h.value }}</div> }
                </div>
              }
            } @empty { <div class="mut pad">Every request sent from this tab is logged here with its console.log lines.</div> }
          </div>
        }
      </div>
    </div>

    @if (varsOpen()) {
      <div class="card varp">
        <div class="ch"><b>Variables in request</b><span class="sp"></span><a class="x" (click)="varsOpen.set(false)" title="Close">✕</a></div>
        <div class="vl">
          @for (n of usedVars(); track n) {
            @let v = varOf(n);
            <div class="vr" [class.no]="!v">
              <span class="vn" translate="no">{{ n }}</span>
              @if (v) { <span class="vv" translate="no">{{ v.secret ? '••••••••' : v.value }}</span><span class="vs">{{ v.source }}</span> }
              @else { <input class="vv" placeholder="Enter value" (change)="setLocal(n, $any($event.target).value)" /> }
            </div>
          } @empty { <div class="mut pad">This request uses no {{ '{{variables}}' }}.</div> }
          <div class="vh">All variables</div>
          @for (g of varGroups(); track g.src) {
            <div class="vg"><span class="badge" [class]="'badge s-' + g.src">{{ g.src[0].toUpperCase() }}</span> {{ srcName(g.src) }}</div>
            @for (v of g.vars; track v.name) { <div class="vr"><span class="vn" translate="no">{{ v.name }}</span><span class="vv" translate="no">{{ v.secret ? '••••••••' : v.value }}</span></div> }
          }
          <div class="mut pad">Values win in this order: local (set by scripts in this tab) › environment › session › collection › globals. <a class="lk" (click)="openEnvs()">Environments…</a></div>
        </div>
      </div>
    }

    @if (envsOpen()) {
      <div class="ov" (click)="envsOpen.set(false)"><div class="dlg wide envd" (click)="$event.stopPropagation()">
        <h3>Environments</h3>
        <div class="mut">Named sets of variables, as in Postman: choose one at the top right and its values win over the API session's. Secret values are kept in the computer's credential store and never shown again. Globals are used by every request.</div>
        <div class="eg">
          <div class="el">
            @for (e of envs(); track e.id) { <div class="ei" [class.on]="envEdit()?.id === e.id" (click)="editEnv(e)" translate="no">{{ e.id === 'globals' ? 'Globals' : e.name }}<span class="ct">{{ e.vars.length }}</span></div> }
            <button type="button" class="add" (click)="newEnv()">+ New Environment</button>
            <button type="button" class="add" (click)="importEnvFile()">Import Postman Environment…</button>
            <textarea class="code small" [(ngModel)]="envImportText" placeholder="…or paste a Postman environment (JSON) here" spellcheck="false"></textarea>
            @if (envImportText.trim()) { <button type="button" (click)="importEnvText()">Import Pasted</button> }
          </div>
          <div class="ee">
            @if (envEdit(); as e) {
              <div class="row"><input [(ngModel)]="e.name" [disabled]="e.id === 'globals'" placeholder="Environment name" aria-label="Environment name" />
                @if (e.id && e.id !== 'globals') { <a class="lk" (click)="useEnv(e.id)">Use It</a> }
                @if (e.id) { <a class="lk" (click)="exportEnv(e.id)">Export…</a><a class="lk bad" (click)="deleteEnv(e)">{{ e.id === 'globals' ? 'Clear' : 'Delete' }}</a> }</div>
              <table class="kv"><thead><tr><th></th><th>Variable</th><th>Value</th><th title="Kept in the credential store">Secret</th><th>Description</th><th></th></tr></thead><tbody>
                @for (v of e.vars; track $index) {
                  <tr><td><input type="checkbox" [ngModel]="!v.off" (ngModelChange)="v.off = !$event" /></td>
                    <td><input [(ngModel)]="v.key" placeholder="name" spellcheck="false" /></td>
                    <td>@if (v.secret) { <div class="fr"><input [type]="reveal().has($index) ? 'text' : 'password'" [(ngModel)]="v.value" [placeholder]="v.hasValue ? '•••••••• saved' : 'secret value'" autocomplete="new-password" />
                        <a class="x" (click)="toggleReveal($index)" [title]="reveal().has($index) ? 'Hide' : 'Show what you typed'">{{ reveal().has($index) ? '🙈' : '👁' }}</a></div> }
                      @else { <input [(ngModel)]="v.value" placeholder="value" spellcheck="false" /> }</td>
                    <td><input type="checkbox" [(ngModel)]="v.secret" /></td>
                    <td><input [(ngModel)]="v.desc" placeholder="description" /></td><td><a class="x" (click)="e.vars.splice($index, 1)">✕</a></td></tr>
                }
              </tbody></table>
              <button type="button" class="add" (click)="e.vars.push({ key: '', value: '' })">+ Add Variable</button>
            } @else { <div class="mut pad">Choose an environment, make a new one or import one from Postman.</div> }
          </div>
        </div>
        <div class="btns"><span class="sp"></span><button type="button" (click)="envsOpen.set(false)">Close</button>
          <button type="button" class="pri" (click)="saveEnv()" [disabled]="!envEdit()">Save</button></div>
      </div></div>
    }
    @if (cookiesOpen()) {
      <div class="ov" (click)="cookiesOpen.set(false)"><div class="dlg" (click)="$event.stopPropagation()">
        <h3>Cookies</h3>
        <div class="mut">Cookies kept for {{ path() ? 'the session ' + path() : 'requests sent without a session' }}; they are sent with the next requests to the same server and forgotten when Termwright quits.</div>
        <div class="cks">
          @for (h of cookieList(); track h.host) {
            <div class="ckh" translate="no"><b>{{ h.host }}</b><span class="sp"></span><a class="lk" (click)="delCookie(h.host, '')">Remove All</a></div>
            @for (c of h.cookies; track c.name) { <div class="ckr" translate="no"><span>{{ c.name }}</span><span class="mut ell">{{ c.value }}</span><a class="x" (click)="delCookie(h.host, c.name)">✕</a></div> }
          } @empty { <div class="mut pad">No cookies.</div> }
        </div>
        <div class="btns">@if (cookieList().length) { <button type="button" (click)="delCookie('', '')">Clear All Cookies</button> }<span class="sp"></span><button type="button" class="pri" (click)="cookiesOpen.set(false)">Close</button></div>
      </div></div>
    }
    @if (collEdit(); as c) {
      <div class="ov" (click)="collEdit.set(null)"><div class="dlg wide" (click)="$event.stopPropagation()">
        <h3 translate="no">{{ c.name }}</h3>
        <div class="tabs flat">@for (t of collTabs; track t.k) { <a [class.on]="collTab() === t.k" (click)="collTab.set(t.k)">{{ t.l }}</a> }</div>
        @switch (collTab()) {
          @case ('overview') {
            <label>Name<input [(ngModel)]="c.name" /></label>
            <label>API session it uses<select class="fs" [(ngModel)]="c.session"><option [ngValue]="undefined">None</option>@for (s of sessions(); track s.path) { <option [ngValue]="s.path">{{ s.path }}</option> }</select></label>
            <label>Description<textarea class="code" [(ngModel)]="c.description" placeholder="What this collection is for"></textarea></label>
            <div class="mut">{{ c.requests.length }} requests. <a class="lk" (click)="collEdit.set(null); openRun(c)">Run Collection…</a></div>
          }
          @case ('auth') {
            <app-api-auth [auth]="c.auth ?? ''" [data]="c.authData" [forCollection]="true" [known]="known()" (changed)="c.auth = $event.auth || undefined; c.authData = $event.authData" />
            <div class="mut">Requests whose Authorization is "Inherit auth from parent" use this.</div>
          }
          @case ('scripts') {
            <div class="mut">Run before (and after) every request of the collection, before the request's own scripts.</div>
            <div class="sub0">Before request</div><app-code-area class="ce" [value]="c.preScript ?? ''" (valueChange)="c.preScript = $event" [minHeight]="110" />
            <div class="sub0">After response</div><app-code-area class="ce" [value]="c.tests ?? ''" (valueChange)="c.tests = $event" [minHeight]="110" />
          }
          @case ('vars') {
            <div class="mut">Collection variables ({{ '{{name}}' }}) are saved in the collection's file: no secrets here (use an environment's secret variables).</div>
            <table class="kv"><thead><tr><th></th><th>Variable</th><th>Value</th><th>Description</th><th></th></tr></thead><tbody>
              @for (v of c.vars ?? []; track $index) {
                <tr><td><input type="checkbox" [ngModel]="!v.off" (ngModelChange)="v.off = !$event" /></td><td><input [(ngModel)]="v.key" spellcheck="false" /></td><td><input [(ngModel)]="v.value" spellcheck="false" /></td>
                  <td><input [(ngModel)]="v.desc" /></td><td><a class="x" (click)="c.vars!.splice($index, 1)">✕</a></td></tr>
              }
            </tbody></table>
            <button type="button" class="add" (click)="c.vars = [...(c.vars ?? []), { key: '', value: '' }]">+ Add Variable</button>
          }
        }
        <div class="btns"><span class="sp"></span><button type="button" (click)="collEdit.set(null)">Cancel</button><button type="button" class="pri" (click)="saveCollEdit()">Save</button></div>
      </div></div>
    }
    @if (importOpen()) {
      <div class="ov" (click)="importOpen.set(false)"><div class="dlg" (click)="$event.stopPropagation()">
        <h3>Import</h3>
        <div class="mut">A Postman collection (v2.0 / v2.1: folders, authorization, scripts, variables, settings) or environment, an OpenAPI 3 or Swagger 2 document (YAML or JSON: one request per operation, by tag) or cURL commands.</div>
        <textarea class="code" [(ngModel)]="importText" placeholder="Paste here, or choose a file" spellcheck="false"></textarea>
        <div class="row"><input [(ngModel)]="importName" placeholder="Collection name (default: from the document)" />
          <label class="mut"><input type="checkbox" [(ngModel)]="importUseSession" [disabled]="!path()" /> Use the session {{ path() || '(none chosen)' }}</label></div>
        @if (importErr()) { <div class="err">{{ importErr() }}</div> }
        <div class="btns"><button type="button" (click)="importFile()">Choose File…</button><button type="button" (click)="importOpen.set(false); importEnvFile(); openEnvs()">Import Postman Environment…</button><span class="sp"></span>
          <button type="button" (click)="importOpen.set(false)">Cancel</button><button type="button" class="pri" (click)="doImport()" [disabled]="!importText.trim()">Import</button></div>
      </div></div>
    }
    @if (saveOpen()) {
      <div class="ov" (click)="saveOpen.set(false)"><div class="dlg sm" (click)="$event.stopPropagation()">
        <h3>Save Request</h3>
        <label>Name<input [(ngModel)]="saveName" /></label>
        <label>Collection<select class="fs" [(ngModel)]="saveColl"><option value="">New collection…</option>@for (c of colls(); track c.id) { <option [value]="c.id">{{ c.name }}</option> }</select></label>
        @if (!saveColl) { <label>New collection's name<input [(ngModel)]="saveNewColl" /></label> }
        <label>Folder (optional)<input [(ngModel)]="saveFolder" placeholder="Interfaces/Ports" /></label>
        <div class="btns"><span class="sp"></span><button type="button" (click)="saveOpen.set(false)">Cancel</button><button type="button" class="pri" (click)="saveAs()" [disabled]="!saveName.trim() || (!saveColl && !saveNewColl.trim())">Save</button></div>
      </div></div>
    }
    @if (runColl(); as c) {
      <div class="ov" (click)="!running() && runColl.set(null)"><div class="dlg wide" (click)="$event.stopPropagation()">
        <h3 translate="no">Run {{ c.name }}{{ runFolder ? ' / ' + runFolder : '' }}</h3>
        <div class="row">
          <label>Session<select class="fs" [(ngModel)]="runSession"><option value="">{{ c.session ? 'The collection\\'s (' + c.session + ')' : 'No session' }}</option>@for (s of sessions(); track s.path) { <option [value]="s.path">{{ s.path }}</option> }</select></label>
          <label>Environment<select class="fs" [(ngModel)]="runEnvId"><option value="">No Environment</option>@for (e of envList(); track e.id) { <option [value]="e.id">{{ e.name }}</option> }</select></label>
          <label>Values<select class="fs" [(ngModel)]="runEnv"><option value="">Session's</option><option value="production">Production</option></select></label>
          <label>Iterations<input type="number" min="1" [(ngModel)]="runIter" style="width: 60px" /></label>
          <label>Delay (ms)<input type="number" min="0" [(ngModel)]="runDelay" style="width: 70px" /></label>
          <label class="chk"><input type="checkbox" [(ngModel)]="runStop" /> Stop at the first failure</label>
        </div>
        <textarea class="code small" [(ngModel)]="runData" placeholder="Data (optional): CSV with a header line, or a JSON array of objects - one iteration per row, its columns are variables" spellcheck="false"></textarea>
        <div class="mut">Requests run in order; a script can choose the next one with postman.setNextRequest("name") (null ends the iteration) or skip one with pm.execution.skipRequest().</div>
        @if (runRes() || runSteps().length) {
          <div class="sum">@if (runRes(); as rr) { <b [class.bad]="rr.failed">{{ rr.passed }} passed · {{ rr.failed }} failed</b> · {{ rr.tests }} tests · {{ rr.ms }} ms{{ rr.stopped ? ' · stopped' : '' }} } @else { Running… {{ runSteps().length }} done }</div>
          <div class="steps"><table class="kv ro"><tbody>
            @for (s of runSteps(); track $index) {
              <tr [class.bad]="!s.pass"><td>{{ s.pass ? '✓' : '✕' }}</td><td>#{{ s.iteration }}</td><td><span [class]="'m ' + s.method">{{ s.method }}</span> {{ s.name }}</td><td>{{ s.status || '' }}</td><td>{{ s.ms }} ms</td>
                <td>{{ s.error }}@for (t of s.tests ?? []; track $index) { @if (!t.pass) { <div>✕ {{ t.name }}: {{ t.error }}</div> } }</td></tr>
            }
          </tbody></table></div>
        }
        @if (runErr()) { <div class="err">{{ runErr() }}</div> }
        <div class="btns"><span class="sp"></span><button type="button" (click)="runColl.set(null)" [disabled]="running()">Close</button>
          <button type="button" class="pri" (click)="doRun()" [disabled]="running()">{{ running() ? 'Running…' : '▶ Run' }}</button></div>
      </div></div>
    }
  </div>
  `,
  styles: [`
    :host { display: block; height: 100%; }
    .ac.withvars { grid-template-columns: minmax(230px, 300px) 1fr 290px; }
    .ac { height: 100%; display: grid; grid-template-columns: minmax(230px, 300px) 1fr; gap: 10px; padding: 10px 12px; box-sizing: border-box; background: var(--bg); color: var(--text); font-size: 13px; position: relative; }
    .side { display: flex; flex-direction: column; gap: 10px; min-height: 0; }
    .card { background: var(--panel); border: 1px solid var(--border); border-radius: var(--r-lg); display: flex; flex-direction: column; min-height: 0; }
    .colls { flex: 3; } .hist { flex: 2; }
    .ch { display: flex; align-items: center; gap: 10px; padding: 9px 12px; border-bottom: 1px solid var(--border); }
    .sp { flex: 1; }
    .lk { color: var(--accent); cursor: pointer; font-size: 12px; white-space: nowrap; }
    .q { margin: 8px 10px 4px; }
    .tl { overflow: auto; flex: 1; padding-bottom: 6px; }
    .cn, .fn, .rn { display: flex; align-items: center; gap: 6px; padding: 4px 10px; cursor: pointer; white-space: nowrap; }
    .cn:hover, .fn:hover, .rn:hover { background: var(--hover); } .rn.on { background: var(--selected); }
    .cn .nm { font-weight: 500; } .nm { overflow: hidden; text-overflow: ellipsis; flex: 1; }
    .ct { color: var(--faint); font-size: 11px; } .more { opacity: 0; color: var(--muted); } .cn:hover .more { opacity: 1; }
    .tw { width: 10px; color: var(--muted); }
    .fn { color: var(--muted); }
    .m { font: 600 10.5px var(--mono); min-width: 38px; color: var(--ok); }
    .m.POST { color: #e5a33b; } .m.PUT { color: #4f9cf7; } .m.PATCH { color: #a78bfa; } .m.DELETE { color: var(--danger); } .m.HEAD, .m.OPTIONS { color: var(--muted); }
    .st { font-size: 11px; color: var(--ok); } .st.bad, .bad { color: var(--danger); }
    .ren { flex: 1; padding: 1px 4px; }
    .mut { color: var(--muted); font-size: 12px; } .pad { padding: 10px 12px; } .pad2 { padding: 12px 14px; display: flex; flex-direction: column; gap: 10px; }
    .main { display: flex; flex-direction: column; gap: 8px; min-width: 0; min-height: 0; }
    .bar { display: flex; gap: 8px; align-items: center; }
    .bar .url { flex: 1; min-width: 200px; font-family: var(--mono); }
    .bar > select, .bar > input, .bar > button, .bar > app-session-select { height: 30px; box-sizing: border-box; }
    .sess { width: 220px; flex: none; } .env { max-width: 170px; }
    button.pri { background: var(--accent); color: #fff; border-color: var(--accent); min-width: 72px; }
    .setb { height: 24px; padding: 0 9px; font-size: 12px; }
    .chips { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
    .chip { border: 1px solid var(--border); border-radius: 12px; padding: 2px 10px; font-size: 12px; color: var(--muted); }
    .chip.s { border-color: color-mix(in srgb, var(--accent) 55%, transparent); color: var(--accent); }
    .chip.ok { border-color: color-mix(in srgb, var(--ok) 55%, transparent); color: var(--ok); }
    .chip.warn { border-color: color-mix(in srgb, var(--warn) 55%, transparent); color: var(--warn); }
    .chip.bad { border-color: color-mix(in srgb, var(--danger) 55%, transparent); color: var(--danger); }
    .split { flex: 1; min-height: 0; display: grid; grid-template-rows: minmax(170px, 46%) 1fr; gap: 8px; }
    .tabs { display: flex; gap: 2px; align-items: center; border-bottom: 1px solid var(--border); padding: 0 8px; flex-wrap: nowrap; overflow-x: auto; white-space: nowrap; scrollbar-width: none; }
    .tabs > a { padding: 8px 9px; cursor: pointer; color: var(--muted); border-bottom: 2px solid transparent; }
    .tabs > a.on { color: var(--text); border-bottom-color: var(--accent); }
    .tabs i { font-style: normal; font-size: 11px; color: var(--faint); margin-left: 4px; } .tabs i.bad { color: var(--danger); }
    .tabs .mut, .tabs .pill, .tabs .pretty, .tabs button { margin-left: 8px; }
    .pill { border: 1px solid var(--ok); color: var(--ok); border-radius: 12px; padding: 1px 10px; font-size: 12px; }
    .pill.wn { border-color: var(--warn); color: var(--warn); } .pill.bad { border-color: var(--danger); color: var(--danger); }
    .body { flex: 1; overflow: auto; min-height: 0; }
    table.kv { width: 100%; border-collapse: collapse; }
    .kv th { text-align: left; font-weight: 500; color: var(--muted); padding: 5px 8px; border-bottom: 1px solid var(--line); }
    .kv td { padding: 3px 6px; border-bottom: 1px solid var(--line); vertical-align: top; }
    .kv td:first-child { width: 22px; } .kv input:not([type=checkbox]) { width: 100%; box-sizing: border-box; font-family: var(--mono); font-size: 12px; border-color: transparent; background: transparent; }
    .kv input:focus { border-color: var(--accent); background: var(--input-bg); }
    .kv.ro td { font-family: var(--mono); font-size: 12px; padding: 5px 10px; word-break: break-all; } .kv.ro td:first-child { width: auto; color: var(--muted); white-space: nowrap; }
    .kv tr.auto td { color: var(--muted); font-size: 12px; padding: 6px; }
    .x { cursor: pointer; color: var(--muted); }
    .hc ::ng-deep input { font-family: var(--mono); font-size: 12px; border-color: transparent; background: transparent; width: 100%; }
    .hc ::ng-deep input:focus { border-color: var(--accent); background: var(--input-bg); }
    .add { margin: 8px; } .add0 { margin: 0; }
    .modes { display: flex; gap: 12px; align-items: center; padding: 8px 12px; flex-wrap: wrap; } .modes label { display: inline-flex; gap: 4px; align-items: center; color: var(--muted); }
    textarea.code { width: calc(100% - 20px); margin: 0 10px 8px; min-height: 120px; font: 12px var(--mono); box-sizing: border-box; resize: vertical; }
    textarea.code.small { min-height: 60px; }
    .aiw { display: flex; gap: 8px; align-items: center; margin: 10px 10px 2px; padding: 6px 8px; border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent); border-radius: var(--r); background: var(--accent-soft); }
    .aiw input { flex: 1; } .ai-ic { color: var(--accent); } .pad0 { padding: 4px 12px; }
    .scr .sh { display: flex; align-items: center; gap: 8px; padding: 8px 12px 6px; }
    pre.out { margin: 0; padding: 10px 14px; font: 12px/1.5 var(--mono); white-space: pre-wrap; word-break: break-word; }
    pre.out.small { font-size: 11.5px; background: var(--surface-2); margin: 6px 12px; border-radius: var(--r); }
    pre.json ::ng-deep .k { color: #60a5fa; } pre.json ::ng-deep .s { color: #34d399; } pre.json ::ng-deep .n { color: #f59e0b; } pre.json ::ng-deep .b { color: #c084fc; }
    .mono-btn { display: inline-flex; align-items: center; justify-content: center; padding: 3px 6px; color: var(--muted); }
    .mono-btn.on { color: var(--accent); border-color: var(--accent); background: var(--accent-soft); }
    pre.out.mono, pre.json.mono ::ng-deep span { color: #fff !important; }
    :host-context(.light) pre.out.mono, :host-context(.light) pre.json.mono ::ng-deep span { color: #000 !important; }
    .err { margin: 10px 12px; padding: 8px 10px; border-radius: var(--r); background: color-mix(in srgb, var(--danger) 14%, transparent); color: var(--danger); white-space: pre-wrap; }
    .empty { padding: 40px; color: var(--muted); text-align: center; line-height: 1.7; }
    .tr { padding: 6px 14px; border-bottom: 1px solid var(--line); } .tr b { color: var(--ok); margin-right: 6px; } .tr.bad b { color: var(--danger); }
    .sub { padding: 8px 14px 2px; color: var(--muted); font-size: 12px; } .sub code { margin-right: 6px; color: var(--text); }
    .tl2 { padding: 10px 14px; display: grid; gap: 4px; }
    .ph { display: grid; grid-template-columns: 110px 1fr 80px; gap: 10px; align-items: center; font-size: 12px; }
    .bar2 { height: 8px; background: var(--surface-2); border-radius: 4px; overflow: hidden; } .bar2 i { display: block; height: 100%; background: var(--accent); }
    .rad { display: flex; gap: 6px; align-items: center; } .hint { color: var(--muted); font-size: 12px; line-height: 1.5; }
    .ov { position: absolute; inset: 0; background: rgba(0, 0, 0, .45); display: flex; align-items: center; justify-content: center; z-index: 5; }
    .dlg { background: var(--panel); border: 1px solid var(--border); border-radius: var(--r-lg); box-shadow: var(--shadow); padding: 16px 18px; width: min(720px, 92%); display: flex; flex-direction: column; gap: 10px; max-height: 90%; }
    .dlg.sm { width: min(420px, 92%); } .dlg.wide { width: min(980px, 95%); }
    .dlg h3 { margin: 0; font-size: 15px; } .dlg textarea.code { width: 100%; margin: 0; min-height: 200px; } .dlg textarea.code.small { min-height: 60px; }
    .dlg label { display: flex; flex-direction: column; gap: 4px; color: var(--muted); font-size: 12px; } .dlg label.chk, .dlg .row label.mut { flex-direction: row; align-items: center; }
    .dlg .row { display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; } .dlg .row > input { flex: 1; }
    .btns { display: flex; gap: 8px; }
    .mons .rn { display: flex; align-items: center; gap: 6px; } .mons .nm { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .mons .m.MOCK { color: var(--accent); } .mdot { width: 8px; height: 8px; border-radius: 50%; background: var(--faint); flex: none; } .mdot.ok { background: var(--ok); } .mdot.bad { background: var(--danger); } .mdot.off { opacity: .4; }
    .mons .hist { display: flex; gap: 2px; } .mons .hist i { width: 4px; height: 12px; border-radius: 1px; background: var(--ok); } .mons .hist i.bad { background: var(--danger); }
    .exr { display: flex; align-items: center; gap: 8px; padding: 5px 8px; border: 1px solid var(--border); border-radius: 6px; margin-bottom: 4px; min-width: 0; } .exr .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; } .exr .st { font-weight: 600; color: var(--ok); } .exr .st.bad { color: var(--danger); }
    .rtabs { display: flex; align-items: stretch; gap: 2px; border-bottom: 1px solid var(--border); min-height: 32px; overflow-x: auto; }
    .rt { display: flex; align-items: center; gap: 6px; padding: 0 8px 0 10px; max-width: 220px; cursor: pointer; border-bottom: 2px solid transparent; color: var(--muted); font-size: 12.5px; white-space: nowrap; }
    .rt.on { color: var(--text); border-bottom-color: var(--accent); background: var(--panel); } .rt .nm { overflow: hidden; text-overflow: ellipsis; }
    .rt .m { min-width: 0; } .rt .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--accent); flex: none; }
    .rt .cx { color: var(--faint); font-size: 11px; padding: 2px 4px; border-radius: 4px; visibility: hidden; } .rt:hover .cx, .rt.on .cx { visibility: visible; } .rt .cx:hover { background: var(--hover); color: var(--text); }
    .nt { align-self: center; padding: 2px 10px; cursor: pointer; color: var(--muted); font-size: 16px; }
    .envb { align-self: center; height: 28px; display: inline-flex; align-items: center; gap: 6px; max-width: 220px; white-space: nowrap; overflow: hidden; }
    .envb.on { border-color: color-mix(in srgb, var(--ok) 55%, transparent); color: var(--ok); } .cv { color: var(--muted); font-size: 10px; }
    .ib { align-self: center; height: 28px; padding: 0 9px; font: 600 12px var(--mono); margin-left: 6px; } .ib.on { color: var(--accent); border-color: var(--accent); background: var(--accent-soft); }
    .crumb { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; row-gap: 6px; } .crumb > app-session-select, .crumb > select, .crumb button { height: 28px; box-sizing: border-box; } .cc { color: var(--muted); white-space: nowrap; }
    .rn0 { border-color: transparent; background: transparent; font-weight: 600; min-width: 120px; flex: 0 1 360px; } .rn0:hover, .rn0:focus { border-color: var(--border); background: var(--input-bg); }
    .split-btn { display: inline-flex; } .split-btn > button:first-child { border-top-right-radius: 0; border-bottom-right-radius: 0; }
    .split-btn > .dd { border-top-left-radius: 0; border-bottom-left-radius: 0; border-left: 1px solid color-mix(in srgb, #000 25%, transparent); padding: 0 7px; min-width: 0; }
    .bar .url { flex: 1; min-width: 120px; height: 30px; } .bar .send { height: 30px; } .bar .send button { height: 30px; }
    .meth { width: 124px; flex: none; height: 30px; } .meth ::ng-deep input { font: 700 12px var(--mono); height: 30px; }
    .meth.m-GET ::ng-deep input, .meth.m-HEAD ::ng-deep input { color: var(--ok); } .meth.m-POST ::ng-deep input { color: #e5a33b; } .meth.m-PUT ::ng-deep input { color: #4f9cf7; }
    .meth.m-PATCH ::ng-deep input { color: #a78bfa; } .meth.m-DELETE ::ng-deep input { color: var(--danger); } .meth.m-OPTIONS ::ng-deep input { color: #ec4899; }
    .m.OPTIONS { color: #ec4899 !important; } .m.HEAD { color: var(--ok) !important; }
    .gd { display: inline-block; width: 6px; height: 6px; border-radius: 50%; background: var(--ok); margin-left: 5px; vertical-align: middle; }
    .sh { display: flex; align-items: center; gap: 10px; padding: 8px 12px 4px; } .sh b { font-weight: 600; font-size: 12.5px; }
    .hid { display: inline-flex; align-items: center; gap: 4px; color: var(--muted); font-size: 12px; cursor: pointer; border: 1px solid var(--line); border-radius: 10px; padding: 1px 8px; }
    .kv td.ro { font: 12px var(--mono); padding: 6px 8px; color: var(--muted); }
    .ty { width: 62px; } .tyb { font-size: 11.5px; color: var(--muted); cursor: pointer; white-space: nowrap; }
    .fr { display: flex; gap: 6px; align-items: center; } .fr input { flex: 1; }
    .bin { display: flex; gap: 8px; padding: 6px 12px; } .bin input { flex: 1; font-family: var(--mono); }
    .gq { display: grid; grid-template-columns: 1.4fr 1fr; gap: 10px; padding: 0 12px 8px; } .sub0 { color: var(--muted); font-size: 12px; margin: 4px 0; }
    .ce { display: block; height: calc(100% - 8px); } .pad3 { margin: 0 12px 8px; height: calc(100% - 52px); }
    .lang { font-weight: 600; }
    .docs { padding: 0 12px 10px; height: calc(100% - 10px); display: flex; flex-direction: column; } .docs .sh { padding-left: 0; } .docs .ce { flex: 1; }
    .scr { display: grid; grid-template-columns: 150px 1fr; height: 100%; }
    .sl { border-right: 1px solid var(--line); padding: 8px 6px; display: flex; flex-direction: column; gap: 2px; }
    .sl a { padding: 6px 8px; border-radius: 6px; cursor: pointer; color: var(--muted); font-size: 12.5px; } .sl a.on { background: var(--selected); color: var(--text); }
    .se { display: flex; flex-direction: column; min-width: 0; padding-bottom: 8px; } .se .ce { margin: 0 10px; flex: 1; }
    .sets { padding: 6px 14px 12px; } .st { display: flex; align-items: center; gap: 20px; padding: 9px 0; border-bottom: 1px solid var(--line); }
    .st > div { flex: 1; } .st b { font-weight: 600; font-size: 12.5px; color: var(--text); } .st select { width: 120px; } .num { width: 100px; }
    .sw { display: inline-flex; align-items: center; gap: 8px; font-size: 11.5px; color: var(--muted); width: 120px; cursor: pointer; }
    .sw input { position: absolute; opacity: 0; width: 0; } .sw span { width: 28px; height: 16px; border-radius: 8px; background: var(--border); position: relative; transition: background .15s; }
    .sw span::after { content: ''; position: absolute; left: 2px; top: 2px; width: 12px; height: 12px; border-radius: 50%; background: #fff; transition: left .15s; }
    .sw input:checked + span { background: var(--accent); } .sw input:checked + span::after { left: 14px; } .sw input:focus-visible + span { outline: 2px solid var(--accent); outline-offset: 2px; }
    .rh { padding: 8px 10px 8px 4px; font-weight: 600; }
    .con { border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--panel); flex: none; display: flex; flex-direction: column; max-height: 34%; }
    .con.open { min-height: 140px; } .conh { display: flex; align-items: center; gap: 10px; padding: 5px 12px; cursor: pointer; font-size: 12px; }
    .conb { overflow: auto; border-top: 1px solid var(--line); font: 11.5px var(--mono); }
    .cl { display: grid; grid-template-columns: 70px 56px 1fr auto 70px; gap: 8px; padding: 3px 12px; cursor: pointer; } .cl:hover { background: var(--hover); }
    .cl .u { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .cl .stt { color: var(--ok); } .cl.bad .stt { color: var(--danger); }
    .cd { padding: 4px 12px 8px 30px; background: var(--surface-2); word-break: break-all; } .cd pre { margin: 0; white-space: pre-wrap; } .cd .lg { color: var(--accent); }
    .varp { min-width: 0; } .vl { overflow: auto; flex: 1; padding: 4px 0; }
    .vr { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto; gap: 6px; align-items: center; padding: 4px 12px; font: 12px var(--mono); }
    .vr .vn { overflow: hidden; text-overflow: ellipsis; } .vr .vv { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--muted); } .vr.no .vn { color: var(--danger); }
    .vr input.vv { min-height: 24px; padding: 1px 6px; font: 12px var(--mono); } .vs { font: 10.5px var(--ui-font, sans-serif); color: var(--faint); }
    .vh { padding: 10px 12px 4px; font-weight: 600; border-top: 1px solid var(--line); margin-top: 6px; }
    .vg { padding: 8px 12px 2px; color: var(--muted); font-size: 12px; display: flex; align-items: center; gap: 6px; }
    .badge { width: 16px; height: 16px; border-radius: 4px; font: 700 10px/16px var(--mono); text-align: center; color: #fff; background: var(--muted); }
    .s-environment { background: #22a06b; } .s-collection { background: #c48a1d; } .s-globals { background: #4f86f7; } .s-session { background: #8b5cf6; } .s-local { background: #e05252; }
    .eg { display: grid; grid-template-columns: 240px 1fr; gap: 12px; min-height: 320px; overflow: hidden; }
    .el { display: flex; flex-direction: column; gap: 4px; border-right: 1px solid var(--line); padding-right: 10px; overflow: auto; }
    .ei { display: flex; padding: 6px 8px; border-radius: 6px; cursor: pointer; } .ei:hover { background: var(--hover); } .ei.on { background: var(--selected); } .ei .ct { margin-left: auto; }
    .el .add { margin: 2px 0; } .ee { overflow: auto; } .ee .row { margin-bottom: 8px; align-items: center; }
    .lk.bad { color: var(--danger); }
    .cks { max-height: 340px; overflow: auto; border: 1px solid var(--line); border-radius: var(--r); }
    .ckh { display: flex; padding: 6px 10px; background: var(--surface-2); } .ckr { display: grid; grid-template-columns: 160px 1fr auto; gap: 8px; padding: 4px 10px; font: 12px var(--mono); }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .tabs.flat { padding: 0; }
    .sum { font-size: 13px; } .steps { overflow: auto; max-height: 340px; border: 1px solid var(--line); border-radius: var(--r); }
  `],
})
export class ApiClient implements OnInit, OnDestroy {
  readonly tab = input.required<TabView>();
  readonly visible = input(true);
  private readonly backend = inject(Backend);
  readonly ui = inject(Ui);
  private readonly rec = inject(Recorder);

  readonly methods = METHODS; readonly modes = MODES; readonly langs = LANGS; readonly snippets = SNIPPETS;
  readonly reqTabs = [{ k: 'docs', l: 'Docs' }, { k: 'params', l: 'Params' }, { k: 'auth', l: 'Authorization' }, { k: 'headers', l: 'Headers' }, { k: 'body', l: 'Body' }, { k: 'scripts', l: 'Scripts' }, { k: 'settings', l: 'Settings' }, { k: 'code', l: 'Code' }];
  readonly methodOpts: ComboOption[] = METHODS.map(m => ({ value: m }));
  readonly bodyKinds = [{ v: '', l: 'none' }, { v: 'form', l: 'form-data' }, { v: 'urlencoded', l: 'x-www-form-urlencoded' }, { v: 'raw', l: 'raw' }, { v: 'binary', l: 'binary' }, { v: 'graphql', l: 'GraphQL' }];
  readonly collTabs = [{ k: 'overview', l: 'Overview' }, { k: 'auth', l: 'Authorization' }, { k: 'scripts', l: 'Scripts' }, { k: 'vars', l: 'Variables' }];
  readonly settingToggles: { k: keyof HSettings; l: string; d: string; inv?: boolean }[] = [
    { k: 'noRedirects', l: 'Automatically follow redirects', d: 'Follow HTTP 3xx answers as redirects.', inv: true },
    { k: 'followMethod', l: 'Follow original HTTP method', d: 'Redirect with the original method and body instead of changing to GET.' },
    { k: 'followAuth', l: 'Follow Authorization header', d: 'Keep the Authorization header when a redirect goes to another host.' },
    { k: 'removeReferer', l: 'Remove referer header on redirect', d: 'Send no Referer header after a redirect.' },
    { k: 'noEncode', l: 'Encode URL automatically', d: 'Encode the query parameters; off sends them as typed.', inv: true },
    { k: 'noCookies', l: 'Disable cookie jar', d: 'Neither send the cookies kept nor keep the ones this answer sets.' },
  ];
  readonly resTabs = [{ k: 'body', l: 'Body' }, { k: 'headers', l: 'Headers' }, { k: 'cookies', l: 'Cookies' }, { k: 'tests', l: 'Tests' }, { k: 'timeline', l: 'Timeline' }];
  readonly presets: HKV[] = [
    { key: 'Accept', value: 'application/json', desc: 'answer in JSON' },
    { key: 'Accept', value: 'application/yang-data+json', desc: 'RESTCONF, JSON' },
    { key: 'Accept', value: 'application/yang-data+xml', desc: 'RESTCONF, XML' },
    { key: 'Accept', value: 'application/xml', desc: 'answer in XML' },
    { key: 'Content-Type', value: 'application/json', desc: 'JSON body' },
    { key: 'Content-Type', value: 'application/yang-data+json', desc: 'RESTCONF body' },
    { key: 'Cache-Control', value: 'no-cache', desc: 'no cached answer' },
    { key: 'Accept-Encoding', value: 'gzip, deflate', desc: 'compressed answer' },
    { key: 'Accept-Language', value: 'en-US', desc: 'language' },
    { key: 'X-Request-Id', value: '{{$guid}}', desc: 'a new id each time' },
    { key: 'If-None-Match', value: '"etag"', desc: 'only if changed' },
    { key: 'If-Match', value: '"etag"', desc: 'only if not changed' },
    { key: 'Connection', value: 'close', desc: 'no keep-alive' },
  ];
  /** Header names not used yet in this request (the row's own stays), with what each is for. */
  keyOpts(cur: string): ComboOption[] {
    const used = new Set((this.draft().req.headers ?? []).map(h => h.key.toLowerCase()));
    return HTTP_HEADERS.filter(h => !used.has(h.key.toLowerCase()) || h.key.toLowerCase() === cur.toLowerCase()).map(h => ({ value: h.key, hint: h.desc }));
  }
  valueOpts(key: string): ComboOption[] { return headerValues(key).map(v => ({ value: v })); }
  hasHeader(k: string, v?: string) { return (this.draft().req.headers ?? []).some(h => !h.off && h.key.toLowerCase() === k.toLowerCase() && (v === undefined || h.value === v)); }
  addPreset(i: number) {
    const p = this.presets[i];
    const hs = (this.draft().req.headers ?? []).filter(h => h.key.trim());
    const same = hs.find(h => h.key.toLowerCase() === p.key.toLowerCase());
    if (same) same.value = p.value; else hs.push({ key: p.key, value: p.value });
    this.setReq({ headers: hs });
  }
  hostOf() {
    const u = this.draft().req.url.trim(), base = this.sess()?.apiBaseUrl ?? '';
    try { return new URL(/^https?:/i.test(u) ? u : base).host; } catch { return ''; }
  }
  bodyType() { const m = this.draft().req.body?.mode; return ({ json: 'application/json', text: 'text/plain', xml: 'application/xml', urlencoded: 'application/x-www-form-urlencoded', form: 'multipart/form-data', graphql: 'application/json' } as Record<string, string>)[m ?? ''] ?? ''; }
  readonly commonHeaders = ['Accept', 'Content-Type', 'Authorization', 'Cache-Control', 'If-Match', 'If-None-Match', 'User-Agent', 'X-Request-Id', 'X-Auth-Token', 'X-API-Key'];

  filter = '';
  readonly path = signal('');
  readonly sess = signal<Session | null>(null);
  readonly env = signal('');
  readonly token = signal<{ held: boolean; expires?: string } | null>(null);
  readonly apiFilter = (n: SessionNode) => n.protocol === 'API';
  readonly sessions = computed(() => flat(this.ui.tree()).filter(this.apiFilter));
  readonly colls = signal<HCollection[]>([]);
  readonly opened = signal<Set<string>>(new Set());
  readonly draft = signal<Draft>(mkDraft(newReq()));
  /** The open request tabs; the active one's live copy is draft(). */
  readonly openTabs = signal<Draft[]>([this.draft()]);
  readonly active = signal(0);
  readonly scriptTab = signal<'pre' | 'post'>('post');
  readonly showHidden = signal(false);
  readonly bulk = signal<'' | 'params' | 'headers'>('');
  bulkText = '';
  readonly varsOpen = signal(false);
  readonly varList = signal<HVar[]>([]);
  readonly known = computed(() => new Set(this.varList().filter(v => v.value !== '' || v.secret).map(v => v.name)));
  readonly envList = signal<HEnv[]>([]);
  readonly envId = signal(readEnv());
  readonly envName = computed(() => this.envList().find(e => e.id === this.envId())?.name ?? '');
  readonly envsOpen = signal(false);
  readonly envEdit = signal<HEnv | null>(null);
  readonly reveal = signal<Set<number>>(new Set());
  envImportText = '';
  readonly cookiesOpen = signal(false);
  readonly cookieList = signal<HHostCookies[]>([]);
  readonly collEdit = signal<HCollection | null>(null);
  readonly collTab = signal('overview');
  readonly consoleOpen = signal(false);
  readonly consoleLog = signal<ConsoleEntry[]>([]);
  readonly reqTab = signal('params');
  readonly resTab = signal('body');
  readonly res = signal<HResponse | null>(null);
  readonly busy = signal(false);
  readonly pretty = signal(true);
  /** The answer's body in plain black / white instead of colours (kept for this viewer). */
  readonly mono = signal(readMono());
  toggleMono() { this.mono.update(v => !v); try { localStorage.setItem('tw.api.mono', this.mono() ? '1' : ''); } catch { /* not kept */ } }
  readonly history = signal<HHistoryItem[]>([]);
  readonly lang = signal('curl');
  readonly code = signal('');
  readonly renaming = signal('');
  renameText = '';
  /** Variables set by scripts in this tab (sent with the next requests). */
  private vars: Record<string, string> = {};

  readonly importOpen = signal(false);
  importText = ''; importName = ''; importUseSession = true;
  readonly importErr = signal('');
  readonly saveOpen = signal(false);
  saveName = ''; saveColl = ''; saveNewColl = ''; saveFolder = '';
  readonly runColl = signal<HCollection | null>(null);
  runEnvId = '';
  runFolder = ''; runSession = ''; runEnv = ''; runIter = 1; runDelay = 0; runStop = false; runData = '';
  readonly running = signal(false);
  readonly runSteps = signal<HRunStep[]>([]);
  readonly runRes = signal<HRunResult | null>(null);
  readonly runErr = signal('');

  readonly prod = computed(() => this.env() === 'production' || this.sess()?.environment === 'production');
  readonly failed = computed(() => (this.res()?.tests ?? []).filter(t => !t.pass).length);
  readonly jsonHtml = computed(() => { const r = this.res(); return r ? jsonHtml(r.body) : ''; });
  private offRun: (() => void) | null = null;
  private offMon: (() => void) | null = null;

  constructor() {
    effect(() => { if (this.ui.apiImportAsked() && this.visible()) { this.importOpen.set(true); this.ui.apiImportAsked.set(0); } });
  }

  async ngOnInit() {
    void this.loadExtras();
    if (!this.ui.tree()) this.ui.tree.set(await this.backend.getSessionTree().catch(() => null));
    const p = this.tab().apiSession || '';
    if (p) await this.choose(p);
    else if (this.sessions().length === 1) await this.choose(this.sessions()[0].path);
    await this.loadColls();
    await this.loadEnvs();
    this.history.set(await this.backend.httpHistory().catch(() => []));
    void this.refreshVars();
    this.offMon = this.backend.on('api:monitor', () => void this.loadExtras());
    this.offRun = this.backend.on('http:run', (st: HRunStep) => { if (this.running()) this.runSteps.update(l => [...l, st]); });
  }
  ngOnDestroy() { this.offRun?.(); this.offMon?.(); }

  async choose(p: string) {
    this.path.set(p);
    this.sess.set(p ? await this.backend.getSession(p).catch(() => null) : null);
    this.vars = {};
    void this.loadToken();
    void this.refreshVars();
  }
  async loadToken() { this.token.set(this.path() ? await this.backend.httpToken(this.path(), this.env()).catch(() => null) : null); }
  sessEnvName() { const e = this.sess()?.environment; return e ? e.charAt(0).toUpperCase() + e.slice(1) : 'Session'; }
  authName(a: string) { return AUTH_NAMES[a] ?? a; }
  authHeader() { const s = this.sess(); if (!s) return ''; return s.apiAuth === 'apikey' ? (s.apiKeyIn === 'query' ? '?' : '') + (s.apiKeyName || 'X-API-Key') : s.apiAuth === 'login' && s.apiKeyName ? s.apiKeyName : 'Authorization'; }
  editSession() { if (this.path()) void this.ui.editSession(this.path(), undefined, true).then(() => this.choose(this.path())); }

  async loadColls() {
    const list = await this.backend.httpCollections().catch(() => []);
    const full = await Promise.all(list.map(c => this.backend.httpCollection(c.id).catch(() => null)));
    this.colls.set(full.filter((c): c is HCollection => !!c));
  }
  collName(id: string) { return this.colls().find(c => c.id === id)?.name ?? id; }
  isOpen(k: string) { return this.opened().has(k) || !!this.filter.trim(); }
  toggle(k: string) { const s = new Set(this.opened()); if (s.has(k)) s.delete(k); else s.add(k); this.opened.set(s); }

  /** A collection's requests under their folders (folders first), filtered. */
  rows(c: HCollection): Row[] {
    const q = this.filter.trim().toLowerCase();
    const reqs = c.requests.filter(r => !q || (r.name + ' ' + r.url + ' ' + r.method + ' ' + (r.folder ?? '')).toLowerCase().includes(q));
    const out: Row[] = [];
    const walk = (prefix: string, depth: number) => {
      const subs = new Set<string>();
      for (const r of reqs) {
        const f = r.folder ?? '';
        if (prefix ? f.startsWith(prefix + '/') : f !== '') subs.add((prefix ? prefix + '/' : '') + f.slice(prefix ? prefix.length + 1 : 0).split('/')[0]);
      }
      for (const f of [...subs].sort()) {
        const open = this.isOpen('f:' + c.id + ':' + f);
        out.push({ kind: 'folder', depth, folder: f, name: f.split('/').pop(), open, coll: c.id });
        if (open) walk(f, depth + 1);
      }
      for (const r of reqs) if ((r.folder ?? '') === prefix) out.push({ kind: 'req', depth, req: r, coll: c.id });
    };
    walk('', 0);
    return out;
  }

  openReq(c: HCollection, r: HRequest) {
    const i = this.openTabs().findIndex((t, j) => { const d = j === this.active() ? this.draft() : t; return d.coll === c.id && d.id === r.id; });
    if (i >= 0) { this.switchTab(i); return; }
    this.openDraft(mkDraft(withQuery(structuredClone(r)), c.id, r.id ?? ''));
    if (c.session && c.session !== this.path() && !this.path()) void this.choose(c.session);
    else void this.refreshVars();
    if (this.reqTab() === 'code') void this.loadCode();
  }
  openHist(h: HHistoryItem) {
    this.openDraft(mkDraft(withQuery(structuredClone(h.request)), '', '', true));
    if (h.session && h.session !== this.path()) void this.choose(h.session);
    if (h.env !== undefined) this.env.set(h.env ?? '');
  }
  /** Opens a draft in a new tab (or in the active one when it is an empty, untouched request). */
  private openDraft(d: Draft) {
    const cur = this.draft();
    if (!cur.dirty && !cur.coll && !cur.req.url.trim()) {
      this.draft.set(d); this.res.set(null);
      this.openTabs.update(l => l.map((t, i) => i === this.active() ? d : t));
      return;
    }
    this.stash();
    this.openTabs.update(l => [...l, d]);
    this.active.set(this.openTabs().length - 1);
    this.draft.set(d); this.res.set(null);
  }
  private stash() { const a = this.active(), d = { ...this.draft(), res: this.res() }; this.openTabs.update(l => l.map((t, i) => i === a ? d : t)); }
  switchTab(i: number) {
    if (i === this.active()) return;
    this.stash();
    const d = this.openTabs()[i];
    this.active.set(i); this.draft.set(d); this.res.set(d.res ?? null); this.bulk.set('');
    void this.refreshVars();
    if (this.reqTab() === 'code') void this.loadCode();
  }
  newTab() { this.stash(); const d = mkDraft(newReq()); this.openTabs.update(l => [...l, d]); this.active.set(this.openTabs().length - 1); this.draft.set(d); this.res.set(null); }
  async closeTab(i: number, ev?: Event) {
    ev?.stopPropagation();
    const d = i === this.active() ? this.draft() : this.openTabs()[i];
    if (d.dirty && (d.req.url.trim() || d.coll) && !(await this.ui.confirm(tr('Close Request'), tr('{0} has changes that are not saved. Close it anyway?', d.req.name || d.req.url || tr('This request')), tr('Close'), true))) return;
    this.stash();
    const l = this.openTabs().filter((_, j) => j !== i);
    if (!l.length) l.push(mkDraft(newReq()));
    const a = Math.min(i <= this.active() ? Math.max(0, this.active() - (i < this.active() ? 1 : 0)) : this.active(), l.length - 1);
    this.openTabs.set(l); this.active.set(a); this.draft.set(l[a]); this.res.set(l[a].res ?? null);
  }
  setReq(p: Partial<HRequest>) { this.draft.update(d => ({ ...d, req: { ...d.req, ...p }, dirty: true })); }
  /** The URL changed: a pasted cURL command becomes the request; the query goes to Params, :names to Path Variables. */
  setUrl(u: string) {
    if (/^\s*curl\s/i.test(u)) { void this.pasteCurl(u); return; }
    const d = this.draft().req, q = u.indexOf('?');
    let params = d.params ?? [];
    if (q >= 0) {
      const kept = params.filter(p => p.off);
      params = [...u.slice(q + 1).split('&').filter(x => x).map(x => { const i = x.indexOf('='); return { key: i < 0 ? x : x.slice(0, i), value: i < 0 ? '' : x.slice(i + 1) }; }), ...kept];
    } else if ((d.url ?? '').includes('?')) params = params.filter(p => p.off);
    const names = pathNames(u), old = new Map((d.pathVars ?? []).map(p => [p.key, p]));
    const pathVars = names.map(n => old.get(n) ?? { key: n, value: '' });
    this.setReq({ url: q >= 0 ? u.slice(0, q) + '?' + u.slice(q + 1) : u, params, pathVars });
  }
  private async pasteCurl(text: string) {
    try {
      const r = withQuery(await this.backend.httpParseCurl(text));
      this.setReq({ ...r, id: undefined, name: this.draft().req.name || r.name, pathVars: pathNames(r.url).map(k => ({ key: k, value: '' })) });
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  /** Params changed: rebuild the URL's query. */
  urlFromParams() {
    const d = this.draft().req, base = d.url.split('?')[0];
    const q = (d.params ?? []).filter(p => !p.off && p.key).map(p => p.key + (p.value !== '' ? '=' + p.value : '')).join('&');
    this.setReq({ url: q ? base + '?' + q : base });
  }
  setBody(p: Partial<NonNullable<HRequest['body']>>) {
    this.draft.update(d => {
      const body = { ...(d.req.body ?? {}), ...p };
      if ((p.mode === 'urlencoded' || p.mode === 'form') && !body.form?.length) body.form = [{ key: '', value: '' }];
      const req = { ...d.req, body };
      if (p.mode && p.mode !== '' && d.req.method === 'GET') req.method = 'POST';
      return { ...d, req, dirty: true };
    });
  }
  touch() { this.draft.update(d => ({ ...d, dirty: true })); }
  add(k: 'params' | 'headers') { this.draft.update(d => ({ ...d, req: { ...d.req, [k]: [...(d.req[k] ?? []), { key: '', value: '' }] }, dirty: true })); }
  addForm() { this.setBody({ form: [...(this.draft().req.body?.form ?? []), { key: '', value: '' }] }); }
  del(l: HKV[], i: number) { l.splice(i, 1); this.draft.update(d => ({ ...d, req: { ...d.req }, dirty: true })); }
  count(k: string): number {
    const r = this.draft().req;
    if (k === 'params') return (r.params ?? []).filter(p => p.key && !p.off).length;
    if (k === 'headers') return (r.headers ?? []).filter(p => p.key && !p.off).length + this.autoHeaders().length;
    return 0;
  }
  dot(k: string): boolean {
    const r = this.draft().req;
    if (k === 'auth') return !!r.auth;
    if (k === 'body') return !!r.body?.mode;
    if (k === 'scripts') return !!(r.tests?.trim() || r.preScript?.trim());
    if (k === 'docs') return !!r.description?.trim();
    if (k === 'settings') return !!r.settings && Object.values(r.settings).some(v => v !== undefined && v !== '' && v !== false);
    return false;
  }
  /** Headers Termwright adds when it sends (Postman's hidden headers). */
  autoHeaders(): HKV[] {
    const out: HKV[] = [], r = this.draft().req, s = this.sess();
    const mode = r.auth || this.collAuth();
    if (mode === 'bearer' || mode === 'basic') out.push({ key: 'Authorization', value: mode === 'bearer' ? 'Bearer ••••••••' : 'Basic ••••••••', desc: 'from Authorization' });
    else if (mode === 'apikey') { const a = r.auth ? r.authData : this.collAuthData(); if (a?.in !== 'query') out.push({ key: a?.key || 'API key', value: '••••••••', desc: 'from Authorization' }); }
    else if (mode !== 'none' && s?.apiAuth) out.push({ key: this.authHeader(), value: '••••••••', desc: '🔒 the session\'s authentication' });
    const ct = this.bodyType();
    if (ct && !this.hasHeader('Content-Type')) out.push({ key: 'Content-Type', value: ct, desc: 'from the Body' });
    if (!this.hasHeader('Host')) out.push({ key: 'Host', value: this.hostOf() || '(from the URL)', desc: 'from the URL' });
    if (!this.hasHeader('User-Agent')) out.push({ key: 'User-Agent', value: 'Termwright', desc: 'added by Termwright' });
    if (!this.hasHeader('Accept-Encoding')) out.push({ key: 'Accept-Encoding', value: 'gzip', desc: 'added by Termwright' });
    if (!this.hasHeader('Connection')) out.push({ key: 'Connection', value: 'keep-alive', desc: 'added by Termwright' });
    return out;
  }
  private coll() { return this.colls().find(c => c.id === this.draft().coll); }
  collAuth() { return this.coll()?.auth ?? ''; }
  collAuthData() { return this.coll()?.authData; }
  collScript() { const c = this.coll(); return this.scriptTab() === 'pre' ? !!c?.preScript?.trim() : !!c?.tests?.trim(); }
  inheritText() {
    const c = this.coll(), name = (v: string) => AUTH_TYPES.find(t => t.v === v)?.l ?? v;
    if (c?.auth) return tr('From the collection {0}: {1}.', c.name, c.auth === 'none' ? 'No Auth' : name(c.auth));
    const s = this.sess();
    if (s) return s.apiAuth ? tr('From the API session {0}: {1} (its secrets are in the credential store).', s.name, this.authName(s.apiAuth)) : tr('The API session {0} has no authentication.', s.name);
    return tr('No session and no collection authorization: nothing is sent.');
  }
  // ---- body ----
  bodyKind() { const m = this.draft().req.body?.mode ?? ''; return RAW_LANGS.some(l => l.v === m) ? 'raw' : m; }
  setKind(k: string) { this.setBody({ mode: k === 'raw' ? (RAW_LANGS.some(l => l.v === this.draft().req.body?.mode) ? this.draft().req.body!.mode : 'json') : k }); }
  rawLabel() { return RAW_LANGS.find(l => l.v === this.draft().req.body?.mode)?.l ?? 'JSON'; }
  langMenu(ev: MouseEvent) { this.ui.showMenu(ev, RAW_LANGS.map(l => ({ label: (l.v === this.draft().req.body?.mode ? '✓ ' : '') + l.l, icon: '', action: () => this.setBody({ mode: l.v }) }))); }
  async pickFile(p: HKV) { const f = await this.backend.chooseOpenFile(tr('File to send')).catch(() => ''); if (f) { p.value = f; this.touch(); } }
  async pickBinary() { const f = await this.backend.chooseOpenFile(tr('File to send')).catch(() => ''); if (f) this.setBody({ file: f }); }
  // ---- settings ----
  st(): HSettings { return this.draft().req.settings ?? {}; }
  isOn(o: { k: keyof HSettings; inv?: boolean }) { const v = !!this.st()[o.k]; return o.inv ? !v : v; }
  flip(o: { k: keyof HSettings; inv?: boolean }, on: boolean) { this.setSt({ [o.k]: o.inv ? !on : on } as Partial<HSettings>); }
  setSt(p: Partial<HSettings>) {
    const n: HSettings = { ...this.st(), ...p };
    for (const k of Object.keys(n) as (keyof HSettings)[]) if (n[k] === undefined || n[k] === '' || n[k] === false || n[k] === 0) delete n[k];
    this.setReq({ settings: Object.keys(n).length ? n : undefined });
  }
  // ---- bulk edit ----
  toggleBulk(k: 'params' | 'headers') {
    if (this.bulk() === k) { this.bulk.set(''); return; }
    this.bulkText = (this.draft().req[k] ?? []).filter(p => p.key).map(p => (p.off ? '//' : '') + p.key + ':' + p.value).join('\n');
    this.bulk.set(k);
  }
  bulkApply(k: 'params' | 'headers') {
    const l: HKV[] = this.bulkText.split('\n').map(x => x.trim()).filter(x => x && x !== '//').map(x => {
      const off = x.startsWith('//'); if (off) x = x.slice(2);
      const i = x.indexOf(':');
      return { key: (i < 0 ? x : x.slice(0, i)).trim(), value: i < 0 ? '' : x.slice(i + 1).trim(), off: off || undefined };
    });
    this.setReq({ [k]: l });
    if (k === 'params') this.urlFromParams();
  }
  // ---- header presets ----
  presetMenu(ev: MouseEvent) {
    const saved = readPresets();
    this.ui.showMenu(ev, [
      ...this.presets.map((h, i) => ({ label: h.key + ': ' + h.value, hint: h.desc, icon: '', disabled: this.hasHeader(h.key, h.value), action: () => this.addPreset(i) })),
      ...(saved.length ? [{ separator: true }, ...saved.map(p => ({ label: tr('Preset: {0}', p.name), hint: p.headers.length + ' headers', icon: '', action: () => this.applyPreset(p.headers) }))] : []),
      { separator: true },
      { label: 'Save These Headers as a Preset…', icon: 'save', disabled: !(this.draft().req.headers ?? []).some(h => h.key), action: () => void this.savePreset() },
      ...saved.map(p => ({ label: tr('Remove Preset: {0}', p.name), icon: 'trash', action: () => writePresets(readPresets().filter(x => x.name !== p.name)) })),
    ]);
  }
  private applyPreset(hs: HKV[]) {
    const cur = (this.draft().req.headers ?? []).filter(h => h.key.trim());
    for (const h of hs) { const same = cur.find(x => x.key.toLowerCase() === h.key.toLowerCase()); if (same) same.value = h.value; else cur.push({ ...h }); }
    this.setReq({ headers: cur });
  }
  private async savePreset() {
    const name = (await this.ui.input(tr('Save Header Preset'), tr('Name'), ''))?.trim();
    if (!name) return;
    writePresets([...readPresets().filter(p => p.name !== name), { name, headers: (this.draft().req.headers ?? []).filter(h => h.key).map(h => ({ key: h.key, value: h.value })) }]);
    this.ui.toast(tr('Saved the preset {0}', name), 'success');
  }
  // ---- menus ----
  sendMenu(ev: MouseEvent) { this.ui.showMenu(ev, [{ label: 'Send', icon: 'play', action: () => void this.send() }, { label: 'Send and Download', icon: 'download', action: () => void this.send(true) }]); }
  saveMenu(ev: MouseEvent) {
    this.ui.showMenu(ev, [{ label: 'Save', icon: 'save', action: () => void this.save() }, { label: 'Save As…', icon: 'save', action: () => this.askSave() },
      { separator: true }, { label: 'Duplicate in a New Tab', icon: 'copy', action: () => this.openDraft(mkDraft(structuredClone({ ...this.draft().req, id: undefined, name: (this.draft().req.name || '') + ' copy' }), '', '', true)) }]);
  }
  snippetMenu(ev: MouseEvent) {
    const pre = this.scriptTab() === 'pre';
    this.ui.showMenu(ev, (pre ? PRE_SNIPPETS : SNIPPETS).map(sn => ({ label: sn.l, icon: '', action: () => {
      const cur = (pre ? this.draft().req.preScript : this.draft().req.tests)?.trim();
      const code = (cur ? cur + '\n' : '') + sn.code;
      if (pre) this.setReq({ preScript: code }); else this.setReq({ tests: code });
    } })));
  }
  resMenu(ev: MouseEvent) {
    const r = this.res(); if (!r) return;
    this.ui.showMenu(ev, [
      { label: 'Save as Example…', icon: 'save', disabled: !!r.base64, action: () => void this.saveExample(r) },
      { label: 'Save Response to File…', icon: 'save', action: () => void this.saveRes(r) },
      { label: 'Copy Body', icon: 'copy', disabled: !!r.base64, action: () => this.copy(r.body) },
      { label: 'Copy as cURL', icon: 'copy', action: () => void this.backend.httpCode(this.sendReq(), 'curl').then(c => this.copy(c)).catch(() => {}) },
      { separator: true },
      { label: 'Clear Response', icon: 'clear', action: () => this.res.set(null) },
    ]);
  }
  private async saveRes(r: HResponse) {
    const ext = /json/i.test(r.contentType ?? '') ? '.json' : /xml/i.test(r.contentType ?? '') ? '.xml' : /html/i.test(r.contentType ?? '') ? '.html' : r.base64 ? '' : '.txt';
    try { const p = await this.backend.httpSaveResponse(r.body, !!r.base64, (this.draft().req.name || 'response').replace(/[^\w.-]+/g, '-') + ext); if (p) this.ui.toast(tr('Saved {0}', p), 'success'); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  // ---- variables ----
  async refreshVars() {
    try { this.varList.set(await this.backend.httpVariables(this.sendReq())); } catch { this.varList.set([]); }
  }
  varOf(n: string) { return this.varList().find(v => v.name === n); }
  /** {{names}} the request uses (URL, params, path variables, headers, body, authorization). */
  usedVars(): string[] {
    const r = this.draft().req, a = (r.auth ? r.authData : this.collAuthData()) ?? {};
    const text = [r.url, ...(r.params ?? []).flatMap(p => [p.key, p.value]), ...(r.pathVars ?? []).map(p => p.value), ...(r.headers ?? []).flatMap(h => [h.key, h.value]),
      r.body?.raw, r.body?.graphqlVars, r.body?.file, ...(r.body?.form ?? []).flatMap(f => [f.key, f.value]), a.token, a.user, a.password, a.key, a.value].join(' ');
    const out = new Set<string>();
    for (const m of text.matchAll(/\{\{\s*([^{}]+?)\s*\}\}/g)) if (!m[1].startsWith('$')) out.add(m[1]);
    return [...out];
  }
  unresolved() { const k = this.known(); return this.usedVars().filter(n => !k.has(n)); }
  varGroups() {
    const order = ['local', 'environment', 'session', 'collection', 'globals'];
    return order.map(src => ({ src, vars: this.varList().filter(v => v.source === src) })).filter(g => g.vars.length);
  }
  srcName(s: string) { return ({ local: 'Local (set by scripts in this tab)', environment: 'Environment: ' + this.envName(), session: 'API session: ' + (this.sess()?.name ?? ''), collection: 'Collection: ' + this.collName(this.draft().coll), globals: 'Globals' } as Record<string, string>)[s] ?? s; }
  setLocal(n: string, v: string) { if (v === '') return; this.vars[n] = v; void this.refreshVars(); }
  // ---- environments ----
  async loadEnvs() {
    const l = await this.backend.httpEnvironments().catch(() => [] as HEnv[]);
    this.envs.set(l); this.envList.set(l.filter(e => e.id !== 'globals'));
    if (this.envId() && !this.envList().some(e => e.id === this.envId())) this.useEnv('');
  }
  readonly envs = signal<HEnv[]>([]);
  envMenu(ev: MouseEvent) {
    this.ui.showMenu(ev, [
      { label: (this.envId() ? '' : '✓ ') + 'No Environment', icon: '', action: () => this.useEnv('') },
      ...this.envList().map(e => ({ label: (e.id === this.envId() ? '✓ ' : '') + e.name, hint: e.vars.length + ' variables', icon: '', action: () => this.useEnv(e.id) })),
      { separator: true },
      { label: 'Manage Environments…', icon: 'settings', action: () => this.openEnvs() },
    ]);
  }
  useEnv(id: string) { this.envId.set(id); try { localStorage.setItem('tw.api.environment', id); } catch { /* not kept */ } void this.refreshVars(); }
  openEnvs() {
    void this.loadEnvs().then(() => { const cur = this.envs().find(e => e.id === this.envId()) ?? this.envs()[0]; if (cur) this.editEnv(cur); });
    this.envsOpen.set(true);
  }
  editEnv(e: HEnv) { this.envEdit.set(structuredClone(e)); this.reveal.set(new Set()); }
  newEnv() { this.envEdit.set({ id: '', name: tr('New Environment'), vars: [{ key: '', value: '' }] }); }
  toggleReveal(i: number) { const s = new Set(this.reveal()); if (s.has(i)) s.delete(i); else s.add(i); this.reveal.set(s); }
  async saveEnv() {
    const e = this.envEdit(); if (!e) return;
    try { const saved = await this.backend.httpSaveEnvironment({ ...e, vars: e.vars.filter(v => v.key.trim()) }); await this.loadEnvs(); this.editEnv(saved); if (!this.envId() && saved.id !== 'globals' && !e.id) this.useEnv(saved.id); else void this.refreshVars(); this.ui.toast(tr('Saved {0}', saved.name), 'success'); }
    catch (err) { this.ui.toast(errText(err), 'error'); }
  }
  async deleteEnv(e: HEnv) {
    if (!(await this.ui.confirm(e.id === 'globals' ? tr('Clear Globals') : tr('Delete Environment'), e.id === 'globals' ? tr('Remove every global variable?') : tr('Delete the environment {0} and its secret values?', e.name), e.id === 'globals' ? tr('Clear') : tr('Delete'), true))) return;
    try { await this.backend.httpDeleteEnvironment(e.id); if (this.envId() === e.id) this.useEnv(''); await this.loadEnvs(); this.envEdit.set(null); } catch (err) { this.ui.toast(errText(err), 'error'); }
  }
  async importEnvFile() { try { const e = await this.backend.httpImportEnvironmentFile(); if (e?.id) this.envImported(e); } catch (err) { this.ui.toast(errText(err), 'error'); } }
  async importEnvText() { try { this.envImported(await this.backend.httpImportEnvironment(this.envImportText, '')); this.envImportText = ''; } catch (err) { this.ui.toast(errText(err), 'error'); } }
  private envImported(e: HEnv) { void this.loadEnvs().then(() => this.editEnv(e)); this.ui.toast(tr('Imported the environment {0} ({1} variables)', e.name, e.vars.length), 'success'); }
  async exportEnv(id: string) { try { const p = await this.backend.httpExportEnvironmentFile(id); if (p) this.ui.toast(tr('Saved {0}', p), 'success'); } catch (e) { this.ui.toast(errText(e), 'error'); } }
  // ---- cookies ----
  async openCookies() { this.cookieList.set(await this.backend.httpCookies(this.path()).catch(() => [])); this.cookiesOpen.set(true); }
  async delCookie(host: string, name: string) { await this.backend.httpDeleteCookie(this.path(), host, name).catch(() => {}); this.cookieList.set(await this.backend.httpCookies(this.path()).catch(() => [])); }
  // ---- collection editor ----
  editColl(c: HCollection, tab = 'overview') { this.collEdit.set(structuredClone(c)); this.collTab.set(tab); }
  async saveCollEdit() {
    const c = this.collEdit(); if (!c) return;
    if (await this.store({ ...c, vars: (c.vars ?? []).filter(v => v.key.trim()) })) { this.collEdit.set(null); void this.refreshVars(); }
  }
  resCount(k: string): number | string {
    const r = this.res();
    if (!r) return 0;
    if (k === 'headers') return r.headers.length;
    if (k === 'cookies') return r.cookies?.length ?? 0;
    if (k === 'tests') return r.tests?.length ? `${r.tests.length - this.failed()}/${r.tests.length}` : 0;
    return 0;
  }
  beautify() { try { this.setBody({ raw: JSON.stringify(JSON.parse(this.draft().req.body?.raw ?? ''), null, 2) }); } catch (e) { this.ui.toast(tr('The body is not JSON: {0}', errText(e)), 'error'); } }
  snippet(i: number) { const t = this.draft().req.tests?.trim(); this.setReq({ tests: (t ? t + '\n' : '') + SNIPPETS[i].code }); }

  private sendReq(): HSendRequest {
    const d = this.draft();
    return { session: this.path(), env: this.env(), collection: d.coll || undefined, environment: this.envId() || undefined, request: wire(d.req), vars: this.vars };
  }

  async send(download = false) {
    if (this.busy() || !this.draft().req.url.trim()) return;
    this.busy.set(true);
    const r = this.sendReq();
    try {
      let res: HResponse;
      try {
        res = await this.backend.httpSend(r);
      } catch (e) {
        if (!/production session/i.test(errText(e))) throw e;
        const ok = await this.ui.confirm(tr('Change a Production API?'), tr('{0} {1} with the Production session {2}.', r.request.method, r.request.url, this.path()), tr('Send'), true);
        if (!ok) { this.busy.set(false); return; }
        res = await this.backend.httpSend({ ...r, confirmed: true });
      }
      this.res.set(res);
      this.log(res);
      if (this.rec.recording()) {
        const q = r.request, h: Record<string, string> = {};
        for (const x of q.headers ?? []) if (!x.off && x.key) h[x.key] = x.value;
        this.rec.http({ session: r.session || undefined, environment: this.envName() || undefined, collection: r.collection, method: q.method, url: q.url, headers: Object.keys(h).length ? h : undefined, body: q.body?.raw || undefined }, res.status, res.ms, res.body);
      }
      Object.assign(this.vars, res.vars ?? {});
      if (res.envSet || res.globalSet || res.vars) { void this.loadEnvs(); void this.refreshVars(); }
      if (download && res.status) void this.saveRes(res);
      if (res.tests?.length && this.resTab() === 'body' && res.tests.some(t => !t.pass)) this.resTab.set('tests');
      this.history.set(await this.backend.httpHistory().catch(() => this.history()));
      void this.loadToken();
    } catch (e) {
      this.res.set(null);
      this.ui.toast(errText(e), 'error');
    }
    this.busy.set(false);
  }

  private log(r: HResponse) {
    const e: ConsoleEntry = { at: new Date().toLocaleTimeString(), method: r.method, url: r.url, status: r.status, ms: r.ms, error: r.error, logs: r.logs ?? [], reqHeaders: r.reqHeaders ?? [], reqBody: r.reqBody, resHeaders: r.headers ?? [] };
    this.consoleLog.update(l => [e, ...l].slice(0, 300));
  }

  async loadCode() {
    try { this.code.set(await this.backend.httpCode(this.sendReq(), this.lang())); } catch (e) { this.code.set(errText(e)); }
  }

  async save() {
    const d = this.draft();
    const c = this.colls().find(x => x.id === d.coll);
    if (c && d.id) {
      const reqs = c.requests.map(r => r.id === d.id ? { ...wire(d.req), id: d.id } : r);
      await this.store({ ...c, requests: reqs });
      this.draft.update(x => ({ ...x, dirty: false }));
      return;
    }
    this.askSave();
  }
  askSave() {
    const d = this.draft();
    this.saveName = d.req.name || (d.req.method + ' ' + shortPath(d.req.url));
    this.saveColl = this.colls()[0]?.id ?? '';
    this.saveNewColl = ''; this.saveFolder = d.req.folder ?? '';
    this.saveOpen.set(true);
  }
  async saveAs() {
    let c = this.colls().find(x => x.id === this.saveColl);
    const req: HRequest = { ...wire(this.draft().req), id: '', name: this.saveName.trim(), folder: this.saveFolder.trim() };
    c = c ? { ...c, requests: [...c.requests, req] } : { id: '', name: this.saveNewColl.trim(), session: this.path() || undefined, requests: [req] };
    const saved = await this.store(c);
    if (saved) {
      const r = saved.requests[saved.requests.length - 1];
      this.draft.update(d => ({ ...d, req: { ...d.req, id: r.id, name: r.name, folder: r.folder }, coll: saved.id, id: r.id ?? '', dirty: false }));
      this.opened.update(s => new Set([...s, 'c:' + saved.id]));
    }
    this.saveOpen.set(false);
  }
  private async store(c: HCollection): Promise<HCollection | null> {
    try {
      const saved = await this.backend.httpSaveCollection(c);
      this.colls.update(l => { const i = l.findIndex(x => x.id === saved.id); return i < 0 ? [...l, saved].sort((a, b) => a.name.localeCompare(b.name)) : l.map(x => x.id === saved.id ? saved : x); });
      return saved;
    } catch (e) { this.ui.toast(errText(e), 'error'); return null; }
  }
  async newCollection() {
    const saved = await this.store({ id: '', name: tr('New collection'), session: this.path() || undefined, requests: [] });
    if (saved) { this.renameText = saved.name; this.renaming.set('c:' + saved.id); }
  }
  async renameColl(c: HCollection) {
    if (this.renaming() !== 'c:' + c.id) return;
    this.renaming.set('');
    if (this.renameText.trim() && this.renameText.trim() !== c.name) await this.store({ ...c, name: this.renameText.trim() });
  }
  async renameReq(c: HCollection, r: HRequest) {
    if (this.renaming() !== 'r:' + c.id + ':' + r.id) return;
    this.renaming.set('');
    const name = this.renameText.trim();
    if (name && name !== r.name) {
      await this.store({ ...c, requests: c.requests.map(x => x.id === r.id ? { ...x, name } : x) });
      if (this.draft().id === r.id && this.draft().coll === c.id) this.draft.update(d => ({ ...d, req: { ...d.req, name } }));
    }
  }

  readonly mocks = signal<HMockInfo[]>([]);
  readonly monitors = signal<APIMonitorState[]>([]);
  async loadExtras() {
    this.mocks.set((await this.backend.httpMocks().catch(() => [])) ?? []);
    this.monitors.set((await this.backend.apiMonitors().catch(() => [])) ?? []);
  }
  async saveExample(r: HResponse) {
    const name = await this.ui.input(tr('Save as Example'), tr('A name for this answer (the mock server answers with the first example, or the one named in the x-mock-example header)'), r.status >= 400 ? 'Error ' + r.status : 'OK');
    if (!name) return;
    const headers = (r.headers ?? []).filter(h => /^content-type$/i.test(h.key)).map(h => ({ key: h.key, value: h.value }));
    this.setReq({ examples: [...(this.draft().req.examples ?? []), { name, status: r.status, headers, body: r.body }] });
    this.ui.toast(tr('Example added - Save the request to keep it'), 'success');
  }
  delExample(i: number) { this.setReq({ examples: (this.draft().req.examples ?? []).filter((_, j) => j !== i) }); }
  async toggleMock(c: HCollection) {
    try {
      if (this.mocks().some(m => m.collection === c.id)) await this.backend.httpMockStop(c.id);
      else { const m = await this.backend.httpMockStart(c.id, 0); this.copy('http://127.0.0.1:' + m.port); this.ui.toast(tr('Mock server on http://127.0.0.1:{0} - address copied', m.port), 'success'); }
    } catch (e) { this.ui.toast(errText(e), 'error'); }
    await this.loadExtras();
  }
  mockMenu(ev: MouseEvent, m: HMockInfo) {
    this.ui.showMenu(ev, [
      { label: 'Copy Address (http://127.0.0.1:' + m.port + ')', icon: 'copy', action: () => this.copy('http://127.0.0.1:' + m.port) },
      { label: 'Stop the Mock Server', action: () => void this.stopMock(m.collection) },
    ]);
  }
  async stopMock(id: string) { await this.backend.httpMockStop(id).catch(() => {}); await this.loadExtras(); }
  async addMonitor(c: HCollection) {
    const every = await this.ui.input(tr('Monitor {0}', c.name), tr('Run the collection every how many minutes while Termwright is open? You get a notification when it starts failing and when it passes again. Changing requests are refused on production sessions.'), '5');
    if (!every) return;
    try { await this.backend.apiSaveMonitor({ name: c.name, collection: c.id, session: this.path() || c.session, every: Math.max(1, parseInt(every, 10) || 5), enabled: true }); this.ui.toast(tr('Monitor added'), 'success'); } catch (e) { this.ui.toast(errText(e), 'error'); }
    await this.loadExtras();
  }
  monMenu(ev: MouseEvent, m: APIMonitorState) {
    ev.preventDefault();
    this.ui.showMenu(ev, [
      { label: 'Run Now', icon: 'play', action: () => void this.backend.apiRunMonitor(m.id).then(() => setTimeout(() => void this.loadExtras(), 3000)) },
      { label: m.enabled ? 'Pause' : 'Resume', action: () => void this.backend.apiSaveMonitor({ ...m, enabled: !m.enabled }).then(() => this.loadExtras()) },
      { label: 'Every…', action: async () => { const v = await this.ui.input(tr('Run Every'), tr('Minutes between runs'), String(m.every)); if (v) { await this.backend.apiSaveMonitor({ ...m, every: Math.max(1, parseInt(v, 10) || m.every) }); await this.loadExtras(); } } },
      { separator: true },
      { label: 'Delete Monitor', action: () => void this.backend.apiDeleteMonitor(m.id).then(() => this.loadExtras()) },
    ]);
  }
  collMenu(ev: MouseEvent, c: HCollection) {
    ev.preventDefault();
    this.ui.showMenu(ev, [
      { label: 'Run Collection...', icon: 'play', action: () => this.openRun(c) },
      { label: this.mocks().some(m => m.collection === c.id) ? 'Stop the Mock Server' : 'Start a Mock Server', action: () => void this.toggleMock(c) },
      { label: 'Monitor (Run Every Few Minutes)...', action: () => void this.addMonitor(c) },
      { label: 'Edit Collection...', icon: 'edit', action: () => this.editColl(c) },
      { label: 'Authorization...', icon: 'lock', action: () => this.editColl(c, 'auth') },
      { label: 'Variables...', icon: 'variables', action: () => this.editColl(c, 'vars') },
      { label: 'New Request', action: () => { this.openDraft(mkDraft(newReq(), c.id, '', true)); this.saveColl = c.id; } },
      { label: 'Rename', action: () => { this.renameText = c.name; this.renaming.set('c:' + c.id); } },
      { label: 'Use the Chosen Session' + (this.path() ? ' (' + this.path() + ')' : ''), disabled: !this.path(), action: () => void this.store({ ...c, session: this.path() }) },
      { separator: true },
      { label: 'Export...', action: () => void this.exportColl(c, '') },
      { label: 'Export for Postman...', icon: 'export', action: () => void this.exportColl(c, 'postman') },
      { separator: true },
      { label: 'Delete Collection', action: () => void this.deleteColl(c) },
    ]);
  }
  reqMenu(ev: MouseEvent, c: HCollection, r: HRequest) {
    ev.preventDefault();
    this.ui.showMenu(ev, [
      { label: 'Open', action: () => this.openReq(c, r) },
      { label: 'Run', icon: 'play', action: () => { this.openRun(c); this.runOnly = [r.id ?? '']; } },
      { label: 'Rename', action: () => { this.renameText = r.name ?? ''; this.renaming.set('r:' + c.id + ':' + r.id); } },
      { label: 'Duplicate', action: () => void this.store({ ...c, requests: [...c.requests, { ...structuredClone(r), id: '', name: (r.name ?? '') + ' copy' }] }) },
      { separator: true },
      { label: 'Delete Request', action: () => void this.deleteReq(c, r) },
    ]);
  }
  async deleteColl(c: HCollection) {
    if (!(await this.ui.confirm(tr('Delete Collection'), tr('Delete the collection {0} and its {1} requests?', c.name, c.requests.length), tr('Delete'), true))) return;
    try { await this.backend.httpDeleteCollection(c.id); this.colls.update(l => l.filter(x => x.id !== c.id)); } catch (e) { this.ui.toast(errText(e), 'error'); }
  }
  async deleteReq(c: HCollection, r: HRequest) {
    if (!(await this.ui.confirm(tr('Delete Request'), tr('Delete {0} from {1}?', r.name || r.url, c.name), tr('Delete'), true))) return;
    await this.store({ ...c, requests: c.requests.filter(x => x.id !== r.id) });
  }
  async exportColl(c: HCollection, format: string) {
    try { const p = await this.backend.httpExportFile(c.id, format); if (p) this.ui.toast(tr('Saved {0}', p), 'success'); } catch (e) { this.ui.toast(errText(e), 'error'); }
  }

  async doImport() {
    this.importErr.set('');
    try {
      const c = await this.backend.httpImport(this.importText, this.importName.trim(), this.importUseSession ? this.path() : '');
      this.importDone(c);
    } catch (e) { this.importErr.set(errText(e)); }
  }
  async importFile() {
    this.importErr.set('');
    try {
      const c = await this.backend.httpImportFile(this.importUseSession ? this.path() : '');
      if (c?.id) this.importDone(c);
    } catch (e) { this.importErr.set(errText(e)); }
  }
  private importDone(c: HCollection) {
    this.colls.update(l => [...l.filter(x => x.id !== c.id), c].sort((a, b) => a.name.localeCompare(b.name)));
    this.opened.update(s => new Set([...s, 'c:' + c.id]));
    this.importOpen.set(false); this.importText = ''; this.importName = '';
    this.ui.toast(tr('Imported {0} with {1} requests', c.name, c.requests.length), 'success');
  }

  private runOnly: string[] = [];
  openRun(c: HCollection) {
    this.runOnly = []; this.runFolder = ''; this.runSession = ''; this.runEnv = this.env(); this.runEnvId = this.envId();
    this.runSteps.set([]); this.runRes.set(null); this.runErr.set('');
    this.runColl.set(c);
  }
  async doRun() {
    const c = this.runColl();
    if (!c) return;
    this.running.set(true); this.runSteps.set([]); this.runRes.set(null); this.runErr.set('');
    const req = { collection: c.id, session: this.runSession || undefined, env: this.runEnv, environment: this.runEnvId || undefined, requests: this.runOnly.length ? this.runOnly : undefined, folder: this.runFolder || undefined,
      iterations: Number(this.runIter) || 1, delayMs: Number(this.runDelay) || 0, stopOnFail: this.runStop, data: this.runData };
    try {
      let r: HRunResult;
      const sess = this.sessions().find(s => s.path === (this.runSession || c.session));
      const prod = this.runEnv === 'production' || envOf(sess) === 'production';
      const changes = c.requests.some(x => !['GET', 'HEAD', 'OPTIONS'].includes(x.method) && (!this.runOnly.length || this.runOnly.includes(x.id ?? '')));
      if (prod && changes && !(await this.ui.confirm(tr('Run on Production?'), tr('{0} has requests that change the server (POST, PUT, PATCH, DELETE); they run on a Production session.', c.name), tr('Run'), true))) {
        r = await this.backend.httpRun(req);
      } else {
        r = await this.backend.httpRun({ ...req, confirmed: prod && changes });
      }
      this.runRes.set(r); this.runSteps.set(r.steps);
      void this.loadEnvs();
    } catch (e) { this.runErr.set(errText(e)); }
    this.running.set(false);
  }

  // ---- scripts written by the AI assistant ----
  aiAskText = '';
  get aiTarget(): 'tests' | 'pre' { return this.scriptTab() === 'pre' ? 'pre' : 'tests'; }
  readonly aiBusy = signal(false);
  readonly aiNote = signal('');
  async writeWithAI() {
    const q = this.aiAskText.trim();
    if (!q || this.aiBusy()) return;
    this.aiBusy.set(true); this.aiNote.set('');
    const d = this.draft().req, r = this.res();
    const ctx = [`The request: ${d.method} ${d.url}`, d.body?.raw ? 'Its body:\n' + d.body.raw.slice(0, 3000) : '',
      r ? `The last answer: ${r.statusText}, ${r.ms} ms, Content-Type ${r.contentType ?? ''}\n${r.base64 ? '(binary)' : r.body.slice(0, 6000)}` : 'No answer yet (send the request first for a better script).',
      `The current ${this.aiTarget === 'pre' ? 'pre-request script' : 'tests'}:\n${(this.aiTarget === 'pre' ? d.preScript : d.tests) || '(none)'}`].filter(Boolean).join('\n\n');
    try {
      const a = await this.backend.aiAsk({ kind: this.aiTarget === 'pre' ? 'apiprescript' : 'apiscript', text: ctx, question: q, deviceType: '', protocol: 'HTTP' });
      const m = /```(?:javascript|js)?\s*\n([\s\S]*?)```/.exec(a.text);
      const code = (m ? m[1] : a.text).trim();
      if (this.aiTarget === 'pre') this.setReq({ preScript: code }); else this.setReq({ tests: code });
      this.aiNote.set(tr('Written by the AI assistant ({0}). Check it, then Send to run it.', a.model) + (a.removed ? ' ' + tr('{0} secrets were removed before sending.', a.removed) : ''));
      this.aiAskText = '';
    } catch (e) { this.aiNote.set(errText(e)); }
    this.aiBusy.set(false);
  }

  async clearHistory() { await this.backend.httpClearHistory().catch(() => {}); this.history.set([]); }
  copy(t: string) { void navigator.clipboard?.writeText(t).then(() => this.ui.toast(tr('Copied'), 'success')).catch(() => {}); }
  size(n: number) { return n < 1024 ? n + ' B' : n < 1 << 20 ? (n / 1024).toFixed(1) + ' KB' : (n / (1 << 20)).toFixed(1) + ' MB'; }
  isJson(r: HResponse) { const t = r.body.trimStart(); return /json/i.test(r.contentType ?? '') || t.startsWith('{') || t.startsWith('['); }
  keys(o: Record<string, string>) { return Object.keys(o); }
  shortUrl(u: string) { return shortPath(u); }
  phases(r: HResponse) {
    const t = r.timing, total = Math.max(t.total, 0.001);
    return [{ l: 'DNS', v: t.dns }, { l: 'Connect', v: t.connect }, { l: 'TLS', v: t.tls }, { l: 'First byte', v: t.firstByte }, { l: 'Total', v: t.total }].map(p => ({ ...p, w: Math.min(100, (p.v / total) * 100) }));
  }
}

function readMono(): boolean { try { return localStorage.getItem('tw.api.mono') === '1'; } catch { return false; } }

function readEnv(): string { try { return localStorage.getItem('tw.api.environment') ?? ''; } catch { return ''; } }
interface Preset { name: string; headers: HKV[]; }
function readPresets(): Preset[] { try { return JSON.parse(localStorage.getItem('tw.api.hpresets') ?? '[]'); } catch { return []; } }
function writePresets(l: Preset[]) { try { localStorage.setItem('tw.api.hpresets', JSON.stringify(l)); } catch { /* not kept */ } }
/** The :name parts of a URL's path. */
export function pathNames(u: string): string[] {
  let p = u.split(/[?#]/)[0];
  const i = p.indexOf('://'); if (i >= 0) { p = p.slice(i + 3); const j = p.indexOf('/'); p = j < 0 ? '' : p.slice(j); }
  return p.split('/').filter(sg => sg.length > 1 && sg[0] === ':').map(sg => sg.slice(1));
}
/** A saved request as the editor shows it: its enabled parameters in the URL, as Postman does. */
export function withQuery(r: HRequest): HRequest {
  const q = (r.params ?? []).filter(p => !p.off && p.key).map(p => p.key + (p.value !== '' ? '=' + p.value : '')).join('&');
  return { ...r, url: q && !r.url.includes('?') ? r.url + '?' + q : r.url };
}
/** The editor's request as it is saved and sent: the query lives in Params only. */
export function wire(r: HRequest): HRequest {
  const i = r.url.indexOf('?');
  if (i < 0) return r;
  if (!(r.params ?? []).length) return r;
  return { ...r, url: r.url.slice(0, i) };
}

function newReq(): HRequest { return { method: 'GET', url: '', params: [], headers: [{ key: 'Accept', value: 'application/json' }, { key: 'Cache-Control', value: 'no-cache' }], body: { mode: '' } }; }

function flat(n: SessionNode | null, out: SessionNode[] = []): SessionNode[] {
  if (!n) return out;
  if (!n.isFolder && n.path) out.push(n);
  for (const c of n.children ?? []) flat(c, out);
  return out;
}

function envOf(n: SessionNode | undefined) { return n?.environment ?? ''; }

function shortPath(u: string) {
  try { const x = new URL(u); return x.pathname + x.search; } catch { return u.replace(/^\{\{[^}]+\}\}/, ''); }
}

/** JSON with keys, strings, numbers and literals in colour (escaped). */
export function jsonHtml(body: string): string {
  let text: string;
  try { text = JSON.stringify(JSON.parse(body), null, 2); } catch { text = body; }
  const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  return esc(text).replace(/("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g,
    (_m, str: string, colon: string, lit: string, num: string) => str ? (colon ? `<span class="k">${str}</span>${colon}` : `<span class="s">${str}</span>`) : lit ? `<span class="b">${lit}</span>` : `<span class="n">${num}</span>`);
}
