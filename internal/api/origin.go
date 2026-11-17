package api

import (
	"context"
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

// adminOrigin is v for an administrator and nil for anyone else: how a title
// came in (the list's name, the person who added it) is for administrators
// only, like the line under a download.
func adminOrigin(r *http.Request, v *originView) *originView {
	if !isAdminRequest(r) {
		return nil
	}
	return v
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

// origin is what the list adds and starts: its name as it is now, and its id,
// which is what its history counts by (the name can change, or be shared).
func (l importList) origin() origin.Origin {
	return origin.Origin{Kind: origin.List, Name: l.Name, ListID: l.ID}
}

type grabByCtxKey struct{}

// withGrabBy says who is behind the search run under ctx: the downloads that
// run starts are attributed to o instead of the scheduled search. A search a
// person starts, or an import list starts for what it just added, is not a
// scheduled one. It travels with the run itself, so two runs for the same
// title at once each keep their own, and a scheduled search or RSS check that
// happens meanwhile is not credited to either.
func withGrabBy(ctx context.Context, o origin.Origin) context.Context {
	return context.WithValue(ctx, grabByCtxKey{}, o)
}

// grabByFrom is who withGrabBy put on ctx; empty when nobody did.
func grabByFrom(ctx context.Context) origin.Origin {
	o, _ := ctx.Value(grabByCtxKey{}).(origin.Origin)
	return o
}

// grabOrigin works out the origin of a download. When the caller knows who
// asked (by: the person, the list or the job behind the run), that is it.
// Otherwise a release a person picked is a manual one, a search started by
// hand is a search, and anything else is the scheduled job's.
func (s *Server) grabOrigin(kind grabKind, by origin.Origin, job string) origin.Origin {
	if by.Known() {
		return by
	}
	if kind == grabPicked {
		return origin.Origin{Kind: origin.Manual}
	}
	if kind == grabSearchNow {
		return origin.Origin{Kind: origin.Search, UserID: by.UserID, User: by.User}
	}
	if job == "" {
		job = origin.JobSearch
	}
	return origin.Origin{Kind: origin.Auto, Name: job}
}
