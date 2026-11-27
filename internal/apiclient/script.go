package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// prelude is the pm.* subset (Postman's sandbox) and a chai-like expect.
const prelude = `
var __tests = [], __logs = [], __req = JSON.parse(__reqJSON);
var console = { log: function() { __logs.push(Array.prototype.map.call(arguments, function(a) { return typeof a === 'string' ? a : JSON.stringify(a); }).join(' ')); } };
console.info = console.warn = console.error = console.log;
function __eq(a, b) { return JSON.stringify(a) === JSON.stringify(b); }
function __type(v) { return v === null ? 'null' : Array.isArray(v) ? 'array' : typeof v; }
function Assertion(v) { this.v = v; this.neg = false; }
['to','be','been','is','that','which','and','has','have','with','at','of','same','does','deep','nested','own','any','all'].forEach(function(w) {
  Object.defineProperty(Assertion.prototype, w, { get: function() { return this; } });
});
Object.defineProperty(Assertion.prototype, 'not', { get: function() { this.neg = !this.neg; return this; } });
Assertion.prototype.__ok = function(ok, msg) { if (this.neg ? ok : !ok) throw new Error((this.neg ? 'expected not: ' : 'expected: ') + msg); return this; };
Assertion.prototype.equal = Assertion.prototype.equals = Assertion.prototype.eq = function(x) { return this.__ok(this.v === x, JSON.stringify(this.v) + ' to equal ' + JSON.stringify(x)); };
Assertion.prototype.eql = function(x) { return this.__ok(__eq(this.v, x), JSON.stringify(this.v) + ' to deeply equal ' + JSON.stringify(x)); };
Assertion.prototype.include = Assertion.prototype.includes = Assertion.prototype.contain = Assertion.prototype.contains = function(x) {
  var v = this.v, ok = typeof v === 'string' ? v.indexOf(x) >= 0 : Array.isArray(v) ? v.some(function(e) { return __eq(e, x) || e === x; }) :
    (v && typeof v === 'object' && x && typeof x === 'object') ? Object.keys(x).every(function(k) { return __eq(v[k], x[k]); }) : false;
  return this.__ok(ok, JSON.stringify(v) + ' to include ' + JSON.stringify(x)); };
Assertion.prototype.above = Assertion.prototype.greaterThan = Assertion.prototype.gt = function(n) { return this.__ok(this.v > n, this.v + ' to be above ' + n); };
Assertion.prototype.below = Assertion.prototype.lessThan = Assertion.prototype.lt = function(n) { return this.__ok(this.v < n, this.v + ' to be below ' + n); };
Assertion.prototype.least = Assertion.prototype.gte = function(n) { return this.__ok(this.v >= n, this.v + ' to be at least ' + n); };
Assertion.prototype.most = Assertion.prototype.lte = function(n) { return this.__ok(this.v <= n, this.v + ' to be at most ' + n); };
Assertion.prototype.within = function(a, b) { return this.__ok(this.v >= a && this.v <= b, this.v + ' to be within ' + a + '..' + b); };
Assertion.prototype.oneOf = function(l) { var v = this.v; return this.__ok(l.some(function(e) { return __eq(e, v); }), JSON.stringify(v) + ' to be one of ' + JSON.stringify(l)); };
Assertion.prototype.a = Assertion.prototype.an = function(t) { return this.__ok(__type(this.v) === t.toLowerCase(), JSON.stringify(this.v) + ' to be a ' + t); };
Assertion.prototype.match = function(re) { return this.__ok(re.test(String(this.v)), JSON.stringify(this.v) + ' to match ' + re); };
Assertion.prototype.property = function(k, x) { var has = this.v != null && Object.prototype.hasOwnProperty.call(Object(this.v), k);
  this.__ok(has && (arguments.length < 2 || __eq(this.v[k], x)), 'property ' + k + (arguments.length > 1 ? ' = ' + JSON.stringify(x) : ''));
  if (has && !this.neg) this.v = this.v[k]; return this; };
Assertion.prototype.lengthOf = Assertion.prototype.length = function(n) { var l = this.v == null ? -1 : this.v.length; return this.__ok(l === n, 'length ' + l + ' to be ' + n); };
Assertion.prototype.keys = function() { var ks = Array.isArray(arguments[0]) ? arguments[0] : Array.prototype.slice.call(arguments), v = this.v;
  return this.__ok(v && ks.every(function(k) { return Object.prototype.hasOwnProperty.call(v, k); }), 'keys ' + ks.join(', ')); };
['ok','true','false','null','undefined','exist','empty','NaN'].forEach(function(w) {
  Object.defineProperty(Assertion.prototype, w, { get: function() { var v = this.v, ok = {
    ok: !!v, 'true': v === true, 'false': v === false, 'null': v === null, 'undefined': v === undefined, exist: v !== null && v !== undefined,
    empty: v == null || v.length === 0 || (typeof v === 'object' && Object.keys(v).length === 0), NaN: v !== v }[w];
    return this.__ok(ok, JSON.stringify(v) + ' to be ' + w); } });
});
function expect(v) { return new Assertion(v); }
function __mkres(r) {
  var o = {
    code: r.status, status: r.statusText, responseTime: r.ms, responseSize: r.size,
    text: function() { return r.body; }, json: function() { return JSON.parse(r.body); },
    headers: { get: function(n) { n = n.toLowerCase(); for (var i = 0; i < (r.headers || []).length; i++) if (r.headers[i].key.toLowerCase() === n) return r.headers[i].value; },
      has: function(n) { return this.get(n) !== undefined; } },
    cookies: { get: function(n) { for (var i = 0; i < (r.cookies || []).length; i++) if (r.cookies[i].name === n) return r.cookies[i].value; } },
  };
  o.to = { have: {
      status: function(c) { if (typeof c === 'number' ? r.status !== c : r.statusText.indexOf(c) < 0) throw new Error('expected status ' + c + ', got ' + r.status); },
      header: function(n, v) { var h = o.headers.get(n); if (h === undefined || (arguments.length > 1 && h !== v)) throw new Error('expected header ' + n); },
      body: function(s) { if (s !== undefined && r.body.indexOf(s) < 0) throw new Error('expected the body to contain ' + s); },
      jsonBody: function(k) { var j = JSON.parse(r.body); if (k !== undefined && !(k in j)) throw new Error('expected property ' + k); },
      jsonSchema: function(s) { var e = __schema(typeof s === 'string' ? s : JSON.stringify(s), r.body); if (e) throw new Error('schema: ' + e); } },
    be: {} };
  Object.defineProperty(o.to.be, 'ok', { get: function() { if (r.status < 200 || r.status > 299) throw new Error('expected a 2xx status, got ' + r.status); } });
  Object.defineProperty(o.to.be, 'success', { get: function() { if (r.status < 200 || r.status > 299) throw new Error('expected a 2xx status, got ' + r.status); } });
  Object.defineProperty(o.to.be, 'json', { get: function() { JSON.parse(r.body); } });
  [['clientError', 400, 499], ['serverError', 500, 599], ['info', 100, 199], ['redirection', 300, 399], ['notFound', 404, 404], ['unauthorized', 401, 401],
   ['forbidden', 403, 403], ['badRequest', 400, 400], ['accepted', 202, 202], ['rateLimited', 429, 429]].forEach(function(e) {
    Object.defineProperty(o.to.be, e[0], { get: function() { if (r.status < e[1] || r.status > e[2]) throw new Error('expected ' + e[0] + ', got ' + r.status); } }); });
  o.to.not = { have: {
      status: function(c) { var bad = typeof c === 'number' ? r.status === c : r.statusText.indexOf(c) >= 0; if (bad) throw new Error('expected status not ' + c); },
      header: function(n) { if (o.headers.get(n) !== undefined) throw new Error('expected no header ' + n); },
      body: function(s) { if (r.body.indexOf(s) >= 0) throw new Error('expected the body not to contain ' + s); } }, be: {} };
  Object.defineProperty(o.to.not.be, 'ok', { get: function() { if (r.status >= 200 && r.status <= 299) throw new Error('expected a non-2xx status, got ' + r.status); } });
  o.reason = function() { return r.statusText.replace(/^\d+\s*/, ''); };
  o.headers.toObject = function() { var x = {}; (r.headers || []).forEach(function(h) { x[h.key.toLowerCase()] = h.value; }); return x; };
  o.headers.all = function() { return r.headers || []; };
  return o;
}
var __next = { set: false, v: '' }, __skip = false, __envSet = {}, __globSet = {}, __collSet = {};
function __scope(store) {
  return { get: function(k) { return __vars[k]; }, has: function(k) { return k in __vars; },
    set: function(k, v) { v = String(v); __vars[k] = v; __set[k] = v; if (store) store[k] = v; },
    unset: function(k) { delete __vars[k]; if (store) delete store[k]; }, clear: function() {},
    toObject: function() { var o = {}; for (var k in __vars) o[k] = __vars[k]; return o; },
    replaceIn: function(s) { return String(s).replace(/\{\{\s*([^{}]+?)\s*\}\}/g, function(m, k) { if (k in __vars) return __vars[k]; var d = k.charAt(0) === '$' ? __dyn(k) : null; return d === null ? m : d; }); } };
}
var pm = {
  test: function(name, fn) { try { fn(); __tests.push({ name: name, pass: true }); } catch (e) { __tests.push({ name: name, pass: false, error: String(e && e.message || e) }); } },
  expect: function(v, m) { return require('chai').expect(v, m); }, // Postman's pm.expect is chai's
  variables: __scope(null),
  environment: __scope(__envSet),
  globals: __scope(__globSet),
  collectionVariables: __scope(__collSet),
  iterationData: __scope(null),
  request: __req,
  info: __info,
};
pm.environment.name = __info.environment;
var postman = {
  setNextRequest: function(n) { __next.set = true; __next.v = n === null || n === undefined ? '' : String(n); },
  getEnvironmentVariable: pm.environment.get, setEnvironmentVariable: pm.environment.set, clearEnvironmentVariable: pm.environment.unset,
  getGlobalVariable: pm.globals.get, setGlobalVariable: pm.globals.set, clearGlobalVariable: pm.globals.unset,
};
pm.setNextRequest = postman.setNextRequest;
pm.execution = { setNextRequest: postman.setNextRequest, skipRequest: function() { __skip = true; } };
pm.sendRequest = function(r, cb) {
  var res = null, err = null;
  try { res = __mkres(__send(typeof r === 'string' ? { url: r } : r)); } catch (e) { err = e; }
  if (typeof cb === 'function') cb(err, res);
  return err ? Promise.reject(err) : Promise.resolve(res);
};
// pm.request: headers, the URL's query and the body can be changed by a pre-request script (Postman's
// pm.request.headers.add/upsert/remove, pm.request.url.addQueryParams, pm.request.body.raw)
(function(r) {
  var hs = r.headers || (r.headers = []);
  function find(k) { k = String(k).toLowerCase(); for (var i = 0; i < hs.length; i++) if (String(hs[i].key).toLowerCase() === k && !hs[i].off) return i; return -1; }
  hs.add = function(h) { if (typeof h === 'string') { var p = h.split(':'); h = { key: p.shift().trim(), value: p.join(':').trim() }; } hs.push({ key: String(h.key), value: String(h.value) }); };
  hs.upsert = function(h) { var i = find(h.key); if (i >= 0) hs[i].value = String(h.value); else hs.add(h); };
  hs.remove = function(k) { var i; while ((i = find(k)) >= 0) hs.splice(i, 1); };
  hs.get = function(k) { var i = find(k); return i >= 0 ? hs[i].value : undefined; };
  hs.has = function(k) { return find(k) >= 0; };
  hs.toObject = function() { var o = {}; hs.forEach(function(h) { if (!h.off) o[h.key] = h.value; }); return o; };
  var u = String(r.url || '');
  r.url = { toString: function() { return u; }, getRaw: function() { return u; },
    addQueryParams: function(ps) { (Array.isArray(ps) ? ps : [ps]).forEach(function(p) { u += (u.indexOf('?') >= 0 ? '&' : '?') + (typeof p === 'string' ? p : encodeURIComponent(p.key) + '=' + encodeURIComponent(p.value == null ? '' : p.value)); }); },
    update: function(s) { u = String(s); } };
  Object.defineProperty(r, '__url', { get: function() { return u; } });
  var b = r.body == null ? '' : String(r.body);
  r.body = { mode: 'raw', toString: function() { return b; }, update: function(s) { b = typeof s === 'string' ? s : (s && s.raw !== undefined ? String(s.raw) : JSON.stringify(s)); } };
  Object.defineProperty(r.body, 'raw', { get: function() { return b; }, set: function(v) { b = String(v); } });
  Object.defineProperty(r, '__body', { get: function() { return b; } });
  r.addHeader = hs.add; r.removeHeader = hs.remove;
})(__req);
function btoa(s) { return __b64(String(s)); }
function atob(s) { return __unb64(String(s)); }
var tests = {}, globals = {}, environment = {};
if (__res) {
  pm.response = __mkres(__res);
  var responseBody = __res.body, responseCode = { code: __res.status };
}
`

