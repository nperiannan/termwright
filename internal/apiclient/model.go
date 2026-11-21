// Package apiclient is Termwright's REST API client (a free alternative to Postman): requests with
// {{variables}}, session authentication (API key, bearer token, login, basic, OAuth 2.0), collections
// kept as plain JSON files, test scripts in a pm.* subset, a collection runner, imports (cURL, Postman
// v2.1, OpenAPI 3 / Swagger 2) and code export. Standard library and goja only.
package apiclient

import "strings"

// KV is a parameter, header or form field. Off keeps it in the request without sending it.
type KV struct {
	// Key: the name.
	Key string `json:"key"`
	// Value: the value; {{variables}} are replaced when sent.
	Value string `json:"value"`
	// Off: kept but not sent.
	Off bool `json:"off,omitempty"`
	// Desc: a note.
	Desc string `json:"desc,omitempty"`
	// Type of a multipart form field: "" (text) or "file" (Value is the file's path).
	Type string `json:"type,omitempty"`
}

// ReqAuth is a request's (or a collection's) own authentication. Values may hold {{variables}}; typed
// passwords, tokens and API key values are not saved in a collection (use a secret variable).
type ReqAuth struct {
	// Token of a bearer token.
	Token string `json:"token,omitempty"`
	// User of basic authentication.
	User string `json:"user,omitempty"`
	// Password of basic authentication.
	Password string `json:"password,omitempty"`
	// Key: the API key's header or query parameter name.
	Key string `json:"key,omitempty"`
	// Value of the API key.
	Value string `json:"value,omitempty"`
	// In: where the API key goes, "header" (default) or "query".
	In string `json:"in,omitempty"`
}

// Settings of a request (Postman's request settings). Zero values are the defaults.
type Settings struct {
	// HTTPVersion: "" (automatic), "1.1" or "2".
	HTTPVersion string `json:"httpVersion,omitempty"`
	// Verify: "" (the session's choice), "on" (check the certificate) or "off" (accept any).
	Verify string `json:"verify,omitempty"`
	// NoRedirects: answer 3xx instead of following.
	NoRedirects bool `json:"noRedirects,omitempty"`
	// FollowMethod: follow a redirect with the original method and body (not GET).
	FollowMethod bool `json:"followMethod,omitempty"`
	// FollowAuth: keep the Authorization header when a redirect goes to another host.
	FollowAuth bool `json:"followAuth,omitempty"`
	// RemoveReferer: send no Referer header after a redirect.
	RemoveReferer bool `json:"removeReferer,omitempty"`
	// NoEncode: send the query parameters as typed (not URL-encoded).
	NoEncode bool `json:"noEncode,omitempty"`
	// NoCookies: neither send nor keep cookies.
	NoCookies bool `json:"noCookies,omitempty"`
	// MaxRedirects followed (0: 10).
	MaxRedirects int `json:"maxRedirects,omitempty"`
	// TimeoutMs of the request (0: the session's, 30 s).
	TimeoutMs int `json:"timeoutMs,omitempty"`
}

// Body of a request.
type Body struct {
	// Mode: "" (none), json, text, xml, html, javascript, urlencoded, form (multipart), graphql, binary.
	Mode string `json:"mode,omitempty"`
	// Raw: the text of json, text, xml and the GraphQL query.
	Raw string `json:"raw,omitempty"`
	// Form: the fields of urlencoded and form.
	Form []KV `json:"form,omitempty"`
	// GraphQLVars: the GraphQL variables as JSON.
	GraphQLVars string `json:"graphqlVars,omitempty"`
	// File: the path of the file sent as it is (binary).
	File string `json:"file,omitempty"`
}

