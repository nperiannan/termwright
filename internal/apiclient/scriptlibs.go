package apiclient

import (
	"encoding/xml"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// The libraries Postman's sandbox has: lodash (as _ and require('lodash')), moment, xml2Json and tv4.
// Compiled once; run in each script's runtime before the pm.* prelude.

var (
	libsOnce sync.Once
	libsProg *goja.Program
	libsErr  error
)

func runLibs(vm *goja.Runtime) error {
	libsOnce.Do(func() { libsProg, libsErr = goja.Compile("libs.js", scriptLibs, false) })
	if libsErr != nil {
		return libsErr
	}
	_ = vm.Set("__xml2json", xmlToMap)
	_ = vm.Set("__fmtTime", func(ms int64, layout string, utc bool) string {
		t := time.UnixMilli(ms)
		if utc {
			t = t.UTC()
		}
		return t.Format(layout)
	})
	_ = vm.Set("__parseTime", func(s, layout string, utc bool) any {
		loc := time.Local
		if utc {
			loc = time.UTC
		}
		var t time.Time
		var err error
		if layout == "" {
			for _, l := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02", time.RFC1123, time.RFC1123Z, time.RFC822, time.RFC850, time.ANSIC} {
				if t, err = time.ParseInLocation(l, s, loc); err == nil {
					break
				}
			}
		} else {
			t, err = time.ParseInLocation(layout, s, loc)
		}
		if err != nil {
			return nil
		}
		return t.UnixMilli()
	})
	_, err := vm.RunProgram(libsProg)
	return err
}

// xmlToMap turns XML into Postman's xml2Json shape: elements are objects, attributes go in "$",
// text in "_" (or the plain text when an element has nothing else), repeated elements are arrays.
func xmlToMap(s string) any {
	d := xml.NewDecoder(strings.NewReader(s))
	d.Strict = false
	type node struct {
		name string
		m    map[string]any
		text strings.Builder
	}
	root := &node{m: map[string]any{}}
	stack := []*node{root}
	add := func(m map[string]any, k string, v any) {
		if old, ok := m[k]; ok {
			if l, ok := old.([]any); ok {
				m[k] = append(l, v)
			} else {
				m[k] = []any{old, v}
			}
			return
		}
		m[k] = v
	}
	for {
		tok, err := d.Token()
		if err == io.EOF || err != nil {
			break
		}
		switch x := tok.(type) {
		case xml.StartElement:
			n := &node{name: x.Name.Local, m: map[string]any{}}
			if len(x.Attr) > 0 {
				at := map[string]any{}
				for _, a := range x.Attr {
					at[a.Name.Local] = a.Value
				}
				n.m["$"] = at
			}
			stack = append(stack, n)
		case xml.CharData:
			stack[len(stack)-1].text.Write(x)
		case xml.EndElement:
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			txt := strings.TrimSpace(n.text.String())
			var v any = n.m
			if len(n.m) == 0 {
				v = txt
			} else if txt != "" {
				n.m["_"] = txt
			}
			add(stack[len(stack)-1].m, n.name, v)
		}
	}
	return root.m
}

