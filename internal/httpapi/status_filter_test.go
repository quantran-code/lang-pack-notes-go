package httpapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListNotesByStatus checks GET /notes?status=<name>: it lists only the notes in that status, in
// id order, accepts the name in any case, and still lists every note when the parameter is absent.
func TestListNotesByStatus(t *testing.T) {
	e := newTestRouter(t)

	draft := createWithStatus(t, e, `{"title":"draft","status":"draft"}`)
	published := createWithStatus(t, e, `{"title":"published","status":"published"}`)
	archived := createWithStatus(t, e, `{"title":"archived","status":"archived"}`)
	require.Equal(t, StatusDraft, draft.Status)
	require.Equal(t, StatusPublished, published.Status)
	require.Equal(t, StatusArchived, archived.Status)

	// A tag on the published note, so a filtered list that dropped the tag set would show.
	require.Equal(t, http.StatusOK, do(t, e, http.MethodPut, tagsPath(published.ID), `{"tags":["go","echo"]}`).Code)

	tests := []struct {
		name   string
		target string
		want   []int64
	}{
		{"draft", "/notes?status=draft", []int64{draft.ID}},
		{"published", "/notes?status=published", []int64{published.ID}},
		{"archived", "/notes?status=archived", []int64{archived.ID}},
		{"any case", "/notes?status=PuBlIsHeD", []int64{published.ID}},
		{"absent parameter lists every note", "/notes", []int64{draft.ID, published.ID, archived.ID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notes := loadNotes(t, e, tt.target)
			ids := make([]int64, 0, len(notes))
			for _, note := range notes {
				ids = append(ids, note.ID)
			}
			assert.Equal(t, tt.want, ids)
		})
	}

	// A filtered list carries each note's whole tag set and the lowercase status name.
	rec := do(t, e, http.MethodGet, "/notes?status=published", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"status":"published"`)
	notes := loadNotes(t, e, "/notes?status=published")
	require.Len(t, notes, 1)
	assert.Equal(t, StatusPublished, notes[0].Status)
	assert.Equal(t, []string{"echo", "go"}, notes[0].Tags, "a matching note shows all of its tags")

	notes = loadNotes(t, e, "/notes?status=draft")
	require.Len(t, notes, 1)
	assert.Equal(t, []string{}, notes[0].Tags, "an untagged note lists [] and not null")
}

// TestListNotesStatusAndTag checks that ?tag= and ?status= combine with AND: only notes carrying the
// tag and holding the status are listed. An unknown or invalid tag still answers 200 with [], while
// an invalid status is a 400 even next to a tag parameter.
func TestListNotesStatusAndTag(t *testing.T) {
	e := newTestRouter(t)

	goDraft := createWithStatus(t, e, `{"title":"go draft","status":"draft"}`)
	goPublished := createWithStatus(t, e, `{"title":"go published","status":"published"}`)
	echoPublished := createWithStatus(t, e, `{"title":"echo published","status":"published"}`)
	untagged := createWithStatus(t, e, `{"title":"untagged","status":"published"}`)

	require.Equal(t, http.StatusOK, do(t, e, http.MethodPut, tagsPath(goDraft.ID), `{"tags":["go"]}`).Code)
	require.Equal(t, http.StatusOK, do(t, e, http.MethodPut, tagsPath(goPublished.ID), `{"tags":["go","echo"]}`).Code)
	require.Equal(t, http.StatusOK, do(t, e, http.MethodPut, tagsPath(echoPublished.ID), `{"tags":["echo"]}`).Code)

	// Both halves match this note; the others match only one of them.
	notes := loadNotes(t, e, "/notes?tag=go&status=published")
	require.Len(t, notes, 1, "only the note carrying the tag and holding the status")
	assert.Equal(t, goPublished.ID, notes[0].ID)
	assert.Equal(t, StatusPublished, notes[0].Status)
	assert.Equal(t, []string{"echo", "go"}, notes[0].Tags, "the whole tag set is still reported")

	// The order of the parameters does not change the answer.
	notes = loadNotes(t, e, "/notes?status=published&tag=go")
	require.Len(t, notes, 1)
	assert.Equal(t, goPublished.ID, notes[0].ID)

	// The same tag with another status.
	notes = loadNotes(t, e, "/notes?tag=go&status=draft")
	require.Len(t, notes, 1)
	assert.Equal(t, goDraft.ID, notes[0].ID)

	// Without the tag half every published note is listed, whatever its tags.
	notes = loadNotes(t, e, "/notes?status=published")
	require.Len(t, notes, 3)
	assert.Equal(t, []int64{goPublished.ID, echoPublished.ID, untagged.ID},
		[]int64{notes[0].ID, notes[1].ID, notes[2].ID})
	assert.Equal(t, []string{}, notes[2].Tags, "an untagged note lists [] and not null")

	// An unknown tag, and any value that cannot be a tag at all, answer [] with a 200 rather than
	// a 404 or a 400 — even when the status half is perfectly valid.
	for _, target := range []string{
		"/notes?tag=nope&status=draft",
		"/notes?tag=GO!&status=draft",
		"/notes?tag=&status=published",
	} {
		rec := do(t, e, http.MethodGet, target, "")
		assert.Equal(t, http.StatusOK, rec.Code, target)
		assert.JSONEq(t, `[]`, rec.Body.String(), target)
	}

	// An invalid status is a 400 even when it travels next to a tag parameter, and so is an
	// explicitly empty one.
	for _, target := range []string{
		"/notes?status=live",
		"/notes?tag=go&status=live",
		"/notes?status=",
		"/notes?tag=go&status=",
	} {
		rec := do(t, e, http.MethodGet, target, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, target)
		assert.JSONEq(t, `{"error":"invalid status"}`, rec.Body.String(), target)
	}
}
