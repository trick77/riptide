# The Go module lives in backend/; hack/ and docs/ are at the root.
.PHONY: build test lint backend-coverage coverage

build:
	cd backend && CGO_ENABLED=0 go build \
		-ldflags="-s -w" \
		-o ../bin/riptide ./cmd/riptide

test:
	cd backend && go test -race ./...

lint:
	gofmt -l . | (! grep .)
	cd backend && go vet ./... && golangci-lint run ./...

# Database tests skip without RIPTIDE_TEST_DSN; see AGENTS.md.
backend-coverage:
	mkdir -p coverage
	cd backend && go test -race -covermode=atomic -coverpkg=./... \
		-coverprofile=../coverage/backend.out -count=1 ./...
	cd backend && go run github.com/boumenot/gocover-cobertura@v1.5.0 \
		< ../coverage/backend.out > ../coverage/backend.xml
	./hack/coverage-gate.sh backend

coverage: backend-coverage
