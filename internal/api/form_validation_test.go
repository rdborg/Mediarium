package api_test

import (
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

// badRequestCase is one request that must be refused with a 400 whose message
// contains want, so a direct API call cannot save what the form would refuse.
type badRequestCase struct {
	name   string
	method string
	path   string
	body   map[string]any
	want   string
}

func runBadRequests(t *testing.T, client *http.Client, base string, cases []badRequestCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := postJSONMethod[map[string]any](t, client, tc.method, base+tc.path, tc.body, http.StatusBadRequest)
			msg, _ := got["error"].(string)
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("message = %q, want it to contain %q", msg, tc.want)
			}
		})
	}
}

func TestOnboardingAdminIsChecked(t *testing.T) {
	_, srv, client := newHuntTestServer(t)
	ok := func(over map[string]any) map[string]any {
		body := map[string]any{"username": "ryan", "password": "correct-horse-battery-staple"}
		for k, v := range over {
			body[k] = v
		}
		return body
	}
	runBadRequests(t, client, srv.URL, []badRequestCase{
		{"short username", "POST", "/api/onboarding/admin", ok(map[string]any{"username": "ab"}), "3 to 32 characters"},
		{"long username", "POST", "/api/onboarding/admin", ok(map[string]any{"username": strings.Repeat("a", 33)}), "3 to 32 characters"},
		{"username with a space", "POST", "/api/onboarding/admin", ok(map[string]any{"username": "ryan b"}), "letters, numbers, dots"},
		{"blank username", "POST", "/api/onboarding/admin", ok(map[string]any{"username": "  "}), "Choose a username"},
		{"short password", "POST", "/api/onboarding/admin", ok(map[string]any{"password": "short"}), "at least 8 characters"},
		{"huge password", "POST", "/api/onboarding/admin", ok(map[string]any{"password": strings.Repeat("a", 80)}), "at most 72"},
		{"bad email", "POST", "/api/onboarding/admin", ok(map[string]any{"email": "ryan@localhost"}), "name@example.com"},
		{"very long name", "POST", "/api/onboarding/admin", ok(map[string]any{"name": strings.Repeat("n", 101)}), "at most 100"},
	})
}

func TestAccountFormsAreChecked(t *testing.T) {
	server, base, admin, _, memberID := familyServer(t)
	users := base + "/api/users"
	them := "/api/users/" + ftoa(memberID)
	newUser := func(over map[string]any) map[string]any {
		body := map[string]any{"username": "alex", "password": "long-enough-pw", "role": "member"}
		for k, v := range over {
			body[k] = v
		}
		return body
	}
	_ = users
	runBadRequests(t, admin, base, []badRequestCase{
		{"new account, short username", "POST", "/api/users", newUser(map[string]any{"username": "al"}), "3 to 32 characters"},
		{"new account, odd characters", "POST", "/api/users", newUser(map[string]any{"username": "al;ex"}), "letters, numbers, dots"},
		{"new account, no password", "POST", "/api/users", newUser(map[string]any{"password": ""}), "Choose a password"},
		{"new account, bad email", "POST", "/api/users", newUser(map[string]any{"email": "alex@@example.com"}), "name@example.com"},
		{"new account, no role", "POST", "/api/users", newUser(map[string]any{"role": ""}), "Pick a role"},
		{"edit account, bad email", "PUT", them, map[string]any{"email": "nope"}, "name@example.com"},
		{"edit account, long name", "PUT", them, map[string]any{"name": strings.Repeat("n", 101)}, "at most 100"},
		{"edit account, short password", "PUT", them, map[string]any{"password": "short"}, "at least 8 characters"},
		{"edit account, bad role", "PUT", them, map[string]any{"role": "owner"}, "Pick a role"},
		{"change password, too short", "POST", "/api/auth/change-password", map[string]any{"currentPassword": "correct-horse-battery-staple", "newPassword": "short"}, "at least 8 characters"},
		{"profile, blank username", "PUT", "/api/auth/profile", map[string]any{"username": "", "email": ""}, "Choose a username"},
		{"profile, changed to a bad username", "PUT", "/api/auth/profile", map[string]any{"username": "no way"}, "letters, numbers, dots"},
		{"profile, bad email", "PUT", "/api/auth/profile", map[string]any{"username": "ryan", "email": "ryan@"}, "name@example.com"},
		{"api key, no name", "POST", "/api/auth/api-keys", map[string]any{"name": "   "}, "Give the key a name"},
		{"api key, name too long", "POST", "/api/auth/api-keys", map[string]any{"name": strings.Repeat("k", 61)}, "at most 60"},
	})

	// A username an older version saved keeps working: changing only the email
	// must not trip over the stricter username rule.
	if _, err := server.Auth.CreateAccount("a b", "long-enough-pw", "Old", "", "", false); err != nil {
		t.Fatalf("create legacy account: %v", err)
	}
	jar, _ := cookiejar.New(nil)
	legacy := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	postJSON[map[string]any](t, legacy, base+"/api/auth/login", map[string]string{"username": "a b", "password": "long-enough-pw"}, http.StatusOK)
	got := putJSONStatus(t, legacy, base+"/api/auth/profile", map[string]any{"username": "a b", "email": "old@example.com"}, http.StatusOK)
	if got["email"] != "old@example.com" || got["username"] != "a b" {
		t.Fatalf("legacy profile edit: %+v", got)
	}
	putJSONStatus(t, legacy, base+"/api/auth/profile", map[string]any{"username": "still bad", "email": ""}, http.StatusBadRequest)
}

