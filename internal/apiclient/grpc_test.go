package apiclient

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const labProto = `syntax = "proto3";
package lab.v1;
import "google/protobuf/timestamp.proto";
import "google/protobuf/duration.proto";
import "google/protobuf/wrappers.proto";
import "google/protobuf/struct.proto";

// A port of a switch.
message Port {
  enum State { STATE_UNSPECIFIED = 0; UP = 1; DOWN = 2; }
  string name = 1;
  State state = 2;
  int64 rx_bytes = 3;
  uint32 speed_mbps = 4;
  sint32 delta = 5;
  sint64 big_delta = 6;
  fixed32 f32 = 7;
  fixed64 f64 = 8;
  double load = 9;
  float temp = 10;
  bool poe = 11;
  bytes mac = 12;
  repeated int32 vlans = 13;
  repeated string tags = 14 [json_name = "labels"];
  map<string, int32> counters = 15;
  oneof addr { string ipv4 = 16; string ipv6 = 17; }
  google.protobuf.Timestamp seen = 18;
  google.protobuf.Duration uptime = 19;
  google.protobuf.Int32Value mtu = 20;
  google.protobuf.Struct extra = 21;
  repeated State history = 22;
}
message Switch { string name = 1; repeated Port ports = 2; map<string, Port> by_name = 3; }
message GetReq { string name = 1; int32 count = 2; }
message Sum { int64 total = 1; int32 n = 2; }
service Inventory {
  rpc Echo (Switch) returns (Switch);
  rpc Get (GetReq) returns (Port);
  rpc Watch (GetReq) returns (stream Port);
  rpc Count (stream Port) returns (Sum);
  rpc Chat (stream GetReq) returns (stream GetReq);
}
`

const portJSON = `{"name":"1/1","state":"UP","rxBytes":"9007199254740993","speedMbps":10000,"delta":-5,"bigDelta":"-70000000000","f32":7,"f64":"8",
 "load":0.25,"temp":41.5,"poe":true,"mac":"AAEC/w==","vlans":[10,20,4094],"labels":["core","uplink"],"counters":{"crc":3,"drops":0},"ipv4":"10.0.0.1",
 "seen":"2026-11-27T10:00:00.5Z","uptime":"3600.25s","mtu":9000,"extra":{"site":"lab","rack":4,"ok":true,"list":[1,"a",null],"nested":{"x":1}},"history":["DOWN","UP"]}`

func canon(t *testing.T, s string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("%v: %s", err, s)
	}
	return v
}

func TestProtoParseAndCodec(t *testing.T) {
	ps, err := ParseProtoText("lab.proto", labProto)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps.Warnings) > 0 {
		t.Fatal(ps.Warnings)
	}
	_, m, err := ps.method("lab.v1.Inventory/Watch")
	if err != nil || !m.ServerStream || m.In != ".lab.v1.GetReq" || m.Out != ".lab.v1.Port" {
		t.Fatalf("%v %+v", err, m)
	}
	if _, m, _ := ps.method("Inventory.Chat"); m == nil || !m.ClientStream || !m.ServerStream {
		t.Fatal("chat")
	}
	b, err := ps.encodeJSON(".lab.v1.Port", portJSON)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ps.decodeJSON(".lab.v1.Port", b)
	if err != nil {
		t.Fatal(err)
	}
	want := canon(t, strings.ReplaceAll(portJSON, `"drops":0`, `"drops":0`))
	if got := canon(t, back); !reflect.DeepEqual(got, want) {
		gb, _ := json.Marshal(got)
		t.Fatalf("round trip:\n%s\n%s", gb, portJSON)
	}
	// errors that help
	for in, want := range map[string]string{`{"nme":"x"}`: "has no field \"nme\"", `{"state":"SIDEWAYS"}`: "not one of", `{"speedMbps":"fast"}`: "not a valid uint32",
		`{"vlans":5}`: "array is needed", `{"seen":"yesterday"}`: "a time like"} {
		if _, err := ps.encodeJSON(".lab.v1.Port", in); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v", in, err)
		}
	}
	ex, _ := json.Marshal(ps.example(".lab.v1.Switch", 0))
	if !strings.Contains(string(ex), `"ports":[{"name":""`) || !strings.Contains(string(ex), `"byName":{"key":{`) {
		t.Fatal(string(ex))
	}
}

