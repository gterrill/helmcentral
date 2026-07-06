.PHONY: dev down logs

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
