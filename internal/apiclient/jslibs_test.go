package apiclient

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Each library of Postman's sandbox, used the way Postman scripts use it.
func TestPostmanSandboxLibraries(t *testing.T) {
	s := echoSrv(t)
	src := `
pm.test("lodash (real)", function() {
  pm.expect(_.VERSION).to.eql('4.17.21');
  pm.expect(_.partition([1, 2, 3, 4], function(n) { return n % 2; })).to.eql([[1, 3], [2, 4]]);
  pm.expect(_.template('hi <%= user %>!')({ user: 'ops' })).to.eql('hi ops!');
  pm.expect(require('lodash').zipObject(['a', 'b'], [1, 2])).to.eql({ a: 1, b: 2 });
});
pm.test("moment (real)", function() {
  var m = require('moment');
  pm.expect(m.utc('2026-03-04T05:06:07Z').format('dddd, MMMM Do YYYY, h:mm:ss a')).to.eql('Wednesday, March 4th 2026, 5:06:07 am');
  pm.expect(m.duration(90, 'minutes').humanize()).to.eql('2 hours');
  pm.expect(m.utc('2026-01-31').add(1, 'month').format('YYYY-MM-DD')).to.eql('2026-02-28');
});
pm.test("cheerio", function() {
  var $ = cheerio.load('<table><tr><td class="p">1/1</td><td>up</td></tr><tr><td class="p">1/2</td><td>down</td></tr></table>');
  pm.expect($('td.p').length).to.eql(2);
  pm.expect($('tr').eq(1).find('td').last().text()).to.eql('down');
  pm.expect(require('cheerio').load('<p id="x">hello</p>')('#x').text()).to.eql('hello');
});
pm.test("xml2js", function() {
  var got;
  require('xml2js').parseString('<a><b n="1">x</b><b n="2">y</b></a>', function(err, r) { got = r; });
  pm.expect(got.a.b[1]._).to.eql('y');
  pm.expect(got.a.b[0].$.n).to.eql('1');
  var j = xml2Json('<reply><port><name>1/1</name></port></reply>');
  pm.expect(j.reply.port.name).to.eql('1/1');
});
pm.test("crypto-js (real)", function() {
  var enc = CryptoJS.AES.encrypt('secret text', 'pass').toString();
  pm.expect(CryptoJS.AES.decrypt(enc, 'pass').toString(CryptoJS.enc.Utf8)).to.eql('secret text');
  pm.expect(CryptoJS.SHA256('abc').toString()).to.eql('ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad');
  pm.expect(CryptoJS.enc.Base64.stringify(CryptoJS.HmacSHA1('m', 'k'))).to.have.lengthOf(28);
});
pm.test("chai (pm.expect)", function() {
  pm.expect({ a: { b: [1, 2] }, c: 3 }).to.deep.include({ a: { b: [1, 2] } });
  pm.expect(201).to.be.oneOf([200, 201]);
  pm.expect({ id: 5 }).to.have.property('id').that.is.a('number');
  pm.expect([3, 1]).to.have.members([1, 3]);
  pm.expect(function() { throw new Error('boom'); }).to.throw(/boom/);
  require('chai').assert.strictEqual(1, 1);
});
pm.test("tv4 and ajv", function() {
  var schema = { type: 'object', required: ['id'], properties: { id: { type: 'integer' } } };
  pm.expect(tv4.validate({ id: 1 }, schema)).to.be.true;
  pm.expect(tv4.validate({ id: 'x' }, schema)).to.be.false;
  var Ajv = require('ajv'), v = new Ajv({ allErrors: true }).compile(schema);
  pm.expect(v({ id: 2 })).to.be.true;
  pm.expect(v({})).to.be.false;
  pm.expect(v.errors[0].params.missingProperty).to.eql('id');
});
pm.test("csv-parse", function() {
  var rows = require('csv-parse/lib/sync')('port,state\n1/1,up\n1/2,"down, err"', { columns: true });
  pm.expect(rows[1].state).to.eql('down, err');
});
pm.test("uuid", function() { pm.expect(require('uuid').v4()).to.match(/^[0-9a-f]{8}-[0-9a-f]{4}-4/); });
pm.test("node modules", function() {
  pm.expect(require('path').join('/a', 'b', '../c')).to.eql('/a/c');
  pm.expect(require('url').parse('https://h:8443/x?y=1').port).to.eql('8443');
  pm.expect(require('querystring').stringify({ a: 1, b: 'x y' })).to.eql('a=1&b=x%20y');
  pm.expect(require('util').format('%s=%d', 'n', 5)).to.eql('n=5');
  pm.expect(Buffer.from('user:pw').toString('base64')).to.eql('dXNlcjpwdw==');
  pm.expect(require('buffer').Buffer.from('6869', 'hex').toString()).to.eql('hi');
  var E = require('events'), e = new E(), seen = 0; e.on('x', function(n) { seen = n; }); e.emit('x', 7); pm.expect(seen).to.eql(7);
  require('assert').deepStrictEqual({ a: [1] }, { a: [1] });
  pm.expect(require('punycode').toASCII('bücher.example')).to.eql('xn--bcher-kva.example');
  pm.expect(new (require('string_decoder').StringDecoder)('utf8').write(Buffer.from([0xc3, 0xbc]))).to.eql('ü');
});
setTimeout(function() { pm.test("setTimeout runs", function() { pm.expect(1).to.eql(1); }); }, 30);
require('timers').setImmediate(function() { pm.test("setImmediate runs", function() {}); });
var n = 0, iv = setInterval(function() { if (++n === 3) { clearInterval(iv); pm.test("setInterval ran 3 times", function() {}); } }, 5);`
	start := time.Now()
	res := Run(context.Background(), Request{URL: s.URL, PreScript: src}, &Target{})
	if f := failed(res); f != "" || len(res.Tests) != 13 {
		var names []string
		for _, x := range res.Tests {
			names = append(names, x.Name)
		}
		t.Fatalf("%d tests (%s): %s", len(res.Tests), strings.Join(names, ", "), f)
	}
	t.Logf("all libraries in %v", time.Since(start))
	pc := time.Now()
	res = Run(context.Background(), Request{URL: s.URL, PreScript: `
var sdk = require('postman-collection');
pm.test("postman-collection", function() {
  var u = new sdk.Url('https://api.example.com:8443/v1/ports/{{id}}?state=up&limit=10');
  pm.expect(u.getHost()).to.eql('api.example.com');
  pm.expect(u.query.get('limit')).to.eql('10');
  var h = new sdk.HeaderList(null, 'Content-Type: application/json\nX-Id: 7');
  pm.expect(h.get('x-id')).to.eql('7');
  var r = new sdk.Request({ url: 'https://h/x', method: 'POST', body: { mode: 'raw', raw: '{"a":1}' } });
  pm.expect(r.method).to.eql('POST');
  pm.expect(r.body.raw).to.eql('{"a":1}');
  var c = new sdk.Collection({ info: { name: 'lab' }, item: [{ name: 'ports', request: 'https://h/ports' }] });
  pm.expect(c.items.count()).to.eql(1);
  var v = new sdk.VariableScope({ values: [{ key: 'id', value: '5' }] });
  pm.expect(v.replaceIn('/ports/{{id}}')).to.eql('/ports/5');
  pm.expect(sdk.Collection.isCollection(c)).to.be.true;
});`}, &Target{})
	if f := failed(res); f != "" || len(res.Tests) != 1 {
		t.Fatalf("%d: %s", len(res.Tests), f)
	}
	t.Logf("postman-collection loaded and used in %v", time.Since(pc))
	res = Run(context.Background(), Request{URL: s.URL, PreScript: `require('newman')`}, &Target{})
	if !strings.Contains(res.Error, "is not available") || !strings.Contains(res.Error, "postman-collection") {
		t.Fatal(res.Error)
	}
	// a script that never ends its timers is stopped
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res = Run(ctx, Request{URL: s.URL, PreScript: `setInterval(function() {}, 10);`}, &Target{})
	if time.Since(start) > 20*time.Second || !strings.Contains(res.Error, "stopped") {
		t.Fatalf("timers were not stopped: %q", res.Error)
	}
}
