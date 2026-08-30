package jobs

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestDeliverRequiresAssignedProvider(t *testing.T) {
	m := New()
	ch := m.Register("job-1", "provider-a")
	defer m.Close("job-1")

	if m.Deliver("job-1", "provider-b", Event{Kind: KindDone}) {
		t.Fatal("accepted a completion from a provider that was not assigned the job")
	}
	if !m.Deliver("job-1", "provider-a", Event{Kind: KindDone}) {
		t.Fatal("rejected the assigned provider")
	}
	if got := <-ch; got.Kind != KindDone {
		t.Fatalf("kind = %v, want KindDone", got.Kind)
	}
}

func TestDeliverAndCloseAreSerialized(t *testing.T) {
	for i := 0; i < 1000; i++ {
		m := New()
		m.Register("job-1", "provider-a")
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			m.Deliver("job-1", "provider-a", Event{Kind: KindDone})
		}()
		go func() {
			defer wg.Done()
			m.Close("job-1")
		}()
		wg.Wait()
	}
}

func TestForeignProvidersCannotWinCompletionRace(t *testing.T) {
	m := New()
	ch := m.Register("job-1", "provider-a")
	defer m.Close("job-1")

	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m.Deliver("job-1", "foreign-provider", Event{Kind: KindDone}) {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 0 {
		t.Fatalf("accepted %d foreign completions", accepted.Load())
	}
	if !m.Deliver("job-1", "provider-a", Event{Kind: KindDone}) {
		t.Fatal("assigned provider could not complete after attack")
	}
	<-ch
}
