package protocol

import (
	"strings"
	"testing"
)

func TestGenerateAgentName(t *testing.T) {
	// Generate names and verify format
	for i := 0; i < 100; i++ {
		name := GenerateAgentName()

		// Should contain exactly one underscore
		parts := strings.Split(name, "_")
		if len(parts) != 2 {
			t.Errorf("Name %q should have exactly 2 parts, got %d", name, len(parts))
		}

		// Both parts should be non-empty
		if parts[0] == "" || parts[1] == "" {
			t.Errorf("Name %q has empty parts", name)
		}
	}
}

func TestGenerateAgentNameCombinations(t *testing.T) {
	// Verify we have enough combinations
	total := len(adjectives) * len(nouns)

	// Should have at least 10k combinations for practical uniqueness
	if total < 10000 {
		t.Errorf("Total combinations = %d, want at least 10000", total)
	}

	t.Logf("Total combinations: %d adjectives × %d nouns = %d",
		len(adjectives), len(nouns), total)
}

func TestAdjectivesUnique(t *testing.T) {
	seen := make(map[string]bool)
	for _, adj := range adjectives {
		if seen[adj] {
			t.Errorf("Duplicate adjective: %q", adj)
		}
		seen[adj] = true
		if adj == "" {
			t.Error("Empty adjective found")
		}
		if strings.Contains(adj, "_") {
			t.Errorf("Adjective %q contains underscore", adj)
		}
	}
}

func TestNounsUnique(t *testing.T) {
	seen := make(map[string]bool)
	for _, noun := range nouns {
		if seen[noun] {
			t.Errorf("Duplicate noun: %q", noun)
		}
		seen[noun] = true
		if noun == "" {
			t.Error("Empty noun found")
		}
		if strings.Contains(noun, "_") {
			t.Errorf("Noun %q contains underscore", noun)
		}
	}
}

func TestNewAgentID(t *testing.T) {
	id := NewAgentID()

	// Should be non-empty
	if id == "" {
		t.Error("AgentID should not be empty")
	}

	// String() should return the same value
	if id.String() != string(id) {
		t.Errorf("String() = %q, want %q", id.String(), string(id))
	}
}

func TestNewAgentIDFrom(t *testing.T) {
	id := NewAgentIDFrom("ghost_wolf")
	if id != "ghost_wolf" {
		t.Errorf("NewAgentIDFrom() = %q, want %q", id, "ghost_wolf")
	}
}

func TestNameGeneratorUniqueness(t *testing.T) {
	gen := NewNameGenerator()

	// Generate many unique names
	names := make(map[AgentID]bool)
	count := 500
	for i := 0; i < count; i++ {
		id := gen.Generate()
		if names[id] {
			t.Errorf("Duplicate name generated: %s", id)
		}
		names[id] = true
	}

	if len(names) != count {
		t.Errorf("Generated %d unique names, want %d", len(names), count)
	}
}

func TestNameGeneratorIsUsed(t *testing.T) {
	gen := NewNameGenerator()

	id := gen.Generate()

	if !gen.IsUsed(id) {
		t.Error("Generated ID should be marked as used")
	}

	other := NewAgentIDFrom("definitely_unused")
	if gen.IsUsed(other) {
		t.Error("Unused ID should not be marked as used")
	}
}

func TestNameGeneratorRelease(t *testing.T) {
	gen := NewNameGenerator()

	id := gen.Generate()

	if !gen.IsUsed(id) {
		t.Error("Generated ID should be marked as used")
	}

	gen.Release(id)

	if gen.IsUsed(id) {
		t.Error("Released ID should not be marked as used")
	}
}

func TestNameGeneratorFallback(t *testing.T) {
	gen := NewNameGenerator()

	// Mark all combinations as used (simulating exhaustion)
	for _, adj := range adjectives {
		for _, noun := range nouns {
			gen.used[adj+"_"+noun] = true
		}
	}

	// Should still generate a unique name (with timestamp fallback)
	id := gen.Generate()
	if id == "" {
		t.Error("Should generate fallback ID when pool exhausted")
	}

	// Fallback should have more than one underscore (timestamp suffix)
	parts := strings.Split(string(id), "_")
	if len(parts) < 3 {
		t.Errorf("Fallback ID %q should have timestamp suffix", id)
	}
}
