// termwright_crt.js - the "crt" script object for VBScript logon scripts.
//
// Termwright runs a .vbs script with Windows Script Host (cscript) inside a
// .wsf job: this JScript part defines `crt` (JScript functions accept optional
// arguments, which VBScript classes cannot), the user's VBScript follows, and
// Main is called. Calls go to Termwright as HTTP POSTs to a local port with a
// random token. Property-style values (CurrentRow, Connected, …) are refreshed
// with every reply.

var TW = (function () {
  var env = new ActiveXObject("WScript.Shell").Environment("PROCESS");
  return { addr: env("TERMWRIGHT_RPC"), token: env("TERMWRIGHT_TOKEN"), tab: env("TERMWRIGHT_TAB"), n: 0 };
})();

function twStr(s) {
  s = String(s);
  var o = "";
  for (var i = 0; i < s.length; i++) {
    var c = s.charAt(i), a = s.charCodeAt(i);
    if (c == '"') o += '\\"';
    else if (c == "\\") o += "\\\\";
    else if (a < 32) o += "\\u" + ("000" + a.toString(16)).slice(-4);
    else o += c;
  }
  return '"' + o + '"';
}

function twJSON(v) {
  if (v === null || typeof v == "undefined") return "null";
  if (typeof v == "boolean" || typeof v == "number") return String(v);
  if (typeof v == "unknown") { // a VBScript Array arrives as a SAFEARRAY
    try { v = new VBArray(v).toArray(); } catch (e) { return "null"; }
  }
  if (v instanceof Array) {
    var a = [];
    for (var i = 0; i < v.length; i++) a.push(twJSON(v[i]));
    return "[" + a.join(",") + "]";
  }
  if (typeof v == "object") {
    var p = [];
    for (var k in v) p.push(twStr(k) + ":" + twJSON(v[k]));
    return "{" + p.join(",") + "}";
  }
  return twStr(v);
}

// toList turns a VBScript Array (SAFEARRAY) or a JScript array into a JScript array.
function twList(v) {
  if (v instanceof Array) return v;
  try { return new VBArray(v).toArray(); } catch (e) { return [String(v)]; }
}

function twIsArray(v) {
  return v instanceof Array || typeof v == "unknown";
}

var twLast = null; // last state piggybacked on a reply

function twCall(op, args) {
  var http = new ActiveXObject("MSXML2.ServerXMLHTTP.6.0");
  http.setTimeouts(5000, 5000, 60000, 86400000); // waits may last long
  http.open("POST", "http://" + TW.addr + "/rpc", false);
  http.setRequestHeader("Content-Type", "application/json; charset=utf-8");
  http.setRequestHeader("X-Termwright-Token", TW.token);
  TW.n++;
  http.send('{"id":' + TW.n + ',"op":' + twStr(op) + ',"args":' + twJSON(args || {}) + "}");
  if (http.status != 200) throw new Error("Termwright: HTTP " + http.status);
  var r = eval("(" + http.responseText + ")"); // reply of the local Termwright bridge
  if (r.st) { twLast = r.st; twRefresh(); }
  if (r.error) throw new Error(r.error);
  return r.result;
}

// ---- Screen ----

function TwScreen(tab) {
  this._tab = tab;
  this.Synchronous = false;
  this.IgnoreEscape = false;
  this.IgnoreCase = false;
  this.MatchIndex = 0;
  this.CurrentRow = 1;
  this.CurrentColumn = 1;
  this.Rows = 24;
  this.Columns = 80;
}
TwScreen.prototype.Send = function (text) { twCall("send", { tab: this._tab.Id, text: String(text) }); };
TwScreen.prototype.SendKeys = TwScreen.prototype.Send;
TwScreen.prototype.SendSpecial = function (name) {
  var keys = { VT_ENTER: "\r", VK_RETURN: "\r", VK_TAB: "\t", VK_ESCAPE: "\x1b", VK_BACK: "\x7f" };
  if (/BREAK/.test(name)) twCall("break", { tab: this._tab.Id });
  else if (keys[name]) this.Send(keys[name]);
};
// args: strings / an Array, then optional timeout (seconds) and case-insensitive flag
TwScreen.prototype._wait = function (a) {
  var strs = [], rest = [];
  for (var i = 0; i < a.length; i++) {
    if (typeof a[i] == "undefined") continue;
    if (twIsArray(a[i])) strs = strs.concat(twList(a[i]));
    else if (typeof a[i] == "string" && rest.length == 0) strs.push(a[i]);
    else rest.push(a[i]);
  }
  var timeout = rest.length > 0 && typeof rest[0] != "boolean" ? Number(rest[0]) || 0 : 0;
  var ci = rest.length > 1 ? !!rest[1] : (rest.length == 1 && typeof rest[0] == "boolean" ? rest[0] : false);
  var r = twCall("wait", { tab: this._tab.Id, patterns: strs, timeout: timeout, ci: ci || this.IgnoreCase });
  this.MatchIndex = r.index >= 0 ? r.index + 1 : 0;
  return r;
};
TwScreen.prototype.WaitForString = function () { return this._wait(arguments).index == 0; };
TwScreen.prototype.WaitForStrings = function () { return this._wait(arguments).index + 1; };
TwScreen.prototype.ReadString = function () { var r = this._wait(arguments); return r.index >= 0 ? r.text : ""; };
TwScreen.prototype.WaitForCursor = function (timeout) { return !!twCall("waitany", { tab: this._tab.Id, timeout: Number(timeout) || 0 }); };
TwScreen.prototype.WaitForKey = TwScreen.prototype.WaitForCursor;
TwScreen.prototype._lines = function () {
  var s = twCall("screen", { tab: this._tab.Id });
  var rows = s.rows || 24, cols = s.cols || 80, lines;
  if (s.lines) lines = s.lines;
  else { lines = String(s.text).replace(/\r/g, "").split("\n"); lines = lines.slice(Math.max(0, lines.length - rows)); }
  while (lines.length < rows) lines.push("");
  return { lines: lines, rows: rows, cols: cols };
};
function twPad(s, n) { while (s.length < n) s += " "; return s; }
TwScreen.prototype.Get = function (r1, c1, r2, c2) {
  var s = this._lines(), out = "";
  for (var r = r1; r <= r2; r++) out += twPad(s.lines[r - 1] || "", s.cols).substring(c1 - 1, c2);
  return out;
};
TwScreen.prototype.Get2 = function (r1, c1, r2, c2) {
  var s = this._lines(), out = [];
  for (var r = r1; r <= r2; r++) out.push(twPad(s.lines[r - 1] || "", s.cols).substring(c1 - 1, c2));
  return out.join("\r\n");
};
TwScreen.prototype.Clear = function () {};

