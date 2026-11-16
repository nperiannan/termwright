# Termwright build helpers. The Wails CLI is the normal way to build:
#   go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2
# the version of the newest release tag (v0.39.0 -> 0.39.0) plus the commit; releases are tagged on main, so the
# newest tag by version, not the last one develop reaches; 0.23.0 when there is no tag here
BASE_VERSION ?= $(or $(shell git tag --list 'v*' --sort=-v:refname 2>/dev/null | head -1 | sed 's/^v//'),0.23.0)
VERSION ?= $(BASE_VERSION)~git$(shell git rev-parse --short=7 HEAD 2>/dev/null)
LDFLAGS := -X main.Version=$(VERSION)
# The Wails 2.10 CLI cannot load packages compiled by Go 1.27+, so builds use a
# pinned Go toolchain (downloaded automatically by the go command).
export GOTOOLCHAIN ?= go1.24.7
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Linux)
  # Ubuntu 24.04+/Debian 13+ ship webkit2gtk-4.1 only
  TAGS := webkit2_41
endif

.PHONY: dev build test frontend clean fastbin
dev:            ## live-reload development
	wails dev -skipbindings -tags "$(TAGS)"
fastbin:        ## the fast traffic engine for the KVM hosts (Linux x86-64 and ARM64), embedded in the app
	for a in amd64 arm64; do GOOS=linux GOARCH=$$a CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o internal/emu/fastbin/bin/tw-fast-$$a ./cmd/twfast || exit 1; done
build: fastbin  ## production build for this OS -> build/bin
	wails build -clean -skipbindings -tags "$(TAGS)" -ldflags "$(LDFLAGS)"
frontend:       ## build only the Angular UI
	cd frontend && npm ci && npm run build
test:           ## Go vet, unit and integration tests, and the Python client's tests
	go vet ./app/... ./internal/... ./cmd/... ./mobile/... && go test -race ./internal/... ./mobile/... && go test -count=1 ./app/...
	cd scripts/python && python3 -m unittest discover -p 'test*.py'
build-nocli: frontend fastbin  ## build without the Wails CLI (no icons/manifests)
	go build -tags "desktop,production,$(TAGS)" -ldflags "-s -w $(LDFLAGS)" -o build/bin/termwright .
clean:
	rm -rf build/bin frontend/dist/* frontend/.angular internal/emu/fastbin/bin/tw-fast-*
