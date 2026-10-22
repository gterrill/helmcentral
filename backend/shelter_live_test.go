package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// Live anchorage_shelter run through the real, built osm-overpass plugin and
// a live Overpass server. Skipped under -short and when the plugin is not
// built. SHELTER_LIVE_OVERPASS_URL overrides the mirror (default: the boat's
// mirror) for a network that refuses it.
func TestAnchorageShelterLive(t *testing.T) {
	provider := mustNewOsmOverpassPOIProviderLive(t)
	if u := os.Getenv("SHELTER_LIVE_OVERPASS_URL"); u != "" {
		store := withTestPluginOverridesStore(t)
		if err := store.SetConfigValues(provider.Path(), map[string]string{"overpass_url": u}); err != nil {
			t.Fatal(err)
		}
	}
	shorelineCache.reset()
	deps := assistantToolDeps{
		placeNames: func() (placeNameProvider, string, error) { return provider, provider.ID(), nil },
	}
	for _, c := range []struct{ name, args string }{
		{"stonehaven", `{"lat":-20.097633,"lon":148.902533}`},
		{"fairey", `{"lat":-19.5434,"lon":148.939367}`},
	} {
		raw, err := deps.execute(context.Background(), "anchorage_shelter", json.RawMessage(c.args))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		var out struct {
			Summary string `json:"summary"`
		}
		_ = json.Unmarshal([]byte(raw), &out)
		t.Logf("%s (%d bytes): %s", c.name, len(raw), out.Summary)
	}
}
