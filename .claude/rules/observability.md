# Observability Requirements

Every service in the project must implement these observability standards.

## Structured Logging

All logs must be JSON-formatted with these required fields:

```json
{
  "service": "user-service",
  "level": "info",
  "timestamp": "2026-03-03T12:00:00.000Z",
  "trace_id": "abc123def456",
  "message": "User created successfully",
  "context": {
    "user_id": "u-789",
    "action": "create"
  }
}
```

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `service` | string | Service name (kebab-case) |
| `level` | string | `debug`, `info`, `warn`, `error`, `fatal` |
| `timestamp` | string | ISO 8601 with milliseconds |
| `trace_id` | string | Distributed trace identifier |
| `message` | string | Human-readable log message |

### Optional Fields

| Field | Type | Description |
|-------|------|-------------|
| `context` | object | Structured metadata |
| `error` | object | Error details (stack, code) |
| `duration_ms` | number | Operation duration |
| `span_id` | string | Span within trace |

## Required Metrics

Every service must expose:

| Metric | Type | Description |
|--------|------|-------------|
| `request_latency_ms` | histogram | Request duration in ms |
| `error_rate` | counter | Errors per endpoint |
| `throughput_rps` | gauge | Requests per second |
| `active_connections` | gauge | Current open connections |

## Tracing

- Use **OpenTelemetry** for distributed tracing.
- Propagate `trace_id` across all service boundaries (HTTP headers, message queues).
- Sample at minimum 10% in production, 100% in development.

## Health Check Endpoint

Every service must expose:

```
GET /health
Response: { "status": "healthy", "version": "1.0.0", "uptime_seconds": 12345 }
```

Optional extended health:
```
GET /health/ready    — readiness probe (dependencies available)
GET /health/live     — liveness probe (process alive)
```

## Alerting Thresholds (Defaults)

| Condition | Severity | Action |
|-----------|----------|--------|
| Error rate > 5% for 5 min | Warning | Notify team |
| Error rate > 15% for 2 min | Critical | Page on-call |
| P99 latency > 2s for 5 min | Warning | Investigate |
| Health check fails 3x | Critical | Auto-restart |
