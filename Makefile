CONTAINER_RUNTIME ?= podman-compose
.PHONY: build-agent build-client run-agent run-docker run-client clean

build-agent:
	cd evdi-agent && CGO_ENABLED=1 go build -o bin/agent ./cmd/agent

build-client:
	cd evdi-web-client && npm run build

run-agent:
	cd evdi-agent && go run ./cmd/agent

run-docker:
	$(CONTAINER_RUNTIME) up --build

run-client:
	cd evdi-web-client && npm run dev

clean:
	$(CONTAINER_RUNTIME) down -v
	rm -rf evdi-agent/bin/
	rm -rf evdi-web-client/dist/
