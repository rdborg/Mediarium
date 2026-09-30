package migrate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const bazarrKey = "bazarr-fixture-key-0011"

// newFakeBazarr serves Bazarr's system endpoints behind the X-API-KEY
// header. A failing settings call leaves out the providers only.
func newFakeBazarr(t *testing.T, failSettings bool) *fakeApp {
	t.Helper()
	f := &fakeApp{t: t, key: bazarrKey}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if r.Header.Get("X-API-KEY") != bazarrKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/system/status":
			serveFixture(t, w, "bazarr/status.json")
		case "/api/system/languages":
			serveFixture(t, w, "bazarr/languages.json")
		case "/api/system/settings":
			if failSettings {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			serveFixture(t, w, "bazarr/settings.json")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// subtitleSetting stands in for the subtitle languages setting.
func subtitleSetting(h *harness, current []string) *[]string {
	h.im.deps.SubtitleLanguages = func() []string { return current }
	h.im.deps.SetSubtitleLanguages = func(codes []string) error {
		current = append([]string{}, codes...)
		return nil
	}
	return &current
}

func TestBazarrPreviewChangesNothingAndImportIsOptIn(t *testing.T) {
	h := newHarness(t)
	bz := newFakeBazarr(t, false)
	langs := subtitleSetting(h, []string{"en"})
	src := Sources{Bazarr: &Conn{URL: bz.URL, APIKey: bazarrKey}}
	ctx := context.Background()

	pv, err := h.im.Preview(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	b := pv.Bazarr
	if b == nil || !b.OK || b.Version != "1.4.5" || b.Included || b.Summary != (Summary{Total: 5, Add: 3, Exists: 1, Skip: 1}) {
		t.Fatalf("bazarr preview: %+v", b)
	}
	if !reflect.DeepEqual(b.Current, []string{"en"}) || !reflect.DeepEqual(b.New, []string{"en", "pt-BR", "zh-TW", "nl"}) {
		t.Fatalf("languages now %v, after %v", b.Current, b.New)
	}
	byCode := map[string]SubtitleLanguage{}
	for _, l := range b.Languages {
		byCode[l.Code] = l
	}
	if l := byCode["pb"]; l.MapsTo != "pt-BR" || l.Action != ActionAdd || l.Name != "Brazilian Portuguese" {
		t.Fatalf("pb: %+v", l)
	}
	if l := byCode["en"]; l.Action != ActionExists {
		t.Fatalf("en: %+v", l)
	}
	if l := byCode[""]; l.Action != ActionSkip || l.MapsTo != "" || l.Reason == "" {
		t.Fatalf("a language without a code: %+v", l)
	}
	if len(b.Providers) != 4 || b.Providers[0].Equivalent != "OpenSubtitles" || b.Providers[1].Equivalent != "" || !strings.Contains(b.Providers[1].Reason, "no equivalent") {
		t.Fatalf("providers: %+v", b.Providers)
	}
	if body := mustMarshal(t, pv); strings.Contains(body, bazarrKey) || strings.Contains(body, "bazarr-provider-secret") {
		t.Fatal("the preview must not echo keys or provider passwords")
	}
	if !reflect.DeepEqual(*langs, []string{"en"}) {
		t.Fatalf("preview changed the languages: %v", *langs)
	}

	// Without include.subtitleLanguages an import leaves them alone and does
	// not even read Bazarr.
	bz.mu.Lock()
	before := len(bz.requests)
	bz.mu.Unlock()
	st, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	bz.mu.Lock()
	after := len(bz.requests)
	bz.mu.Unlock()
	if after != before || st.Results.SubtitleLanguages != (Counts{}) || !reflect.DeepEqual(*langs, []string{"en"}) {
		t.Fatalf("an import without the option touched Bazarr or the languages: %+v %v", st.Results.SubtitleLanguages, *langs)
	}

	yes := true
	inc := Options{Sources: src, Include: Include{SubtitleLanguages: &yes}}
	pv2, err := h.im.Preview(ctx, inc)
	if err != nil {
		t.Fatal(err)
	}
	if !pv2.Bazarr.Included {
		t.Fatal("the preview should say the languages are included")
	}
	st, err = h.im.Run(ctx, inc)
	if err != nil {
		t.Fatal(err)
	}
	if st.Step != StepDone || st.Results.SubtitleLanguages != (Counts{Added: 1}) || st.Total != 1 || len(st.Errors) != 0 {
		t.Fatalf("import: %+v total=%d errors=%v", st.Results.SubtitleLanguages, st.Total, st.Errors)
	}
	if !reflect.DeepEqual(*langs, []string{"en", "pt-BR", "zh-TW", "nl"}) {
		t.Fatalf("languages after the import: %v", *langs)
	}
	if len(st.Items) != 1 || st.Items[0].Kind != "subtitleLanguages" || !strings.Contains(st.Items[0].Reason, "en, pt-BR, zh-TW, nl") {
		t.Fatalf("items: %+v", st.Items)
	}

	// Again: nothing to change.
	st, err = h.im.Run(ctx, inc)
	if err != nil {
		t.Fatal(err)
	}
	if st.Results.SubtitleLanguages != (Counts{Existing: 1}) {
		t.Fatalf("second run: %+v", st.Results.SubtitleLanguages)
	}
	bz.assertOnlyGET(t)
	if strings.Contains(h.logs.String(), bazarrKey) || strings.Contains(h.logs.String(), "bazarr-provider-secret") {
		t.Fatal("a secret was logged")
	}
}

func TestBazarrProvidersFailingStillGivesLanguages(t *testing.T) {
	h := newHarness(t)
	subtitleSetting(h, nil)
	bz := newFakeBazarr(t, true)
	pv, err := h.im.Preview(context.Background(), Options{Sources: Sources{Bazarr: &Conn{URL: bz.URL, APIKey: bazarrKey}}})
	if err != nil {
		t.Fatal(err)
	}
	b := pv.Bazarr
	if !b.OK || b.Summary.Total != 5 || b.ProvidersError == "" || len(b.Providers) != 0 {
		t.Fatalf("bazarr preview: %+v", b)
	}
	if !reflect.DeepEqual(b.New, []string{"pt-BR", "zh-TW", "nl", "en"}) {
		t.Fatalf("new languages: %v", b.New)
	}
}

func TestBazarrErrors(t *testing.T) {
	h := newHarness(t)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version": "1.2.3"}`))
	}))
	t.Cleanup(other.Close)
	bz := newFakeBazarr(t, false)
	tests := []struct {
		name    string
		conn    Conn
		wantErr string
	}{
		{"wrong key", Conn{URL: bz.URL, APIKey: "nope-secret"}, "the API key was not accepted"},
		{"no key", Conn{URL: bz.URL}, "enter the API key"},
		{"wrong address", Conn{URL: bz.URL + "/bazarr", APIKey: bazarrKey}, "answered 404"},
		{"another app", Conn{URL: other.URL, APIKey: bazarrKey}, "did not answer like Bazarr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pv, err := h.im.Preview(context.Background(), Options{Sources: Sources{Bazarr: &tt.conn}})
			if err != nil {
				t.Fatal(err)
			}
			if pv.Bazarr.OK || !strings.Contains(pv.Bazarr.Error, tt.wantErr) || strings.Contains(pv.Bazarr.Error, "nope-secret") {
				t.Fatalf("got %+v, want error containing %q", pv.Bazarr.AppStatus, tt.wantErr)
			}
		})
	}
}

func TestBazarrCode(t *testing.T) {
	tests := []struct {
		code2 string
		want  string
		ok    bool
	}{
		{"en", "en", true},
		{"NL", "nl", true},
		{"pb", "pt-BR", true},
		{"zt", "zh-TW", true},
		{"zh", "zh-CN", true},
		{"pt", "pt", true},
		{"sr-Latn", "sr-latn", true},
		{"", "", false},
		{"e", "", false},
		{"en/us", "", false},
		{"en, nl", "", false},
	}
	for _, tt := range tests {
		got, ok := bazarrCode(bazarrLanguage{Code2: tt.code2})
		if got != tt.want || ok != tt.ok {
			t.Errorf("bazarrCode(%q) = %q, %v; want %q, %v", tt.code2, got, ok, tt.want, tt.ok)
		}
	}
}
