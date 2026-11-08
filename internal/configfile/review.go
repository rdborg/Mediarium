package configfile

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ReviewSection is one block or list item and what the file would do to it.
type ReviewSection struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"` // added, changed, removed, unchanged
	Changes []Change `json:"changes,omitempty"`
}

// Review is what checking a file found: what would be added, changed,
// removed or stay as it is, and anything that stops it being applied.
type Review struct {
	FormatVersion  int             `json:"formatVersion"`
	Replace        bool            `json:"replace"`
	Sections       []ReviewSection `json:"sections"`
	Added          int             `json:"added"`
	Changed        int             `json:"changed"`
	Removed        int             `json:"removed"`
	Unchanged      int             `json:"unchanged"`
	MissingSecrets []string        `json:"missingSecrets"` // passwords a new item needs that the file doesn't have
	Problems       []string        `json:"problems"`       // each stops the file being applied
	CanApply       bool            `json:"canApply"`
}

// Check compares the file with the running Mediarium and writes nothing.
func Check(ctx context.Context, c *Client, f File, opts Options) *Review {
	rv := &Review{FormatVersion: f.Version, Replace: opts.Replace, Sections: []ReviewSection{}, MissingSecrets: []string{}, Problems: []string{}}
	if rv.FormatVersion == 0 {
		rv.FormatVersion = FormatVersion
	}
	p, err := build(ctx, c, f, opts)
	if err != nil {
		var pe *ProblemsError
		if errors.As(err, &pe) {
			rv.Problems = pe.Problems()
		} else {
			rv.Problems = []string{err.Error()}
		}
		return rv
	}
	for _, b := range p.blocks {
		switch {
		case b.Err != nil:
			rv.Problems = append(rv.Problems, b.Err.Error())
		case len(b.Changes) > 0:
			rv.Sections = append(rv.Sections, ReviewSection{Name: b.Section, Status: "changed", Changes: b.Changes})
			rv.Changed++
		}
	}
	for _, name := range p.blockUnchanged {
		rv.Sections = append(rv.Sections, ReviewSection{Name: name, Status: "unchanged"})
		rv.Unchanged++
	}
	for _, lr := range p.lists.results {
		switch {
		case lr.Err != nil:
			rv.Problems = append(rv.Problems, lr.Err.Error())
		case len(lr.Changes) > 0 && lr.Changes[0].New:
			rv.Sections = append(rv.Sections, ReviewSection{Name: lr.Section, Status: "added", Changes: lr.Changes})
			rv.Added++
		case len(lr.Changes) > 0:
			rv.Sections = append(rv.Sections, ReviewSection{Name: lr.Section, Status: "changed", Changes: lr.Changes})
			rv.Changed++
		}
	}
	for _, lr := range p.lists.removals {
		rv.Sections = append(rv.Sections, ReviewSection{Name: lr.Section, Status: "removed", Changes: lr.Changes})
		rv.Removed++
	}
	for _, name := range p.lists.unchanged {
		rv.Sections = append(rv.Sections, ReviewSection{Name: name, Status: "unchanged"})
		rv.Unchanged++
	}
	rv.MissingSecrets = append(rv.MissingSecrets, p.lists.missing...)
	rv.CanApply = len(rv.Problems) == 0 && rv.Added+rv.Changed+rv.Removed > 0
	return rv
}

// Outcome is what an apply did.
type Outcome struct {
	Applied    bool     `json:"applied"`
	Changes    int      `json:"changes"`    // blocks and items changed
	RolledBack bool     `json:"rolledBack"` // something went wrong and what had been done was put back
	Error      string   `json:"error,omitempty"`
	NotUndone  []string `json:"notUndone,omitempty"` // what could not be put back, one sentence each
}

type undoStep struct {
	what string
	run  func(context.Context, *Client) error
}

// ApplyAll makes the running Mediarium match the file, all or nothing: the
// changes are made one after another and, when one is refused, the ones
// already made are put back in the opposite order. Removals (replace mode) go
// last. A password or key that was set can't be put back, since Mediarium
// never shows it; the outcome says so when that matters.
func ApplyAll(ctx context.Context, c *Client, f File, opts Options) (*Outcome, error) {
	p, err := build(ctx, c, f, opts)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, b := range p.blocks {
		if b.Err != nil {
			problems = append(problems, b.Err.Error())
		}
	}
	for _, lr := range p.lists.results {
		if lr.Err != nil {
			problems = append(problems, lr.Err.Error())
		}
	}
	if len(problems) > 0 {
		return nil, &ProblemsError{Invalid: problems}
	}
	out := &Outcome{}
	var done []undoStep
	lost := 0

	fail := func(err error) (*Outcome, error) {
		out.Error = err.Error()
		out.RolledBack = true
		// A fresh context: the one that was running may be what ran out.
		uctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for i := len(done) - 1; i >= 0; i-- {
			if done[i].run == nil {
				out.NotUndone = append(out.NotUndone, done[i].what+" was changed and can't be put back.")
				continue
			}
			if err := done[i].run(uctx, c); err != nil {
				out.NotUndone = append(out.NotUndone, fmt.Sprintf("%s was changed and putting it back failed: %s", done[i].what, strings.TrimSpace(err.Error())))
			}
		}
		if lost > 0 {
			out.NotUndone = append(out.NotUndone, fmt.Sprintf("%d password or key you set stays set: Mediarium never shows them, so they can't be put back.", lost))
		}
		return out, nil
	}

	for _, b := range p.blocks {
		if len(b.Changes) == 0 {
			continue
		}
		if _, err := c.do(ctx, http.MethodPut, b.path, b.body); err != nil {
			return fail(fmt.Errorf("%s: %w", b.Section, err))
		}
		cur, path := b.cur, b.path
		done = append(done, undoStep{what: b.Section, run: func(ctx context.Context, c *Client) error {
			_, err := c.do(ctx, http.MethodPut, path, cur)
			return err
		}})
		lost += b.secrets
		out.Changes++
	}
	steps := append(append([]listResult{}, p.lists.results...), p.lists.removals...)
	for _, lr := range steps {
		if len(lr.Changes) == 0 {
			continue
		}
		for _, act := range lr.actions {
			undo, err := act(ctx, c)
			if undo != nil || err == nil {
				done = append(done, undoStep{what: lr.Section, run: undo})
			}
			if err != nil {
				return fail(fmt.Errorf("%s: %w", lr.Section, err))
			}
		}
		lost += lr.secrets
		out.Changes++
	}
	out.Applied = true
	return out, nil
}
