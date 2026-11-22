package tgen

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeSTC answers like the STC REST API for one configuration with two ports and two stream blocks.
func fakeSTC(t *testing.T, calls *[]string) *httptest.Server {
	var mu sync.Mutex
	objs := map[string]map[string]any{
		"project1":                {"children-port": "port1 port2"},
		"port1":                   {"children-generator": "generator1", "children-streamblock": "streamblock1"},
		"port2":                   {"children-generator": "generator2", "children-streamblock": "streamblock2"},
		"streamblock1":            {"Name": "east-west", "children-rxstreamsummaryresults": "rxstreamsummaryresults1", "children-txstreamresults": "txstreamresults1"},
		"streamblock2":            {"Name": "west-east", "children-rxstreamsummaryresults": "rxstreamsummaryresults2", "children-txstreamresults": "txstreamresults2"},
		"txstreamresults1":        {"FrameCount": "1000"},
		"txstreamresults2":        {"FrameCount": "2000"},
		"rxstreamsummaryresults1": {"FrameCount": "1000", "AvgLatency": "12.5"},
		"rxstreamsummaryresults2": {"FrameCount": "1990", "AvgLatency": "13"},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		*calls = append(*calls, r.Method+" "+r.URL.Path)
		if r.URL.Path != "/stcapi/sessions/" && r.Header.Get("X-STC-API-Session") != "tw - termwright" {
			http.Error(w, "no session", 400)
			return
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/stcapi/sessions/":
			io.WriteString(w, `{"session_id": "tw - termwright"}`)
		case r.Method == "POST" && r.URL.Path == "/stcapi/files/":
			if f, _, err := r.FormFile("file"); err != nil || f == nil {
				http.Error(w, "no file", 400)
				return
			}
			io.WriteString(w, `{}`)
		case r.Method == "POST" && r.URL.Path == "/stcapi/perform":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			*calls = append(*calls, "perform "+b["command"].(string))
			io.WriteString(w, `{}`)
		case r.Method == "PUT":
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			objs[strings.TrimPrefix(r.URL.Path, "/stcapi/objects/")]["location"] = b["location"]
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/stcapi/objects/"):
			o := objs[strings.TrimPrefix(r.URL.Path, "/stcapi/objects/")]
			out := map[string]any{}
			for _, a := range strings.Split(r.URL.RawQuery, "&") {
				out[a] = o[a]
			}
			json.NewEncoder(w).Encode(out)
		case r.Method == "DELETE":
		default:
			http.Error(w, "?", 404)
		}
	}))
}

func TestSTC(t *testing.T) {
	var calls []string
	srv := fakeSTC(t, &calls)
	defer srv.Close()
	r, err := Run(context.Background(), Job{Kind: "stc", Server: srv.URL, Config: []byte("<StcSystem/>"), ConfigName: "tgen/l3.xml", Ports: []string{"10.1.1.5/1/1", "10.1.1.5/1/2"}, Duration: 1})
	if err != nil {
		t.Fatal(err, calls)
	}
	if len(r.Streams) != 2 || r.Streams[1].Loss != 0.5 || r.Streams[0].LatencyUs != 12.5 || r.TxFrames != 3000 || r.Loss != 0.333 {
		t.Fatalf("%+v", r)
	}
	all := strings.Join(calls, "|")
	for _, want := range []string{"perform LoadFromXml", "PUT /stcapi/objects/port1", "perform AttachPorts", "perform ResultsSubscribe", "perform GeneratorStart", "perform GeneratorStop", "perform DetachPorts", "DELETE /stcapi/sessions/"} {
		if !strings.Contains(all, want) {
			t.Fatalf("no %s in %s", want, all)
		}
	}
	if !strings.Contains(r.Text(), "west-east") {
		t.Fatal(r.Text())
	}
}

