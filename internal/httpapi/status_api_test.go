package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateNoteStatus checks that POST /notes stores the status it is given — any of the three,
// in any case — that an absent or null status means a draft, and that an unknown status is a 400
// that creates no note.
func TestCreateNoteStatus(t *testing.T) {
	accepted := []struct {
		name   string
		body   string
		status NoteStatus
	}{
		{"absent status is a draft", `{"title":"no status"}`, StatusDraft},
		{"null status is a draft", `{"title":"null status","status":null}`, StatusDraft},
		{"explicit draft", `{"title":"draft","status":"draft"}`, StatusDraft},
		{"explicit published", `{"title":"published","status":"published"}`, StatusPublished},
		{"explicit archived", `{"title":"archived","status":"archived"}`, StatusArchived},
		{"mixed case published", `{"title":"mixed","status":"PuBlIsHeD"}`, StatusPublished},
	}
	for _, tt := range accepted {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestRouter(t)

			rec := do(t, e, http.MethodPost, "/notes", tt.body)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), `"status":"`+tt.status.String()+`"`,
				"the response carries the lowercase status name")
			assert.NotContains(t, rec.Body.String(), `"status":`+strconv.Itoa(int(tt.status)),
				"the response carries the name and not the stored number")

			var created Note
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
			assert.Equal(t, tt.status, created.Status, "the status sent is the status stored")

			assert.Equal(t, tt.status, loadNote(t, e, created.ID).Status, "GET /notes/:id reports it")
			notes := loadNotes(t, e, "/notes")
			require.Len(t, notes, 1)
			assert.Equal(t, tt.status, notes[0].Status, "GET /notes reports it")
		})
	}

	// Nothing but the three names is a status, the empty string included, and a rejected request
	// creates no note at all.
	e := newTestRouter(t)
	for _, name := range []string{"live", "", "drafts", "1", "published draft"} {
		body := `{"title":"bad status","status":"` + name + `"}`
		rec := do(t, e, http.MethodPost, "/notes", body)
		require.Equal(t, http.StatusBadRequest, rec.Code, body)
		assert.JSONEq(t, `{"error":"invalid status"}`, rec.Body.String(), body)
	}
	assert.Empty(t, loadNotes(t, e, "/notes"), "a rejected request creates no note")
}

// TestPutNoteIgnoresStatus checks that PUT /notes/:id never changes a note's status: a body
// carrying one, valid or not, is accepted and the stored status stays exactly as it was.
func TestPutNoteIgnoresStatus(t *testing.T) {
	e := newTestRouter(t)

	// Start from a note that is not a draft, so a PUT that reset the status would show.
	rec := do(t, e, http.MethodPost, "/notes", `{"title":"archived","body":"a","status":"archived"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created Note
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.Equal(t, StatusArchived, created.Status)
	before := loadNote(t, e, created.ID)

	for _, body := range []string{
		`{"title":"renamed","body":"b","status":"draft"}`,
		`{"title":"renamed","status":"published"}`,
		`{"title":"renamed","status":"live"}`,
		`{"title":"renamed","status":null}`,
	} {
		rec := do(t, e, http.MethodPut, notePath(created.ID), body)
		require.Equal(t, http.StatusOK, rec.Code, body)

		var updated Note
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
		assert.Contains(t, rec.Body.String(), `"status":"archived"`, "the response reports the status")
		assert.Equal(t, StatusArchived, updated.Status, "PUT ignores the status: %s", body)
		assert.Equal(t, "renamed", updated.Title, body)

		stored := loadNote(t, e, created.ID)
		assert.Equal(t, StatusArchived, stored.Status, "the stored status is unchanged: %s", body)
		assert.Equal(t, before.CreatedAt, stored.CreatedAt, "created_at does not change: %s", body)
	}
}
