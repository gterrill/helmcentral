.PHONY: dev down logs build-status e2e-up e2e-down e2e-reset e2e-logs help-stage docs-lint worktree prune-merged check-vite-config check-frontend-deps

# Stages the in-app help into backend/help for the assistant's
# read_help tool and the in-app Help sheet's /api/help endpoint
# (backend/assistant_help.go's //go:embed all:help), the same copy the
# Dockerfile and .goreleaser.yaml do for a container or release build.
# docs/index.md is staged alongside the three directories as page "index" -
# it is the hand-written contents page both read_help and the Help sheet
# land on. Needed before `go build`/`go run` picks up help pages at all -
# without it the binary still runs, just with read_help and /api/help
# reporting help isn't staged.
help-stage:
	rm -rf backend/help
	mkdir -p backend/help
	cp -R docs/features docs/how-to docs/reference backend/help/
	cp docs/index.md backend/help/index.md
	touch backend/help/.gitkeep

# Prose lint for the operator help pages, with the rules in .vale/styles/Helm.
# Errors (dashes, semicolons, "widget", ADR citations, filler, named code
# technology) fail. Long sentences and other implementation words warn, and
# reading grade and plainer-word swaps are suggestions. CI runs the same
# command. Install with `brew install vale`.
docs-lint:
	@command -v vale >/dev/null || { echo "docs-lint: vale not installed (brew install vale)"; exit 1; }
	vale --no-global docs

# A second checkout for a second concurrent session, so two sessions editing
# at once cannot race in one working tree. `make worktree NAME=ux-tweaks`
# creates ../helmcentral-ux-tweaks on a new branch of the same name, matching
# the ../helmcentral-security layout already in use. The branch starts from a
# freshly fetched origin/main: local main lags, and a branch cut from it builds
# on superseded code.
#
# frontend/node_modules is 453MB and is not tracked, so a fresh worktree would
# need its own `npm ci` before vitest or tsc would run at all. It gets a
# symlink to this checkout's copy instead, which is correct as long as the
# branch does not change frontend/package-lock.json, and makes the worktree
# usable a second after it is created rather than a few minutes. This
# checkout's copy lags whenever origin/main has changed a dependency since its
# last install, so the target checks the link against the worktree's lockfile
# (`make check-frontend-deps`) and, on a mismatch, swaps it for the worktree's
# own `npm ci`. If that install fails (no network), the worktree is left with
# no node_modules and the target fails. Ship's test step runs the check again,
# for a branch that has merged origin/main since. frontend's .gitignore lists
# node_modules without a trailing slash so this link is
# ignored; a slash there matches directories only and leaves it untracked.
#
# That symlink points at shared state: `npm install` run inside the worktree
# writes into THIS checkout's node_modules, for every session using it. If the
# branch does change dependencies, replace the link with a real install
# (rm frontend/node_modules && npm ci) in the worktree before touching them.
#
# The dev stack is not duplicated. docker-compose.dev.yml binds fixed host
# ports (8080, 5173, 8090, 5174), so only one checkout can run it; a worktree
# session uses the stack the main checkout is already running. Run
# `make help-stage` in the worktree if it needs a local `go build` to embed
# the help pages.
worktree:
	@if [ -z "$(NAME)" ]; then echo "usage: make worktree NAME=<short-name>"; exit 1; fi
	git fetch origin
	git worktree add --no-track -b "$(NAME)" "../$(notdir $(CURDIR))-$(NAME)" origin/main
	ln -s "$(CURDIR)/frontend/node_modules" "../$(notdir $(CURDIR))-$(NAME)/frontend/node_modules"
	@cd "../$(notdir $(CURDIR))-$(NAME)/frontend" && if node "$(CURDIR)/scripts/check-frontend-deps.mjs" .; then \
		echo "worktree: ../$(notdir $(CURDIR))-$(NAME)  (branch $(NAME), node_modules symlinked)"; \
	else \
		echo "worktree: running npm ci in the worktree instead of using the link"; \
		rm node_modules; \
		npm ci || { rm -rf node_modules; \
			echo "worktree: npm ci failed, so the worktree has no node_modules. Run npm ci in its frontend once it can reach the registry."; exit 1; }; \
	fi

# Exits 1 and lists each package whose installed version differs from
# DIR's package-lock.json. DIR defaults to this checkout's frontend.
check-frontend-deps:
	@node scripts/check-frontend-deps.mjs "$(or $(DIR),frontend)"

