package query

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wbreza/archie/contract"
)

func TestContinuationCountsIncludeCursorOverhead(t *testing.T) {
	root := mkdirScratch(t, "cursor-overhead-")
	files := map[string]string{"archie.yaml": "schema_version: \"1\"\nid: app\nname: App\nsummary: Root.\n"}
	for _, id := range []string{"a", "d", "e"} {
		files[id+".archie.yaml"] = "schema_version: \"1\"\nid: " + id + "\nname: Item\nsummary: Item.\n"
	}
	files["d.archie.yaml"] += "description: " + strings.Repeat("x", 3002) + "\n"
	writeFiles(t, root, files)
	o := Defaults(contract.Discover)
	o.Root, o.Limits.Records, o.Limits.OutputBytes = root, 1, 4096
	remaining := -1
	for pages := 0; pages < 5; pages++ {
		raw, code := Run(o)
		validateEnvelope(t, raw)
		var response contract.Response[contract.QueryData]
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		if code != 4 || len(response.Data.Records) != 1 {
			t.Fatalf("unusable page: %s", raw)
		}
		if remaining >= 0 {
			remaining -= len(response.Data.Records)
		}
		if response.Continuation == nil {
			if remaining != 0 {
				t.Fatalf("advertised %d unavailable records: %s", remaining, raw)
			}
			return
		}
		if remaining >= 0 && response.Continuation.Remaining != remaining {
			t.Fatalf("remaining changed non-monotonically: %s", raw)
		}
		remaining = response.Continuation.Remaining
		o.Cursor = response.Continuation.Cursor
	}
	t.Fatal("pagination did not terminate")
}
