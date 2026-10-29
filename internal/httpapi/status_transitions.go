package httpapi

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

// publishNote handles POST /notes/:id/publish: it moves a draft to published.
func publishNote(db *sqlx.DB) echo.HandlerFunc {
	return transitionNote(db, StatusDraft, StatusPublished)
}

// archiveNote handles POST /notes/:id/archive: it moves a published note to archived.
func archiveNote(db *sqlx.DB) echo.HandlerFunc {
	return transitionNote(db, StatusPublished, StatusArchived)
}

// transitionNote builds the handler that moves a note from one status to another. The move is a
// single conditional UPDATE, never a read followed by a write, so two requests racing on the same
// note cannot both succeed: only the one that still finds the note in the from status updates a
// row. When nothing was updated the note is looked up to tell a note that is gone (404) apart from
// a note in the wrong status (409).
func transitionNote(db *sqlx.DB, from, to NoteStatus) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		id, err := parseNoteID(c)
		if err != nil {
			return writeError(c, http.StatusBadRequest, "invalid note id")
		}

		result, err := db.ExecContext(ctx,
			"UPDATE notes SET status = ? WHERE id = ? AND status = ?", to, id, from)
		if err != nil {
			return writeError(c, http.StatusInternalServerError, "updating note status")
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return writeError(c, http.StatusInternalServerError, "updating note status")
		}
		if affected == 0 {
			// Nothing moved: either there is no such note, or it was not in the from status.
			var existing string
			err := db.GetContext(ctx, &existing, "SELECT id FROM notes WHERE id = ?", id)
			if errors.Is(err, sql.ErrNoRows) {
				return writeError(c, http.StatusNotFound, "note not found")
			}
			if err != nil {
				return writeError(c, http.StatusInternalServerError, "loading note")
			}
			return writeError(c, http.StatusConflict, "invalid status transition")
		}

		// The response is the whole note, loaded the same way GET /notes/:id loads it.
		var note Note
		err = db.GetContext(ctx, &note,
			"SELECT id, title, body, created_at, status FROM notes WHERE id = ?", id)
		if errors.Is(err, sql.ErrNoRows) {
			return writeError(c, http.StatusNotFound, "note not found")
		}
		if err != nil {
			return writeError(c, http.StatusInternalServerError, "loading note")
		}

		note.Tags = []string{}
		tags, err := loadTags(ctx, db, []string{note.ID})
		if err != nil {
			return writeError(c, http.StatusInternalServerError, "loading note")
		}
		if names, ok := tags[note.ID]; ok {
			note.Tags = names
		}
		return c.JSON(http.StatusOK, note)
	}
}
