package apiclient

import (
	"strings"
	"testing"
)

func TestValidateSchema(t *testing.T) {
	sch := `{"type":"object","required":["id","name"],"properties":{"id":{"type":"integer","minimum":1},"name":{"type":"string","pattern":"^v"},
	"ports":{"type":"array","items":{"$ref":"#/definitions/port"}}},"definitions":{"port":{"type":"string","enum":["1:1","1:2"]}}}`
	if e := ValidateSchema(sch, `{"id":200,"name":"v200","ports":["1:1"]}`); e != "" {
		t.Fatal(e)
	}
	e := ValidateSchema(sch, `{"id":0,"name":"x","ports":["9:9"],"extra":1}`)
	for _, w := range []string{"$.id: 0 is below", "$.name", "$.ports[0]"} {
		if !strings.Contains(e, w) {
			t.Fatalf("%q misses %q", e, w)
		}
	}
	if e := ValidateSchema(sch, `{"id":"1"}`); !strings.Contains(e, `misses "name"`) || !strings.Contains(e, "is string, should be integer") {
		t.Fatal(e)
	}
}
