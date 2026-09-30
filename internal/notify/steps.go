package notify

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/logbuf"
)

// Step is one line of the log a test message leaves: what Mediarium did, when,
// and whether it worked.
type Step struct {
	Time time.Time `json:"time"`
	Text string    `json:"text"`
	OK   bool      `json:"ok"`
}

// Steps collects the lines of one test. The senders write into it through the
// context (see WithSteps), so sending a real notification, where no one is
// listening, costs nothing. A nil *Steps ignores everything.
type Steps struct {
	mu      sync.Mutex
	list    []Step
	secrets []string
	now     func() time.Time
}

// NewSteps starts an empty log. Every secret given is blanked out of each line
// that is added, on top of the general clean-up the app's log gets.
func NewSteps(secrets ...string) *Steps {
	s := &Steps{now: time.Now}
	for _, v := range secrets {
		if len(strings.TrimSpace(v)) >= 3 {
			s.secrets = append(s.secrets, v)
		}
	}
	return s
}

// Add writes one line. ok false marks it as the point where things went wrong.
func (s *Steps) Add(ok bool, format string, args ...any) {
	if s == nil {
		return
	}
	text := oneLine(fmt.Sprintf(format, args...))
	for _, secret := range s.secrets {
		text = strings.ReplaceAll(text, secret, "•••")
	}
	text = logbuf.Scrub(text)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.list = append(s.list, Step{Time: s.now(), Text: text, OK: ok})
}

// List returns the lines so far, in order.
func (s *Steps) List() []Step {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Step(nil), s.list...)
}

type stepsKey struct{}

// WithSteps makes the senders that get ctx write their progress into steps.
func WithSteps(ctx context.Context, steps *Steps) context.Context {
	return context.WithValue(ctx, stepsKey{}, steps)
}

// stepsFrom returns the log in ctx, or nil (which ignores writes).
func stepsFrom(ctx context.Context) *Steps {
	s, _ := ctx.Value(stepsKey{}).(*Steps)
	return s
}
