# Production Deployment

This project is an API backend. A production deployment should run the Go service, PostgreSQL, and Redis as separate managed services or hardened containers.

## Required services

- Go API container or VM process
- PostgreSQL
- Redis
- TLS terminator or reverse proxy
- Log aggregation
- Metrics and alerting

## Configuration

Configure the service with environment variables or a secret manager. Do not commit `pkg/config/config.yaml` with real secrets.

Required values:

```text
environment
http_port
grpc_port
auth_secret
database_uri
redis_uri
redis_password
redis_db
```

Recommended hardening values:

```text
http_read_timeout_seconds
http_write_timeout_seconds
http_idle_timeout_seconds
http_read_header_timeout_seconds
http_max_header_bytes
max_request_body_bytes
order_idempotency_ttl_seconds
order_rate_limit_limit
order_rate_limit_window_seconds
```

## Build

Example container build:

```bash
docker build -t scalable-ecommerce-platform:latest .
```

## Run

Example local container run:

```bash
docker run --rm -p 8888:8888 \
  -e environment=production \
  -e http_port=8888 \
  -e grpc_port=8889 \
  -e auth_secret="${AUTH_SECRET}" \
  -e database_uri="${DATABASE_URI}" \
  -e redis_uri="${REDIS_URI}" \
  scalable-ecommerce-platform:latest
```

## Health checks

The HTTP service exposes:

```text
GET /health
```

Use this endpoint for readiness/liveness checks at the platform layer.

## Database operations

The current app uses GORM auto-migration during startup. For a long-lived production database, replace startup auto-migration with explicit, reviewed migrations before scaling deployments.

## Security checklist

- Keep `auth_secret` in a secret manager.
- Use TLS at the edge.
- Keep Redis private to the application network.
- Keep PostgreSQL private to the application network.
- Rotate credentials regularly.
- Set resource limits for containers.
- Keep Go and base images patched.
- Run `go test ./...` in CI before deploying.

## Rollout checklist

1. Build the image from a reviewed commit.
2. Run CI tests.
3. Apply database migrations.
4. Deploy the API with new configuration.
5. Verify `/health`.
6. Run a smoke test for login and order placement.
7. Monitor order failure logs, rate-limited counts, and latency.
8. Roll back if error rate or latency exceeds the deployment threshold.
