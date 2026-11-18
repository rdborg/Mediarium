package slskd

import (
	"encoding/json"
	"testing"
)

func FuzzStateJSON(f *testing.F) {
	f.Add([]byte(`"Completed, Succeeded"`))
	f.Add([]byte(`4294967296`))
	f.Add([]byte(`-1`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, b []byte) {
		var s State
		_ = json.Unmarshal(b, &s)
	})
}
