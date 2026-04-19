# =============================================================================
# Makefile — SOLDIER OF GOD Command Centre
# =============================================================================

.PHONY: all build run test lint fmt clean \
        frontend-install frontend-build frontend-dev frontend-lint \
        docker-build docker-up docker-down docker-logs \
        hooks-install help

# Default target
all: lint test build

# ---------------------------------------------------------------------------
# Backend (Go)
# ---------------------------------------------------------------------------

## Build the Go backend binary
build:
	cd backend && CGO_ENABLED=0 go build -ldflags="-s -w" -trimpath -o ../bin/server ./cmd/server

## Run the Go backend locally
run: build
	./bin/server

## Run Go tests with race detection and coverage
test:
	cd backend && go test -race -cover -count=1 ./...

## Run Go linter
lint:
	cd backend && golangci-lint run --timeout 5m ./...

## Format Go source files
fmt:
	cd backend && gofmt -w -s .
	cd backend && goimports -w .

## Run go vet
vet:
	cd backend && go vet ./...

## Generate test coverage report
coverage:
	cd backend && go test -race -coverprofile=coverage.out ./...
	cd backend && go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: backend/coverage.html"

# ---------------------------------------------------------------------------
# Frontend (React + Vite)
# ---------------------------------------------------------------------------

## Install frontend dependencies
frontend-install:
	cd frontend-react && npm ci

## Build frontend for production (output to frontend/)
frontend-build: frontend-install
	cd frontend-react && npm run build

## Start frontend dev server
frontend-dev:
	cd frontend-react && npm run dev

## Lint frontend code
frontend-lint:
	cd frontend-react && npm run lint

## Typecheck frontend
frontend-typecheck:
	cd frontend-react && npx tsc --noEmit

# ---------------------------------------------------------------------------
# Docker
# ---------------------------------------------------------------------------

## Build all Docker images
docker-build:
	docker compose build

## Start all services
docker-up:
	docker compose up -d

## Stop all services
docker-down:
	docker compose down

## View logs from all services
docker-logs:
	docker compose logs -f

## Rebuild and restart all services
docker-restart: docker-down docker-build docker-up

# ---------------------------------------------------------------------------
# Kubernetes / Helm
# ---------------------------------------------------------------------------

## Lint Helm chart
helm-lint:
	helm lint kubernetes/command-centre

## Template Helm chart (dry-run)
helm-template:
	helm template command-centre kubernetes/command-centre

## Install/upgrade Helm release (staging)
helm-deploy-staging:
	helm upgrade --install command-centre kubernetes/command-centre \
		--namespace command-centre-staging \
		--create-namespace \
		--values kubernetes/command-centre/values.yaml \
		--set environment=staging

# ---------------------------------------------------------------------------
# Git Hooks
# ---------------------------------------------------------------------------

## Install git hooks
hooks-install:
	bash scripts/install-hooks.sh

# ---------------------------------------------------------------------------
# Cleanup
# ---------------------------------------------------------------------------

## Remove build artifacts
clean:
	rm -rf bin/
	rm -rf frontend/
	rm -rf frontend-react/dist/
	rm -f backend/coverage.out backend/coverage.html
	@echo "Cleaned build artifacts"

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------

## Show this help message
help:
	@echo "SOLDIER OF GOD Command Centre — Available targets:"
	@echo ""
	@echo "  Backend:"
	@echo "    build              Build Go backend binary"
	@echo "    run                Build and run backend locally"
	@echo "    test               Run Go tests with race detection"
	@echo "    lint               Run golangci-lint"
	@echo "    fmt                Format Go source files"
	@echo "    vet                Run go vet"
	@echo "    coverage           Generate HTML coverage report"
	@echo ""
	@echo "  Frontend:"
	@echo "    frontend-install   Install npm dependencies"
	@echo "    frontend-build     Build frontend for production"
	@echo "    frontend-dev       Start Vite dev server"
	@echo "    frontend-lint      Lint frontend code"
	@echo "    frontend-typecheck Run TypeScript type checking"
	@echo ""
	@echo "  Docker:"
	@echo "    docker-build       Build all Docker images"
	@echo "    docker-up          Start all services"
	@echo "    docker-down        Stop all services"
	@echo "    docker-logs        Tail logs from all services"
	@echo "    docker-restart     Rebuild and restart services"
	@echo ""
	@echo "  Kubernetes:"
	@echo "    helm-lint          Lint Helm chart"
	@echo "    helm-template      Render Helm templates (dry-run)"
	@echo "    helm-deploy-staging Deploy to staging namespace"
	@echo ""
	@echo "  Utilities:"
	@echo "    hooks-install      Install git pre-commit/commit-msg hooks"
	@echo "    clean              Remove build artifacts"
	@echo "    help               Show this help message"
