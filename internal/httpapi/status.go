package httpapi

import (
	"encoding/json"
	"errors"
	"strings"
)

//go:generate stringer -type=NoteStatus -linecomment

// NoteStatus is where a note stands in its life: a new note is a draft, publishing it makes it
// visible, and archiving retires it. The database stores the integer, while JSON carries the
// lowercase name, accepted in any case on the way in.
type NoteStatus int

const (
	StatusDraft     NoteStatus = 0 // draft
	StatusPublished NoteStatus = 1 // published
	StatusArchived  NoteStatus = 2 // archived
)

// errInvalidStatus is the one error an unknown status name produces, so every route answers the
// same message however the value reached it.
var errInvalidStatus = errors.New("invalid status")

// parseNoteStatus turns a status name into its NoteStatus, matching case-insensitively, so
// "published", "Published" and "PUBLISHED" are the same status. Nothing else is a status, the
// empty string included.
func parseNoteStatus(name string) (NoteStatus, error) {
	for _, status := range []NoteStatus{StatusDraft, StatusPublished, StatusArchived} {
		if strings.EqualFold(name, status.String()) {
			return status, nil
		}
	}
	return StatusDraft, errInvalidStatus
}

// MarshalJSON writes the status as its lowercase name, so a note carries "published" and not 1.
func (s NoteStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON reads a status name in any case. A name that is not one of the three statuses is
// errInvalidStatus.
func (s *NoteStatus) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	status, err := parseNoteStatus(name)
	if err != nil {
		return err
	}
	*s = status
	return nil
}