# Clears up after merged work: removes each worktree, local branch and remote
# branch whose GitHub PR is merged. Lists what it would do by default;
# `make prune-merged CONFIRM=1` deletes. Worktrees with uncommitted or
# untracked changes, or with a process still running in them, are skipped.
# Rules in scripts/prune-merged.sh.
prune-merged:
	@CONFIRM="$(CONFIRM)" sh scripts/prune-merged.sh

# Vite loads frontend/vite.config.js ahead of vite.config.ts. An old tsc -b
# left one there (gitignored, so git status never shows it), and the dev
# server ran on that stale config until a page died on a missing define.
# Stop rather than delete it, so whatever wrote it gets noticed.
check-vite-config:
	@if [ -e frontend/vite.config.js ]; then \
		echo "frontend/vite.config.js shadows frontend/vite.config.ts; Vite would load it instead."; \
		echo "It is a stale compiler emit: rm -f frontend/vite.config.js frontend/vite.config.d.ts"; \
		exit 1; \
	fi

dev: check-vite-config
	# --force-recreate: frontend-dev only runs `npm install` once at container
	# startup, so `up -d` on an already-running container (whose config hasn't
	# changed) skips it and leaves the frontend_node_modules volume stale
	# whenever package.json gains a dependency. Recreating on every `make dev`
	# guarantees it reruns; npm install is a fast no-op when nothing changed.
	docker compose -f docker-compose.dev.yml --profile dev up -d --force-recreate backend-dev frontend-dev
	@echo ""
	@echo "Dashboard: http://localhost:5173  (ready once npm install finishes; make logs to watch)"
	@lan=$$(ipconfig getifaddr en0 2>/dev/null || hostname -I 2>/dev/null | awk '{print $$1}'); \
	if [ -n "$$lan" ]; then echo "On the LAN: http://$$lan:5173"; fi

down:
	docker compose -f docker-compose.dev.yml --profile dev down

logs:
	docker compose -f docker-compose.dev.yml --profile dev logs -f backend-dev frontend-dev

# Did my last edit actually build? air keeps the previous binary running when a
# build fails, so the dashboard goes on answering with stale code and nothing
# in the UI says so. backend/scripts/dev-build.sh truncates this file on every
# attempt, so it is either empty (the running backend is current) or it holds
# the compiler errors from the most recent failure.
build-status:
	@if [ ! -f backend/tmp/build-errors.log ]; then \
		echo "no build recorded yet — is the dev stack running? (make dev)"; \
	elif [ -s backend/tmp/build-errors.log ]; then \
		cat backend/tmp/build-errors.log; \
		exit 1; \
	else \
		echo "last build OK — the running backend is up to date"; \
	fi

# Isolated stack for browser-driven verification. Serves the same UI on :5174
# but against a throwaway settings file and state volume, so scripts that
# click Save can't touch ./settings.yaml or ./backend/data. Safe to tear down
# and recreate at will — unlike the dev stack, nothing here is shared.
e2e-up: check-vite-config
	docker compose -f docker-compose.dev.yml --profile e2e up -d --force-recreate backend-e2e frontend-e2e
	@echo "E2E dashboard: http://localhost:5174  (API: http://localhost:8090)"

# `rm -sf`, never `down`: compose's `down` is project-wide and ignores
# --profile for teardown, so it would take the shared long-running dev stack
# with it — and `down -v` would additionally wipe frontend_node_modules.
e2e-down:
	docker compose -f docker-compose.dev.yml --profile e2e rm -sf backend-e2e frontend-e2e

# Discards every mutation an E2E run made and re-seeds from
# e2e/settings.seed.yaml. Use between runs that need a known starting state.
# Settings alone are re-seeded by any restart; this also drops the accumulated
# data/ and cache/ state. Volume name follows compose's default
# <project>_<volume> convention, where <project> is this directory's name.
e2e-reset:
	docker compose -f docker-compose.dev.yml --profile e2e rm -sf backend-e2e frontend-e2e
	-docker volume rm -f "$$(basename "$$(pwd)")_e2e_state"
	$(MAKE) e2e-up

e2e-logs:
	docker compose -f docker-compose.dev.yml --profile e2e logs -f backend-e2e frontend-e2e
