package apiclient

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func echoSrv(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "%s %s\nX-A=%s\nX-Gone=%s\nAuth=%s\nBODY=%s", r.Method, r.URL.RequestURI(), r.Header.Get("X-A"), r.Header.Get("X-Gone"), r.Header.Get("Authorization"), b)
	}))
	t.Cleanup(s.Close)
	return s
}

func failed(r Response) string {
	var b strings.Builder
	for _, x := range r.Tests {
		if !x.Pass {
			b.WriteString(x.Name + ": " + x.Error + "; ")
		}
	}
	if r.Error != "" {
		b.WriteString("error: " + r.Error)
	}
	return b.String()
}

func TestScriptRequestMutation(t *testing.T) {
	s := echoSrv(t)
	r := Request{Method: "POST", URL: s.URL + "/p?a=1", Headers: []KV{{Key: "X-Gone", Value: "1"}}, Body: Body{Mode: "json", Raw: `{"n":1}`},
		PreScript: `pm.request.headers.add({key:"X-A", value:"one"});
pm.request.headers.upsert({key:"X-A", value:"two"});
pm.request.headers.remove("X-Gone");
pm.request.url.addQueryParams("b=2");
var j = JSON.parse(pm.request.body.raw); j.n = 7; pm.request.body.update(JSON.stringify(j));`}
	res := Run(context.Background(), r, &Target{})
	for _, w := range []string{"/p?a=1&b=2", "X-A=two", "X-Gone=\n", `BODY={"n":7}`} {
		if !strings.Contains(res.Body, w) {
			t.Fatalf("want %q in %q (%s)", w, res.Body, failed(res))
		}
	}
}

func TestScriptCryptoAndB64(t *testing.T) {
	m := hmac.New(sha256.New, []byte("key"))
	m.Write([]byte("msg"))
	hx, b6 := hex.EncodeToString(m.Sum(nil)), base64.StdEncoding.EncodeToString(m.Sum(nil))
	md := md5.Sum([]byte("abc"))
	s := echoSrv(t)
	r := Request{URL: s.URL, PreScript: fmt.Sprintf(`
var C = require('crypto-js');
pm.test("hmac hex", function(){ pm.expect(CryptoJS.HmacSHA256("msg","key").toString()).to.eql(%q); });
pm.test("hmac b64", function(){ pm.expect(C.HmacSHA256("msg","key").toString(CryptoJS.enc.Base64)).to.eql(%q); });
pm.test("md5", function(){ pm.expect(CryptoJS.MD5("abc").toString()).to.eql(%q); });
pm.test("btoa", function(){ pm.expect(btoa("user:pw")).to.eql("dXNlcjpwdw=="); pm.expect(atob("dXNlcjpwdw==")).to.eql("user:pw"); });
pm.test("utf8 b64", function(){ pm.expect(CryptoJS.enc.Base64.stringify(CryptoJS.enc.Utf8.parse("hi"))).to.eql("aGk="); });
pm.test("uuid", function(){ pm.expect(require('uuid').v4()).to.match(/^[0-9a-f-]{36}$/); });
`, hx, b6, hex.EncodeToString(md[:]))}
	res := Run(context.Background(), r, &Target{})
	if f := failed(res); f != "" || len(res.Tests) != 6 {
		t.Fatalf("%d tests: %s", len(res.Tests), f)
	}
}

func TestCollectionVariablesAndAssertions(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Id", "9")
		w.WriteHeader(404)
		io.WriteString(w, `{"e":1}`)
	}))
	defer s.Close()
	r := Request{URL: s.URL, PreScript: `pm.collectionVariables.set("cv","a");`, Tests: `
pm.test("get", function(){ pm.expect(pm.collectionVariables.get("cv")).to.eql("a"); });
pm.test("404", function(){ pm.response.to.be.notFound; pm.response.to.be.clientError; pm.response.to.not.be.ok; pm.response.to.not.have.status(200); });
pm.test("reason", function(){ pm.expect(pm.response.reason()).to.eql("Not Found"); });
pm.test("hdrs", function(){ pm.expect(pm.response.headers.toObject()["x-id"] || pm.response.headers.toObject()["X-Id"]).to.eql("9"); });
pm.test("must fail", function(){ pm.response.to.be.ok; });`}
	res := Run(context.Background(), r, &Target{})
	if len(res.Tests) != 5 {
		t.Fatalf("tests %+v", res.Tests)
	}
	for i, x := range res.Tests {
		if x.Pass != (i < 4) {
			t.Fatalf("test %q pass=%v %s", x.Name, x.Pass, x.Error)
		}
	}
	if res.CollSet["cv"] != "a" {
		t.Fatalf("collSet %v", res.CollSet)
	}
}

