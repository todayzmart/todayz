package capture

import (
	"sync"
	"testing"
)

func TestRingCapDropsOldest(t *testing.T) {
	r := NewRing[int](3)
	for i := 1; i <= 5; i++ {
		r.Add(i)
	}
	if r.Len() != 3 || r.Cap() != 3 {
		t.Fatalf("len=%d cap=%d, want 3/3", r.Len(), r.Cap())
	}
	got := r.List(nil, 0)
	want := []int{5, 4, 3}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v (newest first)", got, want)
		}
	}
}

func TestRingBelowCapAndFilterAndLimit(t *testing.T) {
	r := NewRing[int](10)
	for i := 1; i <= 4; i++ {
		r.Add(i)
	}
	if got := r.List(nil, 0); len(got) != 4 || got[0] != 4 || got[3] != 1 {
		t.Fatalf("got %v", got)
	}
	even := r.List(func(v int) bool { return v%2 == 0 }, 0)
	if len(even) != 2 || even[0] != 4 || even[1] != 2 {
		t.Fatalf("filter got %v", even)
	}
	if got := r.List(nil, 2); len(got) != 2 || got[0] != 4 {
		t.Fatalf("limit got %v", got)
	}
}

func TestRingClearAndMinimumCap(t *testing.T) {
	r := NewRing[string](0)
	if r.Cap() != 1 {
		t.Fatalf("cap=%d, want minimum 1", r.Cap())
	}
	r.Add("a")
	r.Add("b")
	if got := r.List(nil, 0); len(got) != 1 || got[0] != "b" {
		t.Fatalf("got %v", got)
	}
	r.Clear()
	if r.Len() != 0 || len(r.List(nil, 0)) != 0 {
		t.Fatal("clear left entries")
	}
	r.Add("c")
	if got := r.List(nil, 0); len(got) != 1 || got[0] != "c" {
		t.Fatalf("after clear got %v", got)
	}
}

// Run with -race: concurrent writers and readers must not race.
func TestRingConcurrent(t *testing.T) {
	r := NewRing[int](50)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				r.Add(i)
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_ = r.List(nil, 10)
				_ = r.Len()
			}
		}()
	}
	wg.Wait()
	if r.Len() != 50 {
		t.Fatalf("len=%d, want cap 50", r.Len())
	}
}

func TestStoreIDsAndClear(t *testing.T) {
	s := NewStore(2, nil)
	a, b := s.Next(), s.Next()
	if b.ID <= a.ID || a.At.IsZero() {
		t.Fatalf("ids not increasing: %v %v", a, b)
	}
	s.SMS.Add(SMS{Meta: a})
	s.LLM.Add(LLM{Meta: b})
	if c := s.Counts(); c["sms"] != 1 || c["llm"] != 1 {
		t.Fatalf("counts %v", c)
	}
	if !s.Clear("sms") || s.Clear("nope") {
		t.Fatal("clear result wrong")
	}
	if s.SMS.Len() != 0 || s.LLM.Len() != 1 {
		t.Fatal("clear touched the wrong kind")
	}
	s.Reset()
	for k, n := range s.Counts() {
		if n != 0 {
			t.Fatalf("%s not reset", k)
		}
	}
}
