// Package httpapi holds the Echo router and the handlers behind it.
package httpapi

import (
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

// errorBody is the one error shape every endpoint answers with.
type errorBody struct {
	Error string `json:"error"`
}

// writeError answers with {"error": "<message>"} and the given status.
func writeError(c echo.Context, status int, message string) error {
	return c.JSON(status, errorBody{Error: message})
}

// NewRouter builds the Echo instance with every route registered. Errors Echo raises itself
// (an unknown path, a method that is not allowed) are rewritten to the same error shape.
func NewRouter(db *sqlx.DB) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}
		status, message := http.StatusInternalServerError, "internal server error"
		if he, ok := err.(*echo.HTTPError); ok {
			status = he.Code
			message = http.StatusText(he.Code)
		}
		_ = writeError(c, status, message)
	}

	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	e.POST("/notes", createNote(db))
	e.POST("/notes/bulk", bulkCreateNotes(db))
	e.GET("/notes", listNotes(db))
	e.GET("/notes/:id", getNote(db))
	e.PUT("/notes/:id", updateNote(db))
	e.PUT("/notes/:id/tags", putNoteTags(db))
	e.POST("/notes/:id/publish", publishNote(db))
	e.POST("/notes/:id/archive", archiveNote(db))
	e.DELETE("/notes/:id", deleteNote(db))
	return e
}
