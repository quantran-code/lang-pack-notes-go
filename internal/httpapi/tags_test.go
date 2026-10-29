package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tagsPath is the path of the tag-replacement route of the note with the given id.
func tagsPath(id int64) string {
	return "/notes/" + strconv.FormatInt(id, 10) + "/tags"
}

// loadNote fetches a note through GET /notes/:id and fails when it is missing.
func loadNote(t *testing.T, e *echo.Echo, id int64) Note {
	t.Helper()
	rec := do(t, e, http.MethodGet, "/notes/"+strconv.FormatInt(id, 10), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var note Note
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &note))
	return note
}

// loadNotes fetches the list answered by the given GET target and fails when it is not a 200.
func loadNotes(t *testing.T, e *echo.Echo, target string) []Note {
	t.Helper()
	rec := do(t, e, http.MethodGet, target, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var notes []Note
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &notes))
	return notes
}

// createNoteWith posts a note and returns it together with the raw response body.
func createNoteWith(t *testing.T, e *echo.Echo, title, body string) (Note, string) {
	t.Helper()
	payload, err := json.Marshal(noteRequest{Title: title, Body: body})
	require.NoError(t, err)
	rec := do(t, e, http.MethodPost, "/notes", string(payload))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var note Note
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &note))
	return note, rec.Body.String()
}

// TestCreateNoteHasEmptyTags checks that POST /notes still takes only {title, body?} and that the
// note it answers with carries an empty tags array instead of null.
func TestCreateNoteHasEmptyTags(t *testing.T) {
	e := newTestRouter(t)

	note, raw := createNoteWith(t, e, "untagged", "x")
	assert.Contains(t, raw, `"tags":[]`, "an untagged note marshals [] and not null")
	assert.NotContains(t, raw, `"tags":null`)
	assert.NotNil(t, note.Tags, "tags is an empty slice")
	assert.Empty(t, note.Tags)

	// Nothing was written to the tag tables: the created note has no tags on a later read either.
	assert.Equal(t, []string{}, loadNote(t, e, note.ID).Tags)

	notes := loadNotes(t, e, "/notes")
	require.Len(t, notes, 1)
	assert.Equal(t, []string{}, notes[0].Tags, "the list carries [] too")
}

