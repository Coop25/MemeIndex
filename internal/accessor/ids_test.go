package accessor

import (
	"testing"
	"time"
)

// IDs made at the pace real uploads arrive, tens of microseconds to
// milliseconds apart rather than back to back, must all differ. The old
// uuid.NewV6 IDs repeated within a few thousand at this spacing, because
// google/uuid v1.6.0 drops four bits of the timestamp; back to back they
// rarely did, since its clock sequence changes whenever two calls share a
// timestamp, which is why only spaced-out calls show it.
func TestNewIDDoesNotRepeatAtUploadPace(t *testing.T) {
	const count = 4000
	const spacing = 50 * time.Microsecond
	seen := make(map[string]int, count)
	for i := 0; i < count; i++ {
		id := NewID()
		if first, ok := seen[id]; ok {
			t.Fatalf("NewID repeated %s: calls %d and %d", id, first, i)
		}
		seen[id] = i
		// Busy-wait instead of time.Sleep, whose resolution on some systems
		// is a millisecond or more and would make this take far too long.
		for start := time.Now(); time.Since(start) < spacing; {
		}
	}
}
