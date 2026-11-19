// Package apiclient is Termwright's REST API client (a free alternative to Postman): requests with
// {{variables}}, session authentication (API key, bearer token, login, basic, OAuth 2.0), collections
// kept as plain JSON files, test scripts in a pm.* subset, a collection runner, imports (cURL, Postman
// v2.1, OpenAPI 3 / Swagger 2) and code export. Standard library and goja only.
package apiclient

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
}

// Body of a request.
type Body struct {
	// Mode: "" (none), json, text, xml, urlencoded, form (multipart), graphql.
	Mode string `json:"mode,omitempty"`
	// Raw: the text of json, text, xml and the GraphQL query.
	Raw string `json:"raw,omitempty"`
	// Form: the fields of urlencoded and form.
	Form []KV `json:"form,omitempty"`
	// GraphQLVars: the GraphQL variables as JSON.
	GraphQLVars string `json:"graphqlVars,omitempty"`
}

// Request is a saved or typed request.
type Request struct {
	// ID: unique in its collection.
	ID string `json:"id,omitempty"`
	// Name shown in the collection.
	Name string `json:"name,omitempty"`
	// Folder in the collection ("Interfaces/Ports"), "" = top level.
	Folder string `json:"folder,omitempty"`
	// Method: GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS.
	Method string `json:"method"`
	// URL: absolute, or a path (/rest/...) that goes after the session's base URL.
	URL string `json:"url"`
	// Params: query parameters added to the URL.
	Params []KV `json:"params,omitempty"`
	// Headers sent with it.
	Headers []KV `json:"headers,omitempty"`
	// Body sent with it.
	Body Body `json:"body,omitempty"`
	// Auth: "" uses the session's authentication, "none" sends none.
	Auth string `json:"auth,omitempty"`
	// PreScript runs before it is sent (pm.variables.set ...).
	PreScript string `json:"preScript,omitempty"`
	// Tests run on the answer (pm.test("ok", () => pm.response.to.have.status(200))).
	Tests string `json:"tests,omitempty"`
	// Description: what it does.
	Description string `json:"description,omitempty"`
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
}

// Changes says whether a method changes something on the server.
func Changes(method string) bool {
	switch method {
	case "", "GET", "HEAD", "OPTIONS":
		return false
	}
	return true
}
