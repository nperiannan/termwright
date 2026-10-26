# Termwright build helpers. The Wails CLI is the normal way to build:
#   go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2
VERSION ?= 0.14.0~git$(shell git rev-parse --short=7 HEAD 2>/dev/null)
LDFLAGS := -X main.Version=$(VERSION)
# The Wails 2.10 CLI cannot load packages compiled by Go 1.27+, so builds use a
# pinned Go toolchain (downloaded automatically by the go command).
export GOTOOLCHAIN ?= go1.24.7
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Linux)
  # Ubuntu 24.04+/Debian 13+ ship webkit2gtk-4.1 only
  TAGS := webkit2_41
endif

.PHONY: dev build test frontend clean
dev:            ## live-reload development
	wails dev -skipbindings -tags "$(TAGS)"
build:          ## production build for this OS -> build/bin
	wails build -clean -skipbindings -tags "$(TAGS)" -ldflags "$(LDFLAGS)"
frontend:       ## build only the Angular UI
	cd frontend && npm ci && npm run build
test:           ## backend unit + integration tests
	go vet ./internal/... && go test -race ./internal/...
build-nocli: frontend  ## build without the Wails CLI (no icons/manifests)
	go build -tags "desktop,production,$(TAGS)" -ldflags "-s -w $(LDFLAGS)" -o build/bin/termwright .
clean:
	rm -rf build/bin frontend/dist/* frontend/.angular
