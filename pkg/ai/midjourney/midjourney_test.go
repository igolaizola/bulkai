package midjourney

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/igolaizola/bulkai/pkg/discord"
)

func TestParseAppSearch(t *testing.T) {
	var appSearch discord.ApplicationCommandSearch
	data, err := os.ReadFile("testdata/app-command-search.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &appSearch); err != nil {
		t.Fatal(err)
	}
}
