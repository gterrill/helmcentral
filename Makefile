.PHONY: dev down logs e2e-up e2e-down e2e-reset e2e-logs

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
