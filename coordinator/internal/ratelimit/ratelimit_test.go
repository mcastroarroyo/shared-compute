package ratelimit

import "testing"

func TestDisabled(t *testing.T) {
	l := New(0)
	for i := 0; i < 1000; i++ {
		if ok, _ := l.Allow("k"); !ok {
			t.Fatal("disabled limiter must always allow")
		}
	}
}

func TestBurstThenBlock(t *testing.T) {
	l := New(60) // 1/sec, burst 60
	allowed := 0
	for i := 0; i < 100; i++ {
		if ok, _ := l.Allow("k"); ok {
			allowed++
		}
	}
	if allowed != 60 {
		t.Fatalf("expected 60 allowed on burst, got %d", allowed)
	}
	ok, wait := l.Allow("k")
	if ok || wait <= 0 {
		t.Fatalf("expected block with positive wait, ok=%v wait=%v", ok, wait)
	}
}

func TestPerKeyIsolation(t *testing.T) {
	l := New(1) // burst 1
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("first for a should pass")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("first for b should pass (separate bucket)")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("second for a should block")
	}
}
