package apiclient

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// ValidateSchema checks a JSON document against a JSON Schema (the parts APIs use: type, properties,
// required, additionalProperties, items, enum, const, minimum/maximum, minLength/maxLength, minItems/maxItems,
// pattern, format date-time/email/ipv4/uuid, allOf/anyOf/oneOf, nullable and local $ref). It returns the
// problems, "" when the document fits.
func ValidateSchema(schema, doc string) string {
	var s, d any
	if err := json.Unmarshal([]byte(schema), &s); err != nil {
		return "the schema is not JSON: " + err.Error()
	}
	if err := json.Unmarshal([]byte(doc), &d); err != nil {
		return "the body is not JSON: " + err.Error()
	}
	root, _ := s.(map[string]any)
	var errs []string
	validate(root, root, d, "$", &errs, 0)
	if len(errs) > 5 {
		errs = append(errs[:5], fmt.Sprintf("… and %d more", len(errs)-5))
	}
	return strings.Join(errs, "; ")
}

var formats = map[string]*regexp.Regexp{
	"date-time": regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:?\d\d)$`),
	"date":      regexp.MustCompile(`^\d{4}-\d\d-\d\d$`),
	"email":     regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`),
	"ipv4":      regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`),
	"uuid":      regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`),
}

func typeOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if x == math.Trunc(x) {
			return "integer"
		}
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "?"
}

func num(x any) (float64, bool) { f, ok := x.(float64); return f, ok }

func validate(root, s map[string]any, v any, at string, errs *[]string, depth int) {
	if s == nil || depth > 32 {
		return
	}
	add := func(f string, a ...any) { *errs = append(*errs, at+": "+fmt.Sprintf(f, a...)) }
	if ref, _ := s["$ref"].(string); strings.HasPrefix(ref, "#/") {
		var cur any = root
		for _, p := range strings.Split(ref[2:], "/") {
			m, _ := cur.(map[string]any)
			cur = m[strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")]
		}
		r, _ := cur.(map[string]any)
		validate(root, r, v, at, errs, depth+1)
		return
	}
	if v == nil && s["nullable"] == true {
		return
	}
	t := typeOf(v)
	if want := s["type"]; want != nil {
		var ws []string
		switch w := want.(type) {
		case string:
			ws = []string{w}
		case []any:
			for _, x := range w {
				if xs, ok := x.(string); ok {
					ws = append(ws, xs)
				}
			}
		}
		ok := false
		for _, w := range ws {
			if w == t || (w == "number" && t == "integer") {
				ok = true
			}
		}
		if !ok {
			add("is %s, should be %s", t, strings.Join(ws, " or "))
			return
		}
	}
	if en, ok := s["enum"].([]any); ok {
		hit := false
		for _, e := range en {
			if fmt.Sprint(e) == fmt.Sprint(v) && typeOf(e) == t {
				hit = true
			}
		}
		if !hit {
			add("%v is not one of %v", v, en)
		}
	}
	if c, ok := s["const"]; ok && fmt.Sprint(c) != fmt.Sprint(v) {
		add("should be %v", c)
	}
	for _, k := range []string{"allOf", "anyOf", "oneOf"} {
		l, _ := s[k].([]any)
		if len(l) == 0 {
			continue
		}
		pass := 0
		for _, x := range l {
			xm, _ := x.(map[string]any)
			var e []string
			validate(root, xm, v, at, &e, depth+1)
			if len(e) == 0 {
				pass++
			} else if k == "allOf" {
				*errs = append(*errs, e...)
			}
		}
		if (k == "anyOf" && pass == 0) || (k == "oneOf" && pass != 1) {
			add("matches %d of the %s schemas", pass, k)
		}
	}
	switch x := v.(type) {
	case float64:
		if m, ok := num(s["minimum"]); ok && x < m {
			add("%v is below the minimum %v", x, m)
		}
		if m, ok := num(s["maximum"]); ok && x > m {
			add("%v is above the maximum %v", x, m)
		}
	case string:
		n := len([]rune(x))
		if m, ok := num(s["minLength"]); ok && float64(n) < m {
			add("is shorter than %v characters", m)
		}
		if m, ok := num(s["maxLength"]); ok && float64(n) > m {
			add("is longer than %v characters", m)
		}
		if p, ok := s["pattern"].(string); ok {
			if re, err := regexp.Compile(p); err == nil && !re.MatchString(x) {
				add("%q does not match %s", x, p)
			}
		}
		if f, ok := s["format"].(string); ok && formats[f] != nil && !formats[f].MatchString(x) {
			add("%q is not a %s", x, f)
		}
	case []any:
		if m, ok := num(s["minItems"]); ok && float64(len(x)) < m {
			add("has %d items, at least %v", len(x), m)
		}
		if m, ok := num(s["maxItems"]); ok && float64(len(x)) > m {
			add("has %d items, at most %v", len(x), m)
		}
		if it, ok := s["items"].(map[string]any); ok {
			for i, e := range x {
				validate(root, it, e, fmt.Sprintf("%s[%d]", at, i), errs, depth+1)
			}
		}
	case map[string]any:
		props, _ := s["properties"].(map[string]any)
		for _, r := range anyStrings(s["required"]) {
			if _, ok := x[r]; !ok {
				add("misses %q", r)
			}
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if p, ok := props[k].(map[string]any); ok {
				validate(root, p, x[k], at+"."+k, errs, depth+1)
			} else if s["additionalProperties"] == false {
				add("has %q, which the schema does not allow", k)
			} else if ap, ok := s["additionalProperties"].(map[string]any); ok {
				validate(root, ap, x[k], at+"."+k, errs, depth+1)
			}
		}
	}
}

func anyStrings(x any) []string {
	l, _ := x.([]any)
	var out []string
	for _, e := range l {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
