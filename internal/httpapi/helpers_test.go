package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/quantran-code/lang-pack-notes-go/internal/database"
)

// newTestRouter returns a router backed by a fresh, migrated in-memory database.
func newTestRouter(t *testing.T) *echo.Echo {
	t.Helper()
	db, err := database.Open(database.MemoryDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return NewRouter(db)
}

// do sends one request to the router and returns the recorded response.
func do(t *testing.T, e *echo.Echo, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}
