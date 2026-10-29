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
| GET | `/notes` | every note, oldest first; `[]` when empty; `?tag=<name>` lists only the notes carrying that tag |
| GET | `/notes/:id` | `400` for a bad id, `404` for a missing note |
| PUT | `/notes/:id` | `{title, body?}`; fully replaces title and body (an omitted body becomes `""`), `id`, `created_at` and the tags are kept, `200` with the updated note |
| PUT | `/notes/:id/tags` | `{tags?}`; replaces the note's tags with that set (an omitted or `null` field and `[]` all clear them), `200` with the note; `400` `tag must be 1 to 30 characters of a-z, 0-9 and -` or `a note has at most 10 tags`, `404` `note not found` |
| DELETE | `/notes/:id` | `204` with no body |

Every error body is `{"error": "<message>"}`. On every note route a non-numeric or non-positive id
is `400` `invalid note id` and a missing note is `404` `note not found` — on `PUT` that `404` wins
even when the body is invalid too, and a second `DELETE` of the same note is a `404` as well.

### Tags

A tag is 1 to 30 characters of `a-z`, `0-9` and `-`, and a note has at most 10 tags — duplicates in
a request count once. Anything else is a `400` carrying the rule that was broken: `tag must be 1 to
30 characters of a-z, 0-9 and -` or `a note has at most 10 tags`. The replacement is all or nothing:
a rejected request stores nothing and the note keeps the tags it had.

Every note in every response carries a `tags` field: a sorted array of its tag names, `[]` (never
`null`) when it has none. Only `PUT /notes/:id/tags` changes the tags — `POST /notes` still takes
just `{title, body?}` and creates an untagged note, and `PUT /notes/:id` leaves the tags untouched.

`GET /notes?tag=<name>` lists only the notes carrying that tag, each once, with its whole tag set.
An unknown tag, and any value that is not a valid tag at all, answers `200` with `[]` rather than a
`400`.

## Conventions

- Handlers take the request context and pass it to sqlx (`...Context` methods).
- The schema changes only through a new migration file; existing migrations are never edited.
- Tests build the router with `newTestRouter(t)`, which opens a fresh migrated in-memory database.
- `gofmt -l .` prints nothing; `go build ./...`, `go vet ./...` and `go test ./...` pass.
