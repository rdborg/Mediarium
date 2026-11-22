package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// Maintainerr and other cleanup apps delete a watched season with
// DELETE /episodefile/{id}: the file goes, the episode is wanted again.
func TestArrDeleteEpisodeFiles(t *testing.T) {
	env := newTVAutoEnv(t, []string{"Fixture.Show.S01E01.1080p.WEB-DL.x264-GRP"}, []episodeSpec{
		{1, 1, "2020-01-01", library.StatusDownloaded, "WEBDL-1080p"},
		{1, 2, "2020-01-08", library.StatusDownloaded, "WEBDL-1080p"},
		{1, 3, "2020-01-15", library.StatusDownloaded, "WEBDL-1080p"},
		{1, 4, "2020-01-22", library.StatusMissing, ""},
	})
	root := tvLibrary(t, env)
	var eps [4]library.Episode
	for i := range eps {
		eps[i], _ = env.server.MovieRepo.GetEpisode(env.seriesID, 1, i+1)
		if i < 3 {
			eps[i].FilePath = placeEpisodeFile(t, env, root, eps[i], fmt.Sprintf("Fixture Show - S01E0%d.mkv", i+1))
		}
	}

	body, _ := json.Marshal(map[string]any{"name": "maintainerr"})
	resp, err := env.client.Post(env.baseURL+"/api/auth/api-keys", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var made map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&made)
	resp.Body.Close()
	key, _ := made["key"].(string)
	if key == "" {
		t.Fatalf("no api key: %v", made)
	}
	sonarr := env.baseURL + "/sonarr/api/v3"

	// One file by id.
	arrCall(t, "DELETE", fmt.Sprintf("%s/episodefile/%d", sonarr, eps[0].ID), key, nil, http.StatusOK)
	if _, err := os.Stat(eps[0].FilePath); err == nil {
		t.Fatal("the file should be gone")
	}
	got, _ := env.server.MovieRepo.GetEpisode(env.seriesID, 1, 1)
	if got.Status != library.StatusMissing || got.FilePath != "" {
		t.Fatalf("the episode should be wanted again with no file: %+v", got)
	}
	// Gone twice, and a missing one, are both "not found".
	arrCall(t, "DELETE", fmt.Sprintf("%s/episodefile/%d", sonarr, eps[0].ID), key, nil, http.StatusNotFound)
	arrCall(t, "DELETE", fmt.Sprintf("%s/episodefile/%d", sonarr, eps[3].ID), key, nil, http.StatusNotFound)

	// Several at once.
	arrCall(t, "DELETE", sonarr+"/episodefile/bulk", key, map[string]any{"episodeFileIds": []int64{eps[1].ID, eps[2].ID}}, http.StatusOK)
	for i := 1; i < 3; i++ {
		if _, err := os.Stat(eps[i].FilePath); err == nil {
			t.Fatalf("file %d should be gone", i+1)
		}
	}
	arrCall(t, "DELETE", sonarr+"/episodefile/bulk", key, map[string]any{"episodeFileIds": []int64{}}, http.StatusBadRequest)
}