// scriptInfo is pm.info.
type scriptInfo struct {
	RequestName string `json:"requestName"`
	RequestID   string `json:"requestId"`
	EventName   string `json:"eventName"`
	Iteration   int    `json:"iteration"`
	Iterations  int    `json:"iterationCount"`
	Environment string `json:"environment"`
}

// scriptOut is what a script did.
type scriptOut struct {
	tests                []TestResult
	logs                 []string
	set, env, glob, coll map[string]string
	next                 string
	nextSet, skip        bool
	// pre-request: the request as the script left it (headers, URL, body)
	reqHeaders []KV
	reqURL     string
	reqBody    string
	changed    bool
}

// script runs a pre-request (res nil) or test script; vars is updated with what it set.
func script(ctx context.Context, src string, req Request, res *Response, vars map[string]string, t *Target, info scriptInfo) (scriptOut, error) {
	var out scriptOut
	if strings.TrimSpace(src) == "" {
		return out, nil
	}
	vm := goja.New()
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	jv := map[string]any{}
	for k, v := range vars {
		jv[k] = v
	}
	set := map[string]any{}
	_ = vm.Set("__vars", jv)
	_ = vm.Set("__set", set)
	reqJSON, _ := json.Marshal(map[string]any{"method": req.Method, "url": req.URL, "headers": req.Headers, "body": req.Body.Raw, "name": req.Name})
	_ = vm.Set("__reqJSON", string(reqJSON)) // a plain JS object (pm.request is changed by the script)
	if res == nil {
		info.EventName = "prerequest"
		_ = vm.Set("__res", nil)
	} else {
		info.EventName = "test"
		b, _ := json.Marshal(res)
		var rv any
		_ = json.Unmarshal(b, &rv)
		_ = vm.Set("__res", rv)
	}
	ib, _ := json.Marshal(info)
	var iv any
	_ = json.Unmarshal(ib, &iv)
	_ = vm.Set("__info", iv)
	_ = vm.Set("__schema", func(schema, doc string) string { return ValidateSchema(schema, doc) })
	scriptHelpers(vm)
	_ = vm.Set("__send", func(call goja.FunctionCall) goja.Value {
		r := scriptRequest(call.Argument(0).Export(), jv)
		if t == nil {
			panic(vm.NewGoError(fmt.Errorf("pm.sendRequest is not available here")))
		}
		if t.ScriptSend != nil {
			if err := t.ScriptSend(r.Method); err != nil {
				panic(vm.NewGoError(err))
			}
		}
		sv := t.Vars
		t.Vars = map[string]string{}
		for k, v := range jv {
			t.Vars[k] = fmt.Sprint(v)
		}
		got := Send(ctx, r, t)
		t.Vars = sv
		if got.Status == 0 {
			panic(vm.NewGoError(fmt.Errorf("pm.sendRequest %s: %s", r.URL, got.Error)))
		}
		b, _ := json.Marshal(got)
		var gv any
		_ = json.Unmarshal(b, &gv)
		return vm.ToValue(gv)
	})
	done, stopped := make(chan struct{}), make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-time.After(60 * time.Second):
			vm.Interrupt("the script ran longer than 60 seconds")
			close(stopped)
		case <-ctx.Done():
			vm.Interrupt("stopped")
			close(stopped)
		case <-done:
		}
	}()
	timers := setTimers(vm)
	if err := runLibs(vm); err != nil {
		return out, err
	}
	if _, err := vm.RunString(prelude); err != nil {
		return out, err
	}
	_, err := vm.RunString(src)
	if err == nil { // setTimeout and friends: their callbacks run now, in order
		err = timers.drain(stopped)
	}
	vm.ClearInterrupt() // the results below are read with the runtime (getters)
	b, _ := json.Marshal(vm.Get("__tests").Export())
	_ = json.Unmarshal(b, &out.tests)
	b, _ = json.Marshal(vm.Get("__logs").Export())
	_ = json.Unmarshal(b, &out.logs)
	out.set = map[string]string{}
	for k, v := range set {
		out.set[k] = fmt.Sprint(v)
		vars[k] = out.set[k]
	}
	strMap := func(name string) map[string]string {
		m := map[string]string{}
		if o, ok := vm.Get(name).Export().(map[string]any); ok {
			for k, v := range o {
				m[k] = fmt.Sprint(v)
			}
		}
		return m
	}
	out.env, out.glob, out.coll = strMap("__envSet"), strMap("__globSet"), strMap("__collSet")
	if res == nil { // what the pre-request script made of the request
		if rv, ok := vm.Get("__req").(*goja.Object); ok && rv != nil {
			out.reqURL = rv.Get("__url").String()
			out.reqBody = rv.Get("__body").String()
			if hs, ok := rv.Get("headers").Export().([]any); ok {
				for _, h := range hs {
					if m, ok := h.(map[string]any); ok {
						kv := KV{Key: fmt.Sprint(m["key"]), Value: fmt.Sprint(m["value"])}
						if off, _ := m["off"].(bool); off {
							kv.Off = true
						}
						out.reqHeaders = append(out.reqHeaders, kv)
					}
				}
			}
			out.changed = true
		}
	}
	if nx, ok := vm.Get("__next").Export().(map[string]any); ok {
		out.nextSet, _ = nx["set"].(bool)
		out.next = fmt.Sprint(nx["v"])
	}
	out.skip = vm.Get("__skip").ToBoolean()
	if err != nil {
		if ex, ok := err.(*goja.Exception); ok {
			err = fmt.Errorf("script: %s", ex.Value().String())
		} else {
			err = fmt.Errorf("script: %w", err)
		}
	}
	return out, err
}

