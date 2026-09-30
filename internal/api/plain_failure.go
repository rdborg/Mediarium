package api

import "github.com/rdborg/mediarium/internal/plainerror"

// plainFailure makes the text of a "failed" activity line readable. Lines
// written before the raw text was kept out of them still carry network errors;
// any other kind of line is passed on as it is.
func plainFailure(kind, message string) string {
	if kind != "failed" {
		return message
	}
	return plainerror.Text(message)
}
