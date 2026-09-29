package mediaservers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Signing in to Jellyfin and Emby, instead of pasting an API key: with a
// username and password (both), or with Jellyfin's Quick Connect code. Either
// way the server hands back an access token, which is what gets saved. The
// password itself is sent once to the server and never kept.

// PublicInfo is what a Jellyfin or Emby server tells anyone who asks,
// without signing in (GET /System/Info/Public).
type PublicInfo struct {
	Kind    Kind
	Name    string
	Version string
	ID      string
}

type embyPublicInfo struct {
	ServerName  string `json:"ServerName"`
	Version     string `json:"Version"`
	ID          string `json:"Id"`
	ProductName string `json:"ProductName"` // "Jellyfin Server" on Jellyfin; absent on Emby
}

// kind tells Jellyfin from Emby: Jellyfin names itself in ProductName, and
// failing that its versions are 10.x while Emby's are 4.x and older.
func (p embyPublicInfo) kind() Kind {
	prod := strings.ToLower(p.ProductName)
	switch {
	case strings.Contains(prod, "jellyfin"):
		return KindJellyfin
	case prod != "":
		return KindEmby
	case strings.HasPrefix(p.Version, "10."):
		return KindJellyfin
	}
	return KindEmby
}

// authResult is the answer to AuthenticateByName and AuthenticateWithQuickConnect.
type authResult struct {
	AccessToken string `json:"AccessToken"`
	ServerID    string `json:"ServerId"`
	User        struct {
		Name   string `json:"Name"`
		Policy struct {
			IsAdministrator bool `json:"IsAdministrator"`
		} `json:"Policy"`
	} `json:"User"`
}

type quickConnectState struct {
	Secret        string `json:"Secret"`
	Code          string `json:"Code"`
	Authenticated bool   `json:"Authenticated"`
}

