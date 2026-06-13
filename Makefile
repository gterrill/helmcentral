.PHONY: dev down logs

dev:
	docker compose -f docker-compose.dev.yml --profile dev up -d backend-dev frontend-dev

down:
	docker compose -f docker-compose.dev.yml --profile dev down

logs:
	docker compose -f docker-compose.dev.yml --profile dev logs -f backend-dev frontend-dev
