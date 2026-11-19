import { Component, OnDestroy, OnInit, computed, effect, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { HCollection, HHistoryItem, HKV, HRequest, HResponse, HRunResult, HRunStep, HSendRequest, Session, SessionNode } from '../core/models';
import { TabView } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';

const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'];
const MODES = [{ v: '', l: 'None' }, { v: 'json', l: 'JSON' }, { v: 'text', l: 'Text' }, { v: 'xml', l: 'XML' }, { v: 'urlencoded', l: 'Form URL-encoded' }, { v: 'form', l: 'Multipart form' }, { v: 'graphql', l: 'GraphQL' }];
const LANGS = [{ v: 'curl', l: 'cURL' }, { v: 'python', l: 'Python (requests)' }, { v: 'javascript', l: 'JavaScript (fetch)' }, { v: 'powershell', l: 'PowerShell' }];
const AUTH_NAMES: Record<string, string> = { '': 'none', apikey: 'API key', bearer: 'bearer token', login: 'login', basic: 'basic', oauth2: 'OAuth 2.0' };
const SNIPPETS: { l: string; code: string }[] = [
  { l: 'Status is 200', code: `pm.test("Status is 200", () => pm.response.to.have.status(200));` },
  { l: 'Answer under 500 ms', code: `pm.test("Answer under 500 ms", () => pm.expect(pm.response.responseTime).to.be.below(500));` },
  { l: 'Body has a value', code: `pm.test("Body has a value", () => {\n  const j = pm.response.json();\n  pm.expect(j).to.have.property("name");\n});` },
  { l: 'Keep a value for the next requests', code: `pm.variables.set("id", pm.response.json().id);` },
  { l: 'Header is present', code: `pm.test("JSON answer", () => pm.response.to.have.header("Content-Type"));` },
];

interface Draft { req: HRequest; coll: string; id: string; dirty: boolean; }
interface Row { kind: 'folder' | 'req'; depth: number; folder?: string; name?: string; open?: boolean; req?: HRequest; coll: string; }

/** The API client: requests with an API session's base URL, login and variables; collections, history, tests, runner, import, code. */
@Component({
  selector: 'app-api-client',
  imports: [FormsModule],
  template: `
  <div class="ac">
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
      <div class="bar">
        <select class="meth" [ngModel]="draft().req.method" (ngModelChange)="setReq({ method: $event })" [class]="'meth ' + draft().req.method">
          @for (m of methods; track m) { <option [value]="m">{{ m }}</option> }</select>
        <input class="url" [ngModel]="draft().req.url" (ngModelChange)="setReq({ url: $event })" (keydown.enter)="send()" placeholder="/path after the session's base URL, or https://…" spellcheck="false" translate="no" />
        <select class="sess" [ngModel]="path()" (ngModelChange)="choose($event)" title="API session: base URL, authentication and variables">
          <option value="">No session</option>
          @for (s of sessions(); track s.path) { <option [value]="s.path">{{ s.path }}</option> }</select>
        <select class="env" [ngModel]="env()" (ngModelChange)="env.set($event); loadToken()" title="Which values of the session's variables">
          <option value="">Env: {{ sessEnvName() }}</option><option value="production">Env: Production</option></select>
        <button type="button" class="pri" (click)="send()" [disabled]="busy() || !draft().req.url.trim()">{{ busy() ? 'Sending…' : 'Send' }}</button>
        <button type="button" (click)="save()" [title]="draft().coll ? 'Save to the collection' : 'Save to a collection'">Save{{ draft().dirty ? ' •' : '' }}</button>
      </div>
      <div class="chips">
        @if (sess(); as s) {
          <span class="chip s" translate="no">Session: {{ s.name }} · {{ s.apiBaseUrl }}</span>
          @if (s.apiAuth) { <span class="chip ok">🔒 Auth: {{ authName(s.apiAuth) }} from the Session Manager{{ token()?.held ? ' · token held, renewed by itself' : '' }}</span> }
          @else { <span class="chip">No authentication</span> }
          @if (s.apiInsecure) { <span class="chip warn">TLS: any certificate</span> } @else if (s.apiCaFile) { <span class="chip">TLS: own CA trusted</span> }
          @if (prod()) { <span class="chip bad">Production: changes (POST, PUT, PATCH, DELETE) ask first</span> }
          <a class="lk" (click)="editSession()">Session settings…</a>
        } @else {
          <span class="chip">No session: the URL is sent as it is. Choose an API session for its base URL and login, or add one (API → New API Session).</span>
        }
        @if (draft().coll) { <span class="chip" translate="no">{{ collName(draft().coll) }}{{ draft().req.folder ? ' / ' + draft().req.folder : '' }} / {{ draft().req.name }}</span> }
      </div>
      <div class="split">
        <div class="card req">
          <div class="tabs">
            @for (t of reqTabs; track t.k) { <a [class.on]="reqTab() === t.k" (click)="reqTab.set(t.k); t.k === 'code' && loadCode()">{{ t.l }}@if (count(t.k); as n) { <i>{{ n }}</i> }</a> }
          </div>
          <div class="body">
            @switch (reqTab()) {
              @case ('params') { 
                <table class="kv"><thead><tr><th></th><th>Key</th><th>Value</th><th>Description</th><th></th></tr></thead><tbody>
                  @for (p of draft().req.params ?? []; track $index) {
                    <tr><td><input type="checkbox" [ngModel]="!p.off" (ngModelChange)="p.off = !$event; touch()" /></td>
                      <td><input [(ngModel)]="p.key" (ngModelChange)="touch()" spellcheck="false" /></td><td><input [(ngModel)]="p.value" (ngModelChange)="touch()" spellcheck="false" /></td>
                      <td><input [(ngModel)]="p.desc" (ngModelChange)="touch()" /></td><td><a class="x" (click)="del(draft().req.params!, $index)">✕</a></td></tr>
                  }
                </tbody></table>
                <button type="button" class="add" (click)="add('params')">+ Add Parameter</button>
              }
              @case ('headers') {
                <table class="kv"><thead><tr><th></th><th>Key</th><th>Value</th><th>Description</th><th></th></tr></thead><tbody>
                  @for (p of draft().req.headers ?? []; track $index) {
                    <tr><td><input type="checkbox" [ngModel]="!p.off" (ngModelChange)="p.off = !$event; touch()" /></td>
                      <td><input [(ngModel)]="p.key" (ngModelChange)="touch()" spellcheck="false" list="hdrs" /></td><td><input [(ngModel)]="p.value" (ngModelChange)="touch()" spellcheck="false" /></td>
                      <td><input [(ngModel)]="p.desc" (ngModelChange)="touch()" /></td><td><a class="x" (click)="del(draft().req.headers!, $index)">✕</a></td></tr>
                  }
                  @if (sess()?.apiAuth && draft().req.auth !== 'none') { <tr class="auto"><td></td><td>{{ authHeader() }}</td><td>••••••••</td><td>🔒 from the session</td><td></td></tr> }
                </tbody></table>
                <datalist id="hdrs">@for (h of commonHeaders; track h) { <option [value]="h"></option> }</datalist>
                <button type="button" class="add" (click)="add('headers')">+ Add Header</button>
              }
              @case ('auth') {
                <div class="pad2">
                  <label class="rad"><input type="radio" name="auth" [checked]="draft().req.auth !== 'none'" (change)="setReq({ auth: '' })" /> The session's authentication ({{ sess() ? authName(sess()!.apiAuth || '') : 'no session' }})</label>
                  <label class="rad"><input type="radio" name="auth" [checked]="draft().req.auth === 'none'" (change)="setReq({ auth: 'none' })" /> None for this request (a login page, a public health check)</label>
                  <div class="hint">Passwords, API keys, tokens and client secrets are set in the API session (right-click it → Properties) and kept in the computer's credential store; they are never saved in a collection. A login or OAuth token is fetched when needed, kept in memory and renewed before it expires (and once more after a 401).</div>
                </div>
              }
              @case ('body') {
                <div class="modes">@for (m of modes; track m.v) { <label><input type="radio" name="bm" [checked]="(draft().req.body?.mode || '') === m.v" (change)="setBody({ mode: m.v })" /> {{ m.l }}</label> }
                  @if (draft().req.body?.mode === 'json') { <a class="lk" (click)="beautify()">Beautify</a> }</div>
                @switch (draft().req.body?.mode || '') {
                  @case ('') { <div class="mut pad">This request has no body.</div> }
                  @case ('urlencoded') { 
                    <table class="kv"><tbody>@for (p of draft().req.body!.form ?? []; track $index) {
                      <tr><td><input type="checkbox" [ngModel]="!p.off" (ngModelChange)="p.off = !$event; touch()" /></td><td><input [(ngModel)]="p.key" (ngModelChange)="touch()" /></td><td><input [(ngModel)]="p.value" (ngModelChange)="touch()" /></td><td><a class="x" (click)="del(draft().req.body!.form!, $index)">✕</a></td></tr>
                    }</tbody></table><button type="button" class="add" (click)="addForm()">+ Add Field</button>
                  }
                  @case ('form') {
                    <table class="kv"><tbody>@for (p of draft().req.body!.form ?? []; track $index) {
                      <tr><td><input type="checkbox" [ngModel]="!p.off" (ngModelChange)="p.off = !$event; touch()" /></td><td><input [(ngModel)]="p.key" (ngModelChange)="touch()" /></td><td><input [(ngModel)]="p.value" (ngModelChange)="touch()" placeholder="value, or @/path/to/file" /></td><td><a class="x" (click)="del(draft().req.body!.form!, $index)">✕</a></td></tr>
                    }</tbody></table><button type="button" class="add" (click)="addForm()">+ Add Field</button>
                  }
                  @case ('graphql') {
                    <textarea class="code" [ngModel]="draft().req.body!.raw" (ngModelChange)="setBody({ raw: $event })" placeholder="query { … }" spellcheck="false"></textarea>
                    <textarea class="code small" [ngModel]="draft().req.body!.graphqlVars" (ngModelChange)="setBody({ graphqlVars: $event })" placeholder="Variables (JSON)" spellcheck="false"></textarea>
                  }
                  @default { <textarea class="code" [ngModel]="draft().req.body!.raw" (ngModelChange)="setBody({ raw: $event })" spellcheck="false" translate="no"></textarea> }
                }
              }
              @case ('scripts') {
                <div class="scr">
                  <div class="aiw"><span class="ai-ic">✦</span>
                    <input [(ngModel)]="aiAskText" (keydown.enter)="writeWithAI()" [disabled]="aiBusy()" placeholder="Describe the tests or script you want, e.g. every port of 1:1-1:4 is UP and the answer is under 300 ms" />
                    <select [(ngModel)]="aiTarget" title="Which script"><option value="tests">Tests</option><option value="pre">Pre-request</option></select>
                    <button type="button" (click)="writeWithAI()" [disabled]="aiBusy() || !aiAskText.trim()">{{ aiBusy() ? 'Writing…' : 'Write with AI' }}</button></div>
                  @if (aiNote()) { <div class="mut pad0">{{ aiNote() }}</div> }
                  <div class="sh"><b>Tests</b> <span class="mut">run on the answer (pm.test, pm.expect, pm.response, pm.variables)</span><span class="sp"></span>
                    <select [ngModel]="''" (ngModelChange)="$event !== '' && snippet(+$event)"><option value="">Add a snippet…</option>@for (s of snippets; track $index) { <option [value]="$index">{{ s.l }}</option> }</select></div>
                  <textarea class="code" [ngModel]="draft().req.tests" (ngModelChange)="setReq({ tests: $event })" spellcheck="false" translate="no" placeholder="pm.test('ok', () => pm.response.to.have.status(200));"></textarea>
                  <div class="sh"><b>Pre-request script</b> <span class="mut">runs before it is sent (pm.variables.set)</span></div>
                  <textarea class="code small" [ngModel]="draft().req.preScript" (ngModelChange)="setReq({ preScript: $event })" spellcheck="false" translate="no"></textarea>
                </div>
              }
              @case ('code') {
                <div class="modes"><select [ngModel]="lang()" (ngModelChange)="lang.set($event); loadCode()">@for (l of langs; track l.v) { <option [value]="l.v">{{ l.l }}</option> }</select>
                  <a class="lk" (click)="copy(code())">Copy</a><span class="mut">Secrets are environment variables in the code, never written out.</span></div>
                <pre class="out" translate="no">{{ code() }}</pre>
              }
            }
          </div>
        </div>
        <div class="card res">
          <div class="tabs">
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
                <select class="pretty" [ngModel]="pretty() ? 'p' : 'r'" (ngModelChange)="pretty.set($event === 'p')"><option value="p">Pretty</option><option value="r">Raw</option></select>
                <button type="button" (click)="copy(r.body)">Copy</button>
              }
            }
          </div>
          <div class="body">
            @if (res(); as r) {
              @if (r.error) { <div class="err">{{ r.error }}</div> }
              @switch (resTab()) {
                @case ('body') {
                  @if (r.base64) { <div class="mut pad">Binary answer ({{ size(r.size) }}, {{ r.contentType }}).</div> }
                  @else if (pretty() && isJson(r)) { <pre class="out json" [class.mono]="mono()" translate="no" [innerHTML]="jsonHtml()"></pre> }
                  @else { <pre class="out" [class.mono]="mono()" translate="no">{{ r.body }}</pre> }
                  @if (r.truncated) { <div class="mut pad">Only the first 5 MB are shown.</div> }
                }
                @case ('headers') { <table class="kv ro" translate="no"><tbody>@for (h of r.headers; track $index) { <tr><td>{{ h.key }}</td><td>{{ h.value }}</td></tr> }</tbody></table> }
                @case ('cookies') {
                  <table class="kv ro" translate="no"><thead><tr><th>Name</th><th>Value</th><th>Domain</th><th>Path</th><th>Expires</th></tr></thead><tbody>
                    @for (c of r.cookies ?? []; track $index) { <tr><td>{{ c.name }}</td><td>{{ c.value }}</td><td>{{ c.domain }}</td><td>{{ c.path }}</td><td>{{ c.expires }}</td></tr> }</tbody></table>
                  @if (!r.cookies?.length) { <div class="mut pad">No cookies set. Cookies are kept per session for the next requests.</div> }
                }
                @case ('tests') {
                  @for (t of r.tests ?? []; track $index) { <div class="tr" [class.bad]="!t.pass"><b>{{ t.pass ? '✓ PASS' : '✕ FAIL' }}</b> {{ t.name }}@if (t.error) { <span class="mut"> — {{ t.error }}</span> }</div> }
                  @empty { <div class="mut pad">No tests. Add some in the request's Scripts tab.</div> }
                  @if (r.vars && keys(r.vars).length) { <div class="sub">Variables set: @for (k of keys(r.vars); track k) { <code>{{ k }}</code> } </div> }
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
            } @else { <div class="empty">Choose an API session, type a path and press Send (or Enter).<br />Import a Postman collection, an OpenAPI / Swagger document or cURL commands with Import.</div> }
          </div>
        </div>
      </div>
    </div>

    @if (importOpen()) {
      <div class="ov" (click)="importOpen.set(false)"><div class="dlg" (click)="$event.stopPropagation()">
        <h3>Import</h3>
        <div class="mut">A Postman collection (v2.0 / v2.1: folders, scripts, variables), an OpenAPI 3 or Swagger 2 document (YAML or JSON: one request per operation, by tag) or cURL commands.</div>
        <textarea class="code" [(ngModel)]="importText" placeholder="Paste here, or choose a file" spellcheck="false"></textarea>
        <div class="row"><input [(ngModel)]="importName" placeholder="Collection name (default: from the document)" />
          <label class="mut"><input type="checkbox" [(ngModel)]="importUseSession" [disabled]="!path()" /> Use the session {{ path() || '(none chosen)' }}</label></div>
        @if (importErr()) { <div class="err">{{ importErr() }}</div> }
        <div class="btns"><button type="button" (click)="importFile()">Choose File…</button><span class="sp"></span>
          <button type="button" (click)="importOpen.set(false)">Cancel</button><button type="button" class="pri" (click)="doImport()" [disabled]="!importText.trim()">Import</button></div>
      </div></div>
    }
    @if (saveOpen()) {
      <div class="ov" (click)="saveOpen.set(false)"><div class="dlg sm" (click)="$event.stopPropagation()">
        <h3>Save Request</h3>
        <label>Name<input [(ngModel)]="saveName" /></label>
        <label>Collection<select [(ngModel)]="saveColl"><option value="">New collection…</option>@for (c of colls(); track c.id) { <option [value]="c.id">{{ c.name }}</option> }</select></label>
        @if (!saveColl) { <label>New collection's name<input [(ngModel)]="saveNewColl" /></label> }
        <label>Folder (optional)<input [(ngModel)]="saveFolder" placeholder="Interfaces/Ports" /></label>
        <div class="btns"><span class="sp"></span><button type="button" (click)="saveOpen.set(false)">Cancel</button><button type="button" class="pri" (click)="saveAs()" [disabled]="!saveName.trim() || (!saveColl && !saveNewColl.trim())">Save</button></div>
      </div></div>
    }
    @if (runColl(); as c) {
      <div class="ov" (click)="!running() && runColl.set(null)"><div class="dlg wide" (click)="$event.stopPropagation()">
        <h3 translate="no">Run {{ c.name }}{{ runFolder ? ' / ' + runFolder : '' }}</h3>
        <div class="row">
          <label>Session<select [(ngModel)]="runSession"><option value="">{{ c.session ? 'The collection\\'s (' + c.session + ')' : 'No session' }}</option>@for (s of sessions(); track s.path) { <option [value]="s.path">{{ s.path }}</option> }</select></label>
          <label>Env<select [(ngModel)]="runEnv"><option value="">Session's</option><option value="production">Production</option></select></label>
          <label>Iterations<input type="number" min="1" [(ngModel)]="runIter" style="width: 60px" /></label>
          <label>Delay (ms)<input type="number" min="0" [(ngModel)]="runDelay" style="width: 70px" /></label>
          <label class="chk"><input type="checkbox" [(ngModel)]="runStop" /> Stop at the first failure</label>
        </div>
        <textarea class="code small" [(ngModel)]="runData" placeholder="Data (optional): CSV with a header line, or a JSON array of objects - one iteration per row, its columns are variables" spellcheck="false"></textarea>
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
    .meth { font: 600 12px var(--mono); width: 96px; } .sess { max-width: 220px; } .env { max-width: 160px; }
    button.pri { background: var(--accent); color: #fff; border-color: var(--accent); min-width: 72px; }
    .chips { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
    .chip { border: 1px solid var(--border); border-radius: 12px; padding: 2px 10px; font-size: 12px; color: var(--muted); }
    .chip.s { border-color: color-mix(in srgb, var(--accent) 55%, transparent); color: var(--accent); }
    .chip.ok { border-color: color-mix(in srgb, var(--ok) 55%, transparent); color: var(--ok); }
    .chip.warn { border-color: color-mix(in srgb, var(--warn) 55%, transparent); color: var(--warn); }
    .chip.bad { border-color: color-mix(in srgb, var(--danger) 55%, transparent); color: var(--danger); }
    .split { flex: 1; min-height: 0; display: grid; grid-template-rows: minmax(150px, 38%) 1fr; gap: 8px; }
    .tabs { display: flex; gap: 2px; align-items: center; border-bottom: 1px solid var(--border); padding: 0 8px; flex-wrap: wrap; }
    .tabs > a { padding: 8px 11px; cursor: pointer; color: var(--muted); border-bottom: 2px solid transparent; }
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
    .add { margin: 8px; }
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
    .sum { font-size: 13px; } .steps { overflow: auto; max-height: 340px; border: 1px solid var(--line); border-radius: var(--r); }
  `],
})
export class ApiClient implements OnInit, OnDestroy {
  readonly tab = input.required<TabView>();
  readonly visible = input(true);
  private readonly backend = inject(Backend);
  readonly ui = inject(Ui);