func TestDigestAuth(t *testing.T) {
	var hits atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Digest ") {
			w.Header().Set("WWW-Authenticate", `Digest realm="lab", nonce="abc123", qop="auth", algorithm=MD5, opaque="op"`)
			w.WriteHeader(401)
			return
		}
		p := map[string]string{}
		for _, kv := range splitParams(strings.TrimPrefix(h, "Digest ")) {
			if k, v, ok := strings.Cut(strings.TrimSpace(kv), "="); ok {
				p[k] = strings.Trim(v, `"`)
			}
		}
		md := func(s string) string { x := md5.Sum([]byte(s)); return hex.EncodeToString(x[:]) }
		ha1, ha2 := md("bob:lab:pw"), md(r.Method+":"+p["uri"])
		want := md(ha1 + ":abc123:" + p["nc"] + ":" + p["cnonce"] + ":auth:" + ha2)
		if p["response"] != want || p["opaque"] != "op" || p["uri"] != r.URL.RequestURI() {
			w.WriteHeader(403)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer s.Close()
	r := Request{URL: s.URL + "/x?y=1", Auth: "digest", AuthData: &ReqAuth{User: "bob", Password: "{{pw}}"}}
	res := Run(context.Background(), r, &Target{Vars: map[string]string{"pw": "pw"}})
	if res.Status != 200 || hits.Load() != 2 {
		t.Fatalf("status %d hits %d err %s", res.Status, hits.Load(), res.Error)
	}
}