// mbCall is do for the sign-in calls: they carry the app's MediaBrowser
// header (with this install's device id) and no token yet.
func (c *Client) mbCall(ctx context.Context, s Server, deviceID, method, path string, query url.Values, body, out any) error {
	base, err := NormalizeURL(s.BaseURL)
	if err != nil {
		return userErr(err, "The server address is not valid: %v", err)
	}
	req, err := newRequest(ctx, s.Kind, method, base, path, query, body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mediarium/"+c.Version)
	h := mediaBrowserAuth(deviceID, c.Version, s.Token)
	req.Header.Set("Authorization", h)
	req.Header.Set("X-Emby-Authorization", h) // Emby reads this one
	return c.exchange(req, s, base, out)
}

// PublicInfo asks a Jellyfin or Emby server who it is. kind is what the
// person chose; a server of the other kind is a UserError saying so.
func (c *Client) PublicInfo(ctx context.Context, kind Kind, baseURL string) (PublicInfo, error) {
	s := Server{Kind: kind, BaseURL: baseURL}
	var info embyPublicInfo
	if err := c.do(ctx, s, http.MethodGet, "/System/Info/Public", nil, nil, &info); err != nil {
		if errors.Is(err, errUnauthorized) { // this page is open on every Jellyfin and Emby
			err = errUnrecognised
		}
		return PublicInfo{}, explain(s, err)
	}
	if info.ID == "" || info.Version == "" {
		return PublicInfo{}, explain(s, errUnrecognised)
	}
	got := info.kind()
	if got != kind {
		return PublicInfo{}, userErr(errWrongKind, "This is %s server, not %s. Choose %s as the server type.", withArticle(got), kind.Label(), got.Label())
	}
	return PublicInfo{Kind: got, Name: info.ServerName, Version: info.Version, ID: info.ID}, nil
}

func withArticle(k Kind) string {
	if k == KindEmby {
		return "an " + k.Label()
	}
	return "a " + k.Label()
}

// serverFromAuth turns a successful sign-in into a server ready to save.
func serverFromAuth(kind Kind, base string, info PublicInfo, res authResult) (Server, error) {
	if res.AccessToken == "" {
		return Server{}, userErr(errUnrecognised, "%s accepted the sign-in but sent no access token back. Use an API key instead.", kind.Label())
	}
	if !res.User.Policy.IsAdministrator {
		return Server{}, userErr(nil, "Signed in, but %q is not an administrator on this %s server. Mediarium needs an administrator account to ask the server to rescan its libraries. Sign in with an administrator account instead.", res.User.Name, kind.Label())
	}
	name := info.Name
	if name == "" {
		name = kind.Label()
	}
	id := info.ID
	if id == "" {
		id = res.ServerID
	}
	return Server{
		Name: name, Kind: kind, BaseURL: base, Token: res.AccessToken,
		Enabled: true, RefreshAfterImport: true, ServerID: id, PathMap: []PathMapping{},
	}, nil
}

// SignIn signs in to Jellyfin or Emby with a username and password and
// returns the server, holding the access token it got, ready to save. The
// password is not kept anywhere. deviceID identifies this install.
func (c *Client) SignIn(ctx context.Context, kind Kind, baseURL, deviceID, username, password string) (Server, error) {
	if kind != KindJellyfin && kind != KindEmby {
		return Server{}, userErr(nil, "Signing in with a username and password works for Jellyfin and Emby. For Plex, use Sign in with Plex.")
	}
	base, err := NormalizeURL(baseURL)
	if err != nil {
		return Server{}, userErr(err, "The server address is not valid: %v", err)
	}
	if strings.TrimSpace(username) == "" {
		return Server{}, userErr(nil, "Enter the username.")
	}
	info, err := c.PublicInfo(ctx, kind, base)
	if err != nil {
		return Server{}, err
	}
	s := Server{Kind: kind, BaseURL: base}
	var res authResult
	err = c.mbCall(ctx, s, deviceID, http.MethodPost, "/Users/AuthenticateByName", nil,
		map[string]string{"Username": strings.TrimSpace(username), "Pw": password}, &res)
	if errors.Is(err, errUnauthorized) {
		return Server{}, userErr(err, "Wrong username or password.")
	}
	if err != nil {
		return Server{}, explain(s, err)
	}
	return serverFromAuth(kind, base, info, res)
}

var (
	// ErrQuickConnectExpired means the Quick Connect code is no longer valid.
	ErrQuickConnectExpired = errors.New("quick connect code expired")
	errWrongKind           = errors.New("another kind of media server")
)

// QuickConnectStart asks a Jellyfin server for a Quick Connect code. The
// person enters code in a Jellyfin app they are signed in to; secret is how
// Mediarium then asks whether they did, and must stay on the server side.
func (c *Client) QuickConnectStart(ctx context.Context, baseURL, deviceID string) (base, code, secret string, err error) {
	base, err = NormalizeURL(baseURL)
	if err != nil {
		return "", "", "", userErr(err, "The server address is not valid: %v", err)
	}
	if _, err := c.PublicInfo(ctx, KindJellyfin, base); err != nil {
		if errors.Is(err, errWrongKind) {
			return "", "", "", userErr(err, "Quick Connect is a Jellyfin feature. For Emby, sign in with a username and password.")
		}
		return "", "", "", err
	}
	s := Server{Kind: KindJellyfin, BaseURL: base}
	disabled := userErr(nil, "Quick Connect is turned off on this Jellyfin server. Turn it on in Jellyfin under Dashboard, General, Quick Connect, or sign in with a username and password instead.")
	var enabled bool
	err = c.mbCall(ctx, s, deviceID, http.MethodGet, "/QuickConnect/Enabled", nil, nil, &enabled)
	switch {
	case errors.Is(err, errNotFound):
		return "", "", "", userErr(err, "This Jellyfin version has no Quick Connect. Sign in with a username and password instead.")
	case err != nil:
		return "", "", "", explain(s, err)
	case !enabled:
		return "", "", "", disabled
	}
	var st quickConnectState
	err = c.mbCall(ctx, s, deviceID, http.MethodPost, "/QuickConnect/Initiate", nil, nil, &st)
	var se *statusError
	if errors.Is(err, errNotFound) || (errors.As(err, &se) && se.code == http.StatusMethodNotAllowed) {
		// Jellyfin before 10.8 started Quick Connect with a GET.
		err = c.mbCall(ctx, s, deviceID, http.MethodGet, "/QuickConnect/Initiate", nil, nil, &st)
	}
	if errors.Is(err, errUnauthorized) {
		return "", "", "", disabled
	}
	if err != nil {
		return "", "", "", explain(s, err)
	}
	if st.Secret == "" || st.Code == "" {
		return "", "", "", explain(s, errUnrecognised)
	}
	return base, st.Code, st.Secret, nil
}

// QuickConnectCheck reports whether the code has been approved yet. An
// expired code is ErrQuickConnectExpired.
func (c *Client) QuickConnectCheck(ctx context.Context, base, deviceID, secret string) (bool, error) {
	s := Server{Kind: KindJellyfin, BaseURL: base}
	var st quickConnectState
	err := c.mbCall(ctx, s, deviceID, http.MethodGet, "/QuickConnect/Connect", url.Values{"secret": {secret}}, nil, &st)
	if errors.Is(err, errNotFound) {
		return false, ErrQuickConnectExpired
	}
	if err != nil {
		return false, explain(s, err)
	}
	return st.Authenticated, nil
}

// QuickConnectFinish trades an approved code for an access token and returns
// the server ready to save.
func (c *Client) QuickConnectFinish(ctx context.Context, base, deviceID, secret string) (Server, error) {
	info, err := c.PublicInfo(ctx, KindJellyfin, base)
	if err != nil {
		return Server{}, err
	}
	s := Server{Kind: KindJellyfin, BaseURL: base}
	var res authResult
	err = c.mbCall(ctx, s, deviceID, http.MethodPost, "/Users/AuthenticateWithQuickConnect", nil, map[string]string{"Secret": secret}, &res)
	if errors.Is(err, errUnauthorized) || errors.Is(err, errNotFound) {
		return Server{}, userErr(ErrQuickConnectExpired, "The Quick Connect code was not accepted or has expired. Start again.")
	}
	if err != nil {
		return Server{}, explain(s, err)
	}
	return serverFromAuth(KindJellyfin, base, info, res)
}