  readonly methods = METHODS; readonly modes = MODES; readonly langs = LANGS; readonly snippets = SNIPPETS;
  readonly reqTabs = [{ k: 'params', l: 'Params' }, { k: 'headers', l: 'Headers' }, { k: 'auth', l: 'Auth' }, { k: 'body', l: 'Body' }, { k: 'scripts', l: 'Scripts' }, { k: 'code', l: 'Code' }];
  readonly resTabs = [{ k: 'body', l: 'Body' }, { k: 'headers', l: 'Headers' }, { k: 'cookies', l: 'Cookies' }, { k: 'tests', l: 'Tests' }, { k: 'timeline', l: 'Timeline' }];
  readonly commonHeaders = ['Accept', 'Content-Type', 'Authorization', 'Cache-Control', 'If-Match', 'If-None-Match', 'User-Agent', 'X-Request-Id', 'X-Auth-Token', 'X-API-Key'];

  filter = '';
  readonly path = signal('');
  readonly sess = signal<Session | null>(null);
  readonly env = signal('');
  readonly token = signal<{ held: boolean; expires?: string } | null>(null);
  readonly sessions = computed(() => flat(this.ui.tree()).filter(n => n.protocol === 'API'));
  readonly colls = signal<HCollection[]>([]);
  readonly opened = signal<Set<string>>(new Set());
  readonly draft = signal<Draft>({ req: newReq(), coll: '', id: '', dirty: false });
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
  runFolder = ''; runSession = ''; runEnv = ''; runIter = 1; runDelay = 0; runStop = false; runData = '';
  readonly running = signal(false);
  readonly runSteps = signal<HRunStep[]>([]);
  readonly runRes = signal<HRunResult | null>(null);
  readonly runErr = signal('');

