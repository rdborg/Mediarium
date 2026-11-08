package api

import "testing"

func TestLanguageInCategory(t *testing.T) {
	cases := map[string]string{
		"EBook (DE)":             "German",
		"Hörbücher Deutsch":      "German",
		"Books/French":           "French",
		"Books/Ebook [ES]":       "Spanish",
		"Ebook ITA":              "",
		"Books/Mags":             "",
		"IT Books":               "", // computers, not Italian: a two-letter code counts only in brackets
		"Ebook GER":              "German",
		"Comics":                 "",
		"Audiobook Nederlands":   "Dutch",
		"Books/Technical (dev)":  "",
		"E-Books Italiano":       "Italian",
		"Books/Foreign":          "",
		"Hörbücher (FR)":         "French",
		"Ebook (de)":             "German", // in brackets, any case
		"Books mit deutscher UT": "",
	}
	for name, want := range cases {
		if got := languageInCategory(name); got != want {
			t.Errorf("languageInCategory(%q) = %q, want %q", name, got, want)
		}
	}
}
