.PHONY: build dev lint test clean fr-dev fr-build fr-lint eval-single eval-batch help

BIN := bin/vmm-rada

help:
	@echo "Backend:"
	@echo "  make build      build binary to bin/vmm-rada"
	@echo "  make dev        go run ./cmd/server"
	@echo "  make lint       go vet + staticcheck"
	@echo "  make test       go test -race ./..."
	@echo "  make clean      remove bin/vmm-rada"
	@echo ""
	@echo "Frontend:"
	@echo "  make fr-dev     vite dev server (localhost:5173)"
	@echo "  make fr-build   production build"
	@echo "  make fr-lint    eslint"
	@echo ""
	@echo "Eval:"
	@echo "  make eval-single Q=\"...\"                  run a single ad-hoc question"
	@echo "  make eval-batch BENCHMARK=<file.yaml>     run a full YAML benchmark"

build:
	go build -o $(BIN) ./cmd/server

dev:
	go run ./cmd/server

lint:
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@v0.6.0 ./...

test:
	go test -race -count=1 ./...

clean:
	rm -f $(BIN)

fr-dev:
	cd frontend && npm run dev

fr-build:
	cd frontend && npm run build

fr-lint:
	cd frontend && npm run lint

eval-single:
	go run ./cmd/eval -question "$(Q)" -baseline-model "$(BASELINE)" -council-type "$(or $(COUNCIL),default)"

eval-batch:
	go run ./cmd/eval -benchmark "$(BENCHMARK)" -baseline-model "$(BASELINE)" -council-type "$(or $(COUNCIL),default)"
