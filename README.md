# lang-pack-notes-go

A small notes API used to exercise Octiq's Go language pack on a stack other than Gin + GORM.

| Area | Choice |
|---|---|
| HTTP | [Echo](https://echo.labstack.com/) v4 |
| Data access | [sqlx](https://github.com/jmoiron/sqlx) over `database/sql` (no ORM) |
| Database | SQLite through the pure-Go `modernc.org/sqlite` driver (no cgo, no Docker) |
| Migrations | [golang-migrate](https://github.com/golang-migrate/migrate), SQL files embedded from `internal/database/migrations` as `NNNNNN_name.up.sql` / `.down.sql` |
| Tests | `testing` + [testify](https://github.com/stretchr/testify), table-driven `httptest` tests against the real router |

## Run

```
PORT=8080 DB_PATH=notes.db go run ./cmd/server
```

`DB_PATH` defaults to an in-memory database; `PORT` defaults to `8080`.

## Endpoints

| Method | Path | Notes |
|---|---|---|
| GET | `/health` | `{"status":"ok"}` |
| POST | `/notes` | `{title, body?}`; title 1-200 characters, `201` with the note |
| GET | `/notes` | every note, oldest first; `[]` when empty |
| GET | `/notes/:id` | `400` for a bad id, `404` for a missing note |

Every error body is `{"error": "<message>"}`.

## Conventions

- Handlers take the request context and pass it to sqlx (`...Context` methods).
- The schema changes only through a new migration file; existing migrations are never edited.
- Tests build the router with `newTestRouter(t)`, which opens a fresh migrated in-memory database.
- `gofmt -l .` prints nothing; `go build ./...`, `go vet ./...` and `go test ./...` pass.
