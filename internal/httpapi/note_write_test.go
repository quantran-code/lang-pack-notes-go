package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/quantran-code/lang-pack-notes-go/internal/database"
)

// noteWriteSuite covers the two write routes, PUT and DELETE. SetupTest gives every test case its
// own fresh, migrated in-memory database, the same isolation newTestRouter gives the plain tests.
type noteWriteSuite struct {
	suite.Suite
	e *echo.Echo
}

// TestNoteWriteSuite runs the suite under go test.
func TestNoteWriteSuite(t *testing.T) {
	suite.Run(t, new(noteWriteSuite))
}

// SetupTest builds a router backed by an empty database for the current test case.
func (s *noteWriteSuite) SetupTest() {
	db, err := database.Open(database.MemoryDSN)
	require.NoError(s.T(), err)
	s.T().Cleanup(func() { _ = db.Close() })
	s.e = NewRouter(db)
}

// notePath is the path of the note with the given id.
func notePath(id int64) string {
	return "/notes/" + strconv.FormatInt(id, 10)
}

// create posts a note and returns the note the API stored.
func (s *noteWriteSuite) create(title, body string) Note {
	s.T().Helper()
	payload, err := json.Marshal(noteRequest{Title: title, Body: body})
	s.Require().NoError(err)
	rec := do(s.T(), s.e, http.MethodPost, "/notes", string(payload))
	s.Require().Equal(http.StatusCreated, rec.Code, rec.Body.String())
	var note Note
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &note))
	return note
}

// mustGet fetches a note through GET /notes/:id and fails when it is missing.
func (s *noteWriteSuite) mustGet(id int64) Note {
	s.T().Helper()
	rec := do(s.T(), s.e, http.MethodGet, notePath(id), "")
	s.Require().Equal(http.StatusOK, rec.Code, rec.Body.String())
	var note Note
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &note))
	return note
}

// assertNoteUnchanged compares every field a client can see, created_at included.
func (s *noteWriteSuite) assertNoteUnchanged(want, got Note, msgAndArgs ...interface{}) {
	s.T().Helper()
	s.Equal(want.ID, got.ID, msgAndArgs...)
	s.Equal(want.Title, got.Title, msgAndArgs...)
	s.Equal(want.Body, got.Body, msgAndArgs...)
	s.Equal(want.CreatedAt, got.CreatedAt, msgAndArgs...)
}

// TestUpdateNote checks that PUT is a full replacement of title and body that keeps id and
// created_at, and that a rejected body changes nothing.
func (s *noteWriteSuite) TestUpdateNote() {
	s.create("first", "a")
	before := s.mustGet(1)

	rec := do(s.T(), s.e, http.MethodPut, notePath(1), `{"title":"second","body":"b"}`)
	s.Require().Equal(http.StatusOK, rec.Code, rec.Body.String())
	var updated Note
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &updated))
	s.Equal(before.ID, updated.ID, "the id does not change")
	s.Equal(before.CreatedAt, updated.CreatedAt, "created_at does not change")
	s.Equal("second", updated.Title)
	s.Equal("b", updated.Body)
	s.assertNoteUnchanged(updated, s.mustGet(1), "the response is the stored note")

	// A full replacement: an omitted body stores the empty string.
	rec = do(s.T(), s.e, http.MethodPut, notePath(1), `{"title":"third"}`)
	s.Require().Equal(http.StatusOK, rec.Code, rec.Body.String())
	var replaced Note
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &replaced))
	s.Equal("third", replaced.Title)
	s.Equal("", replaced.Body)
	s.Equal(before.CreatedAt, replaced.CreatedAt, "created_at does not change")
	s.assertNoteUnchanged(replaced, s.mustGet(1))

	// A rejected update leaves the stored note exactly as it was.
	kept := s.mustGet(1)
	rejected := []struct {
		name   string
		body   string
		errMsg string
	}{
		{"missing title", `{"body":"x"}`, "title is required"},
		{"blank title", `{"title":"   "}`, "title is required"},
		{"title too long", `{"title":"` + strings.Repeat("a", 201) + `"}`, "title must be at most 200 characters"},
		{"malformed JSON", `{"title":`, "invalid JSON body"},
	}
	for _, tt := range rejected {
		rec = do(s.T(), s.e, http.MethodPut, notePath(1), tt.body)
		s.Equal(http.StatusBadRequest, rec.Code, tt.name)
		s.JSONEq(`{"error":"`+tt.errMsg+`"}`, rec.Body.String(), tt.name)
		s.assertNoteUnchanged(kept, s.mustGet(1), tt.name)
	}
}

// TestDeleteNote checks that DELETE answers 204 with no body, that the note is gone afterwards
// and that deleting it again is a 404, while the other notes are untouched.
func (s *noteWriteSuite) TestDeleteNote() {
	s.create("first", "a")
	s.create("second", "b")
	other := s.mustGet(2)

	rec := do(s.T(), s.e, http.MethodDelete, notePath(1), "")
	s.Require().Equal(http.StatusNoContent, rec.Code, rec.Body.String())
	s.Empty(rec.Body.String(), "204 has no body")

	rec = do(s.T(), s.e, http.MethodGet, notePath(1), "")
	s.Equal(http.StatusNotFound, rec.Code)
	s.JSONEq(`{"error":"note not found"}`, rec.Body.String())

	rec = do(s.T(), s.e, http.MethodDelete, notePath(1), "")
	s.Equal(http.StatusNotFound, rec.Code, "deleting the same note twice is a 404")
	s.JSONEq(`{"error":"note not found"}`, rec.Body.String())

	rec = do(s.T(), s.e, http.MethodGet, "/notes", "")
	s.Require().Equal(http.StatusOK, rec.Code, rec.Body.String())
	var notes []Note
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &notes))
	s.Require().Len(notes, 1, "the deleted note is gone from the list")
	s.assertNoteUnchanged(other, notes[0], "the other note is untouched")
}

// TestNoteWriteErrors covers the errors both routes share: a bad id is a 400 and a missing note
// is a 404, for PUT even before the body is parsed or validated.
func (s *noteWriteSuite) TestNoteWriteErrors() {
	s.create("kept", "x")

	for _, target := range []string{"/notes/abc", "/notes/0", "/notes/-1"} {
		rec := do(s.T(), s.e, http.MethodPut, target, `{"title":"x"}`)
		s.Equal(http.StatusBadRequest, rec.Code, target)
		s.JSONEq(`{"error":"invalid note id"}`, rec.Body.String(), target)

		rec = do(s.T(), s.e, http.MethodDelete, target, "")
		s.Equal(http.StatusBadRequest, rec.Code, target)
		s.JSONEq(`{"error":"invalid note id"}`, rec.Body.String(), target)
	}

	// A missing note is a 404 whether the body is valid, invalid or not JSON at all.
	for _, body := range []string{`{"title":"x"}`, `{}`, `{"title":"` + strings.Repeat("a", 201) + `"}`, `{"title":`} {
		rec := do(s.T(), s.e, http.MethodPut, "/notes/99", body)
		s.Equal(http.StatusNotFound, rec.Code, body)
		s.JSONEq(`{"error":"note not found"}`, rec.Body.String(), body)
	}

	rec := do(s.T(), s.e, http.MethodDelete, "/notes/99", "")
	s.Equal(http.StatusNotFound, rec.Code)
	s.JSONEq(`{"error":"note not found"}`, rec.Body.String())

	s.Equal("kept", s.mustGet(1).Title, "the failed requests changed nothing")
}
