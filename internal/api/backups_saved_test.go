package api_test

import (
	"io"
	"net/http"
	"testing"
)

func TestSavedBackupsCanBeMadeListedAndDownloaded(t *testing.T) {
	_, base, client := loginNewServer(t)
	type saved struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	made := postJSON[saved](t, client, base+"/api/system/backups", nil, http.StatusOK)
	if made.Name == "" || made.Size == 0 {
		t.Fatalf("made: %+v", made)
	}
	list := getJSON[struct {
		Auto  bool    `json:"auto"`
		Keep  int     `json:"keep"`
		Items []saved `json:"items"`
	}](t, client, base+"/api/system/backups")
	if !list.Auto || list.Keep != 7 || len(list.Items) != 1 || list.Items[0].Name != made.Name {
		t.Fatalf("list: %+v", list)
	}
	resp, err := client.Get(base + "/api/system/backups/" + made.Name)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(body) < 4 || string(body[:2]) != "PK" {
		t.Fatalf("download: status %d, %d bytes", resp.StatusCode, len(body))
	}
	for _, bad := range []string{"..%2Fapp.db", "secret.key", "mediarium-backup-1.zip"} {
		resp, err := client.Get(base + "/api/system/backups/" + bad)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", bad, resp.StatusCode)
		}
	}
}