func TestUsenetServerFormIsChecked(t *testing.T) {
	_, base, client := loginNewServer(t)
	good := func(over map[string]any) map[string]any {
		body := map[string]any{"name": "Provider", "host": "news.example.com", "port": 563, "useSsl": true, "connections": 10}
		for k, v := range over {
			body[k] = v
		}
		return body
	}
	runBadRequests(t, client, base, []badRequestCase{
		{"no host", "POST", "/api/usenet-servers", good(map[string]any{"host": ""}), "Add the host name"},
		{"host with a scheme", "POST", "/api/usenet-servers", good(map[string]any{"host": "https://news.example.com"}), "without http://"},
		{"host with a port", "POST", "/api/usenet-servers", good(map[string]any{"host": "news.example.com:563"}), "Port box"},
		{"host with spaces", "POST", "/api/usenet-servers", good(map[string]any{"host": "news example"}), "can't contain spaces"},
		{"port zero", "POST", "/api/usenet-servers", good(map[string]any{"port": 0}), "between 1 and 65535"},
		{"port too big", "POST", "/api/usenet-servers", good(map[string]any{"port": 70000}), "between 1 and 65535"},
		{"too many connections", "POST", "/api/usenet-servers", good(map[string]any{"connections": 500}), "Connections must be a whole number between 1 and 100"},
		{"negative connections", "POST", "/api/usenet-servers", good(map[string]any{"connections": -3}), "Connections must be"},
		{"negative priority", "POST", "/api/usenet-servers", good(map[string]any{"priority": -1}), "Priority must be"},
		{"control characters in the username", "POST", "/api/usenet-servers", good(map[string]any{"username": "a\nb"}), "Username"},
	})
	created := postJSON[map[string]any](t, client, base+"/api/usenet-servers", good(nil), http.StatusCreated)
	id := ftoa(created["id"].(float64))
	putJSONStatus(t, client, base+"/api/usenet-servers/"+id, map[string]any{"port": 0}, http.StatusBadRequest)
	putJSONStatus(t, client, base+"/api/usenet-servers/"+id, map[string]any{"enabled": false}, http.StatusOK)

	// The connection test explains a bad address instead of dialling it.
	res := postJSON[map[string]any](t, client, base+"/api/usenet-servers/test", map[string]any{"host": "news.example.com:563", "port": 563}, http.StatusOK)
	if res["ok"] != false || !strings.Contains(res["message"].(string), "Port box") {
		t.Fatalf("test of a bad host: %+v", res)
	}
}

func TestMediaServerFormIsChecked(t *testing.T) {
	_, base, client := loginNewServer(t)
	good := func(over map[string]any) map[string]any {
		body := map[string]any{"kind": "plex", "baseUrl": "http://192.168.1.10:32400", "token": "abc123"}
		for k, v := range over {
			body[k] = v
		}
		return body
	}
	runBadRequests(t, client, base, []badRequestCase{
		{"no address", "POST", "/api/media-servers", good(map[string]any{"baseUrl": ""}), "Add the address of your server"},
		{"address with another scheme", "POST", "/api/media-servers", good(map[string]any{"baseUrl": "ftp://192.168.1.10"}), "http:// or https://"},
		{"address with a bad port", "POST", "/api/media-servers", good(map[string]any{"baseUrl": "http://192.168.1.10:99999"}), "between 1 and 65535"},
		{"address with a space", "POST", "/api/media-servers", good(map[string]any{"baseUrl": "http://my server"}), "can't contain spaces"},
		{"bad public address", "POST", "/api/media-servers", good(map[string]any{"publicUrl": "ftp://plex.example.com"}), "http:// or https://"},
		{"token with a space", "POST", "/api/media-servers", good(map[string]any{"token": "abc 123"}), "space or line break"},
		{"name too long", "POST", "/api/media-servers", good(map[string]any{"name": strings.Repeat("n", 101)}), "at most 100"},
		{"unknown type", "POST", "/api/media-servers", good(map[string]any{"kind": "kodi"}), "Plex, Jellyfin, Emby, Audiobookshelf or Kavita"},
		{"relative folder mapping", "POST", "/api/media-servers", good(map[string]any{"pathMap": []map[string]string{{"from": "media/movies", "to": "/data/movies"}}}), "isn't complete"},
		{"half a folder mapping", "POST", "/api/media-servers", good(map[string]any{"pathMap": []map[string]string{{"from": "/media/movies", "to": ""}}}), "needs a folder in Mediarium"},
	})
}

