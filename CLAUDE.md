# CLAUDE.md

## Before finishing

- `make check` runs everything CI runs: formatting, generated mocks, `go mod tidy`, golangci-lint, eslint, tsc and the tests.
- `make hooks` once per clone: pre-commit checks formatting and lint, pre-push runs `make check`.
- `make fmt` fixes formatting. Edits made through Claude Code are formatted by `.claude/hooks/format.sh`.
- `make lint-tool` builds the pinned golangci-lint into `bin/` when the one on PATH is too old for `go.mod`.

## Layering

- `internal/domain` owns the model and every use case (`Service`). It talks to the outside only through the ports in `ports.go`: `Platform` (Massdriver), `Store`, `Installer`, `SCM`.
- `internal/massdriver` implements `Platform` over GraphQL. The `Map` scalar travels as a JSON-encoded string on input.
- `internal/httpapi` decodes requests, calls one `Service` method and maps domain errors: `ValidationError` 422, `ErrNotFound` 404, `ErrConflict` 409, `ErrForbidden` 403, anything else 502.
- `cmd/rocketsled` wires config into `domain.Options`. Config types live in `internal/config`; the domain has its own copies, converted in `paasFrom`.
- Tenant mode (`massdriver.account_mode: tenant`) lives in `internal/domain/paas.go` and `web/src/tenant`. Install mode is the rest.

## Changing a port or the store

- After changing an interface in `ports.go`, run `make generate`; the moq mocks are checked in.
- Every `Store` method exists in both `internal/store/sqlite` and `internal/store/memory`. SQLite schema changes are a new numbered file in `internal/store/sqlite/migrations`; never edit an applied one.

## Tests

testify (`assert`, `require`), table-driven where it fits, `mocks.PlatformMock` and the memory store for the domain, `httptest` for handlers. Background work runs inline in tests through `Options.RunAsync`.

## Docs

The README's REST API table and the tenant-mode list describe every route and `paas:` key. Update them in the same change. `docs/CATALOG_AUTHORING.md` covers `catalog.yaml`.

## Comments

Only when a reader who understands the code still can't see why. Never narrate what the code does or what changed.