// scriptRequest is the request of pm.sendRequest: a URL, or {url, method, header, body}.
func scriptRequest(x any, vars map[string]any) Request {
	r := Request{Method: "GET", Auth: "none"}
	m, _ := x.(map[string]any)
	if s, ok := x.(string); ok {
		r.URL = s
	}
	if m != nil {
		r.URL = fmt.Sprint(m["url"])
		if u, ok := m["url"].(map[string]any); ok {
			r.URL = fmt.Sprint(u["raw"])
		}
		if v, ok := m["method"].(string); ok && v != "" {
			r.Method = strings.ToUpper(v)
		}
		switch h := m["header"].(type) {
		case []any:
			for _, e := range h {
				if kv, ok := e.(map[string]any); ok {
					r.Headers = append(r.Headers, KV{Key: fmt.Sprint(kv["key"]), Value: fmt.Sprint(kv["value"])})
				}
			}
		case map[string]any:
			for k, v := range h {
				r.Headers = append(r.Headers, KV{Key: k, Value: fmt.Sprint(v)})
			}
		}
		switch b := m["body"].(type) {
		case string:
			r.Body = Body{Mode: "text", Raw: b}
		case map[string]any:
			mode := fmt.Sprint(b["mode"])
			switch mode {
			case "urlencoded", "formdata":
				if l, ok := b[mode].([]any); ok {
					for _, e := range l {
						if kv, ok := e.(map[string]any); ok {
							r.Body.Form = append(r.Body.Form, KV{Key: fmt.Sprint(kv["key"]), Value: fmt.Sprint(kv["value"])})
						}
					}
				}
				r.Body.Mode = map[string]string{"urlencoded": "urlencoded", "formdata": "form"}[mode]
			default:
				r.Body = Body{Mode: "text", Raw: fmt.Sprint(b["raw"])}
			}
		}
		if r.Body.Mode == "text" {
			for _, h := range r.Headers {
				if strings.EqualFold(h.Key, "Content-Type") && strings.Contains(h.Value, "json") {
					r.Body.Mode = "json"
				}
			}
		}
	}
	return r
}

