# API Contracts

This directory contains API specifications for the project.

## Conventions

- **Format**: OpenAPI 3.1.0 (YAML)
- **Versioning**: API version in the `info.version` field; breaking changes require version bump + ADR
- **Naming**: verb-noun for operation IDs (e.g., `createUser`, `listOrders`)
- **Response schemas**: Every endpoint must define response schemas for success and error cases

## Files

| File | Purpose | Owner |
|------|---------|-------|
| `api-design.yaml` | Main API specification | Agent-04 (generates), Agent-03 (reviews) |

## Rules

1. All endpoints must include proper HTTP status codes (200, 201, 400, 401, 403, 404, 500).
2. Shared schemas go in `schema/` at project root — reference via `$ref`.
3. Every API must include `/health` and `/metrics` endpoints.
4. Authentication scheme must be defined in `components/securitySchemes`.
5. Generate client SDKs from this spec, never hand-write API clients.
