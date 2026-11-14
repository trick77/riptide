# The Go module lives in backend/, the UI in ui/; scripts/ and docs/ are at the root.
VERSION ?= dev

.PHONY: build fe-build fe-test

# The find, rather than emptyOutDir: vite runs with emptyOutDir false so the
# tracked dist/.gitkeep survives (//go:embed all:dist needs a non-empty
# directory in a fresh clone), so stale hashed assets of an earlier build
# would otherwise pile up in the binary.
fe-build:
	find backend/web/dist -mindepth 1 ! -name '.gitkeep' -delete
	cd ui && npm ci && npm run build

# npm test does not typecheck; npm run build (tsc -b && vite build) does.
fe-test:
	cd ui && npx tsc -b && npm run test

build: fe-build
	cd backend && CGO_ENABLED=0 go build -trimpath \
		-ldflags="-s -w -X github.com/trick77/ship/kit/version.Version=$(VERSION)" \
		-o ../bin/riptide ./cmd/riptide

# ship:begin (owned by ship init)
# The gate: everything CI checks, configured in ship.yaml. Master's ship, as the pre-push hook and the @master
# workflows run it: its sha asked of GitHub (a proxy lags on @master). A local ship: SHIP_CMD=<go-installed binary>,
# one name for here and the hook, so make check and the push gate never run different ships.
# Exactly refs/heads/master (ls-remote matches by suffix), a 40-hex sha or nothing runs, as in the workflows.
SHIP_CMD ?= sha=$$(git ls-remote https://github.com/trick77/ship refs/heads/master | \
		awk '$$2 == "refs/heads/master" { print $$1 }'); \
	echo "$$sha" | grep -Eqx '[0-9a-f]{40}' || \
		{ echo "cannot resolve ship's master on GitHub: '$$sha' (git ls-remote, error above)"; exit 1; }; \
	go run github.com/trick77/ship@$$sha

.PHONY: check hooks lint

check:
	$(SHIP_CMD) check

hooks:
	$(SHIP_CMD) hooks

# Quick lint while editing, at the version check runs.
lint:
	$(SHIP_CMD) lint
# ship:end
