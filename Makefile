.PHONY: dev down logs build-status e2e-up e2e-down e2e-reset e2e-logs help-stage worktree

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

# A second checkout for a second concurrent session, so two sessions editing
# at once cannot race in one working tree. `make worktree NAME=ux-tweaks`
# creates ../helmcentral-ux-tweaks on a new branch of the same name, matching
# the ../helmcentral-security layout already in use.
#
# frontend/node_modules is 453MB and is not tracked, so a fresh worktree would
# need its own `npm ci` before vitest or tsc would run at all. It gets a
# symlink to this checkout's copy instead, which is correct as long as the
# branch does not change frontend/package-lock.json, and makes the worktree
# usable a second after it is created rather than a few minutes. frontend's
# .gitignore lists node_modules without a trailing slash so this link is
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
	git worktree add -b "$(NAME)" "../$(notdir $(CURDIR))-$(NAME)"
	ln -s "$(CURDIR)/frontend/node_modules" "../$(notdir $(CURDIR))-$(NAME)/frontend/node_modules"
	@echo "worktree: ../$(notdir $(CURDIR))-$(NAME)  (branch $(NAME), node_modules symlinked)"

dev:
	# --force-recreate: frontend-dev only runs `npm install` once at container
	# startup, so `up -d` on an already-running container (whose config hasn't
	# changed) skips it and leaves the frontend_node_modules volume stale
	# whenever package.json gains a dependency. Recreating on every `make dev`
	# guarantees it reruns; npm install is a fast no-op when nothing changed.
	docker compose -f docker-compose.dev.yml --profile dev up -d --force-recreate backend-dev frontend-dev

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
e2e-up:
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
