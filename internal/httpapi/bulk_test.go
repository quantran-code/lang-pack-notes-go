package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quantran-code/lang-pack-notes-go/internal/database"
)

// bulkPath is the path of the bulk import route.
const bulkPath = "/notes/bulk"

// TestBulkCreateNotes checks the happy path of POST /notes/bulk: 201 with every note of the request
// in request order, each one a stored draft carrying its new id, the title trimmed and the body as
// sent, with an empty tags array rather than null.
func TestBulkCreateNotes(t *testing.T) {
	e := newTestRouter(t)
	existing, _ := createNoteWith(t, e, "existing", "before the import")

	payload, err := json.Marshal(bulkNotesRequest{Notes: []bulkNoteInput{
		{Title: "  first  ", Body: "a"},
		{Title: "second", Body: ""},
	}})
	require.NoError(t, err)

	rec := do(t, e, http.MethodPost, bulkPath, string(payload))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"status":"draft"`, "every imported note is a draft")
	assert.Contains(t, rec.Body.String(), `"tags":[]`, "a fresh note marshals [] and not null")
	assert.NotContains(t, rec.Body.String(), `"tags":null`)

	var created bulkNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.Len(t, created.Notes, 2, "one element per request note")

	first, second := created.Notes[0], created.Notes[1]
	assert.NotZero(t, first.ID)
	assert.NotZero(t, second.ID)
	assert.Less(t, first.ID, second.ID, "ids rise with insert order")
	assert.Less(t, existing.ID, first.ID, "the imported notes are new notes")
	assert.Equal(t, "first", first.Title, "the stored title is trimmed")
	assert.Equal(t, "a", first.Body, "the body is stored exactly as sent")
	assert.Equal(t, StatusDraft, first.Status, "a status is never taken from the request")
	assert.False(t, first.CreatedAt.IsZero())
	assert.NotNil(t, first.Tags, "tags is an empty slice")
	assert.Equal(t, []string{}, first.Tags)

	assert.Equal(t, "second", second.Title)
	assert.Equal(t, "", second.Body)
	assert.Equal(t, StatusDraft, second.Status)
	assert.NotNil(t, second.Tags)
	assert.Equal(t, []string{}, second.Tags)

	// Every imported note is readable, exactly as the response described it.
	for i, want := range created.Notes {
		stored := loadNote(t, e, want.ID)
		assert.Equal(t, want.ID, stored.ID, "note %d", i)
		assert.Equal(t, want.Title, stored.Title, "note %d", i)
		assert.Equal(t, want.Body, stored.Body, "note %d", i)
		assert.Equal(t, want.CreatedAt, stored.CreatedAt, "note %d", i)
		assert.Equal(t, StatusDraft, stored.Status, "note %d", i)
		assert.Equal(t, []string{}, stored.Tags, "note %d", i)
	}

	// The list shows the note that was there before, then the imported ones in request order.
	notes := loadNotes(t, e, "/notes")
	require.Len(t, notes, 3)
	assert.Equal(t, []int64{existing.ID, first.ID, second.ID},
		[]int64{notes[0].ID, notes[1].ID, notes[2].ID})

	// A single note is the same shape with one element.
	payload, err = json.Marshal(bulkNotesRequest{Notes: []bulkNoteInput{{Title: "alone"}}})
	require.NoError(t, err)
	rec = do(t, e, http.MethodPost, bulkPath, string(payload))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var single bulkNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &single))
	require.Len(t, single.Notes, 1)
	assert.Equal(t, "alone", single.Notes[0].Title)
	assert.Equal(t, StatusDraft, single.Notes[0].Status)
	assert.Equal(t, []string{}, single.Notes[0].Tags)
}

// TestBulkCreateNotesValidation checks that a body breaking any bulk rule answers 400 with
// "validation failed" and every failing field, each named by its path in the body and the rule it
// broke, and that nothing of a rejected import is stored.
func TestBulkCreateNotesValidation(t *testing.T) {
	e := newTestRouter(t)

	// One body breaks a rule in three notes at once: every one of them has to be reported.
	severalBody := bulkBody(t, []bulkNoteInput{
		{Title: "   "},
		{Title: "ok", Body: strings.Repeat("b", 10001)},
		{Body: "no title"},
	})
	severalFields := []bulkFieldError{
		{Field: "notes[0].title", Rule: "notblank"},
		{Field: "notes[1].body", Rule: "max"},
		{Field: "notes[2].title", Rule: "required"},
	}

	tests := []struct {
		name   string
		body   string
		fields []bulkFieldError
	}{
		{"missing title in the third note", bulkBody(t, []bulkNoteInput{{Title: "a"}, {Title: "b"}, {Body: "c"}}), []bulkFieldError{{Field: "notes[2].title", Rule: "required"}}},
		{"title of only spaces", bulkBody(t, []bulkNoteInput{{Title: "   "}}), []bulkFieldError{{Field: "notes[0].title", Rule: "notblank"}}},
		{"title too long", bulkBody(t, []bulkNoteInput{{Title: strings.Repeat("a", 201)}}), []bulkFieldError{{Field: "notes[0].title", Rule: "max"}}},
		{"body too long", bulkBody(t, []bulkNoteInput{{Title: "a", Body: strings.Repeat("b", 10001)}}), []bulkFieldError{{Field: "notes[0].body", Rule: "max"}}},
		{"absent notes field", `{}`, []bulkFieldError{{Field: "notes", Rule: "min"}}},
		{"null notes", `{"notes":null}`, []bulkFieldError{{Field: "notes", Rule: "min"}}},
		{"empty notes", `{"notes":[]}`, []bulkFieldError{{Field: "notes", Rule: "min"}}},
		{"51 notes is one too many", bulkBody(t, validBulkNotes(51)), []bulkFieldError{{Field: "notes", Rule: "max"}}},
		{"several rules broken in several notes", severalBody, severalFields},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, e, http.MethodPost, bulkPath, tt.body)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), `"error":"validation failed"`)

			var body bulkValidationErrorBody
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, "validation failed", body.Error)
			assert.ElementsMatch(t, tt.fields, body.Fields, "every failing field, not only the first")

			assert.Empty(t, loadNotes(t, e, "/notes"), "a rejected import stores no note")
		})
	}

	// A body that is not JSON at all keeps the repo's existing error shape, without a fields key.
	rec := do(t, e, http.MethodPost, bulkPath, `{"notes":`)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"error":"invalid JSON body"}`, rec.Body.String())
	assert.Empty(t, loadNotes(t, e, "/notes"), "a malformed request stores no note")
}

// TestBulkCreateNotesAllOrNothing checks that the inserts share one transaction: when storing the
// second note fails, the first one is rolled back with it and the request answers 500.
func TestBulkCreateNotesAllOrNothing(t *testing.T) {
	db, err := database.Open(database.MemoryDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	e := NewRouter(db)

	// The trigger aborts the insert of the note titled "boom", which is the second note below.
	_, err = db.Exec(`CREATE TRIGGER fail_boom BEFORE INSERT ON notes WHEN NEW.title = 'boom' BEGIN SELECT RAISE(ABORT, 'boom'); END;`)
	require.NoError(t, err)

	payload, err := json.Marshal(bulkNotesRequest{Notes: []bulkNoteInput{
		{Title: "first", Body: "a"},
		{Title: "boom", Body: "b"},
	}})
	require.NoError(t, err)

	rec := do(t, e, http.MethodPost, bulkPath, string(payload))
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"error":"creating notes"}`, rec.Body.String())

	assert.Empty(t, loadNotes(t, e, "/notes"), "the first insert was rolled back with the second")

	rec = do(t, e, http.MethodGet, "/notes/1", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"error":"note not found"}`, rec.Body.String())
}
