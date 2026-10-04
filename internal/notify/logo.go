package notify

import _ "embed"

// logoCID is the Content-ID the email's HTML uses to show the logo.
const logoCID = "mediarium-logo@mediarium"

// logoPNG is the full Mediarium logo, sent inside every HTML email.
//
//go:embed assets/mediarium-logo.png
var logoPNG []byte
