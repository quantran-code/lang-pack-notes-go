package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// transitionPath is the path of the publish or archive action on the note with the given id.
func transitionPath(id int64, action string) string {
	return "/notes/" + strconv.FormatInt(id, 10) + "/" + action
}

// createWithStatus posts the given body to POST /notes and returns the note the API stored.
func createWithStatus(t *testing.T, e *echo.Echo, body string) Note {
	t.Helper()
	rec := do(t, e, http.MethodPost, "/notes", body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var note Note
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &note))
	return note
}

// TestPublishAndArchiveNote checks the happy path draft -> published -> archived: each transition
// answers 200 with the whole note, showing the new status while id, title, body, created_at and
// tags stay exactly as they were, and a later GET reports the new status too.
func TestPublishAndArchiveNote(t *testing.T) {
	e := newTestRouter(t)
	created := createWithStatus(t, e, `{"title":"draft","body":"a","status":"draft"}`)
	require.Equal(t, StatusDraft, created.Status)

	// A tag, so a transition that clobbered the note's tags would show.
	require.Equal(t, http.StatusOK, do(t, e, http.MethodPut, tagsPath(created.ID), `{"tags":["go","echo"]}`).Code)

	rec := do(t, e, http.MethodPost, transitionPath(created.ID, "publish"), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"status":"published"`, "the response carries the new status")
	var published Note
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &published))
	assert.Equal(t, created.ID, published.ID, "the id does not change")
	assert.Equal(t, created.Title, published.Title, "the title does not change")
	assert.Equal(t, created.Body, published.Body, "the body does not change")
	assert.Equal(t, created.CreatedAt, published.CreatedAt, "created_at does not change")
	assert.Equal(t, StatusPublished, published.Status)
	assert.Equal(t, []string{"echo", "go"}, published.Tags, "the tags are untouched")

	stored := loadNote(t, e, created.ID)
	assert.Equal(t, StatusPublished, stored.Status, "a later GET reports the new status")
	assert.Equal(t, []string{"echo", "go"}, stored.Tags)

	rec = do(t, e, http.MethodPost, transitionPath(created.ID, "archive"), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"status":"archived"`)
	var archived Note
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &archived))
	assert.Equal(t, created.ID, archived.ID, "the id does not change")
	assert.Equal(t, created.Title, archived.Title, "the title does not change")
	assert.Equal(t, created.Body, archived.Body, "the body does not change")
	assert.Equal(t, created.CreatedAt, archived.CreatedAt, "created_at does not change")
	assert.Equal(t, StatusArchived, archived.Status)
	assert.Equal(t, []string{"echo", "go"}, archived.Tags, "the tags are untouched")

	stored = loadNote(t, e, created.ID)
	assert.Equal(t, StatusArchived, stored.Status, "a later GET reports the new status")
	assert.Equal(t, []string{"echo", "go"}, stored.Tags)
}

// TestStatusTransitionErrors covers every rejected transition: a note already past the from status
// is a 409 that changes nothing, a missing note is a 404 and a bad id is a 400.
func TestStatusTransitionErrors(t *testing.T) {
	e := newTestRouter(t)
	draft := createWithStatus(t, e, `{"title":"draft"}`)
	published := createWithStatus(t, e, `{"title":"published","status":"published"}`)
	archived := createWithStatus(t, e, `{"title":"archived","status":"archived"}`)
	require.Equal(t, StatusDraft, draft.Status)
	require.Equal(t, StatusPublished, published.Status)
	require.Equal(t, StatusArchived, archived.Status)

	conflicts := []struct {
		name   string
		id     int64
		action string
	}{
		{"publishing a published note", published.ID, "publish"},
		{"publishing an archived note", archived.ID, "publish"},
		{"archiving a draft", draft.ID, "archive"},
		{"archiving an archived note", archived.ID, "archive"},
	}
	for _, tt := range conflicts {
		t.Run(tt.name, func(t *testing.T) {
			before := loadNote(t, e, tt.id)
			rec := do(t, e, http.MethodPost, transitionPath(tt.id, tt.action), "")
			assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
			assert.JSONEq(t, `{"error":"invalid status transition"}`, rec.Body.String())
			assert.Equal(t, before, loadNote(t, e, tt.id), "a rejected transition changes nothing")
		})
	}

	for _, target := range []string{"/notes/99/publish", "/notes/99/archive"} {
		rec := do(t, e, http.MethodPost, target, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, target)
		assert.JSONEq(t, `{"error":"note not found"}`, rec.Body.String(), target)
	}

	for _, target := range []string{"/notes/abc/publish", "/notes/0/archive", "/notes/-1/publish"} {
		rec := do(t, e, http.MethodPost, target, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, target)
		assert.JSONEq(t, `{"error":"invalid note id"}`, rec.Body.String(), target)
	}

	assert.Equal(t, StatusDraft, loadNote(t, e, draft.ID).Status, "the rejected requests changed nothing")
	assert.Equal(t, StatusPublished, loadNote(t, e, published.ID).Status)
	assert.Equal(t, StatusArchived, loadNote(t, e, archived.ID).Status)
}

// TestConcurrentPublishOnlyOneSucceeds checks that the transition is decided by a single
// conditional UPDATE: of two publishes racing on the same draft exactly one succeeds, the other is
// a 409, and the note ends up published.
func TestConcurrentPublishOnlyOneSucceeds(t *testing.T) {
	e := newTestRouter(t)
	note := createWithStatus(t, e, `{"title":"race"}`)
	require.Equal(t, StatusDraft, note.Status)

	var wg sync.WaitGroup
	codes := make([]int, 2)
	start := make(chan struct{})
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i] = do(t, e, http.MethodPost, transitionPath(note.ID, "publish"), "").Code
		}(i)
	}
	close(start)
	wg.Wait()

	got := make(map[int]int, len(codes))
	for _, code := range codes {
		got[code]++
	}
	assert.Equal(t, 1, got[http.StatusOK], "exactly one publish succeeds")
	assert.Equal(t, 1, got[http.StatusConflict], "the other publish is a conflict")
	assert.Equal(t, StatusPublished, loadNote(t, e, note.ID).Status)
}