func TestProtoFilesImports(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "common"), 0o755)
	os.WriteFile(filepath.Join(d, "common", "types.proto"), []byte(`syntax = "proto3"; package common; message Id { string v = 1; }`), 0o644)
	os.WriteFile(filepath.Join(d, "svc.proto"), []byte(`syntax = "proto3"; package svc;
import "common/types.proto"; import "google/api/annotations.proto"; import "missing/thing.proto";
service S { rpc Get (common.Id) returns (common.Id) { option (google.api.http) = { get: "/v1/{v}" }; } }`), 0o644)
	ps, err := ParseProtoFiles([]string{filepath.Join(d, "svc.proto")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, m, err := ps.method("svc.S/Get"); err != nil || m.In != ".common.Id" {
		t.Fatalf("%v %+v", err, m)
	}
	if len(ps.Warnings) != 1 || !strings.Contains(ps.Warnings[0], "missing/thing.proto") {
		t.Fatal(ps.Warnings)
	}
}

// TW_GRPC_ADDR=127.0.0.1:50071 runs these against a real gRPC server (grpcio, the lab.proto above, reflection on).
func TestGRPCInterop(t *testing.T) {
	addr := os.Getenv("TW_GRPC_ADDR")
	if addr == "" {
		t.Skip("TW_GRPC_ADDR not set")
	}
	d := t.TempDir()
	pf := filepath.Join(d, "lab.proto")
	os.WriteFile(pf, []byte(labProto), 0o644)
	auth := &ReqAuth{Token: "tok"}
	for _, proto := range []string{"", pf} { // reflection, then the .proto file
		tg := &Target{}
		call := func(method, body string) Response {
			return Send(context.Background(), Request{Method: "GRPC", URL: addr, Auth: "bearer", AuthData: auth, Body: Body{Raw: body},
				GRPC: &GRPCSpec{Method: method, Proto: proto}}, tg)
		}
		sch, err := GRPCDescribe(context.Background(), Request{Method: "GRPC", URL: addr, Auth: "bearer", AuthData: auth, GRPC: &GRPCSpec{Proto: proto}}, tg)
		if err != nil || len(sch.Methods) != 5 {
			t.Fatalf("describe (%q): %v %+v", proto, err, sch)
		}
		sw := `{"name":"core-1","ports":[` + portJSON + `],"byName":{"p1":{"name":"p1","state":"DOWN"}}}`
		r := call("lab.v1.Inventory/Echo", sw)
		if r.Status != 200 || r.StatusText != "0 OK" {
			t.Fatalf("echo: %d %s %s", r.Status, r.StatusText, r.Error)
		}
		if got, want := canon(t, r.Body), canon(t, sw); !reflect.DeepEqual(got, want) {
			t.Fatalf("echo changed the message:\n%s", r.Body)
		}
		r = call("lab.v1.Inventory/Get", `{"name":"missing"}`)
		if r.Status != 404 || !strings.Contains(r.StatusText, "NOT_FOUND: port missing not found: ü") {
			t.Fatalf("not found: %d %q %s", r.Status, r.StatusText, r.Error)
		}
		r = call("lab.v1.Inventory/Get", `{"name":"x"}`)
		if !strings.Contains(r.Body, `"rxBytes": "9007199254740993"`) || !strings.Contains(r.Body, `"state": "UP"`) {
			t.Fatal(r.Body)
		}
		r = call("lab.v1.Inventory/Watch", `{"name":"p","count":3}`)
		var l []map[string]any
		if json.Unmarshal([]byte(r.Body), &l) != nil || len(l) != 3 || l[2]["name"] != "p-2" {
			t.Fatalf("stream: %s %s", r.Body, r.Error)
		}
		r = call("lab.v1.Inventory/Count", `[{"rxBytes":"5"},{"rxBytes":"7"}]`)
		if !strings.Contains(r.Body, `"total": "12"`) || !strings.Contains(r.Body, `"n": 2`) {
			t.Fatalf("client stream: %s %s", r.Body, r.Error)
		}
		r = call("lab.v1.Inventory/Chat", "{\"name\":\"a\",\"count\":1}\n{\"name\":\"b\",\"count\":2}")
		if json.Unmarshal([]byte(r.Body), &l) != nil || len(l) != 2 || l[1]["name"] != "B" || l[1]["count"] != float64(4) {
			t.Fatalf("bidi: %s %s", r.Body, r.Error)
		}
		auth2 := Request{Method: "GRPC", URL: addr, Body: Body{Raw: `{}`}, GRPC: &GRPCSpec{Method: "lab.v1.Inventory/Get", Proto: proto}}
		if r := Send(context.Background(), auth2, tg); r.Status != 401 || !strings.Contains(r.StatusText, "UNAUTHENTICATED") {
			t.Fatalf("no auth: %d %s %s", r.Status, r.StatusText, r.Error)
		}
		res := Run(context.Background(), Request{Method: "GRPC", URL: addr, Auth: "bearer", AuthData: auth, Body: Body{Raw: `{"name":"t"}`}, GRPC: &GRPCSpec{Method: "lab.v1.Inventory/Get", Proto: proto},
			Tests: `pm.test("ok", function(){ pm.response.to.have.status(200); pm.expect(pm.response.json().name).to.eql("t"); });`}, tg)
		if f := failed(res); f != "" || len(res.Tests) != 1 {
			t.Fatal(f)
		}
	}
}

func TestStreamCode(t *testing.T) {
	c, err := Code(Request{Method: "GRPC", URL: "lab:50051", Auth: "bearer", Body: Body{Raw: `{"name":"x"}`}, GRPC: &GRPCSpec{Method: "lab.v1.Inventory/Get", Proto: "/p/lab.proto"}}, &Target{}, "curl")
	if err != nil || !strings.Contains(c, "grpcurl -plaintext -proto '/p/lab.proto'") || !strings.Contains(c, `-H "Authorization: Bearer $API_TOKEN"`) || !strings.Contains(c, "lab:50051 lab.v1.Inventory/Get") {
		t.Fatalf("%v\n%s", err, c)
	}
	c, _ = Code(Request{Method: "WS", URL: "wss://h/ws", Headers: []KV{{Key: "X-A", Value: "1"}}}, &Target{}, "curl")
	if c != "websocat -H 'X-A: 1' 'wss://h/ws'\n" {
		t.Fatalf("%q", c)
	}
}
