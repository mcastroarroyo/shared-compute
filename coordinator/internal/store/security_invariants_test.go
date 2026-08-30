package store

import (
	"context"
	"testing"
	"time"
)

func TestMemEarningIsIdempotentByJobAndDevice(t *testing.T) {
	m := NewMem(nil, nil)
	ctx := context.Background()
	ev := EarningEvent{
		JobID: "job-1", ProviderID: "provider-1", StaticPK: "device-1",
		GrossMicros: 100, ProviderMicros: 70,
	}
	for i := 0; i < 1000; i++ {
		if err := m.RecordEarning(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := m.EarningsSince(ctx, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Jobs != 1 || rows[0].ProviderMicros != 70 {
		t.Fatalf("duplicate settlement changed earnings: %+v", rows)
	}
}

func TestMemDebitIsIdempotentByJob(t *testing.T) {
	m := NewMem(nil, nil)
	ctx := context.Background()
	if err := m.AddCredit(ctx, "key-1", 1000, "topup", "pi-1", ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if err := m.AddCredit(ctx, "key-1", -100, "debit", "", "job-1"); err != nil {
			t.Fatal(err)
		}
	}
	balance, err := m.CreditBalance(ctx, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if balance != 900 {
		t.Fatalf("balance = %d, want 900", balance)
	}
}