// TestPutNoteTags checks that PUT /notes/:id/tags replaces the note's whole tag set, that the set
// comes back sorted and deduplicated, and that clearing it with a missing, null or empty array
// leaves the note with [] and not null.
func TestPutNoteTags(t *testing.T) {
	e := newTestRouter(t)
	first, _ := createNoteWith(t, e, "first", "a")
	second, _ := createNoteWith(t, e, "second", "b")

	rec := do(t, e, http.MethodPut, tagsPath(first.ID), `{"tags":["go","echo"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var updated Note
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	assert.Equal(t, []string{"echo", "go"}, updated.Tags, "the response carries the sorted set")
	assert.Equal(t, first.ID, updated.ID, "the id does not change")
	assert.Equal(t, first.Title, updated.Title, "the title does not change")
	assert.Equal(t, first.Body, updated.Body, "the body does not change")
	assert.Equal(t, first.CreatedAt, updated.CreatedAt, "created_at does not change")

	assert.Equal(t, []string{"echo", "go"}, loadNote(t, e, first.ID).Tags, "a later GET returns the same set")
	assert.Equal(t, []string{}, loadNote(t, e, second.ID).Tags, "the other note keeps no tags")

	// A repeated name is stored once.
	rec = do(t, e, http.MethodPut, tagsPath(first.ID), `{"tags":["go","go"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	assert.Equal(t, []string{"go"}, updated.Tags, "duplicates in a request count once")
	assert.Equal(t, []string{"go"}, loadNote(t, e, first.ID).Tags)

	// The full replacement of title and body leaves the tags exactly as they were.
	rec = do(t, e, http.MethodPut, "/notes/"+strconv.FormatInt(first.ID, 10), `{"title":"renamed"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	assert.Equal(t, "renamed", updated.Title)
	assert.Equal(t, []string{"go"}, updated.Tags, "PUT /notes/:id does not change tags")
	assert.Equal(t, []string{"go"}, loadNote(t, e, first.ID).Tags)

	// An absent tags field, an explicit null and an empty array all mean the empty set.
	for _, body := range []string{`{}`, `{"tags":null}`, `{"tags":[]}`} {
		rec = do(t, e, http.MethodPut, tagsPath(first.ID), `{"tags":["go","echo"]}`)
		require.Equal(t, http.StatusOK, rec.Code, body)

		rec = do(t, e, http.MethodPut, tagsPath(first.ID), body)
		require.Equal(t, http.StatusOK, rec.Code, body)
		var cleared Note
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &cleared))
		assert.Equal(t, []string{}, cleared.Tags, "no tags is [] and not null: %s", body)
		assert.NotContains(t, rec.Body.String(), `"tags":null`, body)
		assert.Equal(t, []string{}, loadNote(t, e, first.ID).Tags, body)
	}

	// The list shows each note's full tag set, and [] for a note without any.
	rec = do(t, e, http.MethodPut, tagsPath(first.ID), `{"tags":["go","echo"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	notes := loadNotes(t, e, "/notes")
	require.Len(t, notes, 2)
	assert.Equal(t, first.ID, notes[0].ID)
	assert.Equal(t, []string{"echo", "go"}, notes[0].Tags)
	assert.Equal(t, []string{}, notes[1].Tags, "the untagged note lists [] and not null")
}

// TestPutNoteTagsValidation covers the two tag rules plus malformed JSON, and checks that every
// rejected request leaves the note's stored tags exactly as they were.
func TestPutNoteTagsValidation(t *testing.T) {
	const tagRule = "tag must be 1 to 30 characters of a-z, 0-9 and -"

	e := newTestRouter(t)
	note, _ := createNoteWith(t, e, "kept", "x")
	require.Equal(t, http.StatusOK, do(t, e, http.MethodPut, tagsPath(note.ID), `{"tags":["go"]}`).Code)

	// Exactly ten valid names are accepted, sorted ascending.
	ten := make([]string, 0, maxTagsPerNote)
	for i := 0; i < maxTagsPerNote; i++ {
		ten = append(ten, "tag-"+strconv.Itoa(i))
	}
	payload, err := json.Marshal(tagsRequest{Tags: ten})
	require.NoError(t, err)
	rec := do(t, e, http.MethodPut, tagsPath(note.ID), string(payload))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var stored Note
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &stored))
	assert.Equal(t, ten, stored.Tags, "ten valid tags are accepted and sorted")

	// Duplicates count once, so ten distinct names plus a repeat of one is still ten.
	payload, err = json.Marshal(tagsRequest{Tags: append(append([]string{}, ten...), ten[0])})
	require.NoError(t, err)
	rec = do(t, e, http.MethodPut, tagsPath(note.ID), string(payload))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Eleven distinct names are one too many.
	eleven := append(append([]string{}, ten...), "tag-"+strconv.Itoa(maxTagsPerNote))
	elevenPayload, err := json.Marshal(tagsRequest{Tags: eleven})
	require.NoError(t, err)

	// From here on the note carries one known tag, so a rejected request is easy to spot.
	require.Equal(t, http.StatusOK, do(t, e, http.MethodPut, tagsPath(note.ID), `{"tags":["go"]}`).Code)
	kept := loadNote(t, e, note.ID)

	rejected := []struct {
		name   string
		body   string
		errMsg string
	}{
		{"uppercase", `{"tags":["Go"]}`, tagRule},
		{"empty name", `{"tags":[""]}`, tagRule},
		{"punctuation", `{"tags":["go!"]}`, tagRule},
		{"space", `{"tags":["go lang"]}`, tagRule},
		{"too long", `{"tags":["` + strings.Repeat("a", maxTagLength+1) + `"]}`, tagRule},
		{"one valid and one broken", `{"tags":["go","Go"]}`, tagRule},
		{"too many tags", string(elevenPayload), "a note has at most 10 tags"},
		{"malformed JSON", `{"tags":`, "invalid JSON body"},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, e, http.MethodPut, tagsPath(note.ID), tt.body)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.JSONEq(t, `{"error":"`+tt.errMsg+`"}`, rec.Body.String())
			assert.Equal(t, kept.Tags, loadNote(t, e, note.ID).Tags,
				"a rejected request leaves the stored tags alone")
		})
	}

	// The rejected requests also left the note itself alone.
	after := loadNote(t, e, note.ID)
	assert.Equal(t, kept.Title, after.Title)
	assert.Equal(t, kept.Body, after.Body)
	assert.Equal(t, kept.CreatedAt, after.CreatedAt)
}

// TestPutNoteTagsErrors covers the id and missing-note errors of the tag route: a bad id is a 400,
// and a missing note is a 404 even when the body is invalid too.
func TestPutNoteTagsErrors(t *testing.T) {
	e := newTestRouter(t)
	note, _ := createNoteWith(t, e, "kept", "x")
	require.Equal(t, http.StatusOK, do(t, e, http.MethodPut, tagsPath(note.ID), `{"tags":["go"]}`).Code)

	for _, target := range []string{"/notes/abc/tags", "/notes/0/tags", "/notes/-1/tags"} {
		rec := do(t, e, http.MethodPut, target, `{"tags":["go"]}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code, target)
		assert.JSONEq(t, `{"error":"invalid note id"}`, rec.Body.String(), target)
	}

	for _, body := range []string{`{"tags":["go"]}`, `{}`, `{"tags":["GO!"]}`, `{"tags":`, `{"tags":["` + strings.Repeat("a", maxTagLength+1) + `"]}`} {
		rec := do(t, e, http.MethodPut, "/notes/99/tags", body)
		assert.Equal(t, http.StatusNotFound, rec.Code, body)
		assert.JSONEq(t, `{"error":"note not found"}`, rec.Body.String(), body)
	}

	assert.Equal(t, []string{"go"}, loadNote(t, e, note.ID).Tags, "the failed requests changed nothing")
}

// TestListNotesByTag checks GET /notes?tag=<name>: it lists only the notes carrying the tag, each
// one once with its whole tag set, answers [] for an unknown or invalid value, and still lists
// every note oldest first when there is no tag parameter at all.
func TestListNotesByTag(t *testing.T) {
	e := newTestRouter(t)
	both, _ := createNoteWith(t, e, "both", "a")
	onlyGo, _ := createNoteWith(t, e, "only go", "")
	untagged, _ := createNoteWith(t, e, "none", "")

	require.Equal(t, http.StatusOK, do(t, e, http.MethodPut, tagsPath(both.ID), `{"tags":["go","echo"]}`).Code)
	require.Equal(t, http.StatusOK, do(t, e, http.MethodPut, tagsPath(onlyGo.ID), `{"tags":["go"]}`).Code)

	notes := loadNotes(t, e, "/notes?tag=go")
	require.Len(t, notes, 2, "only the notes carrying the tag, each exactly once")
	assert.Equal(t, both.ID, notes[0].ID)
	assert.Equal(t, []string{"echo", "go"}, notes[0].Tags, "a matching note shows all of its tags")
	assert.Equal(t, onlyGo.ID, notes[1].ID)
	assert.Equal(t, []string{"go"}, notes[1].Tags)

	notes = loadNotes(t, e, "/notes?tag=echo")
	require.Len(t, notes, 1)
	assert.Equal(t, both.ID, notes[0].ID)
	assert.Equal(t, []string{"echo", "go"}, notes[0].Tags)

	// An unknown tag, and a value that cannot be a tag at all, list nothing rather than 400.
	for _, target := range []string{
		"/notes?tag=nope",
		"/notes?tag=GO!",
		"/notes?tag=",
		"/notes?tag=" + strings.Repeat("a", 31),
		"/notes?tag=" + strings.Repeat("a", 40),
		"/notes?tag=Go",
	} {
		rec := do(t, e, http.MethodGet, target, "")
		assert.Equal(t, http.StatusOK, rec.Code, target)
		assert.JSONEq(t, `[]`, rec.Body.String(), target)
	}

	// Without the parameter every note is listed, oldest first, exactly as before.
	notes = loadNotes(t, e, "/notes")
	require.Len(t, notes, 3)
	assert.Equal(t, both.ID, notes[0].ID)
	assert.Equal(t, onlyGo.ID, notes[1].ID)
	assert.Equal(t, untagged.ID, notes[2].ID)
	assert.Equal(t, []string{"echo", "go"}, notes[0].Tags)
	assert.Equal(t, []string{"go"}, notes[1].Tags)
	assert.Equal(t, []string{}, notes[2].Tags, "an untagged note lists [] and not null")
}
