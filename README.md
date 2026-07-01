# Scalable E-Commerce Platform

Production-minded Go backend for an e-commerce ordering system. The project demonstrates clean module boundaries, transaction-safe order placement, atomic inventory deduction, Redis-backed request protection, JWT authentication, PostgreSQL persistence, and CI-ready integration tests.

## Architecture

The codebase is organized by feature under `internal/`:

- `internal/user`: registration, login, JWT refresh, profile, password changes.
- `internal/product`: product catalog APIs with Redis caching.
- `internal/inventory`: stock read/write APIs and atomic stock mutation.
- `internal/order`: order placement, ownership checks, cancellation, and order queries.
- `internal/server`: HTTP and gRPC composition roots.

The order module follows a clean dependency direction:

```text
HTTP handler -> order service -> repository/inventory/product ports -> GORM/Redis adapters
```

`internal/server/http` wires dependencies. `internal/order/repository/unit_of_work.go` creates transaction-scoped dependencies for order use cases.

## Implemented Features

- Gin REST API and gRPC service skeletons.
- PostgreSQL persistence with GORM.
- Redis product caching.
- JWT authentication with access and refresh tokens.
- Admin-only product and inventory write routes.
- Customer order ownership checks.
- Transaction-safe order placement.
- Atomic inventory deduction to prevent overselling.
- Idempotent `POST /orders` with Redis and `Idempotency-Key`.
- Redis-backed order-placement rate limiting.
- HTTP server hardening with explicit timeouts, max header size, body size limits, trusted proxy lockdown, and graceful shutdown.
- Swagger API documentation.
- Docker Compose for PostgreSQL and Redis.
- Unit and HTTP integration tests, including concurrent limited-stock ordering.

## Reliability Highlights

### Transaction-Safe Ordering

Order placement runs product loading, stock deduction, order creation, and order-line creation inside one Unit of Work transaction. If any step fails, the whole use case rolls back.

### Atomic Inventory Deduction

Stock consumption uses a conditional update:

```sql
UPDATE inventories
SET quantity = quantity - ?
WHERE product_id = ?
AND quantity >= ?;
```

The repository checks affected rows to detect insufficient stock. This avoids unsafe read-check-write behavior under concurrent requests.

### Idempotent Order Creation

Clients can send:

```text
Idempotency-Key: checkout-attempt-123
```

Keys are scoped by authenticated user ID and stored in Redis with a TTL. Duplicate successful requests return the original order instead of creating another one.

### Request Protection

`POST /orders` is protected by a Redis-backed per-user rate limit. Defaults are documented in `pkg/config/config.sample.yaml` and `docs/order-production-readiness.md`.

## Permission Model

- Public users can list/read products and inventory.
- Authenticated customers can place orders, list their own orders, read their own orders, and cancel their own cancellable orders.
- Admin users can create/update products and set/adjust inventory.
- Customers cannot mutate products or inventory.
- Users cannot read or cancel another user's order.

## Local Setup

Requirements:

- Go 1.17+
- Docker Desktop or Docker Engine
- Docker Compose

Start infrastructure:

```bash
docker compose -f docker-compose.yml up -d
```

Create local config:

```bash
cp pkg/config/config.sample.yaml pkg/config/config.yaml
```

Example local config:

```yaml
environment: production
http_port: 8888
grpc_port: 8889
auth_secret: local-dev-secret
database_uri: postgres://postgres:postgres@localhost:5432/goshop_test
redis_uri: localhost:6379
redis_password:
redis_db: 0
```

Run the API:

```bash
go run cmd/api/main.go
```

Health check:

```bash
curl http://localhost:8888/health
```

Swagger UI:

```text
http://localhost:8888/swagger/index.html
```

## Testing

Run the full test suite:

```bash
go test ./... -count=1 -timeout 240s
```

Run the concurrent ordering regression repeatedly:

```bash
go test ./test/http -run TestOrderAPI_ConcurrentOrdersNeverOversell -count=5 -timeout 180s
```

Run vet checks:

```bash
go vet ./...
```

## API Examples

Register:

```bash
curl -X POST http://localhost:8888/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"customer@example.com","password":"test123456"}'
```

Login:

```bash
curl -X POST http://localhost:8888/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"customer@example.com","password":"test123456"}'
```

Place an idempotent order:

```bash
curl -X POST http://localhost:8888/api/v1/orders \
  -H "Authorization: Bearer <access_token>" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: checkout-123" \
  -d '{"lines":[{"product_id":"<product_id>","quantity":1}]}'
```

Set inventory as an admin:

```bash
curl -X PUT http://localhost:8888/api/v1/inventory/<product_id> \
  -H "Authorization: Bearer <admin_access_token>" \
  -H "Content-Type: application/json" \
  -d '{"quantity":25}'
```

## Documentation

- `docs/order-transaction-safety.md`: transaction boundary and overselling prevention.
- `docs/order-production-readiness.md`: idempotency, rate limiting, HTTP hardening, and observability.
- `docs/load-testing.md`: load and concurrency testing guidance.
- `docs/production-deployment.md`: production deployment checklist and operational notes.
- `docs/order-outbox-pattern.md`: future reliable order-event publishing design.

## Production-Readiness Notes

- Keep `pkg/config/config.yaml` local and out of git.
- Use environment variables or a secret manager for production secrets.
- Run PostgreSQL and Redis as managed services or hardened containers.
- Put the API behind TLS at the edge.
- Tune order rate limits to match real checkout traffic.
- Add persistent migrations before using this project for a long-lived production database.
- Consider the documented Outbox Pattern before publishing order events to external systems.
