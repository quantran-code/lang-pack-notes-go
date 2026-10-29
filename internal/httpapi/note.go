package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

// maxTitleLength is the longest title, counted in characters.
const maxTitleLength = 200

// Note is a stored note. The db tags map the columns for sqlx.
type Note struct {
	ID        int64     `db:"id" json:"id"`
	Title     string    `db:"title" json:"title"`
	Body      string    `db:"body" json:"body"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// noteRequest is the body accepted when a note is created.
type noteRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// validate applies the rules a note has: a title that is not blank and at most maxTitleLength
// characters. The body is free text and may be empty.
func (r noteRequest) validate() error {
	title := strings.TrimSpace(r.Title)
	if title == "" {
		return errors.New("title is required")
	}
	if utf8.RuneCountInString(title) > maxTitleLength {
		return errors.New("title must be at most 200 characters")
	}
	return nil
}

// createNote handles POST /notes.
func createNote(db *sqlx.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req noteRequest
		if err := c.Bind(&req); err != nil {
			return writeError(c, http.StatusBadRequest, "invalid JSON body")
		}
		if err := req.validate(); err != nil {
			return writeError(c, http.StatusBadRequest, err.Error())
		}

		note := Note{Title: strings.TrimSpace(req.Title), Body: req.Body, CreatedAt: time.Now().UTC()}
		result, err := db.NamedExecContext(c.Request().Context(),
			"INSERT INTO notes (title, body, created_at) VALUES (:title, :body, :created_at)", note)
		if err != nil {
			return writeError(c, http.StatusInternalServerError, "creating note")
		}
		id, err := result.LastInsertId()
		if err != nil {
			return writeError(c, http.StatusInternalServerError, "creating note")
		}
		note.ID = id
		return c.JSON(http.StatusCreated, note)
	}
}

// listNotes handles GET /notes: every note, oldest first. The slice is made up front so an empty
// table marshals as [] and not null.
func listNotes(db *sqlx.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		notes := make([]Note, 0)
		err := db.SelectContext(c.Request().Context(), &notes,
			"SELECT id, title, body, created_at FROM notes ORDER BY id")
		if err != nil {
			return writeError(c, http.StatusInternalServerError, "listing notes")
		}
		return c.JSON(http.StatusOK, notes)
	}
}

// parseNoteID reads the :id path parameter, rejecting anything that is not a positive whole
// number.
func parseNoteID(c echo.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, errors.New("invalid note id")
	}
	return id, nil
}

// updateNote handles PUT /notes/:id. The id is checked and the note loaded before the body is
// looked at, so a missing note is a 404 even when the body is invalid too. It is a full
// replacement: an omitted body stores the empty string, while id and created_at are kept.
func updateNote(db *sqlx.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := parseNoteID(c)
		if err != nil {
			return writeError(c, http.StatusBadRequest, "invalid note id")
		}

		var note Note
		err = db.GetContext(c.Request().Context(), &note,
			"SELECT id, title, body, created_at FROM notes WHERE id = ?", id)
		if errors.Is(err, sql.ErrNoRows) {
			return writeError(c, http.StatusNotFound, "note not found")
		}
		if err != nil {
			return writeError(c, http.StatusInternalServerError, "loading note")
		}

		var req noteRequest
		if err := c.Bind(&req); err != nil {
			return writeError(c, http.StatusBadRequest, "invalid JSON body")
		}
		if err := req.validate(); err != nil {
			return writeError(c, http.StatusBadRequest, err.Error())
		}

		title := strings.TrimSpace(req.Title)
		_, err = db.ExecContext(c.Request().Context(),
			"UPDATE notes SET title = ?, body = ? WHERE id = ?", title, req.Body, id)
		if err != nil {
			return writeError(c, http.StatusInternalServerError, "updating note")
		}
		note.Title = title
		note.Body = req.Body
		return c.JSON(http.StatusOK, note)
	}
}

// deleteNote handles DELETE /notes/:id: 204 with no body, or 404 when there was nothing to
// delete.
func deleteNote(db *sqlx.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := parseNoteID(c)
		if err != nil {
			return writeError(c, http.StatusBadRequest, "invalid note id")
		}

		result, err := db.ExecContext(c.Request().Context(), "DELETE FROM notes WHERE id = ?", id)
		if err != nil {
			return writeError(c, http.StatusInternalServerError, "deleting note")
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return writeError(c, http.StatusInternalServerError, "deleting note")
		}
		if affected == 0 {
			return writeError(c, http.StatusNotFound, "note not found")
		}
		return c.NoContent(http.StatusNoContent)
	}
}

// getNote handles GET /notes/:id. A non-numeric id is a 400, a missing note a 404.
func getNote(db *sqlx.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id < 1 {
			return writeError(c, http.StatusBadRequest, "invalid note id")
		}

		var note Note
		err = db.GetContext(c.Request().Context(), &note,
			"SELECT id, title, body, created_at FROM notes WHERE id = ?", id)
		if errors.Is(err, sql.ErrNoRows) {
			return writeError(c, http.StatusNotFound, "note not found")
		}
		if err != nil {
			return writeError(c, http.StatusInternalServerError, "loading note")
		}
		return c.JSON(http.StatusOK, note)
	}
}
