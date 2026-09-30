package api

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/inputcheck"
)

// Shared checks for values a person types into a form. They mirror
// web/src/validate.ts, so the server says the same thing the page already
// said, and a direct API call cannot save what the page would have refused.
//
// Every check returns "" when the value is fine or a short, friendly message
// that goes straight into a 400 response. A blank value is fine for every
// check except checkRequired, so optional fields can use the same call.
//
// These run when something is written. Values saved by older versions are
// never re-checked when they are read.

const (
	usernameMin = 3
	usernameMax = 32
	passwordMin = 8
	nameMax     = 100
)

// firstProblem returns the first non-empty message, or "".
func firstProblem(msgs ...string) string {
	for _, m := range msgs {
		if m != "" {
			return m
		}
	}
	return ""
}

// rejectBad writes the first problem as a 400 and reports whether there was one.
func rejectBad(w http.ResponseWriter, msgs ...string) bool {
	if m := firstProblem(msgs...); m != "" {
		writeError(w, http.StatusBadRequest, m)
		return true
	}
	return false
}

// checkRequired complains with the given message when v is blank.
func checkRequired(v, message string) string {
	if strings.TrimSpace(v) == "" {
		return message
	}
	return ""
}

// checkMaxLen keeps text fields to a sensible size (counted in characters).
func checkMaxLen(v, label string, max int) string {
	if utf8.RuneCountInString(strings.TrimSpace(v)) > max {
		return fmt.Sprintf("%s can be at most %d characters long.", label, max)
	}
	return ""
}

// checkNoControl refuses control characters (NUL, newlines and so on).
func checkNoControl(v, label string) string {
	if strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return fmt.Sprintf("%s can't contain hidden or control characters.", label)
	}
	return ""
}

// checkEmail: one @, something on both sides, a dot in the domain. Loose on
// purpose; it catches typos, not every RFC quirk.
func checkEmail(v string) string { return inputcheck.Email(v) }

var usernameChars = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// checkUsername is for a new username (or one being changed). Names that older
// versions saved are not re-checked.
func checkUsername(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "Choose a username to sign in with."
	}
	if n := utf8.RuneCountInString(v); n < usernameMin || n > usernameMax {
		return fmt.Sprintf("A username needs to be %d to %d characters long.", usernameMin, usernameMax)
	}
	if !usernameChars.MatchString(v) {
		return "Usernames can only use letters, numbers, dots, dashes and underscores."
	}
	return ""
}

// checkPassword is for a new password. label is "Password" or "New password".
func checkPassword(v, label string) string {
	if len(v) == 0 {
		return fmt.Sprintf("Choose a %s.", strings.ToLower(label))
	}
	if utf8.RuneCountInString(v) < passwordMin {
		return fmt.Sprintf("%ss need at least %d characters.", label, passwordMin)
	}
	if len(v) > auth.MaxPasswordBytes {
		return fmt.Sprintf("%ss can be at most %d characters long.", label, auth.MaxPasswordBytes)
	}
	return ""
}

// checkPort wants a whole number from 1 to 65535.
func checkPort(n int, label string) string {
	if n < 1 || n > 65535 {
		return label + " must be a number between 1 and 65535."
	}
	return ""
}

// checkIntRange wants a whole number from min to max, both included.
func checkIntRange(n int, label string, min, max int) string {
	if n < min || n > max {
		return fmt.Sprintf("%s must be a whole number between %d and %d.", label, min, max)
	}
	return ""
}

// checkAtLeast wants a whole number of at least min.
func checkAtLeast(n int, label string, min int) string {
	if n < min {
		if min == 0 {
			return label + " can't be negative."
		}
		return fmt.Sprintf("%s must be a whole number of %d or more.", label, min)
	}
	return ""
}

// checkHost wants a bare host name or IP address: no scheme, path or port.
func checkHost(v, example string) string { return inputcheck.Host(v, example) }

// checkHTTPURL wants an http or https web address with a server name in it.
// With requireScheme false an address typed without http:// is accepted, as
// the forms that add the scheme themselves do.
func checkHTTPURL(v, example string, requireScheme bool) string {
	return inputcheck.HTTPURL(v, example, requireScheme)
}

// checkAbsPath wants an absolute folder path: /..., D:\..., or \\server\share.
func checkAbsPath(v, example string) string { return inputcheck.AbsPath(v, example) }

// checkAPIKey wants one unbroken string, the way keys and tokens are.
func checkAPIKey(v string) string { return inputcheck.APIKey(v) }

// validEmail reports whether an address passes checkEmail's format rule.
func validEmail(v string) bool { return inputcheck.ValidEmail(v) }
