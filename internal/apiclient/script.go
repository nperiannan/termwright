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
var __tests = [], __logs = [];
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
var pm = {
  test: function(name, fn) { try { fn(); __tests.push({ name: name, pass: true }); } catch (e) { __tests.push({ name: name, pass: false, error: String(e && e.message || e) }); } },
  expect: expect,
  variables: { get: function(k) { return __vars[k]; }, set: function(k, v) { __vars[k] = String(v); __set[k] = String(v); }, has: function(k) { return k in __vars; },
    unset: function(k) { delete __vars[k]; }, replaceIn: function(s) { return String(s).replace(/\{\{\s*([^{}]+?)\s*\}\}/g, function(m, k) { return k in __vars ? __vars[k] : m; }); } },
  request: __req,
};
pm.environment = pm.collectionVariables = pm.globals = pm.iterationData = pm.variables;
if (__res) {
  var r = __res;
  pm.response = {
    code: r.status, status: r.statusText, responseTime: r.ms, responseSize: r.size,
    text: function() { return r.body; }, json: function() { return JSON.parse(r.body); },
    headers: { get: function(n) { n = n.toLowerCase(); for (var i = 0; i < r.headers.length; i++) if (r.headers[i].key.toLowerCase() === n) return r.headers[i].value; },
      has: function(n) { return this.get(n) !== undefined; } },
    cookies: { get: function(n) { for (var i = 0; i < (r.cookies || []).length; i++) if (r.cookies[i].name === n) return r.cookies[i].value; } },
  };
  pm.response.to = { have: {
      status: function(c) { if (typeof c === 'number' ? r.status !== c : r.statusText.indexOf(c) < 0) throw new Error('expected status ' + c + ', got ' + r.status); },
      header: function(n, v) { var h = pm.response.headers.get(n); if (h === undefined || (arguments.length > 1 && h !== v)) throw new Error('expected header ' + n); },
      body: function(s) { if (s !== undefined && r.body.indexOf(s) < 0) throw new Error('expected the body to contain ' + s); },
      jsonBody: function(k) { var j = JSON.parse(r.body); if (k !== undefined && !(k in j)) throw new Error('expected property ' + k); } },
    be: {} };
  Object.defineProperty(pm.response.to.be, 'ok', { get: function() { if (r.status < 200 || r.status > 299) throw new Error('expected a 2xx status, got ' + r.status); } });
  Object.defineProperty(pm.response.to.be, 'success', { get: function() { if (r.status < 200 || r.status > 299) throw new Error('expected a 2xx status, got ' + r.status); } });
  Object.defineProperty(pm.response.to.be, 'json', { get: function() { JSON.parse(r.body); } });
  var responseBody = r.body, responseCode = { code: r.status };
}
`

// script runs a pre-request (res nil) or test script. It returns the tests, the console lines and the
// variables it set; vars is updated.
func script(ctx context.Context, src string, req Request, res *Response, vars map[string]string) ([]TestResult, []string, map[string]string, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil, nil, nil
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
	reqJSON, _ := json.Marshal(map[string]any{"method": req.Method, "url": req.URL, "headers": req.Headers, "body": req.Body.Raw})
	var rq any
	_ = json.Unmarshal(reqJSON, &rq)
	_ = vm.Set("__req", rq)
	if res != nil {
		b, _ := json.Marshal(res)
		var rv any
		_ = json.Unmarshal(b, &rv)
		_ = vm.Set("__res", rv)
	} else {
		_ = vm.Set("__res", nil)
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-time.After(5 * time.Second):
			vm.Interrupt("the script ran longer than 5 seconds")
		case <-ctx.Done():
			vm.Interrupt("stopped")
		case <-done:
		}
	}()
	if _, err := vm.RunString(prelude); err != nil {
		return nil, nil, nil, err
	}
	_, err := vm.RunString(src)
	var tests []TestResult
	var logs []string
	b, _ := json.Marshal(vm.Get("__tests").Export())
	_ = json.Unmarshal(b, &tests)
	b, _ = json.Marshal(vm.Get("__logs").Export())
	_ = json.Unmarshal(b, &logs)
	out := map[string]string{}
	for k, v := range set {
		out[k] = fmt.Sprint(v)
		vars[k] = out[k]
	}
	if err != nil {
		if ex, ok := err.(*goja.Exception); ok {
			err = fmt.Errorf("script: %s", ex.Value().String())
		} else {
			err = fmt.Errorf("script: %w", err)
		}
	}
	return tests, logs, out, err
}

// Run sends a request with its pre-request script and tests; vars is updated with what scripts set.
func Run(ctx context.Context, r Request, t *Target) Response {
	if t.Vars == nil {
		t.Vars = map[string]string{}
	}
	_, logs, set, err := script(ctx, r.PreScript, r, nil, t.Vars)
	if err != nil {
		return Response{Method: r.Method, Error: "pre-request " + err.Error(), Logs: logs}
	}
	res := Send(ctx, r, t)
	res.Logs = append(logs, res.Logs...)
	res.Vars = set
	if res.Status == 0 {
		return res
	}
	tests, logs2, set2, err := script(ctx, r.Tests, r, &res, t.Vars)
	res.Tests, res.Logs = tests, append(res.Logs, logs2...)
	for k, v := range set2 {
		if res.Vars == nil {
			res.Vars = map[string]string{}
		}
		res.Vars[k] = v
	}
	if err != nil {
		res.Tests = append(res.Tests, TestResult{Name: "test script", Error: err.Error()})
	}
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
		for i, r := range reqs {
			if ctx.Err() != nil {
				out.Stopped = true
				break
			}
			if i > 0 && o.Delay > 0 {
				time.Sleep(o.Delay)
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
					continue
				}
			}
			res := Run(ctx, r, t)
			st.URL, st.Status, st.Ms, st.Tests, st.Error = res.URL, res.Status, res.Ms, res.Tests, res.Error
			st.Pass = res.Status > 0 && res.Status < 500 && res.Error == ""
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
		}
		if out.Stopped {
			break
		}
	}
	t.Vars = base
	out.Ms = time.Since(start).Milliseconds()
	return out
}