// Request is a saved or typed request.
type Request struct {
	// ID: unique in its collection.
	ID string `json:"id,omitempty"`
	// Name shown in the collection.
	Name string `json:"name,omitempty"`
	// Folder in the collection ("Interfaces/Ports"), "" = top level.
	Folder string `json:"folder,omitempty"`
	// Method: GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS or any other word.
	Method string `json:"method"`
	// URL: absolute, or a path (/rest/...) that goes after the session's base URL.
	URL string `json:"url"`
	// Params: query parameters added to the URL.
	Params []KV `json:"params,omitempty"`
	// PathVars: values of the :name parts of the URL's path.
	PathVars []KV `json:"pathVars,omitempty"`
	// Headers sent with it.
	Headers []KV `json:"headers,omitempty"`
	// Body sent with it.
	Body Body `json:"body,omitempty"`
	// Auth: "" inherits (the collection's, else the session's), "none", "bearer", "basic" or "apikey".
	Auth string `json:"auth,omitempty"`
	// AuthData: the values of its own authentication.
	AuthData *ReqAuth `json:"authData,omitempty"`
	// Settings of the request.
	Settings *Settings `json:"settings,omitempty"`
	// PreScript runs before it is sent (pm.variables.set ...).
	PreScript string `json:"preScript,omitempty"`
	// Tests run on the answer (pm.test("ok", () => pm.response.to.have.status(200))).
	Tests string `json:"tests,omitempty"`
	// Description: what it does.
	Description string `json:"description,omitempty"`
	// Examples: saved answers, shown as documentation and served by the collection's mock server.
	Examples []Example `json:"examples,omitempty"`
	// Schema: a JSON Schema the answer's body must fit (checked after each send and in runs).
	Schema string `json:"schema,omitempty"`
}

// Example is a saved answer of a request.
type Example struct {
	Name    string `json:"name"`
	Status  int    `json:"status"`
	Headers []KV   `json:"headers,omitempty"`
	Body    string `json:"body,omitempty"`
}

// Collection is a set of requests, kept as one JSON file.
type Collection struct {
	// ID: the file name without .json.
	ID string `json:"id"`
	// Name shown.
	Name string `json:"name"`
	// Session: the API session it uses unless another is chosen.
	Session string `json:"session,omitempty"`
	// Vars: collection variables ({{name}}); no secrets.
	Vars []KV `json:"vars,omitempty"`
	// Requests in order.
	Requests []Request `json:"requests"`
	// Auth of its requests that inherit: "" (the session's), "none", "bearer", "basic" or "apikey".
	Auth string `json:"auth,omitempty"`
	// AuthData: the values of its authentication.
	AuthData *ReqAuth `json:"authData,omitempty"`
	// PreScript runs before each of its requests, before the request's own.
	PreScript string `json:"preScript,omitempty"`
	// Tests run after each of its requests, before the request's own.
	Tests string `json:"tests,omitempty"`
	// Description: what it is for.
	Description string `json:"description,omitempty"`
}

// CollectionInfo is a collection in a list.
type CollectionInfo struct {
	ID       string `json:"id"`                // the collection's id (its file name)
	Name     string `json:"name"`              // its name
	Session  string `json:"session,omitempty"` // the API session it uses
	Requests int    `json:"requests"`
}

// Cookie the server set.
type Cookie struct {
	Name     string `json:"name"`               // cookie name
	Value    string `json:"value"`              // its value
	Domain   string `json:"domain,omitempty"`   // its domain
	Path     string `json:"path,omitempty"`     // its path
	Expires  string `json:"expires,omitempty"`  // when it expires (RFC 3339)
	HTTPOnly bool   `json:"httpOnly,omitempty"` // not readable by scripts in a browser
	Secure   bool   `json:"secure,omitempty"`
}

// Timing of a request in milliseconds (DNS, connect and TLS are 0 when a connection was reused).
type Timing struct {
	DNS       float64 `json:"dns"`       // name lookup
	Connect   float64 `json:"connect"`   // TCP connection
	TLS       float64 `json:"tls"`       // TLS handshake
	FirstByte float64 `json:"firstByte"` // from sending until the first byte of the answer
	Total     float64 `json:"total"`
}

// TestResult of one pm.test.
type TestResult struct {
	Name  string `json:"name"` // the test's name (pm.test)
	Pass  bool   `json:"pass"` // it passed
	Error string `json:"error,omitempty"`
}

