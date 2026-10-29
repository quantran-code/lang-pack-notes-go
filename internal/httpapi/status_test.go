package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoteStatusString checks the names stringer generates: the lowercase words that travel in
// JSON, and the numbers the database stores.
func TestNoteStatusString(t *testing.T) {
	tests := []struct {
		name   string
		status NoteStatus
		value  int
		str    string
	}{
		{"draft", StatusDraft, 0, "draft"},
		{"published", StatusPublished, 1, "published"},
		{"archived", StatusArchived, 2, "archived"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.value, int(tt.status), "the value stored in the database")
			assert.Equal(t, tt.str, tt.status.String())
		})
	}
}

// TestNoteStatusJSON checks the round trip: the status marshals as its lowercase name, and every
// casing of that name parses back to the same status.
func TestNoteStatusJSON(t *testing.T) {
	for _, status := range []NoteStatus{StatusDraft, StatusPublished, StatusArchived} {
		t.Run(status.String(), func(t *testing.T) {
			data, err := json.Marshal(status)
			require.NoError(t, err)
			assert.Equal(t, `"`+status.String()+`"`, string(data), "the status is its lowercase name")

			name := status.String()
			title := strings.ToUpper(name[:1]) + name[1:]
			for _, accepted := range []string{name, title, strings.ToUpper(name)} {
				var parsed NoteStatus
				require.NoError(t, json.Unmarshal([]byte(`"`+accepted+`"`), &parsed), accepted)
				assert.Equal(t, status, parsed, "%q parses as %s", accepted, name)

				got, err := parseNoteStatus(accepted)
				require.NoError(t, err, accepted)
				assert.Equal(t, status, got, "%q parses as %s", accepted, name)
			}
		})
	}
}

// TestNoteStatusInvalid checks that nothing but the three names is a status, for parseNoteStatus
// and for the JSON decoding a request body goes through alike.
func TestNoteStatusInvalid(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"unknown name", "live"},
		{"empty string", ""},
		{"plural", "drafts"},
		{"abbreviation", "pub"},
		{"leading space", " draft"},
		{"trailing space", "draft "},
		{"number", "1"},
		{"generated default", "NoteStatus(1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseNoteStatus(tt.value)
			require.Error(t, err)
			assert.ErrorIs(t, err, errInvalidStatus)

			var status NoteStatus
			err = json.Unmarshal([]byte(`"`+tt.value+`"`), &status)
			require.Error(t, err)
			assert.ErrorIs(t, err, errInvalidStatus)
		})
	}
}