// ---- Session / Tab ----

function TwSession(tab) {
  this._tab = tab;
  this.Connected = true;
  this.Logging = false;
  this.LogFileName = "";
  this.Path = "";
  this.RemoteAddress = "";
  this.RemotePort = 0;
}
TwSession.prototype.Connect = function (cmd) { this._tab.Id = twCall("connect", { cmd: String(cmd || "") }); twRefresh(); return this; };
TwSession.prototype.ConnectInTab = function (cmd) { return new TwTab(twCall("connect", { cmd: String(cmd || "") })); };
TwSession.prototype.Disconnect = function () { twCall("disconnect", { tab: this._tab.Id }); };
TwSession.prototype.Log = function (start, append, raw) {
  var f = twCall("log", { tab: this._tab.Id, start: !!start, append: !!append, raw: !!raw, file: this.LogFileName });
  if (f) this.LogFileName = f;
  this.Logging = !!start;
};

function TwTab(id) {
  this.Id = id;
  this.Screen = new TwScreen(this);
  this.Session = new TwSession(this);
  this.Caption = "";
}
TwTab.prototype.Activate = function () { twCall("activate", { tab: this.Id }); };
TwTab.prototype.Close = function () { twCall("close", { tab: this.Id }); };

// ---- crt ----

var crtArgs = function (i) { return crtArgs._a[i]; };
crtArgs._a = [];
for (var i = 0; i < WScript.Arguments.length; i++) crtArgs._a.push(WScript.Arguments(i));
crtArgs.Count = crtArgs._a.length;
crtArgs.GetArg = function (i) { return crtArgs._a[i]; };

var crt = {
  _tab: new TwTab(TW.tab),
  Dialog: {
    Prompt: function (msg, title, def, isPassword) {
      var r = twCall("prompt", { message: String(msg), title: String(title || ""), "default": String(def || ""), secret: !!isPassword });
      return r === null ? "" : r;
    },
    MessageBox: function (msg, title, buttons) {
      return twCall("msgbox", { message: String(msg), title: String(title || ""), buttons: Number(buttons) || 0 });
    },
    FileOpenDialog: function (title, label, def) { return crt.Dialog.Prompt("File path:", title || "Open", def || ""); },
    FileSaveDialog: function (title, label, def) { return crt.Dialog.Prompt("File path:", title || "Save", def || ""); }
  },
  Window: { Caption: "Termwright", State: 0, Activate: function () {}, Show: function () {} },
  Arguments: crtArgs,
  ScriptFullName: new ActiveXObject("WScript.Shell").Environment("PROCESS")("TERMWRIGHT_SCRIPT"),
  Version: "",
  Sleep: function (ms) { twCall("sleep", { ms: Number(ms) || 0 }); },
  GetScriptTab: function () { return crt._tab; },
  GetActiveTab: function () { return crt._tab; },
  GetTabCount: function () { return twCall("tabs", {}).length; },
  GetTab: function (i) { return new TwTab(twCall("tabs", {})[i - 1]); },
  Quit: function () { WScript.Quit(0); }
};
crt.Screen = crt._tab.Screen;
crt.Session = crt._tab.Session;

// twRefresh copies the state Termwright sent with the last reply into the
// property-style members of the script's tab.
function twRefresh() {
  if (!twLast || typeof crt == "undefined") return;
  var t = crt._tab, s = twLast;
  if (s.tab && s.tab != t.Id) return;
  if (typeof s.connected != "undefined") t.Session.Connected = s.connected;
  if (s.row) { t.Screen.CurrentRow = s.row; t.Screen.CurrentColumn = s.col; }
  if (s.rows) { t.Screen.Rows = s.rows; t.Screen.Columns = s.cols; }
  if (s.path != null) { t.Session.Path = s.path; t.Session.RemoteAddress = s.host; t.Session.RemotePort = s.port; t.Caption = s.caption; }
}

crt.Version = twCall("version", {});
var BUTTON_OK = 0, BUTTON_OKCANCEL = 1, BUTTON_CANCEL = 1, BUTTON_ABORTRETRYIGNORE = 2, BUTTON_YESNOCANCEL = 3,
  BUTTON_YESNO = 4, BUTTON_RETRYCANCEL = 5, ICON_STOP = 16, ICON_QUESTION = 32, ICON_WARN = 48, ICON_INFO = 64,
  DEFBUTTON1 = 0, DEFBUTTON2 = 256, DEFBUTTON3 = 512, IDOK = 1, IDCANCEL = 2, IDABORT = 3, IDRETRY = 4,
  IDIGNORE = 5, IDYES = 6, IDNO = 7;
