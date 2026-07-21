package annotations

import "athenaeum/internal/ulid"

// newID returns a fresh ULID-shaped identifier (spec 03 section 3). The
// generator is shared with notes via internal/ulid so the two never drift.
func newID() string { return ulid.New() }
