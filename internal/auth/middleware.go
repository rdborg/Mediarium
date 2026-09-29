package auth

import (
	"context"
	"net/http"
	"time"
)

const CookieName = "mediarium_session"

type contextKey string

const userContextKey contextKey = "auth_user"

// SetSessionCookie writes a signed-by-opacity (random, server-side-looked-up)
// HTTP-only session cookie.
func SetSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// Middleware resolves the caller's session cookie or X-API-Key header into
// a *User in the request context, or rejects with 401.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.userFromRequest(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) userFromRequest(r *http.Request) (*User, error) {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return s.UserForAPIKey(key)
	}
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return nil, ErrSessionNotFound
	}
	return s.UserForSession(cookie.Value)
}

// UserFromContext retrieves the authenticated user set by Middleware.
func UserFromContext(ctx context.Context) *User {
	user, _ := ctx.Value(userContextKey).(*User)
	return user
}