func TestNotificationFormIsChecked(t *testing.T) {
	_, base, client := loginNewServer(t)
	email := func(over map[string]any) map[string]any {
		cfg := map[string]any{"host": "smtp.example.com", "port": "587", "security": "starttls", "from": "me@example.com", "to": "you@example.com"}
		for k, v := range over {
			cfg[k] = v
		}
		return map[string]any{"type": "email", "config": cfg}
	}
	runBadRequests(t, client, base, []badRequestCase{
		{"email, no server", "POST", "/api/notifications", email(map[string]any{"host": ""}), "SMTP server can't be left empty"},
		{"email, server is a URL", "POST", "/api/notifications", email(map[string]any{"host": "https://smtp.example.com"}), "without http://"},
		{"email, server has a port", "POST", "/api/notifications", email(map[string]any{"host": "smtp.example.com:587"}), "Port box"},
		{"email, port zero", "POST", "/api/notifications", email(map[string]any{"port": "0"}), "between 1 and 65535"},
		{"email, port is text", "POST", "/api/notifications", email(map[string]any{"port": "smtp"}), "between 1 and 65535"},
		{"email, username without password", "POST", "/api/notifications", email(map[string]any{"username": "me"}), "both the username and the password"},
		{"email, bad from", "POST", "/api/notifications", email(map[string]any{"from": "nobody"}), "From address doesn't look right"},
		{"email, bad recipient", "POST", "/api/notifications", email(map[string]any{"to": "you@example.com, oops"}), "\"oops\" doesn't look like an email address"},
		{"email, no recipient", "POST", "/api/notifications", email(map[string]any{"to": " , "}), "Add at least one email address"},
		{"gotify, url without a scheme", "POST", "/api/notifications", map[string]any{"type": "gotify", "config": map[string]any{"url": "gotify.example.com", "token": "abc"}}, "Start the address with http://"},
		{"gotify, token with a space", "POST", "/api/notifications", map[string]any{"type": "gotify", "config": map[string]any{"url": "https://gotify.example.com", "token": "a b"}}, "App token: That key has a space"},
		{"discord, ftp url", "POST", "/api/notifications", map[string]any{"type": "discord", "config": map[string]any{"url": "ftp://discord.example.com/hook"}}, "http:// or https://"},
		{"webhook, no host", "POST", "/api/notifications", map[string]any{"type": "webhook", "config": map[string]any{"url": "http://"}}, "missing a server name"},
		{"telegram, chat id is text", "POST", "/api/notifications", map[string]any{"type": "telegram", "config": map[string]any{"botToken": "123:abc", "chatId": "my chat"}}, "chat ID should be a number"},
		{"ntfy, topic with a slash", "POST", "/api/notifications", map[string]any{"type": "ntfy", "config": map[string]any{"topic": "a/b"}}, "topic can't contain"},
		{"pushover, missing key", "POST", "/api/notifications", map[string]any{"type": "pushover", "config": map[string]any{"token": "abc"}}, "User key can't be left empty"},
	})
	// The same rules apply to the test button, which must not send anything.
	runBadRequests(t, client, base, []badRequestCase{
		{"test of a bad email setup", "POST", "/api/notifications/test", email(map[string]any{"port": "0"}), "between 1 and 65535"},
	})
	// Valid setups still save, including a channel name and a group id for Telegram.
	for _, chat := range []string{"123456789", "-1001234567890", "@mychannel"} {
		postJSON[map[string]any](t, client, base+"/api/notifications", map[string]any{
			"type": "telegram", "config": map[string]any{"botToken": "123:abc", "chatId": chat},
		}, http.StatusCreated)
	}
	postJSON[map[string]any](t, client, base+"/api/notifications", email(nil), http.StatusCreated)
}
