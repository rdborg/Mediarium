package configfile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ListSection is a Settings page that holds several things of the same kind,
// each with its own name. In the file it is a list; Mediarium matches the
// items by name (not case-sensitive).
type ListSection struct {
	Name    string
	Path    string
	Wrap    string   // the word the answer keeps its array under; "" when the answer is the array
	Fields  []string // everything an item may have
	Secrets []string // values Mediarium accepts but never shows back
	// Partial is true when an update may carry only the values that change.
	// The others need the whole item sent back.
	Partial bool
}

// Lists are the lists a file may have, in the order they are applied.
var Lists = []ListSection{
	{
		Name: "usenetServers", Path: "/api/usenet-servers", Partial: true,
		Fields:  []string{"name", "host", "port", "useSsl", "username", "password", "connections", "priority", "enabled"},
		Secrets: []string{"password"},
	},
	{
		Name: "indexers", Path: "/api/indexers", Partial: true,
		Fields:  []string{"name", "kind", "definitionId", "baseUrl", "apiKey", "protocol", "enabled", "settings", "priority", "requestsPerDay", "minSeeders", "extraCategories", "torrentRoute"},
		Secrets: []string{"apiKey"},
	},
	{
		Name: "mediaServers", Path: "/api/media-servers",
		Fields:  []string{"name", "kind", "baseUrl", "publicUrl", "token", "apiKey", "enabled", "refreshAfterImport", "pathMap"},
		Secrets: []string{"token", "apiKey"},
	},
	{
		Name: "notifications", Path: "/api/notifications",
		Fields: []string{"name", "type", "enabled", "events", "config"},
	},
	{
		Name: "qualityProfiles", Path: "/api/quality-profiles", Wrap: "profiles",
		Fields: []string{"name", "allowed", "cutoff", "upgradeAllowed", "mustContain", "mustNotContain", "preferred", "fallback",
			"maxSizeGB", "formatScores", "minFormatScore", "cutoffFormatScore", "noSeasonPacks", "skipBadReleases", "qualityOrder"},
	},
}

func listByName(name string) (ListSection, bool) {
	for _, l := range Lists {
		if l.Name == name {
			return l, true
		}
	}
	return ListSection{}, false
}

func (l ListSection) has(list []string, k string) bool {
	for _, x := range list {
		if x == k {
			return true
		}
	}
	return false
}

// listDerived are the values a page shows that are worked out, not chosen:
// they are left out of an export and never sent back.
var listDerived = regexp.MustCompile(`^(id|inUse|has[A-Z].*|webUrl|machineIdentifier|lastError|lastCheckedAt|lastTestError|lastTestAt|storedSecrets|failures|pausedUntil|pausedNote|pausedWhy|secretError)$`)

// parseList checks the shape of a list in the file and takes ${NAME} from the environment.
func parseList(ls ListSection, v any, env func(string) (string, bool), secrets map[string][]string) ([]map[string]any, error) {
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%q must be a list: each item starts with a dash and has a name", ls.Name)
	}
	seen := map[string]bool{}
	out := []map[string]any{}
	for i, x := range raw {
		m, ok := x.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be name: value lines", ls.Name, i+1)
		}
		name, _ := m["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("%s[%d] needs a name (it is how Mediarium finds the one you mean)", ls.Name, i+1)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("%s has %q twice", ls.Name, name)
		}
		seen[key] = true
		ex, err := expand(m, env, fmt.Sprintf("%s[%s]", ls.Name, name))
		if err != nil {
			return nil, err
		}
		item := ex.(map[string]any)
		if held := takePlaceholders(item, ""); len(held) > 0 {
			secrets[secretKey(ls.Name, name)] = held
		}
		out = append(out, item)
	}
	return out, nil
}