// Response is what came back, with the request as sent (secrets masked).
type Response struct {
	// Status code (200), 0 when nothing came back.
	Status int `json:"status"`
	// StatusText: "200 OK".
	StatusText string `json:"statusText"`
	// Proto: HTTP/1.1, HTTP/2.0.
	Proto string `json:"proto,omitempty"`
	// Headers of the answer.
	Headers []KV `json:"headers"`
	// Body: text, or base64 when Base64.
	Body string `json:"body"`
	// Base64: the body is binary.
	Base64 bool `json:"base64,omitempty"`
	// ContentType of the body.
	ContentType string `json:"contentType,omitempty"`
	// Size of the body in bytes.
	Size int `json:"size"`
	// Truncated: the body was longer than kept (5 MB).
	Truncated bool `json:"truncated,omitempty"`
	// Ms: the whole request in milliseconds.
	Ms int64 `json:"ms"`
	// Timing per phase.
	Timing Timing `json:"timing"`
	// Method sent.
	Method string `json:"method"`
	// URL sent (secret variables masked).
	URL string `json:"url"`
	// ReqHeaders as sent (authorization and secret variables masked).
	ReqHeaders []KV `json:"reqHeaders"`
	// ReqBody as sent (secret variables masked; first 64 KB).
	ReqBody string `json:"reqBody,omitempty"`
	// Cookies the server set.
	Cookies []Cookie `json:"cookies,omitempty"`
	// Tests: the results of the request's tests.
	Tests []TestResult `json:"tests,omitempty"`
	// Logs: console.log of its scripts.
	Logs []string `json:"logs,omitempty"`
	// Vars set by its scripts (kept for the next requests of a run or the tab).
	Vars map[string]string `json:"vars,omitempty"`
	// Error: why it could not be sent or what failed (the answer is still shown when there is one).
	Error string `json:"error,omitempty"`
	// Next: the request a script chose to run next in a collection run (postman.setNextRequest).
	Next string `json:"next,omitempty"`
	// NextSet: a script chose the next request; Next "" ends the run.
	NextSet bool `json:"nextSet,omitempty"`
	// Skipped: the pre-request script skipped it (pm.execution.skipRequest).
	Skipped bool `json:"skipped,omitempty"`
	// EnvSet: values the scripts set with pm.environment.set (kept in the chosen environment).
	EnvSet map[string]string `json:"envSet,omitempty"`
	// GlobalSet: values the scripts set with pm.globals.set (kept in the globals).
	GlobalSet map[string]string `json:"globalSet,omitempty"`
}

// Environment is a named set of variables (Postman's environments); the one with ID "globals" holds
// the global variables. Secret values are kept in the computer's credential store, not in the file.
type Environment struct {
	// ID: the file name without .json ("globals" for the globals).
	ID string `json:"id"`
	// Name shown.
	Name string `json:"name"`
	// Vars: its variables.
	Vars []EnvVar `json:"vars"`
}

// EnvVar is a variable of an environment.
type EnvVar struct {
	// Key: the name.
	Key string `json:"key"`
	// Value: the value ("" in the file when Secret).
	Value string `json:"value"`
	// Secret: kept in the credential store and masked where requests are shown.
	Secret bool `json:"secret,omitempty"`
	// Off: not used.
	Off bool `json:"off,omitempty"`
	// Desc: a note.
	Desc string `json:"desc,omitempty"`
}

// Literal says whether a value is typed rather than a {{variable}} (an empty value is not).
func Literal(v string) bool { return strings.TrimSpace(v) != "" && !strings.Contains(v, "{{") }

// Scrub removes typed secrets (passwords, tokens, API key values) of a request's own authentication.
func (a *ReqAuth) Scrub() {
	if a == nil {
		return
	}
	for _, p := range []*string{&a.Token, &a.Password, &a.Value} {
		if Literal(*p) {
			*p = ""
		}
	}
}

// Changes says whether a method changes something on the server.
func Changes(method string) bool {
	switch method {
	case "", "GET", "HEAD", "OPTIONS":
		return false
	}
	return true
}
