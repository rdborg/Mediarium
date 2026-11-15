package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/origin"
)

// Where a title or a download came from (#190, #191). A title keeps how it
// came into the library (the person is the account that added it); a download
// keeps who or what started it. Both are shown as "Added by ..." and are empty
// for anything from before they were kept.

type originCtxKey struct{}

// withOrigin tells the add handlers that the request is not a person adding a
// title by hand, but an import list (or another part of Mediarium) doing it.
func withOrigin(ctx context.Context, o origin.Origin) context.Context {
	return context.WithValue(ctx, originCtxKey{}, o)
}

// addOrigin is how a title added by this request came in: what the context
// says, or a person adding it by hand.
func addOrigin(r *http.Request) origin.Origin {
	if o, ok := r.Context().Value(originCtxKey{}).(origin.Origin); ok {
		return o
	}
	return origin.Origin{Kind: origin.Manual}
}

// userOrigin is the signed-in person behind a request, as an origin of kind.
func userOrigin(r *http.Request, kind string) origin.Origin {
	o := origin.Origin{Kind: kind}
	if u := auth.UserFromContext(r.Context()); u != nil && u.ID > 0 {
		o.UserID, o.User = u.ID, u.DisplayName()
	}
	return o
}

// originView is an origin as the API gives it. User is the account behind it.
type originView struct {
	Kind string   `json:"kind"`
	Name string   `json:"name,omitempty"`
	User *userRef `json:"user,omitempty"`
}

// originOut turns a stored origin into what the API returns: nil when nothing
// was recorded. A title's person is looked up from the account that added it
// (who); a download carries its own, so a removed account still shows.
func originOut(o origin.Origin, who *userRef) *originView {
	if !o.Known() {
		return nil
	}
	v := &originView{Kind: o.Kind, Name: o.Name, User: who}
	if v.User == nil && o.UserID > 0 && o.User != "" {
		v.User = &userRef{ID: o.UserID, Name: o.User}
	}
	return v
}

// hintKey names a title in the hints below.
func hintKey(tv bool, id int64) string {
	if tv {
		return fmt.Sprintf("tv:%d", id)
	}
	return fmt.Sprintf("movie:%d", id)
}

// hintGrabs says who is behind the searches that run for a title until the
// returned function is called: the downloads they start are attributed to o
// instead of the scheduled search. A search a person starts, or an import list
// starts for what it just added, is not a scheduled one.
func (s *Server) hintGrabs(tv bool, id int64, o origin.Origin) (done func()) {
	key := hintKey(tv, id)
	s.grabHints.Store(key, o)
	return func() { s.grabHints.Delete(key) }
}

// grabOrigin works out the origin of a download. When the caller knows who
// asked (by), that is it. Otherwise a release a person picked is a manual one,
// and anything else is attributed to whoever asked for the search, when that
// is known (see hintGrabs), or to the scheduled job.
func (s *Server) grabOrigin(kind grabKind, by origin.Origin, tv bool, titleID int64, job string) origin.Origin {
	if by.Known() {
		return by
	}
	if kind == grabPicked {
		return origin.Origin{Kind: origin.Manual}
	}
	if v, ok := s.grabHints.Load(hintKey(tv, titleID)); ok {
		if o, ok := v.(origin.Origin); ok {
			return o
		}
	}
	if kind == grabSearchNow {
		return origin.Origin{Kind: origin.Search, UserID: by.UserID, User: by.User}
	}
	if job == "" {
		job = origin.JobSearch
	}
	return origin.Origin{Kind: origin.Auto, Name: job}
}
