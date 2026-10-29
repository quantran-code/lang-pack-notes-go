package httpapi

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bulkBody is the JSON body of a bulk request carrying the given notes.
func bulkBody(t *testing.T, notes []bulkNoteInput) string {
	t.Helper()
	payload, err := json.Marshal(bulkNotesRequest{Notes: notes})
	require.NoError(t, err)
	return string(payload)
}

// validBulkNotes returns count notes that break no rule.
func validBulkNotes(count int) []bulkNoteInput {
	notes := make([]bulkNoteInput, 0, count)
	for i := 0; i < count; i++ {
		notes = append(notes, bulkNoteInput{Title: "note-" + strconv.Itoa(i), Body: "body"})
	}
	return notes
}

// validateBulkBody unmarshals a request body and returns the fields the bulk validator reports for
// it, which is exactly what the handler answers with.
func validateBulkBody(t *testing.T, body string) []bulkFieldError {
	t.Helper()
	var req bulkNotesRequest
	require.NoError(t, json.Unmarshal([]byte(body), &req))
	return bulkFields(bulkValidator.Struct(req))
}

// TestBulkFields checks what the bulk validator reports: every failing field and not only the first,
// the JSON path of each one, the raw rule name, and the boundaries of the note count.
func TestBulkFields(t *testing.T) {
	t.Run("every failing field is reported, not only the first", func(t *testing.T) {
		body := bulkBody(t, []bulkNoteInput{
			{Title: "   ", Body: "a title of spaces"},
			{Title: "ok", Body: strings.Repeat("b", 10001)},
			{Body: "the title is missing"},
		})
		assert.ElementsMatch(t, []bulkFieldError{
			{Field: "notes[0].title", Rule: "notblank"},
			{Field: "notes[1].body", Rule: "max"},
			{Field: "notes[2].title", Rule: "required"},
		}, validateBulkBody(t, body), "one entry per failing field")
	})

	tests := []struct {
		name string
		body string
		want []bulkFieldError
	}{
		{"one valid note", bulkBody(t, validBulkNotes(1)), nil},
		{"50 valid notes", bulkBody(t, validBulkNotes(50)), nil},
		{"title of exactly 200 characters", bulkBody(t, []bulkNoteInput{{Title: strings.Repeat("a", 200)}}), nil},
		{"body of exactly 10000 characters", bulkBody(t, []bulkNoteInput{{Title: "t", Body: strings.Repeat("b", 10000)}}), nil},
		{"missing title", bulkBody(t, []bulkNoteInput{{Body: "b"}}), []bulkFieldError{{Field: "notes[0].title", Rule: "required"}}},
		{"title of spaces", bulkBody(t, []bulkNoteInput{{Title: "   "}}), []bulkFieldError{{Field: "notes[0].title", Rule: "notblank"}}},
		{"title too long", bulkBody(t, []bulkNoteInput{{Title: strings.Repeat("a", 201)}}), []bulkFieldError{{Field: "notes[0].title", Rule: "max"}}},
		{"body too long", bulkBody(t, []bulkNoteInput{{Title: "t", Body: strings.Repeat("b", 10001)}}), []bulkFieldError{{Field: "notes[0].body", Rule: "max"}}},
		{"absent notes", `{}`, []bulkFieldError{{Field: "notes", Rule: "min"}}},
		{"null notes", `{"notes":null}`, []bulkFieldError{{Field: "notes", Rule: "min"}}},
		{"empty notes", `{"notes":[]}`, []bulkFieldError{{Field: "notes", Rule: "min"}}},
		{"51 notes is one too many", bulkBody(t, validBulkNotes(51)), []bulkFieldError{{Field: "notes", Rule: "max"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ElementsMatch(t, tt.want, validateBulkBody(t, tt.body))
		})
	}

	t.Run("no validation error is no field", func(t *testing.T) {
		assert.Empty(t, bulkFields(nil))
		assert.Empty(t, bulkFields(errors.New("not a validation error")))
	})
}

// TestBulkValidationErrorBodyJSON checks the 400 body of a rejected bulk request: the message and
// one entry per failing field, each with its field path and rule.
func TestBulkValidationErrorBodyJSON(t *testing.T) {
	data, err := json.Marshal(bulkValidationErrorBody{
		Error:  "validation failed",
		Fields: []bulkFieldError{{Field: "notes[2].title", Rule: "required"}},
	})
	require.NoError(t, err)
	assert.Equal(t,
		`{"error":"validation failed","fields":[{"field":"notes[2].title","rule":"required"}]}`,
		string(data))
}
