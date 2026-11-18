package indexers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Definition files an administrator added themselves, for a site the shared
// list doesn't have or a fixed copy of one it does (Settings > Finding releases >
// Indexers & Search > Add a definition file). They live in their own folder, apart from
// the downloaded list, which is replaced on every refresh. They're listed
// with the others and win over a shared definition with the same id.

// maxCustomDefinitionBytes is far more than any real definition needs.
const maxCustomDefinitionBytes = 1 << 20

// ErrBadCustomDefinition wraps every reason a file is turned away, with a
// message fit to show.
var ErrBadCustomDefinition = errors.New("bad definition file")

type customSet struct {
	defs []DefinitionSummary
	gen  string
}

// customLocked reads the custom folder once and keeps the result until a
// file is added or removed. Callers hold s.mu.
func (s *DefinitionStore) customLocked() *customSet {
	if s.custom != nil {
		return s.custom
	}
	set := &customSet{}
	s.custom = set
	if s.CustomDir == "" {
		return set
	}
	entries, err := os.ReadDir(s.CustomDir)
	if err != nil {
		return set
	}
	h := sha256.New()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".yml") || !definitionFileRe.MatchString(name) {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() > maxCustomDefinitionBytes {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.CustomDir, name))
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00", name, info.Size(), info.ModTime().UnixNano())
		id := strings.TrimSuffix(name, ".yml")
		def, err := ParseDefinition(data)
		if err != nil || def.ID != id {
			problem := "the file can't be read"
			if err == nil {
				problem = "the id inside the file doesn't match its name"
			}
			set.defs = append(set.defs, DefinitionSummary{
				ID: id, Name: id, Links: []string{}, Settings: []SettingSummary{}, Protocol: string(ProtocolTorrent),
				Problem: problem, Custom: true,
			})
			continue
		}
		sum := summarize(def)
		sum.Custom = true
		set.defs = append(set.defs, sum)
	}
	sort.Slice(set.defs, func(i, j int) bool { return strings.ToLower(set.defs[i].Name) < strings.ToLower(set.defs[j].Name) })
	if len(set.defs) > 0 {
		set.gen = hex.EncodeToString(h.Sum(nil))[:16]
	}
	return set
}

func findCustom(set *customSet, id string) (DefinitionSummary, bool) {
	for _, d := range set.defs {
		if d.ID == id {
			return d, true
		}
	}
	return DefinitionSummary{}, false
}

// withCustom returns the catalogue with the custom definitions in it. The
// cached index itself is never changed.
func withCustom(idx *DefinitionIndex, set *customSet) *DefinitionIndex {
	if len(set.defs) == 0 {
		return idx
	}
	out := &DefinitionIndex{}
	if idx != nil {
		*out = *idx
	}
	mine := map[string]bool{}
	for _, d := range set.defs {
		mine[d.ID] = true
	}
	out.Definitions = make([]DefinitionSummary, 0, len(out.Definitions)+len(set.defs))
	if idx != nil {
		for _, d := range idx.Definitions {
			if !mine[d.ID] {
				out.Definitions = append(out.Definitions, d)
			}
		}
	}
	out.Definitions = append(out.Definitions, set.defs...)
	sort.Slice(out.Definitions, func(i, j int) bool {
		return strings.ToLower(out.Definitions[i].Name) < strings.ToLower(out.Definitions[j].Name)
	})
	return out
}

// CustomDefinitions lists the definition files an administrator added.
func (s *DefinitionStore) CustomDefinitions() []DefinitionSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]DefinitionSummary(nil), s.customLocked().defs...)
}

// AddCustom checks a definition file and keeps it, replacing an earlier
// file with the same id. It returns what the site list will show.
func (s *DefinitionStore) AddCustom(data []byte) (DefinitionSummary, error) {
	if s.CustomDir == "" {
		return DefinitionSummary{}, fmt.Errorf("%w: this server has no folder for definition files", ErrBadCustomDefinition)
	}
	if len(data) == 0 {
		return DefinitionSummary{}, fmt.Errorf("%w: the file is empty", ErrBadCustomDefinition)
	}
	if len(data) > maxCustomDefinitionBytes {
		return DefinitionSummary{}, fmt.Errorf("%w: the file is too large (1 MB at most)", ErrBadCustomDefinition)
	}
	if !utf8.Valid(data) {
		return DefinitionSummary{}, fmt.Errorf("%w: it isn't a text file", ErrBadCustomDefinition)
	}
	def, err := ParseDefinition(data)
	if err != nil {
		return DefinitionSummary{}, fmt.Errorf("%w: it isn't a definition Mediarium can read (%s)", ErrBadCustomDefinition, firstLine(err.Error()))
	}
	if !definitionFileRe.MatchString(def.ID) || len(def.ID) > 100 {
		return DefinitionSummary{}, fmt.Errorf("%w: its id may only use letters, numbers, dots, dashes and underscores", ErrBadCustomDefinition)
	}
	if len(def.Links) == 0 {
		return DefinitionSummary{}, fmt.Errorf("%w: it has no site address (links)", ErrBadCustomDefinition)
	}
	for _, l := range def.Links {
		if !webAddress(l) {
			return DefinitionSummary{}, fmt.Errorf("%w: %q isn't an http or https address", ErrBadCustomDefinition, l)
		}
	}
	sum := summarize(def)
	if !sum.Supported {
		return DefinitionSummary{}, fmt.Errorf("%w: Mediarium can't run it yet (%s)", ErrBadCustomDefinition, sum.Problem)
	}
	sum.Custom = true

	if err := os.MkdirAll(s.CustomDir, 0o755); err != nil {
		return DefinitionSummary{}, fmt.Errorf("create %s: %w", s.CustomDir, err)
	}
	tmp, err := os.CreateTemp(s.CustomDir, ".upload-*")
	if err != nil {
		return DefinitionSummary{}, fmt.Errorf("save the definition: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return DefinitionSummary{}, fmt.Errorf("save the definition: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return DefinitionSummary{}, fmt.Errorf("save the definition: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Rename(tmp.Name(), filepath.Join(s.CustomDir, def.ID+".yml")); err != nil {
		return DefinitionSummary{}, fmt.Errorf("save the definition: %w", err)
	}
	s.custom = nil
	return sum, nil
}

// RemoveCustom deletes an added definition file.
func (s *DefinitionStore) RemoveCustom(id string) error {
	if s.CustomDir == "" || !definitionFileRe.MatchString(id) {
		return fmt.Errorf("%w: %q", ErrDefinitionNotFound, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(filepath.Join(s.CustomDir, id+".yml"))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %q", ErrDefinitionNotFound, id)
	}
	if err != nil {
		return fmt.Errorf("remove definition %q: %w", id, err)
	}
	s.custom = nil
	return nil
}

// webAddress reports whether u is an absolute http or https address.
func webAddress(u string) bool {
	p, err := url.Parse(strings.TrimSpace(u))
	return err == nil && (p.Scheme == "http" || p.Scheme == "https") && p.Host != "" && p.User == nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
