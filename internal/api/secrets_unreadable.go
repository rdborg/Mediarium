package api

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/rdborg/mediarium/internal/settings"
)

// A saved password or key that can't be decrypted (the secret.key is not the one
// the database was saved with) used to make a whole list fail, or to read back
// as "not set" with no word about it. Every place that keeps secrets now flags
// the row that can't be read instead (the Settings pages show it), leaves what
// is stored alone, and the lists below put each of them on Health.

// savedSettingNames are the encrypted settings people know by name.
var savedSettingNames = map[string]string{
	settings.KeyTMDBAPIKey:             "the TMDB key",
	settings.KeyTraktClientID:          "the Trakt key",
	settings.KeySimklClientID:          "the Simkl key",
	settings.KeySimklToken:             "the Simkl sign-in",
	settings.KeySimklRefresh:           "the Simkl sign-in",
	settings.KeyHardcoverToken:         "the Hardcover token",
	settings.KeyTVDBAPIKey:             "the TheTVDB key",
	settings.KeyTVDBPIN:                "the TheTVDB PIN",
	settings.KeyDebridAPIKey:           "the debrid service key",
	settings.KeyDownloadClientPassword: "the download client password",
	settings.KeyUsenetClientPassword:   "the Usenet downloader password",
	settings.KeyUsenetClientAPIKey:     "the Usenet downloader API key",
	settings.KeyOpenSubtitlesPassword:  "the OpenSubtitles password",
	settings.KeyOpenSubtitlesAPIKey:    "the OpenSubtitles key",
	settings.KeySubDLAPIKey:            "the SubDL key",
	settings.KeySubtitleAddons:         "the subtitle add-ons",
	settings.KeyBackupCloud:            "the cloud backup login",
	settings.KeyAuthOIDC:               "the single sign-on secret",
	settings.KeyAuthLDAP:               "the directory sign-in password",
}

// unreadableSettings names the encrypted settings that can't be read, sorted.
func (s *Server) unreadableSettings() []string {
	rows, err := s.db.Query(`SELECT key, value FROM settings WHERE encrypted = 1 AND value <> ''`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var key, value string
		if rows.Scan(&key, &value) != nil {
			continue
		}
		if key == settings.KeyPushVAPID {
			continue // made again by itself when it can't be read
		}
		if _, err := s.box.Decrypt(value); err != nil {
			name := savedSettingNames[key]
			if name == "" {
				name = "the setting " + key
			}
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// unreadableSecretItems are the Health items for every saved secret that can't
// be read, outside the indexers (which have their own).
func (s *Server) unreadableSecretItems() []healthItem {
	var items []healthItem
	addHealth := func(id, level, title, impact, label, path string) {
		it := healthItem{ID: id, Level: level, Title: title, Impact: impact}
		if label != "" {
			it.Action = &healthAction{Label: label, Path: path}
		}
		items = append(items, it)
	}
	if all, err := s.ClientRepo.ListAll(); err == nil {
		for _, sc := range all {
			if sc.SecretUnreadable {
				addHealth(fmt.Sprintf("usenet-secret-unreadable-%d", sc.ID), "error", fmt.Sprintf("%s has a password that can't be read", sc.Name),
					fmt.Sprintf("Mediarium can't read the saved password of %s, so it isn't used for downloads. Open it and enter the password again.", sc.Name),
					"Check Usenet servers", "/settings/usenet")
			}
		}
	}
	if all, err := s.VPNRepo.List(); err == nil {
		for _, c := range all {
			if c.SecretUnreadable {
				addHealth(fmt.Sprintf("vpn-secret-unreadable-%d", c.ID), "error", fmt.Sprintf("%s has keys that can't be read", c.Label),
					fmt.Sprintf("Mediarium can't read the saved keys of %s, so it can't be switched on. Remove it and add the config again.", c.Label),
					"Open VPN settings", "/settings/vpn")
			}
		}
	}
	if s.MediaServers != nil {
		if all, err := s.MediaServers.ListAll(); err == nil {
			for _, m := range all {
				if m.SecretUnreadable {
					addHealth(fmt.Sprintf("media-server-secret-unreadable-%d", m.ID), "warn", fmt.Sprintf("%s has a token that can't be read", m.Name),
						fmt.Sprintf("Mediarium can't read the saved token of %s, so it isn't asked to scan or checked for watched titles. Sign in again or enter the token again.", m.Name),
						"Check media servers", "/settings/media-servers")
				}
			}
		}
	}
	if all, err := s.NotifyRepo.ListAll(); err == nil {
		for _, t := range all {
			if t.SecretUnreadable {
				addHealth(fmt.Sprintf("notify-secret-unreadable-%d", t.ID), "warn", fmt.Sprintf("%s has passwords that can't be read", t.Name),
					fmt.Sprintf("Mediarium can't read the saved passwords of %s, so nothing is sent through it. Open it and enter them again.", t.Name),
					"Check notifications", "/settings/notifications")
			}
		}
	}
	if names := s.unreadableSettings(); len(names) > 0 {
		addHealth("settings-secrets-unreadable", "error", fmt.Sprintf("%d saved keys or passwords can't be read", len(names)),
			fmt.Sprintf("Mediarium treats these as not set until you enter them again: %s.", strings.Join(names, ", ")),
			"", "")
	}
	return items
}

// LogUnreadableSecrets writes one warning to the log for every kind of saved
// secret that can't be read. It never writes a secret. Called once at start.
func (s *Server) LogUnreadableSecrets() {
	for _, it := range s.unreadableSecretItems() {
		slog.Warn("a saved secret can't be read; re-enter it in Settings", "what", it.Title, "health", it.ID)
	}
}
