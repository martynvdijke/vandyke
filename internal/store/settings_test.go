package store

import (
	"context"
	"testing"
)

func TestGetSettingMissing(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	value, exists, err := st.GetSetting(ctx, "nothing.here")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if exists || value != "" {
		t.Fatalf("GetSetting missing = %q, %v; want empty and false", value, exists)
	}
}

func TestSetSettingIfAbsentKeepsFirstValue(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	first, err := st.SetSettingIfAbsent(ctx, "daily_slip.2026-11-11", "7")
	if err != nil {
		t.Fatalf("SetSettingIfAbsent: %v", err)
	}
	if first != "7" {
		t.Fatalf("winner = %q, want 7", first)
	}

	second, err := st.SetSettingIfAbsent(ctx, "daily_slip.2026-11-11", "9")
	if err != nil {
		t.Fatalf("SetSettingIfAbsent second: %v", err)
	}
	if second != "7" {
		t.Fatalf("second winner = %q, want the first value 7", second)
	}

	value, exists, err := st.GetSetting(ctx, "daily_slip.2026-11-11")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if !exists || value != "7" {
		t.Fatalf("GetSetting = %q, %v; want 7 and true", value, exists)
	}
}

func TestPruneSettingsPrefix(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	for key, value := range map[string]string{
		"daily_slip.2026-11-09": "1",
		"daily_slip.2026-11-10": "2",
		"daily_slip.2026-11-11": "3",
		"umami.url":             "https://umami.example.com/script.js",
	} {
		if _, err := st.SetSettingIfAbsent(ctx, key, value); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}

	removed, err := st.PruneSettingsPrefix(ctx, "daily_slip.", "daily_slip.2026-11-11")
	if err != nil {
		t.Fatalf("PruneSettingsPrefix: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}

	all, err := st.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if _, ok := all["daily_slip.2026-11-09"]; ok {
		t.Error("old pin survived the prune")
	}
	if _, ok := all["daily_slip.2026-11-10"]; ok {
		t.Error("old pin survived the prune")
	}
	if all["daily_slip.2026-11-11"] != "3" {
		t.Errorf("kept pin = %q, want 3", all["daily_slip.2026-11-11"])
	}
	if all["umami.url"] == "" {
		t.Error("the prune removed an unrelated setting")
	}
}
