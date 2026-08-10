package store

import (
	"context"
	"os"
	"testing"

	"github.com/leonardo2api/leonardo2api/internal/migrate"
)

func TestSettingJSONRoundTrip(t *testing.T) {
	databaseURL := os.Getenv("LEO_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LEO_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	if err := migrate.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	st, err := New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)

	const key = "integration_sale_pricing_profiles"
	if _, err := st.DB.Exec(ctx, `DELETE FROM settings WHERE key=$1`, key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = st.DB.Exec(ctx, `DELETE FROM settings WHERE key=$1`, key) })
	want := struct {
		Profiles []map[string]any `json:"profiles"`
	}{Profiles: []map[string]any{{"provider_id": "leonardo", "currency": "CNY", "account_cost": 100.0}}}
	if err := st.SetSetting(ctx, key, want); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Profiles []map[string]any `json:"profiles"`
	}
	if err := st.GetSetting(ctx, key, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Profiles) != 1 || got.Profiles[0]["provider_id"] != "leonardo" || got.Profiles[0]["currency"] != "CNY" || got.Profiles[0]["account_cost"] != 100.0 {
		t.Fatalf("unexpected setting round trip: %+v", got)
	}
}
