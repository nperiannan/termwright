package apiclient

import (
	"bytes"
	"compress/gzip"
	"embed"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// The libraries of Postman's script sandbox, the real ones (bundled with esbuild, gzip, in jslibs; their
// licenses in jslibs/LICENSES.txt): lodash, moment, cheerio, xml2js, crypto-js, tv4, chai, ajv, csv-parse,
// uuid and the Node modules path, url, querystring, util, events, buffer, assert, punycode, string_decoder,
// stream and timers. Each is compiled once and run in a script's runtime only when the script uses it.

//go:embed jslibs/*.js.gz
var jsLibFS embed.FS

type jsLib struct {
	once sync.Once
	prog *goja.Program
	err  error
}

var jsLibs = map[string]*jsLib{}
var jsLibsMu sync.Mutex

// jsLibGlobal: where a library leaves its value (the esbuild bundles: __lib; lodash and moment are UMD).
var jsLibGlobal = map[string]string{"lodash": "_", "moment": "moment"}

func jsLibProgram(name string) (*goja.Program, error) {
	jsLibsMu.Lock()
	l := jsLibs[name]
	if l == nil {
		l = &jsLib{}
		jsLibs[name] = l
	}
	jsLibsMu.Unlock()
	l.once.Do(func() {
		gz, err := jsLibFS.ReadFile("jslibs/" + name + ".js.gz")
		if err != nil {
			l.err = fmt.Errorf("require(%q): not available in Termwright scripts", name)
			return
		}
		zr, err := gzip.NewReader(bytes.NewReader(gz))
		if err != nil {
			l.err = err
			return
		}
		src, err := io.ReadAll(zr)
		if err != nil {
			l.err = err
			return
		}
		l.prog, l.err = goja.Compile(name+".js", string(src), false)
	})
	return l.prog, l.err
}

// JSLibNames lists the libraries scripts can require.
func JSLibNames() []string {
	es, _ := jsLibFS.ReadDir("jslibs")
	var out []string
	for _, e := range es {
		if n := e.Name(); len(n) > 6 && n[len(n)-6:] == ".js.gz" {
			out = append(out, n[:len(n)-6])
		}
	}
	sort.Strings(out)
	return out
}

// jsTimers: setTimeout / setInterval / setImmediate of a script, run after the script's own code.
type jsTimers struct {
	seq  int
	list []*jsTimer
}

type jsTimer struct {
	id    int
	due   time.Time
	every time.Duration
	fn    goja.Callable
	args  []goja.Value
}

func runLibs(vm *goja.Runtime) error {
	_ = vm.Set("__loadLib", func(name string) goja.Value {
		prog, err := jsLibProgram(name)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		if _, err := vm.RunProgram(prog); err != nil {
			panic(vm.NewGoError(fmt.Errorf("%s: %w", name, err)))
		}
		g := "__lib"
		if x, ok := jsLibGlobal[name]; ok {
			g = x
		}
		return vm.Get(g)
	})
	_, err := vm.RunString(scriptLibs)
	return err
}

// setTimers gives the runtime its timers; drain runs them (in time order, waiting for each) until none is
// left or stop says so.
func setTimers(vm *goja.Runtime) *jsTimers {
	ts := &jsTimers{}
	add := func(call goja.FunctionCall, every bool) goja.Value {
		fn, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			panic(vm.NewTypeError("the first argument must be a function"))
		}
		ms := call.Argument(1).ToInteger()
		if ms < 0 {
			ms = 0
		}
		ts.seq++
		t := &jsTimer{id: ts.seq, due: time.Now().Add(time.Duration(ms) * time.Millisecond), fn: fn}
		if every {
			t.every = max(time.Duration(ms)*time.Millisecond, time.Millisecond)
		}
		if len(call.Arguments) > 2 {
			t.args = call.Arguments[2:]
		}
		ts.list = append(ts.list, t)
		return vm.ToValue(t.id)
	}
	clear := func(id int64) {
		for i, t := range ts.list {
			if int64(t.id) == id {
				ts.list = append(ts.list[:i], ts.list[i+1:]...)
				return
			}
		}
	}
	_ = vm.Set("setTimeout", func(c goja.FunctionCall) goja.Value { return add(c, false) })
	_ = vm.Set("setInterval", func(c goja.FunctionCall) goja.Value { return add(c, true) })
	_ = vm.Set("setImmediate", func(c goja.FunctionCall) goja.Value {
		args := append([]goja.Value{c.Argument(0), vm.ToValue(0)}, c.Arguments[min(1, len(c.Arguments)):]...)
		return add(goja.FunctionCall{This: c.This, Arguments: args}, false)
	})
	for _, n := range []string{"clearTimeout", "clearInterval", "clearImmediate"} {
		_ = vm.Set(n, func(id int64) { clear(id) })
	}
	return ts
}

