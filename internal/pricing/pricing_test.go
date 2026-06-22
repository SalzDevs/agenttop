package pricing

import "testing"

func TestExactMatch(t *testing.T) {
	r, ok := Lookup("gpt-4o")
	if !ok {
		t.Fatal("exact match should work")
	}
	if r.Input != 2.5 {
		t.Fatalf("gpt-4o input rate = %v, want 2.5", r.Input)
	}
}

func TestPrefixMatchDeterministic(t *testing.T) {
	// gpt-4o-mini should ALWAYS match gpt-4o-mini (input=0.15), never
	// gpt-4o (input=2.5). This was non-deterministic before the fix.
	for i := 0; i < 100; i++ {
		r, ok := Lookup("gpt-4o-mini-2024")
		if !ok {
			t.Fatal("prefix match should work")
		}
		if r.Input != 0.15 {
			t.Fatalf("iteration %d: gpt-4o-mini matched wrong rate (input=%v, want 0.15)", i, r.Input)
		}
	}

	for i := 0; i < 100; i++ {
		r, ok := Lookup("o3-mini-2024")
		if !ok {
			t.Fatal("prefix match should work")
		}
		if r.Input != 1.1 {
			t.Fatalf("iteration %d: o3-mini matched wrong rate (input=%v, want 1.1)", i, r.Input)
		}
	}
}

func TestUnknownModel(t *testing.T) {
	_, ok := Lookup("nonexistent-model-xyz")
	if ok {
		t.Fatal("unknown model should return false")
	}
}

func TestCostCalculation(t *testing.T) {
	// 1M input tokens of gpt-4o = $2.5
	c := Cost("gpt-4o", 1_000_000, 0, 0, 0)
	if c != 2.5 {
		t.Fatalf("1M input of gpt-4o = %.4f, want 2.5", c)
	}

	// 1M input + 1M output of claude-sonnet-4-5 = $3 + $15 = $18
	c = Cost("claude-sonnet-4-5", 1_000_000, 1_000_000, 0, 0)
	if c != 18 {
		t.Fatalf("1M in + 1M out of claude-sonnet-4-5 = %.4f, want 18", c)
	}
}
