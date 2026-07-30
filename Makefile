# wails installs to ~/go/bin, which is not on PATH in a bare shell (the
# pre-commit hook runs there).
export PATH := $(PATH):$(HOME)/go/bin

.PHONY: check vet lint test test-frontend scripts build

# One command that proves everything. The pre-commit hook runs it.
check: vet lint test test-frontend scripts

vet:
	go vet ./...

lint:
	golangci-lint run

test:
	go test ./...

# The frontend tests hold the "never render 0%" line at the view layer, so they
# belong in the same gate as the Go tests rather than in a separate ritual.
test-frontend:
	@test -d frontend/node_modules || (cd frontend && npm install)
	cd frontend && npm test

scripts:
	scripts/check-no-wails-in-internal.sh
	scripts/check-file-size.sh
	scripts/check-no-token-in-cache.sh
	scripts/check-no-pii-in-reports.sh

build:
	go build -o build/bin/acs ./cmd/acs
	wails build
