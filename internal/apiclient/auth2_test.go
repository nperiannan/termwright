package apiclient

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDynamicVars(t *testing.T) {
	for _, n := range DynamicVarNames() {
		v := Resolve("{{"+n+"}}", nil)
		if v == "" || strings.Contains(v, "{{") {
			t.Fatalf("%s gave %q", n, v)
		}
	}
	if v := Resolve("{{$randomEmail}}", nil); !regexp.MustCompile(`^[a-z.]+\d\d@[a-z.]+$`).MatchString(v) {
		t.Fatalf("email %q", v)
	}
	if v := Resolve("{{$randomIP}}", nil); len(strings.Split(v, ".")) != 4 {
		t.Fatalf("ip %q", v)
	}
	s := echoSrv(t)
	res := Run(context.Background(), Request{URL: s.URL, PreScript: `pm.test("replaceIn", function(){ pm.expect(pm.variables.replaceIn("{{$randomFirstName}}")).to.not.contain("{{"); });`}, &Target{})
	if f := failed(res); f != "" {
		t.Fatal(f)
	}
}

// The OAuth 1.0 example of Twitter's "Creating a signature" guide.
func TestOAuth1Vector(t *testing.T) {
	body := "status=" + url.QueryEscape("Hello Ladies + Gentlemen, a signed OAuth request!")
	req, _ := http.NewRequest("POST", "https://api.twitter.com/1.1/statuses/update.json?include_entities=true", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	a := &ReqAuth{ConsumerKey: "xvz1evFS4wEEPTGEFPHBog", ConsumerSecret: "kAcSOqF21Fu85e7zjz7ZN2U4ZRhfV3WpwPAoE3Z7kBw",
		Token: "370773112-GmHxMAgYyLbNEtIKZeRNFsMKPR9EyMZeS9weJAEb", TokenSecret: "LswwdoUaIvS8ltyTt5jkRh4J50vUPVVHtR2YPi5kE"}
	if err := oauth1Sign(req, a, &Target{}, []byte(body), "kYjzVBB8Y0ZFabxSWbWovY3uYSQ2pTgmZeNu2VS4cg", "1318622958"); err != nil {
		t.Fatal(err)
	}
	if h := req.Header.Get("Authorization"); !strings.Contains(h, `oauth_signature="hCtSmYh%2BiHYCEqBWrE7C7hYmtUk%3D"`) {
		t.Fatalf("%s", h)
	}
}

// The examples of the Hawk specification's README.
func TestHawkVector(t *testing.T) {
	a := &ReqAuth{HawkID: "dh37fgj492je", HawkKey: "werxhqb98rpaxn39848xrunpaw3489ruxnpa98w4rxn", Ext: "some-app-ext-data"}
	req, _ := http.NewRequest("GET", "http://example.com:8000/resource/1?b=1&a=2", nil)
	h, err := hawkHeader(req, a, &Target{}, nil, "1353832234", "j4h3g2")
	if err != nil || !strings.Contains(h, `mac="6R4rV5iE+NPoym+WwjeHzjAGXUtLNIxmo1vpMofpLAE="`) {
		t.Fatalf("%v %s", err, h)
	}
	a.Payload = true
	req, _ = http.NewRequest("POST", "http://example.com:8000/resource/1?b=1&a=2", nil)
	req.Header.Set("Content-Type", "text/plain")
	h, _ = hawkHeader(req, a, &Target{}, []byte("Thank you for flying Hawk"), "1353832234", "j4h3g2")
	if !strings.Contains(h, `hash="Yi9LfIIFRtBEPt74PVmbTF/xVAwPn7ub15ePICfgnuY="`) || !strings.Contains(h, `mac="aSe1DERmZuRl3pI36/9BdZmnErTw3sNzOOAUlfeKjVw="`) {
		t.Fatalf("%s", h)
	}
}

// MS-NLMP 4.2.4.1.1: NTOWFv2 of User / Domain / Password.
func TestNTLMv2Key(t *testing.T) {
	if got := hex.EncodeToString(ntowfv2("User", "Password", "Domain")); got != "0c868a403bfd7a93a3001ef22ef02e3f" {
		t.Fatal(got)
	}
}

func TestNTLMHandshake(t *testing.T) {
	server := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(h, "NTLM "))
		if len(raw) < 12 {
			w.Header().Set("WWW-Authenticate", "NTLM")
			w.WriteHeader(401)
			return
		}
		switch binary.LittleEndian.Uint32(raw[8:]) {
		case 1:
			ch := make([]byte, 48)
			copy(ch, "NTLMSSP\x00")
			binary.LittleEndian.PutUint32(ch[8:], 2)
			binary.LittleEndian.PutUint32(ch[20:], ntlmFlags)
			copy(ch[24:], server)
			binary.LittleEndian.PutUint32(ch[44:], 48)
			w.Header().Set("WWW-Authenticate", "NTLM "+base64.StdEncoding.EncodeToString(ch))
			w.WriteHeader(401)
		case 3:
			field := func(at int) []byte {
				l, off := binary.LittleEndian.Uint16(raw[at:]), binary.LittleEndian.Uint32(raw[at+4:])
				return raw[off : off+uint32(l)]
			}
			nt := field(20)
			want := hmacMD5(ntowfv2("bob", "pw", "LAB"), server, nt[16:])
			if string(want) != string(nt[:16]) || string(field(36)) != string(utf16le("bob")) {
				w.WriteHeader(403)
				return
			}
			io.WriteString(w, "welcome")
		}
	}))
	s.Start()
	defer s.Close()
	res := Run(context.Background(), Request{URL: s.URL, Auth: "ntlm", AuthData: &ReqAuth{User: `LAB\bob`, Password: "pw"}}, &Target{})
	if res.Status != 200 || res.Body != "welcome" {
		t.Fatalf("%d %q %s", res.Status, res.Body, res.Error)
	}
}

