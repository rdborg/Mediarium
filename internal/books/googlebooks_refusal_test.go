package books

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A 403 from Google is a refused key only when it says so: the same status
// carries "rate limit exceeded", and a shared key-less quota runs out often.
func TestGoogleBooksRefusalsAreToldApart(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"bad key", 400, `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","errors":[{"reason":"badRequest"}]}}`, ErrGoogleBooksKey},
		{"api not enabled", 403, `{"error":{"code":403,"errors":[{"reason":"accessNotConfigured"}]}}`, ErrGoogleBooksKey},
		{"rate limit", 403, `{"error":{"code":403,"errors":[{"domain":"usageLimits","reason":"rateLimitExceeded"}]}}`, ErrGoogleBooksQuota},
		{"daily limit", 403, `{"error":{"code":403,"message":"Daily Limit Exceeded","errors":[{"reason":"dailyLimitExceeded"}]}}`, ErrGoogleBooksQuota},
		{"quota in words", 403, `{"error":{"message":"Quota exceeded for quota metric 'Queries' and limit 'Queries per day'"}}`, ErrGoogleBooksQuota},
		{"too many requests", 429, ``, ErrGoogleBooksQuota},
		{"server error", 500, ``, errGoogleBooksDown},
		{"nothing said", 403, ``, ErrGoogleBooksKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			g := NewGoogleBooks("k-12345678", "test")
			g.Base = srv.URL
			if _, err := g.Search(context.Background(), "dune", "en", 5); !errors.Is(err, tt.want) {
				t.Errorf("search: %v, want %v", err, tt.want)
			}
			if err := g.Check(context.Background()); !errors.Is(err, tt.want) {
				t.Errorf("check: %v, want %v", err, tt.want)
			}
			if _, err := g.Book(context.Background(), "abcdefghijkl"); !errors.Is(err, tt.want) {
				t.Errorf("book: %v, want %v", err, tt.want)
			}
		})
	}
}