func TestOAuth2Grants(t *testing.T) {
	var issued atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = r.ParseForm()
			id, sec, ok := r.BasicAuth()
			if !ok {
				id, sec = r.Form.Get("client_id"), r.Form.Get("client_secret")
			}
			if id != "cid" || sec != "csec" {
				w.WriteHeader(401)
				return
			}
			g := r.Form.Get("grant_type")
			if g == "password" && (r.Form.Get("username") != "u" || r.Form.Get("password") != "p") {
				w.WriteHeader(400)
				return
			}
			n := issued.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"access_token":"tok%d","token_type":"Bearer","expires_in":3600}`, n)
			return
		}
		// the first token is revoked after the first use
		if a := r.Header.Get("Authorization"); a == "Bearer tok1" && r.URL.Query().Get("n") == "2" || !strings.HasPrefix(a, "Bearer tok") {
			w.WriteHeader(401)
			return
		}
		io.WriteString(w, r.Header.Get("Authorization"))
	}))
	defer s.Close()
	tg := &Target{Tokens: &Tokens{}}
	a := &ReqAuth{TokenURL: s.URL + "/token", ClientID: "cid", ClientSecret: "csec", Scope: "read"}
	res := Run(context.Background(), Request{URL: s.URL + "/r?n=1", Auth: "oauth2", AuthData: a}, tg)
	if res.Body != "Bearer tok1" {
		t.Fatalf("first: %d %q %s", res.Status, res.Body, res.Error)
	}
	res = Run(context.Background(), Request{URL: s.URL + "/r?n=1", Auth: "oauth2", AuthData: a}, tg)
	if res.Body != "Bearer tok1" || issued.Load() != 1 {
		t.Fatalf("cached: %q issued %d", res.Body, issued.Load())
	}
	res = Run(context.Background(), Request{URL: s.URL + "/r?n=2", Auth: "oauth2", AuthData: a}, tg)
	if res.Body != "Bearer tok2" {
		t.Fatalf("refresh on 401: %d %q", res.Status, res.Body)
	}
	pw := &ReqAuth{Grant: "password", User: "u", Password: "p", TokenURL: s.URL + "/token", ClientID: "cid", ClientSecret: "csec", ClientAuth: "body"}
	res = Run(context.Background(), Request{URL: s.URL + "/r?n=3", Auth: "oauth2", AuthData: pw}, &Target{Tokens: &Tokens{}})
	if !strings.HasPrefix(res.Body, "Bearer tok") {
		t.Fatalf("password grant: %d %q %s", res.Status, res.Body, res.Error)
	}
	res = Run(context.Background(), Request{URL: s.URL + "/r?n=3", Auth: "oauth2", AuthData: &ReqAuth{Token: "tokX"}}, &Target{})
	if res.Body != "Bearer tokX" {
		t.Fatalf("typed token: %q", res.Body)
	}
}

// AWS's documented SigV4 example (IAM ListUsers), from the "Create a signed AWS API request" guide.
func TestAWSSigV4Vector(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://iam.amazonaws.com/?Action=ListUsers&Version=2010-05-08", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	a := &ReqAuth{AccessKey: "AKIDEXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", Region: "us-east-1", Service: "iam"}
	now := time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC)
	if err := awsSign(req, a, &Target{}, nil, now); err != nil {
		t.Fatal(err)
	}
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/iam/aws4_request, SignedHeaders=content-type;host;x-amz-date, Signature=5d672d79c15b13162d9279b0855cfba6789a8edb4c82c400e06b5924a6f2b5d7"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestAPIKeyQueryKeepsOrder(t *testing.T) {
	s := echoSrv(t)
	r := Request{URL: s.URL + "/q?z=1&a=2", Auth: "apikey", AuthData: &ReqAuth{Key: "api key", Value: "v&1", In: "query"}}
	res := Run(context.Background(), r, &Target{})
	if !strings.Contains(res.Body, "/q?z=1&a=2&api+key=v%261") {
		t.Fatalf("%q", res.Body)
	}
}

func TestPostmanAuthRoundTrip(t *testing.T) {
	cases := []struct {
		in   string
		mode string
		chk  func(*ReqAuth) bool
	}{
		{`{"type":"digest","digest":[{"key":"username","value":"u"},{"key":"password","value":"{{p}}"}]}`, "digest", func(a *ReqAuth) bool { return a.User == "u" && a.Password == "{{p}}" }},
		{`{"type":"awsv4","awsv4":[{"key":"accessKey","value":"AK"},{"key":"secretKey","value":"{{sk}}"},{"key":"region","value":"eu-west-1"},{"key":"service","value":"s3"}]}`, "awsv4", func(a *ReqAuth) bool { return a.AccessKey == "AK" && a.Region == "eu-west-1" && a.Service == "s3" }},
		{`{"type":"oauth2","oauth2":[{"key":"grant_type","value":"password_credentials"},{"key":"accessTokenUrl","value":"https://h/t"},{"key":"clientId","value":"c"},{"key":"scope","value":"s"}]}`, "oauth2", func(a *ReqAuth) bool { return a.Grant == "password" && a.TokenURL == "https://h/t" && a.ClientID == "c" && a.Scope == "s" }},
	}
	for _, c := range cases {
		col := `{"info":{"name":"c","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[{"name":"r","request":{"method":"GET","url":"https://h/x","auth":` + c.in + `}}]}`
		got, err := Import(col, "c")
		if err != nil || len(got.Requests) != 1 {
			t.Fatalf("%s: %v", c.mode, err)
		}
		r := got.Requests[0]
		if r.Auth != c.mode || r.AuthData == nil || !c.chk(r.AuthData) {
			t.Fatalf("%s import: %q %+v", c.mode, r.Auth, r.AuthData)
		}
		out, err := ExportPostman(got)
		if err != nil {
			t.Fatal(err)
		}
		back, err := Import(string(out), "c")
		if err != nil || back.Requests[0].Auth != c.mode || !c.chk(back.Requests[0].AuthData) {
			t.Fatalf("%s round trip: %v %+v\n%s", c.mode, err, back.Requests, out)
		}
	}
}