  readonly prod = computed(() => this.env() === 'production' || this.sess()?.environment === 'production');
  readonly failed = computed(() => (this.res()?.tests ?? []).filter(t => !t.pass).length);
  readonly jsonHtml = computed(() => { const r = this.res(); return r ? jsonHtml(r.body) : ''; });
  private offRun: (() => void) | null = null;

  constructor() {
    effect(() => { if (this.ui.apiImportAsked() && this.visible()) { this.importOpen.set(true); this.ui.apiImportAsked.set(0); } });
  }

  async ngOnInit() {
    if (!this.ui.tree()) this.ui.tree.set(await this.backend.getSessionTree().catch(() => null));
    const p = this.tab().apiSession || '';
    if (p) await this.choose(p);
    else if (this.sessions().length === 1) await this.choose(this.sessions()[0].path);
    await this.loadColls();
    this.history.set(await this.backend.httpHistory().catch(() => []));
    this.offRun = this.backend.on('http:run', (st: HRunStep) => { if (this.running()) this.runSteps.update(l => [...l, st]); });
  }
  ngOnDestroy() { this.offRun?.(); }

  async choose(p: string) {
    this.path.set(p);
    this.sess.set(p ? await this.backend.getSession(p).catch(() => null) : null);
    this.vars = {};
    void this.loadToken();
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
    this.draft.set({ req: structuredClone(r), coll: c.id, id: r.id ?? '', dirty: false });
    if (c.session && c.session !== this.path() && !this.path()) void this.choose(c.session);
    this.res.set(null);
    if (this.reqTab() === 'code') void this.loadCode();
  }
  openHist(h: HHistoryItem) {
    this.draft.set({ req: structuredClone(h.request), coll: '', id: '', dirty: true });
    if (h.session && h.session !== this.path()) void this.choose(h.session);
    if (h.env !== undefined) this.env.set(h.env ?? '');
  }
  setReq(p: Partial<HRequest>) { this.draft.update(d => ({ ...d, req: { ...d.req, ...p }, dirty: true })); }
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
    if (k === 'headers') return (r.headers ?? []).filter(p => p.key && !p.off).length;
    if (k === 'scripts') return (r.tests?.trim() ? 1 : 0) + (r.preScript?.trim() ? 1 : 0);
    return 0;
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
    return { session: this.path(), env: this.env(), collection: d.coll || undefined, request: d.req, vars: this.vars };
  }

  async send() {
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
      Object.assign(this.vars, res.vars ?? {});
      if (res.tests?.length && this.resTab() === 'body' && res.tests.some(t => !t.pass)) this.resTab.set('tests');
      this.history.set(await this.backend.httpHistory().catch(() => this.history()));
      void this.loadToken();
    } catch (e) {
      this.res.set(null);
      this.ui.toast(errText(e), 'error');
    }
    this.busy.set(false);
  }

  async loadCode() {
    try { this.code.set(await this.backend.httpCode(this.sendReq(), this.lang())); } catch (e) { this.code.set(errText(e)); }
  }

  async save() {
    const d = this.draft();
    const c = this.colls().find(x => x.id === d.coll);
    if (c && d.id) {
      const reqs = c.requests.map(r => r.id === d.id ? { ...d.req, id: d.id } : r);
      await this.store({ ...c, requests: reqs });
      this.draft.update(x => ({ ...x, dirty: false }));
      return;
    }
    this.saveName = d.req.name || (d.req.method + ' ' + shortPath(d.req.url));
    this.saveColl = this.colls()[0]?.id ?? '';
    this.saveNewColl = ''; this.saveFolder = d.req.folder ?? '';
    this.saveOpen.set(true);
  }
  async saveAs() {
    let c = this.colls().find(x => x.id === this.saveColl);
    const req: HRequest = { ...this.draft().req, id: '', name: this.saveName.trim(), folder: this.saveFolder.trim() };
    c = c ? { ...c, requests: [...c.requests, req] } : { id: '', name: this.saveNewColl.trim(), session: this.path() || undefined, requests: [req] };
    const saved = await this.store(c);
    if (saved) {
      const r = saved.requests[saved.requests.length - 1];
      this.draft.set({ req: structuredClone(r), coll: saved.id, id: r.id ?? '', dirty: false });
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

  collMenu(ev: MouseEvent, c: HCollection) {
    ev.preventDefault();
    this.ui.showMenu(ev, [
      { label: 'Run Collection...', icon: 'play', action: () => this.openRun(c) },
      { label: 'New Request', action: () => { this.draft.set({ req: newReq(), coll: '', id: '', dirty: true }); this.saveColl = c.id; } },
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
    this.runOnly = []; this.runFolder = ''; this.runSession = ''; this.runEnv = this.env();
    this.runSteps.set([]); this.runRes.set(null); this.runErr.set('');
    this.runColl.set(c);
  }
  async doRun() {
    const c = this.runColl();
    if (!c) return;
    this.running.set(true); this.runSteps.set([]); this.runRes.set(null); this.runErr.set('');
    const req = { collection: c.id, session: this.runSession || undefined, env: this.runEnv, requests: this.runOnly.length ? this.runOnly : undefined, folder: this.runFolder || undefined,
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
    } catch (e) { this.runErr.set(errText(e)); }
    this.running.set(false);
  }

  // ---- scripts written by the AI assistant ----
  aiAskText = ''; aiTarget: 'tests' | 'pre' = 'tests';
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

function newReq(): HRequest { return { method: 'GET', url: '', params: [], headers: [], body: { mode: '' } }; }

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
