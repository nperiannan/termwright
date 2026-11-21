// Package testrec is Termwright's test format and runner: a test case is a YAML file of steps
// (a command on a saved session, an HTTP request) with checks on what came back. Cases are kept in a
// tests folder (cases/, lib/, testbeds/, reports/) that can be a Git repository; the runner writes
// a JUnit XML file and an HTML report per run. Code generators (Go first) read the same file.
package testrec

// Case is one test: steps run in order with their checks.
type Case struct {
	// Name of the test (default: the file name).
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// Description: what it checks.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Tags to pick tests by (smoke, snmp, vlan …).
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// Vars: values used as {{name}} in the steps; a run's own values win.
	Vars map[string]string `yaml:"vars,omitempty" json:"vars,omitempty"`
	// Params: the test runs once per entry, with its values as {{variables}} (data-driven tests);
	// a testbed's and a run's own values still win.
	Params []map[string]string `yaml:"params,omitempty" json:"params,omitempty"`
	// Restore: what Termwright saves before the test and puts back after its cleanup, for every
	// device the test uses: "config" (the device type's save and restore commands) and "lab" (a
	// checkpoint of an emulated lab switch).
	Restore []string `yaml:"restore,omitempty" json:"restore,omitempty"`
	// Setup: steps before the test's own (a known state); when one fails the steps are skipped.
	Setup []Step `yaml:"setup,omitempty" json:"setup,omitempty"`
	// Steps run in order.
	Steps []Step `yaml:"steps" json:"steps"`
	// Cleanup: steps after the test's own that always run - also when a step failed or the run was
	// stopped - so the device is clean for the next test. Each cleanup step runs even when an
	// earlier one failed.
	Cleanup []Step `yaml:"cleanup,omitempty" json:"cleanup,omitempty"`
}

// Step is one action and its checks. Exactly one of Run, HTTP and SNMP is set.
type Step struct {
	// Name shown in the report (default: the command or the request).
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// Run: a command on a saved session.
	Run *RunStep `yaml:"run,omitempty" json:"run,omitempty"`
	// HTTP: a request, through an API session or to a full URL.
	HTTP *HTTPStep `yaml:"http,omitempty" json:"http,omitempty"`
	// SNMP: an SNMP get, walk or set with a saved session's SNMP settings.
	SNMP *SNMPStep `yaml:"snmp,omitempty" json:"snmp,omitempty"`
	// Expect: the checks; all must pass. With none, the step passes when it ran without an error.
	Expect []Expect `yaml:"expect,omitempty" json:"expect,omitempty"`
	// Save: values taken from the answer for later steps ({{name}}).
	Save []Save `yaml:"save,omitempty" json:"save,omitempty"`
	// Timeout in seconds (default 60).
	Timeout int `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	// ContinueOnFail: later steps run even when this one fails.
	ContinueOnFail bool `yaml:"continueOnFail,omitempty" json:"continueOnFail,omitempty"`
}

// RunStep is a command sent on a saved session (logged in with its saved login, in the background).
type RunStep struct {
	// Session: the saved session's path, e.g. Lab/core-sw1.
	Session string `yaml:"session" json:"session"`
	// Command to send.
	Command string `yaml:"command" json:"command"`
	// Prompt: a regular expression for the prompt that ends the output ("" = the device type's).
	Prompt string `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	// IgnoreErrors: the device's error messages (its device type's error patterns, e.g. "Error:",
	// "% Invalid input") do not fail the step - for commands that may be refused, like removing what
	// may not be there.
	IgnoreErrors bool `yaml:"ignoreErrors,omitempty" json:"ignoreErrors,omitempty"`
}

// HTTPStep is an HTTP request.
type HTTPStep struct {
	// Session: an API session's path (base URL, login, variables); "" sends URL as it is.
	Session string `yaml:"session,omitempty" json:"session,omitempty"`
	// Environment: an API client environment's name whose variables are used.
	Environment string `yaml:"environment,omitempty" json:"environment,omitempty"`
	// Collection: an API client collection (id or name) whose variables are used.
	Collection string `yaml:"collection,omitempty" json:"collection,omitempty"`
	// Method: GET (default), POST, PUT, PATCH, DELETE …
	Method string `yaml:"method,omitempty" json:"method,omitempty"`
	// URL: a path after the session's base URL, or a full URL.
	URL string `yaml:"url" json:"url"`
	// Headers to send.
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	// Body to send (JSON when it starts with { or [).
	Body string `yaml:"body,omitempty" json:"body,omitempty"`
}

