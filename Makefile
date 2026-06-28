APP        := cc-connect
MODULE     := github.com/chenhg5/cc-connect
CMD        := ./cmd/cc-connect
DIST       := dist
WORKTREE_TMP ?= .tmp
TEST_BINARY  ?= $(WORKTREE_TMP)/cc-connect
GO_TEST_CACHE ?= $(WORKTREE_TMP)/go-cache
GO_TEST_ENV := GOCACHE=$(abspath $(GO_TEST_CACHE))

VERSION := v1.3.3
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := -s -w \
  -X main.version=$(VERSION) \
  -X main.commit=$(COMMIT) \
  -X main.buildTime=$(BUILD_TIME)

PLATFORMS := \
  linux/amd64 \
  linux/arm64 \
  darwin/amd64 \
  darwin/arm64 \
  windows/amd64 \
  windows/arm64

# ---------------------------------------------------------------------------
# Selective compilation via build tags.
#
# By default all agents are included. To build with only
# specific ones, set AGENTS:
#
#   make build AGENTS=claudecode
#
# You can also exclude specific ones:
#
#   make build EXCLUDE=gemini,iflow
# ---------------------------------------------------------------------------

ALL_AGENTS    := acp antigravity claudecode codex copilot cursor devin gemini iflow kimi opencode pi qoder tmux traex
ALL_PLATFORMS := feishu
ALL_EXTRAS    := web

COMMA := ,

# Compute exclusion tags from AGENTS / PLATFORMS_INCLUDE / EXCLUDE variables
_EXCLUDE_TAGS :=

ifdef AGENTS
  _WANTED_AGENTS := $(subst $(COMMA), ,$(AGENTS))
  _EXCLUDE_AGENTS := $(filter-out $(_WANTED_AGENTS),$(ALL_AGENTS))
  _EXCLUDE_TAGS += $(addprefix no_,$(_EXCLUDE_AGENTS))
endif

ifdef PLATFORMS_INCLUDE
  _WANTED_PLATFORMS := $(subst $(COMMA), ,$(PLATFORMS_INCLUDE))
  _EXCLUDE_PLATFORMS := $(filter-out $(_WANTED_PLATFORMS),$(ALL_PLATFORMS))
  _EXCLUDE_TAGS += $(addprefix no_,$(_EXCLUDE_PLATFORMS))
endif

ifdef EXCLUDE
  _EXCLUDE_TAGS += $(addprefix no_,$(subst $(COMMA), ,$(EXCLUDE)))
endif

ifdef NO_WEB
  _EXCLUDE_TAGS += no_web
endif

_BUILD_TAGS := $(strip $(_EXCLUDE_TAGS) goolm)
_TAGS_FLAG  := $(if $(_BUILD_TAGS),-tags '$(_BUILD_TAGS)',)

.PHONY: build run clean test test-unit test-contract test-integration test-blackbox test-pyramid-local test-worktree-local test-fast test-full test-smoke test-e2e test-real-feishu-e2e test-release test-release-local test-performance pre-test lint release release-all web

web:
	@if [ ! -d web/node_modules ]; then cd web && npm install; fi
	cd web && npm run build

build: web
	go build $(_TAGS_FLAG) -ldflags "$(LDFLAGS)" -o $(APP) $(CMD)

build-noweb:
	go build $(_TAGS_FLAG) -tags 'no_web' -ldflags "$(LDFLAGS)" -o $(APP) $(CMD)

build-test-noweb:
	@mkdir -p $(dir $(TEST_BINARY)) $(GO_TEST_CACHE)
	$(GO_TEST_ENV) go build $(_TAGS_FLAG) -tags 'no_web' -ldflags "$(LDFLAGS)" -o $(TEST_BINARY) $(CMD)

run: build
	./$(APP)

clean:
	rm -f $(APP)
	rm -rf $(DIST)
	rm -rf $(WORKTREE_TMP)

# ---------------------------------------------------------------------------
# Testing targets.
#
# test-unit:          Default package tests without opt-in build tags.
# test-contract:      Deterministic Engine/Platform/Agent contract checks.
# test-integration:   Mock-platform integration tests; no real Feishu/Lark.
# test-blackbox:      Real agents + MockPlatform; skips without local creds.
# test-pyramid-local: Unit + contract + mock integration; no real Feishu/Lark.
# test-worktree-local: Worktree-safe no_web local test gate; no real Feishu/Lark.
# test-fast:          Unit tests + mock smoke tests (< 2 min).
# test-full:          Unit + mock smoke + mock regression (< 10 min).
# test-smoke:         Mock smoke tests only (< 1 min).
# test-e2e:           Mock E2E/regression tests only.
# test-real-feishu-e2e: Opt-in real Feishu smoke test using isolated config.
# test-release:       Full + performance benchmarks.
# pre-test:           Prerequisites (build + vet) before running tests.
# ---------------------------------------------------------------------------

pre-test:
	go build ./...
	go vet ./...

# Unit/component tests that run without opt-in build tags.
test-unit:
	go test ./config ./core ./platform/feishu ./agent/... ./cmd/cc-connect ./daemon

# Deterministic release-local contracts. No real IM, provider account, or supervisor.
test-contract: test-release-local

