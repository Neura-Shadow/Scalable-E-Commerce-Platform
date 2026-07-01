# Order Transaction Safety

## Order placement flow

Order placement is orchestrated in `internal/order/service`.

The HTTP handler only parses and validates request ownership data. The service owns the use case and runs these steps inside one Unit of Work transaction:

1. Validate the place-order request.
2. Read the authenticated user ID from the request DTO.
3. Load each product and compute line prices.
4. Deduct inventory for every line.
5. Create the order.
6. Create the order lines.
7. Commit the transaction.

If any step fails, the transaction is rolled back and no manual stock compensation is required.

## Inventory deduction

Inventory consumption must not use read-check-write logic under concurrent traffic.

The repository uses a single conditional update:

```sql
UPDATE inventories
SET quantity = quantity - ?
WHERE product_id = ?
AND quantity >= ?;
```

The service treats zero affected rows as a failed consume. It then checks whether the inventory row exists so callers can still distinguish missing inventory from insufficient stock.

This prevents concurrent requests from reading the same stock value and overwriting each other with stale quantities.

## Transaction boundary

`internal/order/repository.UnitOfWork` is the transaction adapter for GORM.

It creates transaction-scoped order, product, and inventory dependencies and passes them to the order service through the `service.UnitOfWork` interface. This keeps the service independent of GORM while still allowing the whole order use case to run under one database transaction.

`OrderRepo.CreateOrder` no longer opens its own transaction. It only persists the order and order lines using whichever database handle it was constructed with.

## Permission rules

Product and inventory write routes require:

- A valid JWT access token.
- `RequireRole("admin")`.

Customers can:

- List and read public products.
- Place orders.
- Read only their own orders.
- Cancel only their own cancellable orders.

Customers cannot:

- Create or update products.
- Set or adjust inventory.
- Read or cancel another user's order.

## Verification

Run the full test suite:

```bash
go test ./... -count=1 -timeout 240s
```

The HTTP integration suite includes a limited-stock concurrent ordering test. It fires multiple requests against stock quantity `3` and asserts that exactly three orders succeed, final stock is `0`, and stock is never negative.