// SNMPStep is an SNMP operation on a saved session (its SNMP settings and saved community or v3
// passwords are used).
type SNMPStep struct {
	// Session: the saved session's path.
	Session string `yaml:"session" json:"session"`
	// Op: get (default), getnext, bulk, walk or set.
	Op string `yaml:"op,omitempty" json:"op,omitempty"`
	// OIDs: names or numeric OIDs (sysDescr.0, IF-MIB::ifOperStatus).
	OIDs []string `yaml:"oids,omitempty" json:"oids,omitempty"`
	// Values to write (op set).
	Values []SNMPSetValue `yaml:"values,omitempty" json:"values,omitempty"`
}

// SNMPSetValue is a value an SNMP set writes.
type SNMPSetValue struct {
	// OID: name or numeric OID with its instance (ifAdminStatus.5).
	OID string `yaml:"oid" json:"oid"`
	// Type: Integer, OctetString, IpAddress … ("" = from the MIB).
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
	// Value as text (an enumerated Integer takes its name too: down).
	Value string `yaml:"value" json:"value"`
}

// SNMPValue is one value an SNMP step got.
type SNMPValue struct {
	// OID: the numeric OID.
	OID string `json:"oid"`
	// Name: MODULE::object.instance (or the OID).
	Name string `json:"name"`
	// Value: the raw value (numbers as digits).
	Value string `json:"value"`
	// Display: the value as read (enum names, time, units).
	Display string `json:"display"`
}

// Expect is one check. Set one subject (output, status, json, field, ms); the rest of the fields say
// what it must be.
type Expect struct {
	// Contains: the output (or the HTTP body) contains this text.
	Contains string `yaml:"contains,omitempty" json:"contains,omitempty"`
	// NotContains: the output does not contain this text.
	NotContains string `yaml:"notContains,omitempty" json:"notContains,omitempty"`
	// Matches: the output matches this regular expression.
	Matches string `yaml:"matches,omitempty" json:"matches,omitempty"`
	// Field: a regular expression with one group, e.g. "Uptime:\s+(\S+)"; its group is compared.
	Field string `yaml:"field,omitempty" json:"field,omitempty"`
	// OID: an SNMP value by name or OID (ifOperStatus.5, sysName.0; "ifOperStatus" = the first one a
	// walk got); equals matches its readable value (up) or its raw value (1); greater / less use the raw one.
	OID string `yaml:"oid,omitempty" json:"oid,omitempty"`
	// JSON: a path into the JSON body, e.g. data.items[0].name; its value is compared.
	JSON string `yaml:"json,omitempty" json:"json,omitempty"`
	// Status: the HTTP status code.
	Status int `yaml:"status,omitempty" json:"status,omitempty"`
	// MaxMs: the step took at most this many milliseconds.
	MaxMs int64 `yaml:"maxMs,omitempty" json:"maxMs,omitempty"`
	// Equals: the field / JSON value is exactly this (also "true", "null", numbers).
	Equals *string `yaml:"equals,omitempty" json:"equals,omitempty"`
	// NotEquals: the field / JSON value is not this.
	NotEquals *string `yaml:"notEquals,omitempty" json:"notEquals,omitempty"`
	// Greater: the field / JSON value is a number above this.
	Greater *float64 `yaml:"greater,omitempty" json:"greater,omitempty"`
	// Less: the field / JSON value is a number below this.
	Less *float64 `yaml:"less,omitempty" json:"less,omitempty"`
	// Exists: the field / JSON value is there (false: is not there).
	Exists *bool `yaml:"exists,omitempty" json:"exists,omitempty"`
}

// Save keeps a value from a step's answer as a variable for later steps.
type Save struct {
	// Var: the variable's name, used later as {{name}}.
	Var string `yaml:"var" json:"var"`
	// Field: a regular expression with one group on the output.
	Field string `yaml:"field,omitempty" json:"field,omitempty"`
	// JSON: a path into the JSON body.
	JSON string `yaml:"json,omitempty" json:"json,omitempty"`
	// OID: an SNMP value (its readable value is saved).
	OID string `yaml:"oid,omitempty" json:"oid,omitempty"`
}