// Run sends a request with its pre-request script and tests (the collection's first); vars is updated with
// what scripts set.
func Run(ctx context.Context, r Request, t *Target) Response {
	return runInfo(ctx, r, t, scriptInfo{Iteration: 0, Iterations: 1})
}

func runInfo(ctx context.Context, r Request, t *Target, info scriptInfo) Response {
	if t.Vars == nil {
		t.Vars = map[string]string{}
	}
	info.RequestName, info.RequestID = r.Name, r.ID
	var logs []string
	res := Response{Method: r.Method}
	merge := func(o scriptOut) {
		logs = append(logs, o.logs...)
		for k, v := range o.set {
			if res.Vars == nil {
				res.Vars = map[string]string{}
			}
			res.Vars[k] = v
		}
		for k, v := range o.env {
			if res.EnvSet == nil {
				res.EnvSet = map[string]string{}
			}
			res.EnvSet[k] = v
		}
		for k, v := range o.glob {
			if res.GlobalSet == nil {
				res.GlobalSet = map[string]string{}
			}
			res.GlobalSet[k] = v
		}
		for k, v := range o.coll {
			if res.CollSet == nil {
				res.CollSet = map[string]string{}
			}
			res.CollSet[k] = v
		}
		if o.nextSet {
			res.NextSet, res.Next = true, o.next
		}
	}
	var pre scriptOut
	var preTests []TestResult
	for _, src := range []string{t.CollPre, r.PreScript} {
		o, err := script(ctx, src, r, nil, t.Vars, t, info)
		merge(o)
		preTests = append(preTests, o.tests...) // Postman lists a pre-request script's pm.test results too
		if o.changed && err == nil {            // pm.request.headers.add, url.addQueryParams, body.raw = ...
			r.Headers, r.URL = o.reqHeaders, o.reqURL
			if o.reqBody != r.Body.Raw && r.Body.Mode != "urlencoded" && r.Body.Mode != "form" && r.Body.Mode != "binary" {
				r.Body.Raw = o.reqBody
				if r.Body.Mode == "" {
					r.Body.Mode = "text"
				}
			}
		}
		if o.skip {
			pre.skip = true
		}
		if err != nil {
			res.Error, res.Logs, res.Tests = "pre-request "+err.Error(), logs, preTests
			return res
		}
	}
	keep := res
	if pre.skip {
		keep.Skipped, keep.Logs, keep.Tests = true, logs, preTests
		return keep
	}
	res = Send(ctx, r, t)
	res.Logs = append(logs, res.Logs...)
	res.Vars, res.EnvSet, res.GlobalSet, res.CollSet, res.NextSet, res.Next = keep.Vars, keep.EnvSet, keep.GlobalSet, keep.CollSet, keep.NextSet, keep.Next
	res.Tests = append(preTests, res.Tests...)
	if res.Status == 0 {
		return res
	}
	logs = res.Logs
	for _, src := range []string{t.CollTests, r.Tests} {
		o, err := script(ctx, src, r, &res, t.Vars, t, info)
		res.Tests = append(res.Tests, o.tests...)
		merge(o)
		if err != nil {
			res.Tests = append(res.Tests, TestResult{Name: "test script", Error: err.Error()})
		}
	}
	if strings.TrimSpace(r.Schema) != "" {
		tr := TestResult{Name: "body fits the schema", Pass: true}
		if bad := ValidateSchema(r.Schema, res.Body); bad != "" {
			tr.Pass, tr.Error = false, bad
		}
		res.Tests = append(res.Tests, tr)
	}
	res.Logs = logs
	return res
}