func TestOAuth2AuthCodePKCE(t *testing.T) {
	var verifier atomic.Value
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/token":
			if r.Form.Get("grant_type") == "refresh_token" && r.Form.Get("refresh_token") == "rt1" {
				io.WriteString(w, `{"access_token":"tok-refreshed","expires_in":3600}`)
				return
			}
			if r.Form.Get("code") != "c0de" || r.Form.Get("code_verifier") == "" || r.Form.Get("client_id") != "pub" {
				w.WriteHeader(400)
				io.WriteString(w, `{"error":"invalid_grant"}`)
				return
			}
			verifier.Store(r.Form.Get("code_verifier"))
			io.WriteString(w, `{"access_token":"tok-code","refresh_token":"rt1","expires_in":3600}`)
		default:
			io.WriteString(w, r.Header.Get("Authorization"))
		}
	}))
	defer s.Close()
	var challenge string
	browse := func(u string) error { // a browser that signs in at once
		au, _ := url.Parse(u)
		q := au.Query()
		challenge = q.Get("code_challenge")
		if q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" {
			return fmt.Errorf("bad auth url %s", u)
		}
		go func() {
			r, err := http.Get(q.Get("redirect_uri") + "?code=c0de&state=" + url.QueryEscape(q.Get("state")))
			if err == nil {
				r.Body.Close()
			}
		}()
		return nil
	}
	a := &ReqAuth{Grant: "authorization_code", AuthURL: s.URL + "/authorize", TokenURL: s.URL + "/token", ClientID: "pub", PKCE: "S256"}
	tg := &Target{Tokens: &Tokens{}, Browse: browse}
	res := Run(context.Background(), Request{URL: s.URL + "/r", Auth: "oauth2", AuthData: a}, tg)
	if res.Body != "Bearer tok-code" {
		t.Fatalf("%d %q %s", res.Status, res.Body, res.Error)
	}
	if v, _ := verifier.Load().(string); v == "" || challenge == "" || challenge == v {
		t.Fatalf("pkce %q %q", v, challenge)
	}
	tg.Browse = func(string) error { return fmt.Errorf("must not open the browser again") }
	tg.Tokens = &Tokens{} // the access token is gone: the refresh token renews it
	res = Run(context.Background(), Request{URL: s.URL + "/r", Auth: "oauth2", AuthData: a}, tg)
	if res.Body != "Bearer tok-refreshed" {
		t.Fatalf("refresh: %q %s", res.Body, res.Error)
	}
}