func (ts *jsTimers) drain(stop <-chan struct{}) error {
	for len(ts.list) > 0 {
		sort.SliceStable(ts.list, func(i, j int) bool { return ts.list[i].due.Before(ts.list[j].due) })
		t := ts.list[0]
		if d := time.Until(t.due); d > 0 {
			select {
			case <-time.After(d):
			case <-stop:
				return errors.New("the script was stopped while waiting for a timer")
			}
		}
		if t.every > 0 {
			t.due = time.Now().Add(t.every)
		} else {
			ts.list = ts.list[1:]
		}
		if _, err := t.fn(goja.Undefined(), t.args...); err != nil {
			return err
		}
	}
	return nil
}

// scriptLibs: the libraries as globals (loaded when first used), require(), and xml2Json as Postman has it.
const scriptLibs = `
var __libs = {};
function __getLib(n) {
  if (!Object.prototype.hasOwnProperty.call(__libs, n)) { __libs[n] = undefined; try { __libs[n] = __loadLib(n); } catch (e) { delete __libs[n]; throw e; } } // undefined while it loads: lodash reads _ then
  return __libs[n];
}
(function(g) {
  function lazy(name, lib, pick) {
    Object.defineProperty(g, name, { configurable: true,
      get: function() { var v = __getLib(lib); if (pick) v = pick(v); Object.defineProperty(g, name, { value: v, writable: true, configurable: true }); return v; },
      set: function(v) { Object.defineProperty(g, name, { value: v, writable: true, configurable: true }); } });
  }
  lazy('_', 'lodash'); lazy('moment', 'moment'); lazy('CryptoJS', 'crypto-js'); lazy('tv4', 'tv4'); lazy('cheerio', 'cheerio');
  lazy('Buffer', 'buffer', function(b) { return b.Buffer; });
})(this);
var __requireNames = { 'lodash': 'lodash', 'moment': 'moment', 'cheerio': 'cheerio', 'xml2js': 'xml2js', 'crypto-js': 'crypto-js', 'tv4': 'tv4', 'chai': 'chai',
  'ajv': 'ajv', 'csv-parse/lib/sync': 'csv-parse', 'uuid': 'uuid', 'path': 'path', 'url': 'url', 'querystring': 'querystring', 'util': 'util', 'events': 'events',
  'buffer': 'buffer', 'assert': 'assert', 'punycode': 'punycode', 'string_decoder': 'string_decoder', 'stream': 'stream' };
function require(n) {
  n = String(n);
  if (n === 'atob') return atob;
  if (n === 'btoa') return btoa;
  if (n === 'timers') return { setTimeout: setTimeout, clearTimeout: clearTimeout, setInterval: setInterval, clearInterval: clearInterval, setImmediate: setImmediate, clearImmediate: clearImmediate };
  if (Object.prototype.hasOwnProperty.call(__requireNames, n)) return __getLib(__requireNames[n]);
  throw new Error('require("' + n + '") is not available in Termwright scripts (' + Object.keys(__requireNames).concat(['atob', 'btoa', 'timers']).join(', ') + ' are)');
}
var xml2Json = function(s) { var out = {}; require('xml2js').parseString(String(s), { explicitArray: false, async: false, trim: true, mergeAttrs: false }, function(e, r) { if (e) throw e; out = r; }); return out; };
`
