package testrec

import "testing"

func TestImportPostman(t *testing.T) {
	doc := `{"info":{"name":"Lab","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
 "item":[{"name":"VLANs","item":[
  {"name":"Create VLAN","request":{"method":"POST","url":{"raw":"{{baseUrl}}/rest/vlans"},"header":[{"key":"Content-Type","value":"application/json"}],"body":{"mode":"raw","raw":"{\"name\":\"v200\"}"}},
   "event":[{"listen":"test","script":{"exec":["pm.response.to.have.status(201);","pm.expect(pm.response.text()).to.include(\"v200\");"]}}]},
  {"name":"List VLANs","request":{"method":"GET","url":"https://sw1/rest/vlans"},"response":[{"code":200}]}]}]}`
	if !IsAPIDoc([]byte(doc)) {
		t.Fatal("not detected")
	}
	cs, err := ImportAPI([]byte(doc), "APIs/sw1")
	if err != nil || len(cs) != 2 {
		t.Fatal(err, cs)
	}
	c := cs[0]
	h := c.Case.Steps[0].HTTP
	if c.File != "cases/vlans/create-vlan.yaml" || h.Method != "POST" || h.URL != "/rest/vlans" || h.Headers["Content-Type"] == "" || h.Body == "" {
		t.Fatalf("%+v %+v", c, h)
	}
	if e := c.Case.Steps[0].Expect; len(e) != 2 || e[0].Status != 201 || e[1].Contains != "v200" {
		t.Fatalf("%+v", e)
	}
	if h := cs[1].Case.Steps[0]; h.HTTP.URL != "/rest/vlans" || h.HTTP.Method != "" || h.Expect[0].Status != 200 {
		t.Fatalf("%+v", h)
	}
}

func TestImportOpenAPI(t *testing.T) {
	doc := `openapi: 3.0.0
servers: [{url: "https://sw1/api/v1"}]
paths:
  /vlans/{id}:
    get:
      summary: Get a VLAN
      tags: [vlans]
      responses: {"200": {description: ok}, "404": {description: no}}
  /vlans:
    post:
      operationId: createVlan
      tags: [vlans]
      requestBody:
        content:
          application/json:
            schema: {$ref: "#/components/schemas/Vlan"}
      responses: {"201": {description: made}}
components:
  schemas:
    Vlan: {type: object, properties: {id: {type: integer, example: 200}, name: {type: string}}}
`
	cs, err := ImportAPI([]byte(doc), "")
	if err != nil || len(cs) != 2 {
		t.Fatal(err, cs)
	}
	if h := cs[0].Case.Steps[0].HTTP; cs[0].Case.Name != "createVlan" || h.Method != "POST" || h.URL != "https://sw1/api/v1/vlans" || h.Body == "" || cs[0].Case.Steps[0].Expect[0].Status != 201 {
		t.Fatalf("%+v %+v", cs[0], h)
	}
	if h := cs[1].Case.Steps[0].HTTP; h.URL != "https://sw1/api/v1/vlans/{{id}}" || cs[1].File != "cases/vlans/get-a-vlan.yaml" {
		t.Fatalf("%+v %s", h, cs[1].File)
	}
}