// currentList reads what Mediarium has now.
func currentList(ctx context.Context, c *Client, ls ListSection) ([]map[string]any, error) {
	data, err := c.doRaw(ctx, http.MethodGet, ls.Path, nil)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("Mediarium's answer for %s wasn't what was expected", ls.Path)
	}
	if ls.Wrap != "" {
		obj, _ := raw.(map[string]any)
		raw = obj[ls.Wrap]
	}
	arr, _ := raw.([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, x := range arr {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

type listResult struct {
	Result
	actions []action
	secrets int // values that are passwords or keys, which can't be put back
}

func idOf(m map[string]any) (int64, bool) {
	f, ok := m["id"].(float64)
	return int64(f), ok
}

// Options are the choices made for a check or an apply.
type Options struct {
	// Replace also removes what Mediarium has in a list the file has but that
	// is not in the file. Without it a file only adds and changes.
	Replace bool
}

// action makes one change. It returns what puts the change back, nil when the
// change can't be undone.
type action func(context.Context, *Client) (undo func(context.Context, *Client) error, err error)

// listPlan is what comparing the file's lists with Mediarium's found.
type listPlan struct {
	results   []listResult
	removals  []listResult // run after everything else
	unknown   []string
	invalid   []string
	unchanged []string // items that already match
	missing   []string // secrets a new item needs but the file doesn't hold
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// planLists compares every list in the file with what Mediarium has and
// returns what would change, with the calls that would make it so.
func planLists(ctx context.Context, c *Client, f File, opts Options) (*listPlan, error) {
	lp := &listPlan{}
	for _, ls := range Lists {
		items, present := f.Lists[ls.Name]
		if !present || (len(items) == 0 && !opts.Replace) {
			continue
		}
		cur, err := currentList(ctx, c, ls)
		if err != nil {
			lp.results = append(lp.results, listResult{Result: Result{Section: ls.Name, Err: fmt.Errorf("read %s: %w", ls.Name, err)}})
			continue
		}
		var refs *profileRefs
		inFile := map[string]bool{}
		if ls.Name == "qualityProfiles" {
			if refs, err = loadProfileRefs(ctx, c, cur); err != nil {
				lp.results = append(lp.results, listResult{Result: Result{Section: ls.Name, Err: fmt.Errorf("read %s: %w", ls.Name, err)}})
				continue
			}
			for i := range cur {
				cur[i] = refs.toNames(cur[i])
			}
		}
		for _, item := range items {
			inFile[strings.ToLower(strings.TrimSpace(item["name"].(string)))] = true
		}
		var defs map[string]map[string]bool
		if ls.Name == "indexers" {
			defs = indexerSettingNames(ctx, c, items)
		}
		byName := map[string]map[string]any{}
		for _, m := range cur {
			if n, _ := m["name"].(string); n != "" {
				byName[strings.ToLower(strings.TrimSpace(n))] = m
			}
		}
		for _, item := range items {
			name := strings.TrimSpace(item["name"].(string))
			label := fmt.Sprintf("%s[%s]", ls.Name, name)
			bad := false
			for k := range item {
				if !ls.has(ls.Fields, k) {
					lp.unknown = append(lp.unknown, label+"."+k)
					bad = true
				}
			}
			if bad {
				continue
			}
			have, exists := byName[strings.ToLower(name)]
			if ls.Name == "indexers" {
				if typos := checkIndexerSettings(label, have, item, defs); len(typos) > 0 {
					lp.unknown = append(lp.unknown, typos...)
					continue
				}
			}
			if probs := valueProblems(label, have, item, ls.Name); len(probs) > 0 {
				lp.invalid = append(lp.invalid, probs...)
				continue
			}
			res := listResult{Result: Result{Section: label}}
			if refs != nil {
				if err := refs.checkNames(item, inFile); err != nil {
					res.Err = fmt.Errorf("%s: %w", label, err)
					lp.results = append(lp.results, res)
					continue
				}
			}
			if !exists {
				res.Changes = append(res.Changes, Change{Section: label, New: true})
				res.actions = append(res.actions, createAction(ls, item))
				for _, k := range f.Secrets[secretKey(ls.Name, name)] {
					lp.missing = append(lp.missing, label+"."+k)
				}
			} else {
				changes, body, prio := diffItem(ls, label, have, item)
				res.Changes = changes
				if len(changes) > 0 {
					res.actions = append(res.actions, updateAction(ls, have, body, prio))
				} else {
					lp.unchanged = append(lp.unchanged, label)
				}
			}
			for _, ch := range res.Changes {
				if ch.Secret {
					res.secrets++
				}
			}
			if len(res.Changes) > 0 {
				lp.results = append(lp.results, res)
			}
		}
		if opts.Replace {
			for _, m := range cur {
				n, _ := m["name"].(string)
				if n == "" || inFile[strings.ToLower(strings.TrimSpace(n))] {
					continue
				}
				label := fmt.Sprintf("%s[%s]", ls.Name, strings.TrimSpace(n))
				lp.removals = append(lp.removals, listResult{
					Result:  Result{Section: label, Changes: []Change{{Section: label, Removed: true}}},
					actions: []action{deleteAction(ls, m)},
				})
			}
		}
	}
	return lp, nil
}

// diffItem lists what differs between one item and what the file wants.
func diffItem(ls ListSection, label string, have, want map[string]any) ([]Change, map[string]any, any) {
	var changes []Change
	body := map[string]any{}
	if !ls.Partial {
		for k, v := range have {
			if !listDerived.MatchString(k) {
				body[k] = v
			}
		}
	}
	var prio any
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w := want[k]
		if fixed(ls, have, k) {
			continue
		}
		switch {
		case ls.Name == "mediaServers" && k == "kind":
			hk, _ := have[k].(string)
			wk, _ := w.(string)
			if !strings.EqualFold(strings.TrimSpace(hk), strings.TrimSpace(wk)) {
				changes = append(changes, Change{Section: label, Key: k, Have: show(have[k]), Want: show(w)})
				body[k] = w
			}
		case ls.has(ls.Secrets, k):
			changes = append(changes, Change{Section: label, Key: k, Secret: true})
			body[k] = w
		case k == "settings" || (k == "config" && ls.Name == "notifications"):
			ws, _ := w.(map[string]any)
			hs, _ := have[k].(map[string]any)
			sub := map[string]any{}
			subKeys := make([]string, 0, len(ws))
			for sk := range ws {
				subKeys = append(subKeys, sk)
			}
			sort.Strings(subKeys)
			for _, sk := range subKeys {
				hv, shown := hs[sk]
				if !shown {
					// A password or cookie: never shown back, so always sent.
					changes = append(changes, Change{Section: label, Key: k + "." + sk, Secret: true})
					sub[sk] = ws[sk]
				} else if !same(hv, fmt.Sprint(ws[sk])) && !(ls.Name != "notifications" && same(hv, ws[sk])) {
					changes = append(changes, Change{Section: label, Key: k + "." + sk, Have: show(hv), Want: show(ws[sk])})
					sub[sk] = ws[sk]
				}
			}
			if len(sub) > 0 {
				if !ls.Partial {
					// The whole block is sent back: what is there, with the changes on top.
					merged := map[string]any{}
					for hk, hv := range hs {
						merged[hk] = hv
					}
					for sk, sv := range sub {
						merged[sk] = sv
					}
					sub = merged
				}
				body[k] = sub
			}
		case ls.Name == "indexers" && k == "priority":
			if !same(have[k], priorityNumber(w)) {
				changes = append(changes, Change{Section: label, Key: k, Have: show(have[k]), Want: show(w)})
				// A number from 1 to 50, or one of the old words; the server
				// checks it.
				prio = w
			}
		default:
			if hv, shown := have[k]; !shown || !same(hv, w) {
				changes = append(changes, Change{Section: label, Key: k, Have: show(have[k]), Want: show(w)})
				body[k] = w
			}
		}
	}
	return changes, body, prio
}

// priorityNumber turns an indexer priority as a file may write it (one of the
// old words, or a number in quotes) into the number the server stores, so a
// file that says "preferred" is not a change for an indexer already at 10.
// Anything else comes back as it is; the server checks it.
func priorityNumber(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	s = strings.ToLower(strings.TrimSpace(s))
	switch strings.NewReplacer("_", " ", "-", " ").Replace(s) {
	case "preferred":
		return 10
	case "normal":
		return 25
	case "last resort", "last", "lastresort":
		return 40
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return v
}

// fixed reports whether k is a value an existing item can't have changed:
// Mediarium ignores it on an update, so comparing it would show the same
// change on every run. An indexer keeps its kind and site definition, and
// one from the site list has no protocol or API key of its own.
func fixed(ls ListSection, have map[string]any, k string) bool {
	if ls.Name != "indexers" {
		return false
	}
	if k == "kind" || k == "definitionId" {
		return true
	}
	def, _ := have["definitionId"].(string)
	return def != "" && (k == "protocol" || k == "apiKey")
}

// indexerSettingNames reads the settings each site definition has, for the
// definitions the file's indexers use in their settings. nil when the list of
// sites can't be read: the names are then not checked.
func indexerSettingNames(ctx context.Context, c *Client, items []map[string]any) map[string]map[string]bool {
	need := false
	for _, it := range items {
		if _, ok := it["settings"]; ok {
			need = true
		}
	}
	if !need {
		return nil
	}
	data, err := c.doRaw(ctx, http.MethodGet, "/api/indexer-definitions", nil)
	if err != nil {
		return nil
	}
	var cat struct {
		Definitions []struct {
			ID       string `json:"id"`
			Settings []struct {
				Name string `json:"name"`
			} `json:"settings"`
		} `json:"definitions"`
	}
	if json.Unmarshal(data, &cat) != nil {
		return nil
	}
	out := map[string]map[string]bool{}
	for _, d := range cat.Definitions {
		names := map[string]bool{}
		for _, s := range d.Settings {
			names[s.Name] = true
		}
		out[d.ID] = names
	}
	return out
}

// checkIndexerSettings lists the names under an indexer's settings: that its
// site doesn't have (Mediarium would drop them without a word), and a
// settings: block on an indexer that isn't from the site list, which has none.
func checkIndexerSettings(label string, have, item map[string]any, defs map[string]map[string]bool) []string {
	ws, ok := item["settings"].(map[string]any)
	if !ok || len(ws) == 0 {
		return nil
	}
	// An indexer Mediarium has keeps its site: what the file says is only
	// used for a new one.
	var def string
	if have != nil {
		def, _ = have["definitionId"].(string)
	} else {
		def, _ = item["definitionId"].(string)
	}
	if def == "" {
		return []string{label + ".settings (only indexers from the site list have settings)"}
	}
	known, ok := defs[def]
	if !ok {
		return nil
	}
	var bad []string
	for k := range ws {
		if !known[k] {
			bad = append(bad, label+".settings."+k)
		}
	}
	sort.Strings(bad)
	return bad
}

func createAction(ls ListSection, item map[string]any) action {
	return func(ctx context.Context, c *Client) (func(context.Context, *Client) error, error) {
		body := map[string]any{}
		for k, v := range item {
			body[k] = v
		}
		// Something added from the file is switched on unless the file says
		// otherwise, as when it is added in Settings.
		if ls.has(ls.Fields, "enabled") {
			if _, said := body["enabled"]; !said {
				body["enabled"] = true
			}
		}
		if ls.Name == "qualityProfiles" {
			if err := toIDs(ctx, c, body); err != nil {
				return nil, fmt.Errorf("add %v: %w", item["name"], err)
			}
		}
		if ls.Name == "notifications" {
			stringifyConfig(body)
		}
		var prio, enabled any
		if ls.Name == "indexers" {
			// A new indexer starts switched on with normal priority; the two
			// are set right after it is added.
			prio, enabled = body["priority"], body["enabled"]
			delete(body, "priority")
		}
		data, err := c.doRaw(ctx, http.MethodPost, ls.Path, body)
		if err != nil {
			return nil, fmt.Errorf("add %v: %w", item["name"], err)
		}
		var made map[string]any
		_ = json.Unmarshal(data, &made)
		id, haveID := idOf(made)
		var undo func(context.Context, *Client) error
		if haveID {
			undo = func(ctx context.Context, c *Client) error {
				_, err := c.doRaw(ctx, http.MethodDelete, fmt.Sprintf("%s/%d", ls.Path, id), nil)
				return err
			}
		}
		if prio == nil && enabled == nil {
			return undo, nil
		}
		if !haveID {
			return nil, nil
		}
		if enabled != nil {
			if _, err := c.do(ctx, http.MethodPut, fmt.Sprintf("%s/%d", ls.Path, id), map[string]any{"enabled": enabled}); err != nil {
				return undo, fmt.Errorf("set %v: %w", item["name"], err)
			}
		}
		if prio != nil {
			if _, err := c.do(ctx, http.MethodPut, fmt.Sprintf("%s/%d/priority", ls.Path, id), map[string]any{"priority": prio}); err != nil {
				return undo, fmt.Errorf("set the priority of %v: %w", item["name"], err)
			}
		}
		return undo, nil
	}
}

// stringifyConfig turns the values under config: into text, the only kind a
// notification's settings hold (a file may write a port as a number).
func stringifyConfig(body map[string]any) {
	cfg, ok := body["config"].(map[string]any)
	if !ok {
		return
	}
	out := map[string]any{}
	for k, v := range cfg {
		switch t := v.(type) {
		case string:
			out[k] = t
		case bool, int, int64, uint64, float64:
			out[k] = fmt.Sprint(t)
		default:
			out[k] = v
		}
	}
	body["config"] = out
}

// undoBody is what puts an item back as it was after body changed it. Values
// Mediarium never shows back (passwords, keys) can't be put back: they are
// left out, and the count says how many.
func undoBody(ls ListSection, have, body map[string]any) (map[string]any, int) {
	out := map[string]any{}
	lost := 0
	for k := range body {
		switch {
		case ls.has(ls.Secrets, k):
			lost++
		case k == "settings" || (k == "config" && ls.Name == "notifications"):
			hs, _ := have[k].(map[string]any)
			ws, _ := body[k].(map[string]any)
			sub := map[string]any{}
			for sk := range ws {
				if hv, shown := hs[sk]; shown {
					sub[sk] = hv
				} else {
					lost++
				}
			}
			if ls.Name == "notifications" {
				// The whole block is sent, so what was there is all of it.
				for sk, hv := range hs {
					sub[sk] = hv
				}
			}
			if len(sub) > 0 {
				out[k] = sub
			}
		default:
			if hv, shown := have[k]; shown {
				out[k] = hv
			}
		}
	}
	return out, lost
}

func updateAction(ls ListSection, have, body map[string]any, prio any) action {
	return func(ctx context.Context, c *Client) (func(context.Context, *Client) error, error) {
		id, ok := idOf(have)
		if !ok {
			return nil, fmt.Errorf("%v has no id", have["name"])
		}
		back, _ := undoBody(ls, have, body)
		if ls.Name == "notifications" {
			stringifyConfig(body)
		}
		if ls.Name == "qualityProfiles" {
			if err := toIDs(ctx, c, body); err != nil {
				return nil, fmt.Errorf("change %v: %w", have["name"], err)
			}
		}
		undo := func(ctx context.Context, c *Client) error {
			if ls.Name == "qualityProfiles" {
				if err := toIDs(ctx, c, back); err != nil {
					return err
				}
			}
			if ls.Name == "notifications" {
				stringifyConfig(back)
			}
			if len(back) > 0 {
				if _, err := c.do(ctx, http.MethodPut, fmt.Sprintf("%s/%d", ls.Path, id), back); err != nil {
					return err
				}
			}
			if prio != nil {
				if old, ok := have["priority"]; ok {
					if _, err := c.do(ctx, http.MethodPut, fmt.Sprintf("%s/%d/priority", ls.Path, id), map[string]any{"priority": old}); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if len(body) > 0 {
			if _, err := c.do(ctx, http.MethodPut, fmt.Sprintf("%s/%d", ls.Path, id), body); err != nil {
				return nil, fmt.Errorf("change %v: %w", have["name"], err)
			}
		}
		if prio != nil {
			if _, err := c.do(ctx, http.MethodPut, fmt.Sprintf("%s/%d/priority", ls.Path, id), map[string]any{"priority": prio}); err != nil {
				return undo, fmt.Errorf("set the priority of %v: %w", have["name"], err)
			}
		}
		return undo, nil
	}
}

// deleteAction removes an item the file doesn't have (replace mode). Putting
// it back adds it again from what Mediarium showed, without its passwords.
func deleteAction(ls ListSection, have map[string]any) action {
	return func(ctx context.Context, c *Client) (func(context.Context, *Client) error, error) {
		id, ok := idOf(have)
		if !ok {
			return nil, fmt.Errorf("%v has no id", have["name"])
		}
		if _, err := c.doRaw(ctx, http.MethodDelete, fmt.Sprintf("%s/%d", ls.Path, id), nil); err != nil {
			return nil, fmt.Errorf("remove %v: %w", have["name"], err)
		}
		item := map[string]any{}
		for k, v := range have {
			if !listDerived.MatchString(k) && ls.has(ls.Fields, k) {
				item[k] = v
			}
		}
		return func(ctx context.Context, c *Client) error {
			_, err := createAction(ls, item)(ctx, c)
			return err
		}, nil
	}
}