// RunStep is one request of a collection run.
type RunStep struct {
	Iteration int          `json:"iteration"`       // which iteration (1, 2, ...)
	ID        string       `json:"id"`              // the request's id
	Name      string       `json:"name"`            // the request's name
	Method    string       `json:"method"`          // its method
	URL       string       `json:"url"`             // the URL sent
	Status    int          `json:"status"`          // the answer's status (0: not sent)
	Ms        int64        `json:"ms"`              // how long it took
	Tests     []TestResult `json:"tests,omitempty"` // its tests
	Error     string       `json:"error,omitempty"` // why it failed or was not sent
	// Pass: answered, no 5xx, every test passed.
	Pass bool `json:"pass"`
}

// RunResult of a collection run.
type RunResult struct {
	Steps  []RunStep `json:"steps"`  // every request sent, in order
	Passed int       `json:"passed"` // requests that passed
	Failed int       `json:"failed"` // requests that failed
	Tests  int       `json:"tests"`  // tests run
	Ms     int64     `json:"ms"`     // the whole run
	// Stopped: StopOnFail, or the run was cancelled.
	Stopped bool `json:"stopped,omitempty"`
}

// RunOptions of a collection run. Data rows (from CSV or JSON) are the variables of each iteration.
type RunOptions struct {
	Iterations int
	Data       []map[string]string
	Delay      time.Duration
	StopOnFail bool
	// Allow says whether a request may be sent (production changes ask); false skips it.
	Allow func(Request) error
	// Each is called after every request (progress).
	Each func(RunStep)
	// Done is called with every answer (to keep what scripts set in environments).
	Done func(Request, Response)
	// EnvName: the chosen environment's name (pm.environment.name).
	EnvName string
}

