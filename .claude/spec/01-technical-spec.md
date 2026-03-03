# Technical Specification

## Architecture
- **Pattern**: [Microservices/Monolith/Modular]
- **API Style**: [REST/GraphQL/gRPC]
- **Deployment**: [Local VM/Docker/K8s]

## Architecture Decision Records
See `docs/adr/` for all architectural decisions. Key ADRs:
- ADR-001: [Title]
- ADR-002: [Title]

## Services
| Service | Purpose | Tech | Port |
|---------|---------|------|------|
| [Name] | [What it does] | [Lang] | [8xxx] |

## Database Schema
```
[Table/Collection]:
  - field1: type
  - field2: type
```

## API Endpoints
```
POST /api/[resource]
GET  /api/[resource]/{id}
```

## Security
- Auth: [Per project brief] -> JWT
- Rate limit: [X req/min]
- CORS: Via middleware only

## Observability
- **Logging**: Structured JSON per `.claude/rules/observability.md`
- **Metrics**: latency, error rate, throughput on all services
- **Tracing**: OpenTelemetry with trace_id propagation
- **Health**: `/health`, `/health/ready`, `/health/live` on all services

## Performance
- Response: <200ms
- Concurrent users: [X]
- Cache: Redis

## Testing
- Unit: >80% coverage
- E2E: Playwright MCP
- Load: K6
