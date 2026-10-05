package mediaservers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestPlexTagsBecomeCollections(t *testing.T) {
	var mu sync.Mutex
	var edits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/identity":
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"m1"}}`)
		case r.URL.Path == "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","type":"movie","title":"Movies"}]}}`)
		case r.URL.Path == "/library/sections/1/all" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"55","type":"movie","guid":"plex://movie/x","Guid":[{"id":"tmdb://603"}]}]}}`)
		case r.URL.Path == "/library/metadata/55":
			fmt.Fprint(w, `{"MediaContainer":{"librarySectionID":1,"Metadata":[{"ratingKey":"55","librarySectionID":1,"Collection":[{"tag":"Old"},{"tag":"Mine"}]}]}}`)
		case r.URL.Path == "/library/sections/1/all" && r.Method == http.MethodPut:
			mu.Lock()
			edits = append(edits, r.URL.Query().Encode())
			mu.Unlock()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), Version: "test"}
	s := Server{Kind: KindPlex, BaseURL: srv.URL, Token: "t"}

	found, err := c.SyncTags(context.Background(), s, MediaMovie, 603, []string{"Kids", "Old"}, []string{"Old", "Gone"})
	if err != nil || !found {
		t.Fatalf("sync: %v %v", found, err)
	}
	if len(edits) != 1 {
		t.Fatalf("one edit, got %v", edits)
	}
	for _, want := range []string{"collection%5B0%5D.tag.tag=Kids", "collection%5B%5D.tag.tag-=Old", "id=55", "type=1", "collection.locked=1"} {
		if !strings.Contains(edits[0], want) {
			t.Errorf("edit %s lacks %s", edits[0], want)
		}
	}
	if strings.Contains(edits[0], "Mine") || strings.Contains(edits[0], "Gone") {
		t.Errorf("only the named tags are touched: %s", edits[0])
	}
	if found, _ := c.SyncTags(context.Background(), s, MediaMovie, 999, []string{"Kids"}, nil); found {
		t.Error("a title the server doesn't have yet is reported as not found")
	}
}

func TestJellyfinTagsBecomeCollections(t *testing.T) {
	var mu sync.Mutex
	collections := map[string][]string{"c1": {"other"}} // id -> item ids
	names := map[string]string{"c1": "4K"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/Items" && q.Get("IncludeItemTypes") == "BoxSet":
			var items []map[string]string
			for id, n := range names {
				if strings.Contains(strings.ToLower(n), strings.ToLower(q.Get("SearchTerm"))) {
					items = append(items, map[string]string{"Id": id, "Name": n})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"Items": items})
		case r.URL.Path == "/Items":
			fmt.Fprint(w, `{"Items":[{"Id":"it9","Name":"The Matrix","Type":"Movie","ProviderIds":{"Tmdb":"603"}}]}`)
		case r.URL.Path == "/Collections" && r.Method == http.MethodPost:
			id := fmt.Sprintf("c%d", len(names)+1)
			names[id] = q.Get("Name")
			collections[id] = []string{q.Get("Ids")}
			fmt.Fprintf(w, `{"Id":%q}`, id)
		case strings.HasPrefix(r.URL.Path, "/Collections/") && strings.HasSuffix(r.URL.Path, "/Items"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/Collections/"), "/Items")
			if r.Method == http.MethodPost {
				collections[id] = append(collections[id], q.Get("Ids"))
			} else {
				var keep []string
				for _, x := range collections[id] {
					if x != q.Get("Ids") {
						keep = append(keep, x)
					}
				}
				collections[id] = keep
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), Version: "test"}
	s := Server{Kind: KindJellyfin, BaseURL: srv.URL, Token: "k"}

	if _, err := c.SyncTags(context.Background(), s, MediaMovie, 603, []string{"Kids", "4K"}, nil); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(collections["c1"]) != "[other it9]" || names["c2"] != "Kids" || fmt.Sprint(collections["c2"]) != "[it9]" {
		t.Fatalf("collections: %v %v", names, collections)
	}
	if _, err := c.SyncTags(context.Background(), s, MediaMovie, 603, nil, []string{"4K"}); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(collections["c1"]) != "[other]" {
		t.Fatalf("taken out of 4K only: %v", collections)
	}
}