// MaxRunSteps limits the requests of one iteration (postman.setNextRequest can loop).
const MaxRunSteps = 10000

func findReq(reqs []Request, nameOrID string) int {
	for i, r := range reqs {
		if r.ID == nameOrID {
			return i
		}
	}
	for i, r := range reqs {
		if r.Name == nameOrID {
			return i
		}
	}
	for i, r := range reqs {
		if strings.EqualFold(r.Name, nameOrID) {
			return i
		}
	}
	return -1
}

// RunCollection runs requests in order, iterations times (or once per data row); variables set by
// scripts carry to the next requests.
func RunCollection(ctx context.Context, reqs []Request, t *Target, o RunOptions) RunResult {
	start := time.Now()
	n := max(1, o.Iterations)
	if len(o.Data) > 0 {
		n = len(o.Data)
	}
	var out RunResult
	base := t.Vars
	for it := 0; it < n; it++ {
		vars := map[string]string{}
		for k, v := range base {
			vars[k] = v
		}
		if len(o.Data) > 0 {
			for k, v := range o.Data[it] {
				vars[k] = v
			}
		}
		t.Vars = vars
		sent := 0
		for i := 0; i < len(reqs); {
			r := reqs[i]
			if ctx.Err() != nil {
				out.Stopped = true
				break
			}
			if sent > 0 && o.Delay > 0 {
				time.Sleep(o.Delay)
			}
			if sent++; sent > MaxRunSteps {
				out.Steps = append(out.Steps, RunStep{Iteration: it + 1, Name: r.Name, Error: fmt.Sprintf("stopped: more than %d requests in one iteration (a setNextRequest loop?)", MaxRunSteps)})
				out.Failed++
				out.Stopped = true
				break
			}
			st := RunStep{Iteration: it + 1, ID: r.ID, Name: r.Name, Method: r.Method}
			if o.Allow != nil {
				if err := o.Allow(r); err != nil {
					st.Error = err.Error()
					out.Steps = append(out.Steps, st)
					out.Failed++
					if o.Each != nil {
						o.Each(st)
					}
					i++
					continue
				}
			}
			res := runInfo(ctx, r, t, scriptInfo{Iteration: it, Iterations: n, Environment: o.EnvName})
			if o.Done != nil {
				o.Done(r, res)
			}
			st.URL, st.Status, st.Ms, st.Tests, st.Error = res.URL, res.Status, res.Ms, res.Tests, res.Error
			st.Pass = res.Status > 0 && res.Status < 500 && res.Error == ""
			if res.Skipped {
				st.Pass, st.Error = true, "skipped by its pre-request script"
			}
			for _, tr := range res.Tests {
				out.Tests++
				if !tr.Pass {
					st.Pass = false
				}
			}
			if st.Pass {
				out.Passed++
			} else {
				out.Failed++
			}
			out.Steps = append(out.Steps, st)
			if o.Each != nil {
				o.Each(st)
			}
			if !st.Pass && o.StopOnFail {
				out.Stopped = true
				break
			}
			i++
			if res.NextSet {
				if res.Next == "" {
					break // setNextRequest(null) ends this iteration
				}
				j := findReq(reqs, res.Next)
				if j < 0 {
					st.Error = fmt.Sprintf("setNextRequest: no request %q in this run", res.Next)
					out.Steps = append(out.Steps, RunStep{Iteration: it + 1, Name: res.Next, Error: st.Error})
					out.Failed++
					break
				}
				i = j
			}
		}
		if out.Stopped {
			break
		}
	}
	t.Vars = base
	out.Ms = time.Since(start).Milliseconds()
	return out
}