# Mock-platform integration tests. These may skip if local agent config is absent.
test-integration:
	go test -tags=integration ./tests/integration/...

# Real agent adapters through a MockPlatform. No real Feishu/Lark event delivery.
test-blackbox:
	go test -tags=blackbox ./tests/blackbox/...

# Local pyramid excluding real Feishu/Lark E2E.
test-pyramid-local: test-unit test-contract test-integration

# Worktree-safe local gate. It uses no_web tags and a per-worktree Go cache, so
# it does not require web/dist and does not share cache/output state with other
# checkouts.
test-worktree-local:
	@mkdir -p $(GO_TEST_CACHE)
	$(GO_TEST_ENV) go test -tags=no_web ./config ./core ./platform/feishu ./agent/... ./cmd/cc-connect ./daemon
	$(GO_TEST_ENV) go test -tags=no_web ./tests/release_local/...
	$(GO_TEST_ENV) go test -tags=integration,no_web ./tests/integration/...
	$(GO_TEST_ENV) go test -tags=smoke,no_web ./tests/e2e/...
	$(GO_TEST_ENV) go test -tags=regression,no_web ./tests/e2e/...

# Fast test: default tests + mock smoke tests
test-fast: pre-test
	go test -parallel=4 -race ./...
	go test -parallel=4 -tags=smoke ./tests/e2e/...

# Full test: unit + smoke + regression (PR requirement)
test-full: pre-test
	go test -parallel=4 -race ./...
	go test -parallel=4 -tags=smoke ./tests/e2e/...
	go test -parallel=2 -tags=regression ./tests/e2e/...

# Mock smoke tests only
test-smoke: pre-test
	go test -v -tags=smoke ./tests/e2e/...

# Mock E2E/regression tests only
test-e2e: pre-test
	go test -v -tags=regression ./tests/e2e/...

test-real-feishu-e2e: build-test-noweb
	CC_E2E_BINARY="$(abspath $(TEST_BINARY))" tools/e2e/real_feishu_e2e.sh

# Performance benchmarks only
test-performance: pre-test
	go test -bench=. -benchmem -tags=performance ./tests/performance/...

# Release test: full + performance benchmarks
test-release: pre-test
	go test -parallel=4 -race ./...
	go test -parallel=4 -tags=smoke ./tests/e2e/...
	go test -parallel=2 -tags=regression ./tests/e2e/...
	go test -bench=. -benchmem -tags=performance ./tests/performance/...

# Release-local gate: deterministic release checks that do not require real IM
# credentials, real provider accounts, or supervisor-managed services.
test-release-local:
	go test ./tests/release_local/...
	go test ./config
	go test ./core -run 'TestEngineSendToSessionWithAttachments|TestProcessInteractiveEvents_SuppressesDuplicateSideChannelText|TestCmdList_AllSessionsVisibleAfterRepeatedNew|TestCmdList_SessionVisibleDuringAgentProcessing|TestEngine_Alias|TestEngine_BannedWords|TestEngine_DisabledCommands'
	go test ./platform/feishu -run 'TestUserIDFromEventFallsBackToUserID|TestResolveUserNameSkipsInvalidLookupID|TestNew_CanDisableInteractiveCards'

# Legacy: runs unit tests only
test:
	go test -v ./...

lint:
	golangci-lint run ./...

release-all: web clean
	@mkdir -p $(DIST)
	@$(foreach platform,$(PLATFORMS), \
		$(eval GOOS   := $(word 1,$(subst /, ,$(platform)))) \
		$(eval GOARCH := $(word 2,$(subst /, ,$(platform)))) \
		$(eval EXT    := $(if $(filter windows,$(GOOS)),.exe,)) \
		$(eval OUT    := $(DIST)/$(APP)-$(VERSION)-$(GOOS)-$(GOARCH)$(EXT)) \
		echo "Building $(OUT)" && \
		GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 \
			go build $(_TAGS_FLAG) -ldflags "$(LDFLAGS)" -o $(OUT) $(CMD) && \
	) true
	@echo "Packaging archives..."
	@cd $(DIST) && for f in $(APP)-*; do \
		case "$$f" in \
			*.tar.gz|*.zip) continue ;; \
			*.exe) zip "$${f%.exe}.zip" "$$f" ;; \
			*)     tar czf "$$f.tar.gz" "$$f" ;; \
		esac; \
	done
	@cd $(DIST) && sha256sum * > checksums.txt
	@echo "Done. Binaries and archives in $(DIST)/"

release:
	@if [ -z "$(TARGET)" ]; then \
		echo "Usage: make release TARGET=linux/amd64"; \
		echo "Available: $(PLATFORMS)"; \
		exit 1; \
	fi
	@mkdir -p $(DIST)
	$(eval GOOS   := $(word 1,$(subst /, ,$(TARGET))))
	$(eval GOARCH := $(word 2,$(subst /, ,$(TARGET))))
	$(eval EXT    := $(if $(filter windows,$(GOOS)),.exe,))
	$(eval OUT    := $(DIST)/$(APP)-$(VERSION)-$(GOOS)-$(GOARCH)$(EXT))
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 \
		go build $(_TAGS_FLAG) -ldflags "$(LDFLAGS)" -o $(OUT) $(CMD)
	@echo "Built: $(OUT)"
