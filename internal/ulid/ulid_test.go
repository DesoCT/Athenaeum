package ulid

import (
	"strings"
	"testing"
	"time"
)

func TestNewAtIsSortableAndSized(t *testing.T) {
	a := NewAt(time.UnixMilli(1000), strings.NewReader("0123456789"))
	b := NewAt(time.UnixMilli(2000), strings.NewReader("0123456789"))
	if len(a) != 26 || len(b) != 26 {
		t.Fatalf("id length: got %d and %d, want 26", len(a), len(b))
	}
	if !(a < b) {
		t.Fatalf("ids not time-sortable: %q should sort before %q", a, b)
	}
}

func TestNewIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := New()
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}
