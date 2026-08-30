package store

import (
	"context"
	"testing"
)

func TestMemKeysAndTokens(t *testing.T) {
	m := NewMem(
		map[string]struct{}{"ck": {}},
		map[string]struct{}{"pt": {}},
	)
	ctx := context.Background()

	id, ok := m.ValidateConsumerKey(ctx, "ck")
	if !ok || id == "" {
		t.Fatalf("valid key rejected")
	}
	if id2, _ := m.ValidateConsumerKey(ctx, "ck"); id2 != id {
		t.Fatalf("key id not stable")
	}
	if _, ok := m.ValidateConsumerKey(ctx, "other"); ok {
		t.Fatalf("unknown key accepted")
	}
	if !m.ValidateProviderToken(ctx, "pt") || m.ValidateProviderToken(ctx, "nope") {
		t.Fatalf("provider token check wrong")
	}
}

func TestMemUsageCountAndUpsert(t *testing.T) {
	m := NewMem(nil, nil)
	ctx := context.Background()
	_ = m.UpsertProvider(ctx, ProviderRecord{ProviderID: "p1"})
	for i := 0; i < 3; i++ {
		if err := m.RecordUsage(ctx, UsageEvent{Model: "m"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := m.UsageCount(); got != 3 {
		t.Fatalf("usage count = %d, want 3", got)
	}
}