func TestOAuth2Implicit(t *testing.T) {
	s := echoSrv(t)
	browse := func(u string) error {
		au, _ := url.Parse(u)
		q := au.Query()
		go func() {
			// a browser lands on the callback with the token in the #fragment: it gets the page, which sends the fragment back
			r, err := http.Get(q.Get("redirect_uri"))
			if err != nil {
				return
			}
			b, _ := io.ReadAll(r.Body)
			r.Body.Close()
			if !strings.Contains(string(b), "__fragment") {
				return
			}
			frag := "access_token=imp-tok&token_type=bearer&expires_in=600&state=" + url.QueryEscape(q.Get("state"))
			r, err = http.Get(q.Get("redirect_uri") + "?__fragment=" + url.QueryEscape(frag))
			if err == nil {
				r.Body.Close()
			}
		}()
		return nil
	}
	a := &ReqAuth{Grant: "implicit", AuthURL: "https://idp.example/authorize", ClientID: "c"}
	res := Run(context.Background(), Request{URL: s.URL, Auth: "oauth2", AuthData: a}, &Target{Tokens: &Tokens{}, Browse: browse})
	if !strings.Contains(res.Body, "Auth=Bearer imp-tok") {
		t.Fatalf("%q %s", res.Body, res.Error)
	}
	old := BrowserWait
	BrowserWait = 200 * time.Millisecond
	defer func() { BrowserWait = old }()
	res = Run(context.Background(), Request{URL: s.URL, Auth: "oauth2", AuthData: a}, &Target{Tokens: &Tokens{}, Browse: func(string) error { return nil }})
	if !strings.Contains(res.Error, "did not finish") {
		t.Fatalf("timeout: %q", res.Error)
	}
}

func TestAuthImportExportAndCode(t *testing.T) {
	for _, c := range []struct{ in, mode string }{
		{`{"type":"oauth1","oauth1":[{"key":"consumerKey","value":"ck"},{"key":"signatureMethod","value":"HMAC-SHA256"},{"key":"addParamsToHeader","value":false}]}`, "oauth1"},
		{`{"type":"hawk","hawk":[{"key":"authId","value":"id"},{"key":"authKey","value":"{{k}}"},{"key":"includePayloadHash","value":true}]}`, "hawk"},
		{`{"type":"ntlm","ntlm":[{"key":"username","value":"u"},{"key":"domain","value":"LAB"}]}`, "ntlm"},
		{`{"type":"oauth2","oauth2":[{"key":"grant_type","value":"authorization_code_with_pkce"},{"key":"authUrl","value":"https://idp/a"},{"key":"accessTokenUrl","value":"https://idp/t"},{"key":"clientId","value":"c"},{"key":"redirect_uri","value":"https://oauth.pstmn.io/v1/callback"}]}`, "oauth2"},
	} {
		col := `{"info":{"name":"c","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[{"name":"r","request":{"method":"GET","url":"https://h/x","auth":` + c.in + `}}]}`
		got, err := Import(col, "c")
		if err != nil || got.Requests[0].Auth != c.mode {
			t.Fatalf("%s: %v %+v", c.mode, err, got.Requests)
		}
		a := got.Requests[0].AuthData
		out, _ := ExportPostman(got)
		back, _ := Import(string(out), "c")
		if c.mode == "hawk" && a.Algorithm == "" {
			a.Algorithm = "sha256" // the export names the default
		}
		if b := back.Requests[0].AuthData; back.Requests[0].Auth != c.mode || fmt.Sprint(*b) != fmt.Sprint(*a) {
			t.Fatalf("%s round trip:\n%+v\n%+v", c.mode, *a, *b)
		}
		if c.mode == "oauth2" && (a.Grant != "authorization_code" || a.PKCE != "S256" || a.RedirectURI != "" || a.AuthURL != "https://idp/a") {
			t.Fatalf("pkce import %+v", *a)
		}
		if c.mode == "oauth1" && (a.In != "query" || a.SignatureMethod != "HMAC-SHA256") {
			t.Fatalf("oauth1 import %+v", *a)
		}
		if c.mode == "hawk" && !a.Payload {
			t.Fatal("hawk payload")
		}
	}
	code, _ := Code(Request{URL: "https://h/x", Auth: "ntlm", AuthData: &ReqAuth{User: "u"}}, &Target{}, "curl")
	if !strings.Contains(code, `--ntlm -u "u:$API_PASSWORD"`) {
		t.Fatal(code)
	}
	code, _ = Code(Request{URL: "https://h/x", Auth: "bearer"}, &Target{}, "javascript")
	if strings.HasPrefix(code, "#") {
		t.Fatal(code)
	}
}

