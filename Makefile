# make verify is the single acceptance gate (see AGENTS.md).
#
# Deferred until CI exists, done by hand before merging into main:
#   - rerunning changed tests on the merge base (they must fail there),
#   - rejecting diffs outside the paths declared in tasks/<id>.md,
#   - the merge queue: rebase onto main, rerun make verify, then merge.

GO ?= go
STATICCHECK ?= staticcheck
GOVULNCHECK ?= govulncheck

.PHONY: verify fmt vet lint deps test vuln build

verify: fmt vet lint deps test vuln

fmt:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then \
		echo "gofmt: these files need formatting (run gofmt -w):"; echo "$$out"; exit 1; fi

vet:
	$(GO) vet ./...

lint:
	$(STATICCHECK) ./...

deps:
	@sh scripts/depcheck.sh

test:
	$(GO) test -race -shuffle=on -count=1 ./...

vuln:
	$(GOVULNCHECK) ./...

build:
	CGO_ENABLED=0 $(GO) build -o bin/agora ./cmd/agora