// fakeIxN answers like the IxNetwork REST API; operations finish after one poll.
func fakeIxN(t *testing.T, calls *[]string) *httptest.Server {
	var mu sync.Mutex
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		p := r.URL.Path
		*calls = append(*calls, r.Method+" "+p)
		if p != "/api/v1/auth/session" && r.Header.Get("X-Api-Key") != "k1" {
			http.Error(w, "unauthorised", 401)
			return
		}
		ix := "/api/v1/sessions/7/ixnetwork"
		switch {
		case p == "/api/v1/auth/session":
			io.WriteString(w, `{"apiKey": "k1"}`)
		case p == "/api/v1/sessions" && r.Method == "POST":
			io.WriteString(w, `{"id": 7}`)
		case strings.Contains(p, "/operations/"):
			if strings.HasSuffix(p, "/operations/assignports") {
				var b map[string]any
				json.NewDecoder(r.Body).Decode(&b)
				if len(b["arg1"].([]any)) != 2 {
					http.Error(w, "bad ports", 400)
					return
				}
			}
			io.WriteString(w, `{"id": 1, "state": "IN_PROGRESS", "url": "/api/v1/poll/1"}`)
		case p == "/api/v1/poll/1":
			io.WriteString(w, `{"id": 1, "state": "SUCCESS", "result": null}`)
		case p == ix+"/files":
			io.WriteString(w, `{}`)
		case p == ix+"/vport":
			io.WriteString(w, `[{"id":1,"links":[{"rel":"self","href":"`+ix+`/vport/1"}]},{"id":2,"links":[{"rel":"self","href":"`+ix+`/vport/2"}]}]`)
		case p == ix+"/traffic/trafficItem":
			io.WriteString(w, `[{"id":1,"links":[{"rel":"self","href":"`+ix+`/traffic/trafficItem/1"}]}]`)
		case p == ix+"/statistics/view":
			io.WriteString(w, `[{"id": 3, "caption": "Port Statistics"}, {"id": 5, "caption": "Traffic Item Statistics"}]`)
		case p == ix+"/statistics/view/5/page":
			io.WriteString(w, `{"columnCaptions": ["Traffic Item", "Tx Frames", "Rx Frames", "Frames Delta", "Loss %", "Store-Forward Avg Latency (ns)"],
				"rowValues": {"arg1": [["v200 east-west", "10000", "9990", "10", "0.100", "15000"]], "arg2": [["v300", "500", "500", "0", "0.000", "8000"]]}}`)
		case r.Method == "DELETE":
		default:
			http.Error(w, "? "+p, 404)
		}
	}))
	return srv
}

func TestIxNetwork(t *testing.T) {
	var calls []string
	srv := fakeIxN(t, &calls)
	defer srv.Close()
	r, err := Run(context.Background(), Job{Kind: "ixnetwork", Server: srv.URL, Username: "admin", Password: "x", Config: []byte("{}"), ConfigName: "l3.ixncfg", Ports: []string{"10.1.1.6/1/1", "10.1.1.6/1/2"}, Duration: 1, Protocols: true, Settle: 0})
	if err != nil {
		t.Fatal(err, calls)
	}
	if len(r.Streams) != 2 || r.Streams[0].Name != "v200 east-west" || r.Streams[0].Loss != 0.1 || r.Streams[0].LatencyUs != 15 || r.RxFrames != 10490 {
		t.Fatalf("%+v", r)
	}
	all := strings.Join(calls, "|")
	for _, want := range []string{"operations/loadconfig", "operations/assignports", "operations/startallprotocols", "trafficItem/operations/generate", "traffic/operations/apply", "traffic/operations/start", "traffic/operations/stop", "DELETE /api/v1/sessions/7"} {
		if !strings.Contains(all, want) {
			t.Fatalf("no %s in %s", want, all)
		}
	}
}

func TestBadKind(t *testing.T) {
	if _, err := Run(context.Background(), Job{Kind: "x", Server: "h"}); err == nil {
		t.Fatal("no error")
	}
}
