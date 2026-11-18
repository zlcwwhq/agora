# make verify is the single acceptance gate (see AGENTS.md).
#
# Deferred until CI exists, done by hand before merging into main:
#   - rerunning changed tests on the merge base (they must fail there),
#   - rejecting diffs outside the paths declared in tasks/<id>.md,
#   - the merge queue: rebase onto main, rerun make verify, then merge.
# .github/workflows/ci.yml also runs the tests on macOS and Windows; from WSL,
# make test-windows runs them on the Windows host.

GO ?= go
STATICCHECK ?= staticcheck
GOVULNCHECK ?= govulncheck
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: verify fmt vet vet-cross lint deps test cross vuln build release test-windows

verify: fmt vet vet-cross lint deps test cross vuln

fmt:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then \
		echo "gofmt: these files need formatting (run gofmt -w):"; echo "$$out"; exit 1; fi

vet:
	$(GO) vet ./...

vet-cross:
	GOOS=windows $(GO) vet ./...
	GOOS=darwin $(GO) vet ./...

lint:
	$(STATICCHECK) ./...

deps:
	@sh scripts/depcheck.sh

test:
	$(GO) test -race -shuffle=on -count=1 ./...

cross:
	@for p in $(PLATFORMS); do \
		GOOS=$${p%/*} GOARCH=$${p#*/} CGO_ENABLED=0 $(GO) build -o /dev/null ./cmd/agora || exit 1; \
	done

vuln:
	$(GOVULNCHECK) ./...

build:
	CGO_ENABLED=0 $(GO) build -o bin/agora ./cmd/agora

release:
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ "$$os" = windows ] && ext=.exe; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build -trimpath -o bin/$$os-$$arch/agora$$ext ./cmd/agora || exit 1; \
		echo bin/$$os-$$arch/agora$$ext; \
	done

test-windows:
	GOOS=windows GOARCH=amd64 $(GO) test -shuffle=on -count=1 ./...
