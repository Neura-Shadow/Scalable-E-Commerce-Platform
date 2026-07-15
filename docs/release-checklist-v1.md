# v1.0.0 Release Checklist

## Source and review

- [ ] Release branch is synchronized with `origin/main`.
- [ ] Pull request has independent architecture, security, migration, cache, concurrency, operations, and test-quality review.
- [ ] Release notes and `CHANGELOG.md` are reviewed.
- [ ] No local config, `.env`, credentials, JWTs, logs, benchmark output, volumes, or Codex metadata are staged.
- [ ] No Windows absolute paths appear in source or documentation.
- [ ] Ingress throttling for HTTP and gRPC login/registration is configured and tested.
- [ ] The 30-day non-rotating refresh-token policy and five-minute access-token revocation window are accepted.

## Required gates

- [ ] GitHub Actions is green.
- [ ] `go vet ./...` passes.
- [ ] `go test ./... -count=1 -timeout 240s` passes.
- [ ] `go test -race ./...` passes.
- [ ] Overselling regression passes ten consecutive runs.
- [ ] `staticcheck ./...` passes for application and generated protobuf packages.
- [ ] Govulncheck output is reviewed and release-blocking findings are resolved.
- [ ] GORM production logging is parameterized and does not emit query values.
- [ ] Actionlint passes.
- [ ] Gitleaks passes with redacted output.
- [ ] Trivy filesystem and image scans are reviewed.
- [ ] Docker build and Compose validation pass.

Current dependency review note: Govulncheck reports no reachable or imported-package
vulnerabilities. `GO-2026-5932` remains a module-graph-only notice for the unmaintained
`golang.org/x/crypto/openpgp` package; application packages neither import nor call it.
Keep this notice visible until the transitive module no longer references that package.

## Database and runtime

- [ ] Migration up succeeds on empty PostgreSQL.
- [ ] A second migration up safely reports no change.
- [ ] Expected tables, indexes, and status constraints exist.
- [ ] The v4 ambiguous-NULL dirty-state repair procedure is tested and understood.
- [ ] Backup and migration version are recorded before rollout.
- [ ] Production has `database_auto_migrate=false`.
- [ ] `/livez` and `/readyz` pass on the release image.
- [ ] PostgreSQL pool size fits the replica connection budget.

## Behavior and load

- [ ] Customer registration/login and admin authorization smoke pass.
- [ ] Idempotent checkout retry returns one order.
- [ ] Limited-stock smoke finishes with successful orders exactly equal to seeded stock and final stock zero.
- [ ] Rate-limit behavior is observed without permanent counter keys.
- [ ] Outbox publish/finalize and Redis consumer/DLQ signals are reviewed.
- [ ] Measured results and missing measurements are recorded without unsupported capacity claims.

## Rollout and rollback

- [ ] Previous accepted image digest is available.
- [ ] Rollback owner and observation window are assigned.
- [ ] Rollback keeps the forward-compatible schema unless a reviewed down migration is required.
- [ ] Release tag is created only after PR CI is green and the merge commit is verified.

Rollback command for a reviewed single migration only:

```bash
migrate -path migrations -database "$DATABASE_URI" down 1
```

Annotated release tag after merge:

```bash
git tag -a v1.0.0 -m "Production-minded e-commerce backend v1.0.0"
git push origin v1.0.0
```
