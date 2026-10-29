package httpapi

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

// bulkNoteInput is one note of a bulk import: only a title and a body, because every imported note
// is created as a draft. The rules live in the tags — a required title of at most 200 characters
// that is not only spaces, and a body of at most 10000 characters — rather than in hand-written
// checks, which is why this is a type of its own instead of noteRequest.
type bulkNoteInput struct {
	Title string `json:"title" validate:"required,max=200,notblank"`
	Body  string `json:"body" validate:"max=10000"`
}

// bulkNotesRequest is the body accepted when several notes are imported at once: 1 to 50 notes.
// Notes deliberately has no required tag, so an absent field, an explicit null and an empty array
// all report the same min rule rather than required for one of them.
type bulkNotesRequest struct {
	Notes []bulkNoteInput `json:"notes" validate:"min=1,max=50,dive"`
}

// bulkNotesResponse is the body answered by a successful bulk import: the stored notes in request
// order, each one the same shape GET /notes/:id would answer with.
type bulkNotesResponse struct {
	Notes []Note `json:"notes"`
}

// bulkFieldError is one field of a bulk request that broke a rule: the field's path in the body,
// such as notes[2].title, and the name of the rule it broke.
type bulkFieldError struct {
	Field string `json:"field"`
	Rule  string `json:"rule"`
}

// bulkValidationErrorBody is the body answered when a bulk request breaks one or more rules: every
// failing field, not only the first.
type bulkValidationErrorBody struct {
	Error  string           `json:"error"`
	Fields []bulkFieldError `json:"fields"`
}

// bulkValidator validates the body of POST /notes/bulk. It is built once, at start-up, and only
// read afterwards, so sharing it between requests is safe.
var bulkValidator = newBulkValidator()

// newBulkValidator returns the validator behind POST /notes/bulk. Field errors report the JSON name
// of the field, so a failure reads notes[2].title and not bulkNotesRequest.Notes[2].Title, and the
// notblank rule rejects a title that is only spaces.
func newBulkValidator() *validator.Validate {
	v := validator.New()
	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
		if name == "" {
			return field.Name
		}
		return name
	})
	// The rule is registered once, on this instance only: no code path mutates a shared validator.
	_ = v.RegisterValidation("notblank", notBlank)
	return v
}

// notBlank reports whether a field is not only spaces. It is the rule a title of "   " breaks,
// which required on its own would accept.
func notBlank(fl validator.FieldLevel) bool {
	return strings.TrimSpace(fl.Field().String()) != ""
}

// bulkFields turns the errors of validating a bulk request into the fields of the 400 body: one
// entry per failing field, in the order validator reports them, each with its field path and the
// raw rule name (required, max, min, notblank). A nil error, or one that is not a validation error,
// has no fields.
func bulkFields(err error) []bulkFieldError {
	if err == nil {
		return nil
	}
	var validationErrs validator.ValidationErrors
	if !errors.As(err, &validationErrs) {
		return nil
	}
	fields := make([]bulkFieldError, 0, len(validationErrs))
	for _, fieldErr := range validationErrs {
		fields = append(fields, bulkFieldError{Field: bulkFieldPath(fieldErr), Rule: fieldErr.Tag()})
	}
	return fields
}

// bulkFieldPath is the path of a failing field inside the request body, such as notes[2].title.
// validator prefixes every path with the name of the struct it validated, which is not part of the
// body, so it is cut off; the index inside notes and the JSON field name are kept.
func bulkFieldPath(fieldErr validator.FieldError) string {
	root := reflect.TypeOf(bulkNotesRequest{}).Name() + "."
	return strings.TrimPrefix(fieldErr.Namespace(), root)
}

// bulkCreateNotes handles POST /notes/bulk. The body is checked against the validator tags before
// anything is written, so a rejected request stores nothing; the notes it does accept are then
// inserted inside one transaction, so a failure part-way leaves the table exactly as it was. Every
// imported note is a draft with no tags, and the response lists them in request order.
func bulkCreateNotes(db *sqlx.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		var req bulkNotesRequest
		if err := c.Bind(&req); err != nil {
			return writeError(c, http.StatusBadRequest, "invalid JSON body")
		}
		if err := bulkValidator.Struct(req); err != nil {
			return c.JSON(http.StatusBadRequest, bulkValidationErrorBody{
				Error:  "validation failed",
				Fields: bulkFields(err),
			})
		}

		tx, err := db.BeginTxx(ctx, nil)
		if err != nil {
			return writeError(c, http.StatusInternalServerError, "creating notes")
		}
		defer func() { _ = tx.Rollback() }()

		// Every note is built the way createNote builds one: title trimmed, body as sent, status a
		// draft and an empty rather than nil tag set so it marshals as [].
		notes := make([]Note, 0, len(req.Notes))
		for _, input := range req.Notes {
			note := Note{
				Title:     strings.TrimSpace(input.Title),
				Body:      input.Body,
				CreatedAt: time.Now().UTC(),
				Status:    StatusDraft,
				Tags:      []string{},
			}
			result, err := tx.NamedExecContext(ctx,
				"INSERT INTO notes (title, body, created_at, status) VALUES (:title, :body, :created_at, :status)", note)
			if err != nil {
				return writeError(c, http.StatusInternalServerError, "creating notes")
			}
			id, err := result.LastInsertId()
			if err != nil {
				return writeError(c, http.StatusInternalServerError, "creating notes")
			}
			note.ID = id
			notes = append(notes, note)
		}

		if err := tx.Commit(); err != nil {
			return writeError(c, http.StatusInternalServerError, "creating notes")
		}
		return c.JSON(http.StatusCreated, bulkNotesResponse{Notes: notes})
	}
}
