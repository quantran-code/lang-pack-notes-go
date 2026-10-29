package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealth(t *testing.T) {
	rec := do(t, newTestRouter(t), http.MethodGet, "/health", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestCreateNote(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status int
		errMsg string
	}{
		{"valid", `{"title":"Buy milk","body":"2 litres"}`, http.StatusCreated, ""},
		{"body is optional", `{"title":"Just a title"}`, http.StatusCreated, ""},
		{"title is trimmed and may be 200 characters", `{"title":"` + strings.Repeat("a", 200) + `"}`, http.StatusCreated, ""},
		{"missing title", `{"body":"x"}`, http.StatusBadRequest, "title is required"},
		{"blank title", `{"title":"   "}`, http.StatusBadRequest, "title is required"},
		{"title too long", `{"title":"` + strings.Repeat("a", 201) + `"}`, http.StatusBadRequest, "title must be at most 200 characters"},
		{"malformed JSON", `{"title":`, http.StatusBadRequest, "invalid JSON body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, newTestRouter(t), http.MethodPost, "/notes", tt.body)
			require.Equal(t, tt.status, rec.Code, rec.Body.String())
			if tt.errMsg != "" {
				assert.JSONEq(t, `{"error":"`+tt.errMsg+`"}`, rec.Body.String())
				return
			}
			var note Note
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &note))
			requireCanonicalID(t, note.ID)
			assert.False(t, note.CreatedAt.IsZero())
		})
	}
}

func TestGetAndListNotes(t *testing.T) {
	e := newTestRouter(t)

	rec := do(t, e, http.MethodGet, "/notes", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `[]`, rec.Body.String(), "an empty list is [] and not null")

	require.Equal(t, http.StatusCreated, do(t, e, http.MethodPost, "/notes", `{"title":"first","body":"a"}`).Code)
	rec = do(t, e, http.MethodPost, "/notes", `{"title":"second"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created Note
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	requireCanonicalID(t, created.ID)

	var notes []Note
	rec = do(t, e, http.MethodGet, "/notes", "")
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &notes))
	require.Len(t, notes, 2)
	assert.Equal(t, "first", notes[0].Title)
	assert.Equal(t, "second", notes[1].Title)

	// The second note is fetched by the id the create response gave it.
	rec = do(t, e, http.MethodGet, notePath(created.ID), "")
	require.Equal(t, http.StatusOK, rec.Code)
	var note Note
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &note))
	assert.Equal(t, "second", note.Title)
	assert.Equal(t, created.ID, note.ID)
}

func TestGetNoteErrors(t *testing.T) {
	tests := []struct {
		name   string
		target string
		status int
		errMsg string
	}{
		{"missing", notePath(unknownNoteID), http.StatusNotFound, "note not found"},
		{"not a UUID", "/notes/abc", http.StatusBadRequest, "invalid note id"},
		{"integer", "/notes/0", http.StatusBadRequest, "invalid note id"},
		{"uppercase UUID", notePath(strings.ToUpper(canonicalUUID)), http.StatusBadRequest, "invalid note id"},
		{"unknown path", "/nope", http.StatusNotFound, "Not Found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, newTestRouter(t), http.MethodGet, tt.target, "")
			assert.Equal(t, tt.status, rec.Code)
			assert.JSONEq(t, `{"error":"`+tt.errMsg+`"}`, rec.Body.String())
		})
	}
}