func TestScriptLibraries(t *testing.T) {
	s := echoSrv(t)
	src := `
var d = { items: [{ n: "b", v: 2 }, { n: "a", v: 1 }, { n: "c", v: 3 }], deep: { x: { y: 5 } } };
pm.test("lodash", function() {
  var L = require('lodash');
  pm.expect(_.get(d, 'deep.x.y')).to.eql(5);
  pm.expect(_.get(d, 'items[1].n')).to.eql('a');
  pm.expect(_.map(_.sortBy(d.items, 'v'), 'n')).to.eql(['a', 'b', 'c']);
  pm.expect(L.find(d.items, { n: 'c' }).v).to.eql(3);
  pm.expect(_.sumBy(d.items, 'v')).to.eql(6);
  pm.expect(_.uniq([1, 1, 2])).to.eql([1, 2]);
  pm.expect(_.isEqual({ a: [1, { b: 2 }] }, { a: [1, { b: 2 }] })).to.be.true;
  pm.expect(_.keys(_.groupBy(d.items, function(i) { return i.v > 1; }))).to.have.lengthOf(2);
  pm.expect(_.camelCase('port speed mbps')).to.eql('portSpeedMbps');
  pm.expect(_.chain([3, 1, 2]).sortBy().take(2).value()).to.eql([1, 2]);
});
pm.test("moment", function() {
  var m = require('moment');
  pm.expect(m.utc('2026-03-04T05:06:07Z').format('YYYY-MM-DD HH:mm:ss')).to.eql('2026-03-04 05:06:07');
  pm.expect(moment.utc('2026-01-31T00:00:00Z').add(1, 'days').format('YYYY-MM-DD')).to.eql('2026-02-01');
  pm.expect(moment.utc('2026-01-10T00:00:00Z').diff(moment.utc('2026-01-01T00:00:00Z'), 'days')).to.eql(9);
  pm.expect(moment.utc('2026-05-05', 'YYYY-MM-DD').format('D MMM YYYY')).to.eql('5 May 2026');
  pm.expect(moment().isValid()).to.be.true;
  pm.expect(moment.unix(0).utc().toISOString()).to.eql('1970-01-01T00:00:00.000Z');
});
pm.test("xml2Json and tv4", function() {
  var j = xml2Json('<rpc-reply><port name="1/1"><status>up</status></port><port name="1/2"><status>down</status></port></rpc-reply>');
  pm.expect(j['rpc-reply'].port).to.have.lengthOf(2);
  pm.expect(j['rpc-reply'].port[1].status).to.eql('down');
  pm.expect(j['rpc-reply'].port[0].$.name).to.eql('1/1');
  pm.expect(tv4.validate({ a: 1 }, { type: 'object', required: ['a'] })).to.be.true;
  pm.expect(tv4.validate({}, { type: 'object', required: ['a'] })).to.be.false;
});`
	res := Run(context.Background(), Request{URL: s.URL, PreScript: src}, &Target{})
	if f := failed(res); f != "" || len(res.Tests) != 3 {
		t.Fatalf("%d: %s", len(res.Tests), f)
	}
}
