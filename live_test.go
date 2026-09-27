package urlpipe

import (
	"os"
	"strings"
	"testing"
)

// TestLiveMarkdown calls the real API. It only runs when URLPIPE_API_KEY is
// set, and spends at most one credit (none on a cache hit).
func TestLiveMarkdown(t *testing.T) {
	if os.Getenv(APIKeyEnv) == "" {
		t.Skip("set " + APIKeyEnv + " to run the live smoke test")
	}
	c, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Markdown(ctx, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusCompleted || !strings.Contains(r.Data, "Example Domain") || r.Token == "" {
		t.Errorf("response = %+v", r)
	}
}