const scriptLibs = `
var _ = (function() {
  var _ = {};
  function path(p) { if (Array.isArray(p)) return p; return String(p).replace(/\[(\w+|"[^"]*"|'[^']*')\]/g, '.$1').replace(/["']/g, '').split('.').filter(function(x) { return x !== ''; }); }
  function iter(f) { if (typeof f === 'function') return f; if (f == null) return function(x) { return x; };
    if (typeof f === 'string') return function(x) { return _.get(x, f); };
    if (Array.isArray(f)) return function(x) { return _.isEqual(_.get(x, f[0]), f[1]); };
    return function(x) { return _.isMatch(x, f); }; }
  function vals(c) { return c == null ? [] : Array.isArray(c) ? c : typeof c === 'string' ? c.split('') : Object.keys(c).map(function(k) { return c[k]; }); }
  function each(c, f) { if (Array.isArray(c)) { for (var i = 0; i < c.length; i++) if (f(c[i], i, c) === false) break; } else if (c != null) { var ks = Object.keys(c); for (var j = 0; j < ks.length; j++) if (f(c[ks[j]], ks[j], c) === false) break; } return c; }
  _.get = function(o, p, d) { var ks = path(p); for (var i = 0; i < ks.length; i++) { if (o == null) return d; o = o[ks[i]]; } return o === undefined ? d : o; };
  _.has = function(o, p) { var ks = path(p); for (var i = 0; i < ks.length; i++) { if (o == null || !Object.prototype.hasOwnProperty.call(o, ks[i])) return false; o = o[ks[i]]; } return true; };
  _.set = function(o, p, v) { var ks = path(p), c = o; for (var i = 0; i < ks.length - 1; i++) { if (c[ks[i]] == null) c[ks[i]] = /^\d+$/.test(ks[i + 1]) ? [] : {}; c = c[ks[i]]; } c[ks[ks.length - 1]] = v; return o; };
  _.unset = function(o, p) { var ks = path(p), last = ks.pop(), c = _.get(o, ks); if (c != null) delete c[last]; return true; };
  _.each = _.forEach = each;
  _.map = function(c, f) { f = iter(f); var r = []; each(c, function(v, k) { r.push(f(v, k, c)); }); return r; };
  _.filter = function(c, f) { f = iter(f); var r = []; each(c, function(v, k) { if (f(v, k, c)) r.push(v); }); return r; };
  _.reject = function(c, f) { f = iter(f); return _.filter(c, function(v, k) { return !f(v, k, c); }); };
  _.find = function(c, f) { f = iter(f); var r; each(c, function(v, k) { if (f(v, k, c)) { r = v; return false; } }); return r; };
  _.findIndex = function(a, f) { f = iter(f); for (var i = 0; i < (a || []).length; i++) if (f(a[i], i, a)) return i; return -1; };
  _.some = function(c, f) { return _.find(c, f) !== undefined || _.filter(c, f).length > 0; };
  _.every = function(c, f) { f = iter(f); var ok = true; each(c, function(v, k) { if (!f(v, k, c)) { ok = false; return false; } }); return ok; };
  _.reduce = function(c, f, acc) { var first = arguments.length < 3; each(c, function(v, k) { if (first) { acc = v; first = false; } else acc = f(acc, v, k, c); }); return acc; };
  _.includes = function(c, v) { return typeof c === 'string' ? c.indexOf(v) >= 0 : vals(c).some(function(x) { return _.isEqual(x, v); }); };
  _.keys = function(o) { return o == null ? [] : Object.keys(o); };
  _.values = vals;
  _.entries = _.toPairs = function(o) { return Object.keys(o || {}).map(function(k) { return [k, o[k]]; }); };
  _.fromPairs = function(l) { var o = {}; (l || []).forEach(function(p) { o[p[0]] = p[1]; }); return o; };
  _.pick = function(o, ks) { ks = Array.isArray(ks) ? ks : Array.prototype.slice.call(arguments, 1); var r = {}; ks.forEach(function(k) { if (_.has(o, k)) _.set(r, k, _.get(o, k)); }); return r; };
  _.omit = function(o, ks) { ks = Array.isArray(ks) ? ks : Array.prototype.slice.call(arguments, 1); var r = _.cloneDeep(o) || {}; ks.forEach(function(k) { _.unset(r, k); }); return r; };
  _.mapValues = function(o, f) { f = iter(f); var r = {}; each(o, function(v, k) { r[k] = f(v, k, o); }); return r; };
  _.mapKeys = function(o, f) { f = iter(f); var r = {}; each(o, function(v, k) { r[f(v, k, o)] = v; }); return r; };
  _.groupBy = function(c, f) { f = iter(f); var r = {}; each(c, function(v) { var k = f(v); (r[k] = r[k] || []).push(v); }); return r; };
  _.keyBy = function(c, f) { f = iter(f); var r = {}; each(c, function(v) { r[f(v)] = v; }); return r; };
  _.countBy = function(c, f) { f = iter(f); var r = {}; each(c, function(v) { var k = f(v); r[k] = (r[k] || 0) + 1; }); return r; };
  _.sortBy = function(c, fs) { fs = (Array.isArray(fs) ? fs : [fs]).map(iter); return vals(c).slice().sort(function(a, b) { for (var i = 0; i < fs.length; i++) { var x = fs[i](a), y = fs[i](b); if (x < y) return -1; if (x > y) return 1; } return 0; }); };
  _.orderBy = function(c, fs, ord) { fs = (Array.isArray(fs) ? fs : [fs]).map(iter); ord = ord || []; return vals(c).slice().sort(function(a, b) { for (var i = 0; i < fs.length; i++) { var x = fs[i](a), y = fs[i](b), d = ord[i] === 'desc' ? -1 : 1; if (x < y) return -d; if (x > y) return d; } return 0; }); };
  _.uniq = function(a) { var r = []; (a || []).forEach(function(v) { if (!r.some(function(x) { return _.isEqual(x, v); })) r.push(v); }); return r; };
  _.uniqBy = function(a, f) { f = iter(f); var seen = [], r = []; (a || []).forEach(function(v) { var k = f(v); if (seen.indexOf(k) < 0) { seen.push(k); r.push(v); } }); return r; };
  _.flatten = function(a) { return [].concat.apply([], a || []); };
  _.flattenDeep = function(a) { return (a || []).reduce(function(r, v) { return r.concat(Array.isArray(v) ? _.flattenDeep(v) : v); }, []); };
  _.compact = function(a) { return (a || []).filter(Boolean); };
  _.chunk = function(a, n) { n = n || 1; var r = []; for (var i = 0; i < (a || []).length; i += n) r.push(a.slice(i, i + n)); return r; };
  _.difference = function(a, b) { return (a || []).filter(function(v) { return !_.includes(b || [], v); }); };
  _.intersection = function(a, b) { return _.uniq((a || []).filter(function(v) { return _.includes(b || [], v); })); };
  _.union = function() { return _.uniq(_.flatten(Array.prototype.slice.call(arguments))); };
  _.without = function(a) { var ex = Array.prototype.slice.call(arguments, 1); return (a || []).filter(function(v) { return ex.indexOf(v) < 0; }); };
  _.zip = function() { var ls = Array.prototype.slice.call(arguments), n = Math.max.apply(null, ls.map(function(l) { return l.length; })), r = []; for (var i = 0; i < n; i++) r.push(ls.map(function(l) { return l[i]; })); return r; };
  _.first = _.head = function(a) { return a ? a[0] : undefined; };
  _.last = function(a) { return a ? a[a.length - 1] : undefined; };
  _.take = function(a, n) { return (a || []).slice(0, n == null ? 1 : n); };
  _.drop = function(a, n) { return (a || []).slice(n == null ? 1 : n); };
  _.range = function(s, e, st) { if (e === undefined) { e = s; s = 0; } st = st || (e < s ? -1 : 1); var r = []; for (var i = s; st > 0 ? i < e : i > e; i += st) r.push(i); return r; };
  _.times = function(n, f) { var r = []; for (var i = 0; i < n; i++) r.push(f ? f(i) : i); return r; };
  _.sum = function(a) { return (a || []).reduce(function(s, v) { return s + v; }, 0); };
  _.sumBy = function(a, f) { f = iter(f); return _.sum(_.map(a, f)); };
  _.mean = function(a) { return a && a.length ? _.sum(a) / a.length : NaN; };
  _.max = function(a) { return a && a.length ? Math.max.apply(null, a) : undefined; };
  _.min = function(a) { return a && a.length ? Math.min.apply(null, a) : undefined; };
  _.maxBy = function(a, f) { f = iter(f); return _.reduce(a, function(m, v) { return f(v) > f(m) ? v : m; }); };
  _.minBy = function(a, f) { f = iter(f); return _.reduce(a, function(m, v) { return f(v) < f(m) ? v : m; }); };
  _.size = function(c) { return c == null ? 0 : (Array.isArray(c) || typeof c === 'string') ? c.length : Object.keys(c).length; };
  _.isEmpty = function(v) { return v == null || (typeof v === 'object' ? Object.keys(v).length === 0 : typeof v === 'string' ? v.length === 0 : false); };
  _.isEqual = function(a, b) { if (a === b) return true; if (a == null || b == null || typeof a !== 'object' || typeof b !== 'object') return a !== a && b !== b;
    if (Array.isArray(a) !== Array.isArray(b)) return false; var ka = Object.keys(a), kb = Object.keys(b); if (ka.length !== kb.length) return false;
    for (var i = 0; i < ka.length; i++) if (!_.isEqual(a[ka[i]], b[ka[i]])) return false; return true; };
  _.isMatch = function(o, src) { if (o == null) return false; for (var k in src) { if (typeof src[k] === 'object' && src[k] !== null ? !_.isMatch(o[k], src[k]) : !_.isEqual(o[k], src[k])) return false; } return true; };
  _.cloneDeep = function(v) { return v === undefined ? v : JSON.parse(JSON.stringify(v)); };
  _.clone = function(v) { return Array.isArray(v) ? v.slice() : v && typeof v === 'object' ? Object.assign({}, v) : v; };
  _.merge = function(t) { for (var i = 1; i < arguments.length; i++) { var s = arguments[i]; for (var k in s) { if (s[k] && typeof s[k] === 'object' && !Array.isArray(s[k]) && t[k] && typeof t[k] === 'object') _.merge(t[k], s[k]); else if (s[k] !== undefined) t[k] = _.cloneDeep(s[k]); } } return t; };
  _.assign = _.extend = function() { return Object.assign.apply(Object, arguments); };
  _.defaults = function(t) { for (var i = 1; i < arguments.length; i++) for (var k in arguments[i]) if (t[k] === undefined) t[k] = arguments[i][k]; return t; };
  _.invert = function(o) { var r = {}; for (var k in o) r[o[k]] = k; return r; };
  _.isArray = Array.isArray;
  _.isString = function(v) { return typeof v === 'string'; };
  _.isNumber = function(v) { return typeof v === 'number'; };
  _.isInteger = Number.isInteger;
  _.isBoolean = function(v) { return typeof v === 'boolean'; };
  _.isObject = function(v) { return v !== null && (typeof v === 'object' || typeof v === 'function'); };
  _.isPlainObject = function(v) { return v !== null && typeof v === 'object' && !Array.isArray(v); };
  _.isFunction = function(v) { return typeof v === 'function'; };
  _.isNil = function(v) { return v == null; };
  _.isNull = function(v) { return v === null; };
  _.isUndefined = function(v) { return v === undefined; };
  _.isNaN = function(v) { return typeof v === 'number' && v !== v; };
  _.toNumber = Number; _.toString = function(v) { return v == null ? '' : String(v); };
  _.toInteger = function(v) { v = Number(v); return v !== v ? 0 : Math.trunc(v); };
  _.random = function(a, b, fl) { if (b === undefined) { b = a; a = 0; } return fl ? a + Math.random() * (b - a) : a + Math.floor(Math.random() * (b - a + 1)); };
  _.sample = function(a) { a = vals(a); return a[Math.floor(Math.random() * a.length)]; };
  _.shuffle = function(a) { a = vals(a).slice(); for (var i = a.length - 1; i > 0; i--) { var j = Math.floor(Math.random() * (i + 1)), t = a[i]; a[i] = a[j]; a[j] = t; } return a; };
  _.clamp = function(n, lo, hi) { return Math.min(Math.max(n, lo), hi); };
  _.inRange = function(n, s, e) { if (e === undefined) { e = s; s = 0; } return n >= Math.min(s, e) && n < Math.max(s, e); };
  _.round = function(n, p) { var m = Math.pow(10, p || 0); return Math.round(n * m) / m; };
  _.words = function(s) { return String(s).match(/[A-Z]{2,}(?=[A-Z][a-z]|\b|\d)|[A-Z]?[a-z]+|[A-Z]+|\d+/g) || []; };
  _.camelCase = function(s) { return _.words(s).map(function(w, i) { w = w.toLowerCase(); return i ? w.charAt(0).toUpperCase() + w.slice(1) : w; }).join(''); };
  _.snakeCase = function(s) { return _.words(s).map(function(w) { return w.toLowerCase(); }).join('_'); };
  _.kebabCase = function(s) { return _.words(s).map(function(w) { return w.toLowerCase(); }).join('-'); };
  _.capitalize = function(s) { s = String(s); return s.charAt(0).toUpperCase() + s.slice(1).toLowerCase(); };
  _.upperFirst = function(s) { s = String(s); return s.charAt(0).toUpperCase() + s.slice(1); };
  _.trim = function(s, c) { s = String(s == null ? '' : s); if (!c) return s.trim(); var e = c.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'); return s.replace(new RegExp('^[' + e + ']+|[' + e + ']+$', 'g'), ''); };
  _.padStart = function(s, n, c) { return String(s).padStart(n, c || ' '); };
  _.padEnd = function(s, n, c) { return String(s).padEnd(n, c || ' '); };
  _.repeat = function(s, n) { return String(s).repeat(n); };
  _.startsWith = function(s, t) { return String(s).indexOf(t) === 0; };
  _.endsWith = function(s, t) { s = String(s); return s.slice(-String(t).length) === String(t); };
  _.escape = function(s) { return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;'); };
  _.template = function(s) { return function(d) { return String(s).replace(/<%=\s*([\w.]+)\s*%>/g, function(m, k) { var v = _.get(d, k); return v == null ? '' : v; }); }; };
  _.uniqueId = (function() { var n = 0; return function(p) { return (p || '') + (++n); }; })();
  _.identity = function(v) { return v; };
  _.noop = function() {};
  _.now = function() { return Date.now(); };
  _.chain = function(v) { var w = { value: function() { return v; } }; Object.keys(_).forEach(function(k) { if (typeof _[k] === 'function') w[k] = function() { v = _[k].apply(null, [v].concat(Array.prototype.slice.call(arguments))); return w; }; }); return w; };
  _.VERSION = '4.17-subset';
  return _;
})();

var moment = (function() {
  var pad = function(n, l) { n = String(Math.abs(n)); while (n.length < (l || 2)) n = '0' + n; return n; };
  var MON = ['January','February','March','April','May','June','July','August','September','October','November','December'];
  var DAY = ['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'];
  var UNIT = { ms: 1, millisecond: 1, milliseconds: 1, s: 1000, second: 1000, seconds: 1000, m: 60000, minute: 60000, minutes: 60000, h: 3600000, hour: 3600000, hours: 3600000, d: 86400000, day: 86400000, days: 86400000, w: 604800000, week: 604800000, weeks: 604800000 };
  function M(ms, utc) { this._d = new Date(ms); this._utc = !!utc; }
  function g(m, f) { return m._utc ? m._d['getUTC' + f]() : m._d['get' + f](); }
  function s(m, f, v) { m._utc ? m._d['setUTC' + f](v) : m._d['set' + f](v); }
  function tok(m, t) {
    var Y = g(m, 'FullYear'), Mo = g(m, 'Month'), D = g(m, 'Date'), H = g(m, 'Hours'), mi = g(m, 'Minutes'), se = g(m, 'Seconds'), ms = g(m, 'Milliseconds'), wd = g(m, 'Day');
    var off = m._utc ? 0 : -m._d.getTimezoneOffset(), z = (off >= 0 ? '+' : '-') + pad(Math.floor(Math.abs(off) / 60)) + ':' + pad(Math.abs(off) % 60);
    switch (t) {
      case 'YYYY': return String(Y); case 'YY': return pad(Y % 100); case 'MMMM': return MON[Mo]; case 'MMM': return MON[Mo].slice(0, 3);
      case 'MM': return pad(Mo + 1); case 'M': return String(Mo + 1); case 'DD': return pad(D); case 'D': return String(D);
      case 'dddd': return DAY[wd]; case 'ddd': return DAY[wd].slice(0, 3); case 'd': return String(wd);
      case 'HH': return pad(H); case 'H': return String(H); case 'hh': return pad(H % 12 || 12); case 'h': return String(H % 12 || 12);
      case 'mm': return pad(mi); case 'm': return String(mi); case 'ss': return pad(se); case 's': return String(se);
      case 'SSS': return pad(ms, 3); case 'A': return H < 12 ? 'AM' : 'PM'; case 'a': return H < 12 ? 'am' : 'pm';
      case 'Z': return z; case 'ZZ': return z.replace(':', ''); case 'X': return String(Math.floor(m._d.getTime() / 1000)); case 'x': return String(m._d.getTime());
    }
    return t;
  }
  M.prototype = {
    format: function(f) { var me = this; if (!f) return this._utc ? this._d.toISOString().replace('.000Z', 'Z').replace(/\.\d{3}Z$/, 'Z') : tok(this, 'YYYY') + '-' + tok(this, 'MM') + '-' + tok(this, 'DD') + 'T' + tok(this, 'HH') + ':' + tok(this, 'mm') + ':' + tok(this, 'ss') + tok(this, 'Z');
      return f.replace(/\[([^\]]*)\]|YYYY|YY|MMMM|MMM|MM|M|DD|D|dddd|ddd|d|HH|H|hh|h|mm|m|ss|s|SSS|A|a|ZZ|Z|X|x/g, function(t, lit) { return lit !== undefined ? lit : tok(me, t); }); },
    toISOString: function() { return this._d.toISOString(); }, toJSON: function() { return this._d.toISOString(); }, toDate: function() { return new Date(this._d.getTime()); },
    toString: function() { return this._d.toString(); }, valueOf: function() { return this._d.getTime(); }, unix: function() { return Math.floor(this._d.getTime() / 1000); },
    utc: function() { this._utc = true; return this; }, local: function() { this._utc = false; return this; }, isValid: function() { return !isNaN(this._d.getTime()); },
    clone: function() { return new M(this._d.getTime(), this._utc); },
    add: function(n, u) { if (typeof n === 'object') { for (var k in n) this.add(n[k], k); return this; } u = String(u || 'ms');
      if (/^(M|months?)$/.test(u)) s(this, 'Month', g(this, 'Month') + Number(n)); else if (/^(y|years?)$/.test(u)) s(this, 'FullYear', g(this, 'FullYear') + Number(n));
      else this._d = new Date(this._d.getTime() + Number(n) * (UNIT[u] || 1)); return this; },
    subtract: function(n, u) { if (typeof n === 'object') { for (var k in n) this.add(-n[k], k); return this; } return this.add(-n, u); },
    diff: function(o, u, fl) { o = moment(o); var d = this._d.getTime() - o._d.getTime(); u = String(u || 'ms');
      if (/^(M|months?)$/.test(u)) d = (g(this, 'FullYear') - g(o, 'FullYear')) * 12 + g(this, 'Month') - g(o, 'Month');
      else if (/^(y|years?)$/.test(u)) d = g(this, 'FullYear') - g(o, 'FullYear'); else d = d / (UNIT[u] || 1);
      return fl ? d : Math.trunc(d); },
    startOf: function(u) { u = String(u); if (/^(y|years?)$/.test(u)) s(this, 'Month', 0); if (/^(y|years?|M|months?)$/.test(u)) s(this, 'Date', 1);
      if (/^(y|years?|M|months?|d|days?|day)$/.test(u)) s(this, 'Hours', 0); if (!/^(m|minutes?|s|seconds?)$/.test(u)) s(this, 'Minutes', 0);
      if (!/^(s|seconds?)$/.test(u)) s(this, 'Seconds', 0); s(this, 'Milliseconds', 0); return this; },
    endOf: function(u) { u = String(u); var unit = /^(y|years?)$/.test(u) ? 'year' : /^(M|months?)$/.test(u) ? 'month' : u; return this.startOf(u).add(1, unit === 'year' ? 'y' : unit === 'month' ? 'M' : unit).subtract(1, 'ms'); },
    isBefore: function(o) { return this.valueOf() < moment(o).valueOf(); }, isAfter: function(o) { return this.valueOf() > moment(o).valueOf(); },
    isSame: function(o) { return this.valueOf() === moment(o).valueOf(); }, isBetween: function(a, b) { return this.isAfter(a) && this.isBefore(b); },
    year: function() { return g(this, 'FullYear'); }, month: function() { return g(this, 'Month'); }, date: function() { return g(this, 'Date'); }, day: function() { return g(this, 'Day'); },
    hour: function() { return g(this, 'Hours'); }, minute: function() { return g(this, 'Minutes'); }, second: function() { return g(this, 'Seconds'); }, millisecond: function() { return g(this, 'Milliseconds'); },
    fromNow: function() { var d = (Date.now() - this.valueOf()) / 1000, a = Math.abs(d), t = a < 45 ? 'a few seconds' : a < 90 ? 'a minute' : a < 2700 ? Math.round(a / 60) + ' minutes' : a < 5400 ? 'an hour' : a < 79200 ? Math.round(a / 3600) + ' hours' : a < 129600 ? 'a day' : Math.round(a / 86400) + ' days'; return d >= 0 ? t + ' ago' : 'in ' + t; },
  };
  function moment(v, f, utc) {
    if (v instanceof M) return v.clone();
    if (v === undefined || v === null) return new M(Date.now(), utc);
    if (v instanceof Date) return new M(v.getTime(), utc);
    if (typeof v === 'number') return new M(v, utc);
    if (typeof v === 'string' && f) { var lay = String(f).replace(/YYYY/g, '2006').replace(/MM/g, '01').replace(/DD/g, '02').replace(/HH/g, '15').replace(/mm/g, '04').replace(/ss/g, '05').replace(/SSS/g, '000');
      var p = __parseTime(v, lay, !!utc); return new M(p === null ? NaN : p, utc); }
    var t = Date.parse(v); if (isNaN(t)) { var q = __parseTime(String(v), '', !!utc); t = q === null ? NaN : q; } return new M(t, utc);
  }
  moment.utc = function(v, f) { var m = moment(v, f, true); m._utc = true; return m; };
  moment.unix = function(sec) { return new M(sec * 1000); };
  moment.isMoment = function(v) { return v instanceof M; };
  moment.duration = function(n, u) { var ms = Number(n) * (UNIT[u || 'ms'] || 1); return { asMilliseconds: function() { return ms; }, asSeconds: function() { return ms / 1000; }, asMinutes: function() { return ms / 60000; }, asHours: function() { return ms / 3600000; }, asDays: function() { return ms / 86400000; } }; };
  moment.version = '2-subset';
  return moment;
})();

var xml2Json = function(x) { return __xml2json(String(x)); };
var tv4 = { error: null, validate: function(data, schema) { var e = __schema(typeof schema === 'string' ? schema : JSON.stringify(schema), JSON.stringify(data)); tv4.error = e ? { message: e } : null; return !e; } };
tv4.validateResult = function(d, s) { var ok = tv4.validate(d, s); return { valid: ok, error: tv4.error }; };
`
