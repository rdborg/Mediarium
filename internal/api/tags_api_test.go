package api_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestTagsOnTitles(t *testing.T) {
	server, base, client := loginNewServer(t)
	m1 := seedMovie(t, server.MovieRepo, 101, "Toy Story", 1995, "")
	m2 := seedMovie(t, server.MovieRepo, 102, "Up", 2009, "")
	sh := seedShow(t, server, 201, "Bluey", 2, 0)

	got := postJSONMethod[map[string]any](t, client, http.MethodPut, fmt.Sprintf("%s/api/movies/%d/tags", base, m1), map[string]any{"tags": []string{"Kids", " kids ", "4K"}}, http.StatusOK)
	if fmt.Sprint(got["tags"]) != "[4K Kids]" {
		t.Fatalf("tags: %v", got)
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, fmt.Sprintf("%s/api/series/%d/tags", base, sh), map[string]any{"tags": []string{"kids"}}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, fmt.Sprintf("%s/api/movies/%d/tags", base, 999), map[string]any{"tags": []string{"x"}}, http.StatusNotFound)

	movies := getJSON[[]map[string]any](t, client, base+"/api/movies")
	byID := map[float64]map[string]any{}
	for _, m := range movies {
		byID[m["id"].(float64)] = m
	}
	if fmt.Sprint(byID[float64(m1)]["tags"]) != "[4K Kids]" || fmt.Sprint(byID[float64(m2)]["tags"]) != "[]" {
		t.Fatalf("movie list tags: %v", movies)
	}
	if show := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/series/%d", base, sh)); fmt.Sprint(show["tags"]) != "[Kids]" {
		t.Fatalf("show tags (spelled like the existing tag): %v", show["tags"])
	}

	res := bulkCall(t, client, http.MethodPut, base+"/api/library/bulk/tags", map[string]any{
		"items": []map[string]any{{"kind": "movie", "id": m1}, {"kind": "movie", "id": m2}, {"kind": "movie", "id": 999}},
		"add":   []string{"Family"}, "remove": []string{"4K"},
	}, http.StatusOK)
	if res.Updated != 2 || len(res.Failed) != 1 {
		t.Fatalf("bulk: %+v", res)
	}
	all := getJSON[[]map[string]any](t, client, base+"/api/tags")
	if fmt.Sprint(all) != "[map[movies:2 name:Family shows:0] map[movies:1 name:Kids shows:1]]" {
		t.Fatalf("all tags: %v", all)
	}
}
