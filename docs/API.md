# Termwright local API

The emulated lab of Termwright (switch VMs on KVM hosts) for your own scripts,
test frameworks and CI, on the computer Termwright runs on.

- **The contract** is [`docs/api/openapi.yaml`](api/openapi.yaml), an
  [OpenAPI 3.2.1](https://spec.openapis.org/oas/v3.2.1.html) document. It is
  generated from the code (the Go types the API sends and takes, their field
  comments, and the lab tools' own argument schemas), so it does not drift:
  `go test -run TestOpenAPIDoc -update ./app/` writes it, and the tests fail when it is
  out of date. It lints clean with Redocly CLI (`redocly lint docs/api/openapi.yaml`).
- The running app serves it too: `GET /api/v1/openapi.json` and
  `GET /api/v1/openapi.yaml` (no key needed). Load either into any OpenAPI
  viewer or client generator.
- Errors are [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem details.

## Turning it on

Settings → AI Assistant → **Local API**. The same page shows the address and has
**Copy Key**. A script can read both from `mcp.json` in Termwright's data folder
(`url` without `/mcp`, plus `/api/v1`; `token`). The key changes every time
Termwright starts.

| | |
|---|---|
| Address | `http://127.0.0.1:<port>/api/v1` (the port is chosen when Termwright starts) |
| Key | `Authorization: Bearer <key>` |
| Listens on | 127.0.0.1 only. Requests carrying an `Origin` header (web pages) are refused |
| Confirmation | None. The API is your own automation; its changes show as **API** in the lab's Events |

The Claude connector uses the same local server. Each has its own switch in
Settings, and turning one on does not turn on the other.

## Resources

| Method and path | What it does | Success |
|---|---|---|
| `GET /lab` | The open topology: switches, state, addresses, consoles, cables, LAGs, hosts, images, Events | 200 `Lab` |
| `GET /lab/tools` | Every lab tool and its arguments (JSON Schema) | 200 `Tool[]` |
| `POST /lab/tools/{tool}` | Runs a tool (`power`, `cable`, `console`, `wait`, `move`, `capture`, `link_check`, `address`, `cpu`, `traffic`, …). The body holds its arguments | 200 `{"result": text}` |
| `GET /lab/traffic` | Test ports, streams and results. Results are live while a run goes on, with rates and rate history | 200 `TrafficView` |
| `GET /lab/traffic/report?format=json\|csv\|html\|text\|pdf\|xlsx` | The run's report: pass or fail per stream, with its counters | 200 |
| `POST /lab/traffic/ports` | Adds a test port: on a free switch port (`switch`, `port`), on a KVM host's spare network card cabled to a real switch (`host`, `nic`, `cabled`), or on a TRex port (`trex`, `trexPort`) | 201 `TrafficView` |
| `GET /lab/traffic/nics?host=` | A KVM host's network cards and whether each can be a test port | 200 `[HostNIC]` |
| `GET /lab/traffic/engines`, `PUT /lab/traffic/engines` | What sends the streams; `{"engine": "fast"}` or `"python"` chooses the KVM hosts' engine | 200 `Engines` |
| `POST /lab/traffic/trex` | Adds (or, with `id`, checks again) a TRex server: `name`, `addr`, `port` (4501) | 201 `Engines` |
| `DELETE /lab/traffic/trex/{server}` | Forgets a TRex server, its test ports and their streams | 204 |
| `PATCH /lab/traffic/ports/{port}` | Changes a test port's name, addresses, gateway, VLAN or multicast groups | 200 `TrafficView` |
| `DELETE /lab/traffic/ports/{port}` | Removes a test port and its streams | 204 |
| `POST /lab/traffic/streams` | Adds a stream | 201 `TrafficView` |
| `PUT /lab/traffic/streams/{stream}` | Replaces a stream (it keeps its id) | 200 `TrafficView` |
| `DELETE /lab/traffic/streams/{stream}` | Removes a stream | 204 |
| `POST /lab/traffic/start` | Starts the given streams (`{"streams": [...]}`), or every stream not marked off | 200 `TrafficView` |
| `POST /lab/traffic/stop` | Stops sending. The counters stay, and are final a moment later | 200 `TrafficView` |
| `GET /lab/traffic/tests` | The saved tests (RFC 2544, RFC 2889 learning, convergence, iperf3 between hosts) | 200 `TrafficTest[]` |
| `POST /lab/traffic/tests` | Saves a test | 201 `TrafficTest[]` |
| `PUT /lab/traffic/tests/{test}` | Replaces a test | 200 `TrafficTest[]` |
| `DELETE /lab/traffic/tests/{test}` | Removes a test | 204 |
| `POST /lab/traffic/tests/{test}/run` | Starts it in the background; `Location` is the run | 202 `TestRun` |
| `GET /lab/traffic/runs` | The running test and the last 20 runs | 200 `TestRun[]` |
| `GET /lab/traffic/runs/{run}?format=json\|csv\|html\|text` | A run's progress and results (`latest` for the newest) | 200 |
| `POST /lab/traffic/runs/stop` | Stops the running test | 200 |
| `POST /snmp` | An SNMP request: `op` (`get`, `getnext`, `bulk`, `walk`, `table`, `set`), `oids` or `values`, and a saved `session` (its address and credentials) or a `target` | 200 `SNMPResult` |
| `GET /snmp/mib?q=` | Searches the loaded MIBs by name or OID | 200 `SNMPMibNode[]` |
| `GET /snmp/mib/{object}` | One object (`IF-MIB::ifOperStatus`, `sysDescr`, an OID): syntax, values, access, description | 200 `SNMPMibNode` |
| `POST /snmp/trap` | Sends a test notification (v1, v2c, v3) to a receiver | 200 |
| `GET /snmp/traps` | The trap receiver's state and the traps received, newest first | 200 `SNMPTrapsView` |
| `POST /http/send` | Sends a request with a saved API `session` (base URL, login, variables) or as it is: `{"session": "APIs/rest", "request": {"method": "GET", "url": "/rest/..."}}`; its tests run | 200 `HTTPResponse` |
| `GET /http/collections`, `POST /http/collections` | The API client's collections; add one | 200 / 201 |
| `GET`, `PUT`, `DELETE /http/collections/{collection}` | One collection (`?format=postman` for Postman v2.1); replace; delete | 200 / 204 |
| `POST /http/collections/{collection}/run` | Runs it (requests, folder, iterations, CSV / JSON data) with its tests | 200 `HTTPRunResult` |
| `POST /http/import` | Imports a Postman collection, an OpenAPI / Swagger document or cURL (`{"text": "..."}`) | 201 `HTTPCollection` |
| `GET /http/history` | The requests sent, newest first | 200 |
| `GET /http/environments` | The environments, the globals (id `globals`) first; secret values never given | 200 |
| `POST /http/environments` | Add or replace an environment (secret values go to the credential store) | 200 |
| `POST /http/variables` | The variables a request can use, with their values and sources (secrets left out) | 200 |
| `GET /syslog` | The syslog server: listening, counts per severity, senders | 200 `SyslogState` |
| `POST /syslog/start`, `POST /syslog/stop` | Starts (with settings, or the saved ones) or stops it | 200 `SyslogState` |
| `GET /syslog/messages?q=&host=&severity=&minutes=&after=&limit=`, `DELETE /syslog/messages` | Messages received (words or /regex/, sender, severity or worse); clear them | 200 `[SyslogView]` / 204 |
| `POST /syslog/send` | Sends a test message to a syslog server | 200 |
| `GET`, `PUT /syslog/rules` | Rules that colour, notify or hide | 200 |
| `GET /ai/skills` | The AI skills (SKILL.md procedures the AI assistant follows); `POST` adds or replaces one from its text | 200 |
| `GET /ai/skills/{skill}` | One skill with its whole SKILL.md | 200 |
| `GET /tools` | The network tools (`device`, `snmp`, `http`, `syslog`, `tests`) with their arguments (JSON Schema), the same as the MCP connector's | 200 `Tool[]` |
| `POST /tools/{tool}` | Runs a network tool; the body holds its arguments (`device`: a saved session's facts and device type - facts, refresh_facts, identify, set_type, types; `tests`: Termwright tests - list, show, save (`file`, `yaml`), run (`files` - tests, folders or suites/… -, `testbed` or `testbeds` (at the same time), `vars`, `tags`, `cleanup` steps|off|auto, `on_cleanup_fail` stop|restore|factory, `force_lock`; pass/fail per step and the report's path), generate (the Go, Python and TCL tests), step (one `run`, `http` or `snmp` step without its checks, as JSON - used by the generated Go tests), new_testbed (`name`, `folder`: a role per saved session of a session folder), schedules, schedule (`repeat` daily|weekdays|weekly|hours, `at`, `day`, `hours`, `files`, `testbed`, `production`, `on_cleanup_fail`; with `id` it changes one), unschedule (`id`), results, history (`file`: pass rate, flips, per software version), send_qtest (`dir`, default the last run), defect (`file`, `dir`: a Jira defect)) | 200 `{"result": text}` |

`{port}` and `{stream}` take a name (`Test port 1`, `voice`) or an id. In a
stream, `from` and `to` take names too. The OpenAPI document has every field,
its meaning and examples.

A Set through `POST /snmp` needs *Allow Set* in the session's SNMP settings.
The community and the v3 passwords of a session never leave the computer's
credential store: the API uses them but does not return them.

```sh
curl -s -H "Authorization: Bearer $KEY" -d '{"session":"Lab/core-sw1","op":"walk","oids":["IF-MIB::ifOperStatus"]}' $API/snmp
```

## A traffic test with curl

```sh
API=http://127.0.0.1:PORT/api/v1/lab
AUTH="Authorization: Bearer KEY"

curl -s -H "$AUTH" -H 'Content-Type: application/json' -X POST $API/traffic/ports \
  -d '{"switch":"leaf-1","port":"5","ip":"10.10.0.1/24"}'
curl -s -H "$AUTH" -H 'Content-Type: application/json' -X POST $API/traffic/ports \
  -d '{"switch":"leaf-2","port":"5","ip":"10.10.0.2/24"}'
curl -s -H "$AUTH" -H 'Content-Type: application/json' -X POST $API/traffic/streams \
  -d '{"name":"voice","from":"Test port 1","to":"Test port 2","kind":"udp","size":200,"pps":1000,"seconds":30,"dscp":46,"maxLoss":0.1,"maxJitter":1}'
curl -s -H "$AUTH" -X POST $API/traffic/start -d '{"streams":["voice"]}'
sleep 35
curl -s -H "$AUTH" "$API/traffic/report?format=text"
```

## A convergence test

```sh
curl -s -H "$AUTH" -H 'Content-Type: application/json' -X POST $API/traffic/tests \
  -d '{"name":"lag-failover","kind":"convergence","from":"Test port 1","to":"Test port 2","both":true,"rate":1000,"seconds":30,"action":"pull","switch":"leaf-1","port":"1","at":5,"hold":10}'
curl -s -H "$AUTH" -X POST $API/traffic/tests/lag-failover/run
curl -s -H "$AUTH" "$API/traffic/runs/latest?format=text"     # until its status is done
```

## The same in Python (pytest)

`scripts/python/termwright_lab.py` is a ready client (standard library only, with RFC 2544, convergence and iperf3 in one call each); see [the Python guide](PYTHON.md). Without it, the same with `requests`:

```python
import json, os, pathlib, platform, time, requests

def data_dir():
    # TERMWRIGHT_HOME when set; else macOS ~/Library/Application Support/Termwright,
    # Linux ~/.config/Termwright, Windows %AppData%\Termwright (a portable install: termwright-data next to the program)
    if os.environ.get("TERMWRIGHT_HOME"):
        return pathlib.Path(os.environ["TERMWRIGHT_HOME"])
    home = pathlib.Path.home()
    return {"Darwin": home / "Library/Application Support/Termwright",
            "Windows": pathlib.Path(os.environ.get("APPDATA", "")) / "Termwright"}.get(platform.system(), home / ".config/Termwright")

def api():
    ep = json.loads((data_dir() / "mcp.json").read_text())
    s = requests.Session()
    s.headers["Authorization"] = "Bearer " + ep["token"]
    return s, ep["url"].removesuffix("/mcp") + "/api/v1/lab"

def test_voice_has_no_loss():
    s, base = api()
    s.post(base + "/traffic/streams", json={"name": "voice", "from": "Test port 1", "to": "Test port 2",
                                            "pps": 1000, "seconds": 20, "dscp": 46, "maxLoss": 0.1}).raise_for_status()
    s.post(base + "/traffic/start", json={"streams": ["voice"]}).raise_for_status()
    time.sleep(3)
    while (view := s.get(base + "/traffic").json())["running"]:
        time.sleep(2)
    voice = next(x for x in view["streams"] if x["name"] == "voice")
    result = next(r for r in view["results"] if r["id"] == voice["id"])
    assert result["result"] == "pass", result.get("why")
```

## Command line

`termwright lab …` calls the running app's local API, so the API must be on.

```sh
termwright lab help
termwright lab show                                   # GET /lab
termwright lab power action=start switches=leaf-1,leaf-2
termwright lab wait timeout=900
termwright lab debug switch=leaf-1 lines=300 > leaf-1-debug.txt
termwright lab cpu switches=leaf-1 max_cores=1 share=50
termwright lab address action=set switch=pc-1 port=1 address=10.10.0.11/24
termwright lab traffic add_port switch=leaf-1 port=5 ip=10.10.0.1/24
termwright lab traffic add_stream name=voice from="Test port 1" to="Test port 2" pps=1000 seconds=30 dscp=46
termwright lab traffic start voice
termwright lab traffic report --csv > report.csv      # --json, --html, or text by default
```

Arguments are `name=value`. Numbers and `true`/`false` are passed as they
are, lists are split at commas, and anything starting with `[` or `{` is read
as JSON (`mods='[{"field":"srcIp","count":100}]'`). The command exits with 0
when the request succeeds, 1 when the API refused it (the reason goes to
stderr), and 2 when Termwright is not reachable.

## Problem types

Every error has the body
`{"type": "…/docs/API.md#<kind>", "title", "status", "detail"}`, served as
`application/problem+json`. `detail` says what went wrong.

### unauthorized
401: no key, or a wrong one. The key changes each time Termwright starts, so read it again from `mcp.json`.

### forbidden
403: the request came from a web page (it has an `Origin` header).

### api-off
403: the local API is off. Turn it on in Settings → AI Assistant → Local API.

### bad-json
400: the body is not a JSON object.

### bad-format
400: `format` must be `json`, `csv`, `html` or `text`.

### bad-stream
400: the stream does not match `StreamInput`: an unknown field, or a value of the wrong type.

### not-found
404: there is no such path. `GET /api/v1/openapi.json` lists the paths.

### method-not-allowed
405: this method is not allowed on this path. The `Allow` header lists the methods that are.

### no-port
404: there is no test port with that name or id.

### no-stream
404: there is no stream with that name or id.

### bad-test
400: the test is not a `TestInput`: an unknown field, or a value of the wrong type.

### no-test
404: there is no saved test with that name or id.

### no-run
404: there is no run with that id (the runs are kept in memory until Termwright restarts).

### no-tool
404: there is no lab tool with that name. `GET /api/v1/lab/tools` lists them.

### lab
422: the lab could not do what was asked, for example because there is no such switch, the port is in use, there is no stream to start, or the switch is not running.

### report
500: the report could not be made.

## Versioning

The path carries the major version (`/api/v1`). Within v1, changes only add
things: new paths, new optional fields, new tools. Clients should ignore
fields they do not know. The document's `info.version` follows semantic
versioning.
