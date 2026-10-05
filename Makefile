# Launch control for the docker compose stack (docker-compose.yml + optional .env).

COMPOSE := docker compose -f docker-compose.yml

.PHONY: help
help:
	@echo "Targets:"
	@echo "  launch   start the stack in the background"
	@echo "  stop     stop and remove the stack"
	@echo "  restart  stop, then launch"
	@echo "  build    build the local image (compose build: section)"
	@echo "  pull     pull the images referenced by the stack"
	@echo "  logs     follow stack logs"
	@echo "  status   show container status"

.PHONY: launch
launch:
	$(COMPOSE) up -d

.PHONY: stop
stop:
	$(COMPOSE) down

.PHONY: restart
restart: stop launch
	@echo "done"

.PHONY: build
build:
	$(COMPOSE) build

.PHONY: pull
pull:
	$(COMPOSE) pull

.PHONY: logs
logs:
	$(COMPOSE) logs -f

.PHONY: status
status:
	$(COMPOSE) ps
