// Runs vbs/termwright_crt.js under Node with a minimal stand-in for Windows
// Script Host (WScript, ActiveXObject, VBArray), then a test script that makes
// the same calls a VBScript would. HTTP is done synchronously with curl.
const fs = require('fs');
const { execFileSync } = require('child_process');
const vm = require('vm');
const [crtFile, userFile, ...args] = process.argv.slice(2);
class ServerXMLHTTP {
  constructor() { this.h = {}; }
  setTimeouts() {}
  open(m, url) { this.url = url; }
  setRequestHeader(k, v) { this.h[k] = v; }
  send(body) {
    const a = ['-s', '-o', '-', '-w', '\n%{http_code}', '-X', 'POST', '--data-binary', '@-'];
    for (const k in this.h) a.push('-H', k + ': ' + this.h[k]);
    a.push(this.url);
    const out = execFileSync('curl', a, { input: body }).toString();
    const i = out.lastIndexOf('\n');
    this.responseText = out.slice(0, i); this.status = Number(out.slice(i + 1));
  }
}
const ctx = {
  WScript: { Arguments: Object.defineProperty((i) => args[i], 'length', { value: args.length }), Quit: (c) => process.exit(c), Echo: console.log },
  ActiveXObject: function (name) {
    if (name === 'WScript.Shell') return { Environment: () => (k) => process.env[k] || '' };
    if (/XMLHTTP/i.test(name)) return new ServerXMLHTTP();
    throw new Error('no ' + name);
  },
  VBArray: function (a) { if (!Array.isArray(a)) throw new Error('not a VB array'); this.toArray = () => a; },
  console,
};
vm.createContext(ctx);
vm.runInContext(fs.readFileSync(crtFile, 'utf8'), ctx);
vm.runInContext(fs.readFileSync(userFile, 'utf8'), ctx);
