package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unknownNoteID is a canonical lowercase hyphenated UUID that no test ever stores, so a request
// naming it answers 404 note not found.
const unknownNoteID = "00000000-0000-4000-8000-000000000000"

// canonicalUUID is a canonical lowercase hyphenated UUID v4, used to spell the near misses a note id
// must reject: its uppercase, hyphenless, braced and urn: forms are all valid UUIDs and none of
// them is a note id.
const canonicalUUID = "3f2a6c1e-0b7d-4a52-9c8e-6d1f4b2a7c30"

// requireCanonicalID fails unless id is a canonical lowercase hyphenated UUID. uuid.Parse accepts
// more spellings than the routes do, so the value is written back and compared with what came in.
func requireCanonicalID(t *testing.T, id string) {
	t.Helper()
	parsed, err := uuid.Parse(id)
	require.NoError(t, err, "the id %q is a UUID", id)
	require.Equal(t, id, parsed.String(), "the id %q is in canonical lowercase hyphenated form", id)
	require.Len(t, id, 36, "the id %q is 36 characters", id)
}

// idRoute is one route carrying an :id path parameter, together with the method and the body a
// request against it uses. Every rule about the id is checked on all of them.
type idRoute struct {
	name   string
	method string
	path   func(id string) string
	body   string
}

// idRoutes is every route with an :id: the note itself, its tags, and the two transitions.
func idRoutes() []idRoute {
	return []idRoute{
		{name: "get", method: http.MethodGet, path: notePath},
		{name: "put", method: http.MethodPut, path: notePath, body: `{"title":"x"}`},
		{name: "put tags", method: http.MethodPut, path: tagsPath, body: `{"tags":["go"]}`},
		{name: "publish", method: http.MethodPost, path: func(id string) string { return transitionPath(id, "publish") }},
		{name: "archive", method: http.MethodPost, path: func(id string) string { return transitionPath(id, "archive") }},
		{name: "delete", method: http.MethodDelete, path: notePath},
	}
}

// TestNoteIDAcceptance checks that a note route takes one spelling of an id and no other: a UUID in
// uppercase, with the hyphens left out, wrapped in braces, behind the urn:uuid: prefix, one
// character short or one too long, a plain integer and free text are all 400 invalid note id. An
// empty segment is not in the table because it never reaches a handler: Echo's router does not
// match /notes/ to /notes/:id, so it answers its own 404 before the id is looked at.
func TestNoteIDAcceptance(t *testing.T) {
	rejected := []struct {
		name string
		id   string
	}{
		{"uppercase", strings.ToUpper(canonicalUUID)},
		{"hyphens left out", strings.ReplaceAll(canonicalUUID, "-", "")},
		{"braces", "{" + canonicalUUID + "}"},
		{"urn form", "urn:uuid:" + canonicalUUID},
		{"one character short", canonicalUUID[:len(canonicalUUID)-1]},
		{"one character too long", canonicalUUID + "0"},
		{"integer", "1"},
		{"free text", "abc"},
	}
	for _, route := range idRoutes() {
		for _, tt := range rejected {
			t.Run(route.name+" "+tt.name, func(t *testing.T) {
				rec := do(t, newTestRouter(t), route.method, route.path(tt.id), route.body)
				assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
				assert.JSONEq(t, `{"error":"invalid note id"}`, rec.Body.String())
			})
		}
	}
}

// TestUnknownUUIDIsNotFound checks that a canonical UUID no note has is a 404 note not found on
// every route carrying an :id, and that PUT answers that 404 even when the body is invalid too.
func TestUnknownUUIDIsNotFound(t *testing.T) {
	for _, route := range idRoutes() {
		t.Run(route.name, func(t *testing.T) {
			rec := do(t, newTestRouter(t), route.method, route.path(unknownNoteID), route.body)
			assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
			assert.JSONEq(t, `{"error":"note not found"}`, rec.Body.String())
		})
	}

	// The id is checked and the note loaded before the body is looked at, on both write routes.
	for _, body := range []string{`{}`, `{"title":""}`, `{"title":"` + strings.Repeat("a", 201) + `"}`, `{"title":`} {
		rec := do(t, newTestRouter(t), http.MethodPut, notePath(unknownNoteID), body)
		assert.Equal(t, http.StatusNotFound, rec.Code, body)
		assert.JSONEq(t, `{"error":"note not found"}`, rec.Body.String(), body)
	}
	for _, body := range []string{`{}`, `{"tags":["GO!"]}`, `{"tags":`} {
		rec := do(t, newTestRouter(t), http.MethodPut, tagsPath(unknownNoteID), body)
		assert.Equal(t, http.StatusNotFound, rec.Code, body)
		assert.JSONEq(t, `{"error":"note not found"}`, rec.Body.String(), body)
	}
}

// TestIDIsAStringInEveryResponse checks that every response carrying a note carries its id as a
// JSON string in the canonical lowercase hyphenated form, never as a number and never in another
// spelling. The bodies are decoded into map[string]any, so a numeric id shows up as a float64.
func TestIDIsAStringInEveryResponse(t *testing.T) {
	e := newTestRouter(t)

	created := createWithStatus(t, e, `{"title":"first","body":"a"}`)
	requireCanonicalID(t, created.ID)

	payload, err := json.Marshal(bulkNotesRequest{Notes: []bulkNoteInput{{Title: "imported"}}})
	require.NoError(t, err)
	bulkRec := do(t, e, http.MethodPost, bulkPath, string(payload))
	require.Equal(t, http.StatusCreated, bulkRec.Code, bulkRec.Body.String())
	var imported bulkNotesResponse
	require.NoError(t, json.Unmarshal(bulkRec.Body.Bytes(), &imported))
	require.Len(t, imported.Notes, 1)
	requireCanonicalID(t, imported.Notes[0].ID)

	responses := []struct {
		name   string
		method string
		target string
		body   string
		status int
	}{
		{"create", http.MethodPost, "/notes", `{"title":"another"}`, http.StatusCreated},
		{"get", http.MethodGet, notePath(created.ID), "", http.StatusOK},
		{"put", http.MethodPut, notePath(created.ID), `{"title":"renamed","body":"b"}`, http.StatusOK},
		{"put tags", http.MethodPut, tagsPath(created.ID), `{"tags":["go"]}`, http.StatusOK},
		{"publish", http.MethodPost, transitionPath(created.ID, "publish"), "", http.StatusOK},
		{"archive", http.MethodPost, transitionPath(created.ID, "archive"), "", http.StatusOK},
	}
	for _, tt := range responses {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, e, tt.method, tt.target, tt.body)
			require.Equal(t, tt.status, rec.Code, rec.Body.String())

			var body map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			id, ok := body["id"].(string)
			require.True(t, ok, "the id is a JSON string: %s", rec.Body.String())
			requireCanonicalID(t, id)
		})
	}

	// The list carries one id per note, all of them strings in the same form.
	rec := do(t, e, http.MethodGet, "/notes", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.NotEmpty(t, list)
	for _, note := range list {
		id, ok := note["id"].(string)
		require.True(t, ok, "every listed note carries a string id: %s", rec.Body.String())
		requireCanonicalID(t, id)
	}
}
