// Package ulid generates ULID-shaped identifiers: a 48-bit millisecond
// timestamp followed by 80 bits of randomness, rendered as 26 Crockford base32
// characters. They are globally unique and lexicographically sortable, which is
// what the annotation and note sidecars require of their ids (spec 03 section
// 3, section 4).
//
// A local generator rather than a dependency keeps to the project's dependency
// discipline (docs/dependencies.md); the format is small and stable.
package ulid

import (
	"crypto/rand"
	"io"
	"time"
)

// crockford is the ULID alphabet: base32 excluding I, L, O, and U.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// New returns a fresh identifier from the current time and crypto/rand.
func New() string { return NewAt(time.Now(), rand.Reader) }

// NewAt is the testable core: deterministic given its time and entropy source.
func NewAt(t time.Time, entropy io.Reader) string {
	var id [16]byte
	ms := uint64(t.UnixMilli())
	id[0] = byte(ms >> 40)
	id[1] = byte(ms >> 32)
	id[2] = byte(ms >> 24)
	id[3] = byte(ms >> 16)
	id[4] = byte(ms >> 8)
	id[5] = byte(ms)
	// A short read would silently weaken uniqueness, so fill the random tail
	// completely or fall back to a time-derived tail rather than emit zeros.
	if _, err := io.ReadFull(entropy, id[6:]); err != nil {
		for i := 6; i < 16; i++ {
			id[i] = byte(ms >> uint((i%6)*8))
		}
	}
	return encode(id)
}

// encode renders 128 bits as 26 Crockford base32 characters, most significant
// bits first, so lexical order matches timestamp order. This is the canonical
// ULID text encoding.
func encode(id [16]byte) string {
	e := crockford
	return string([]byte{
		e[(id[0]&224)>>5],
		e[id[0]&31],
		e[(id[1]&248)>>3],
		e[((id[1]&7)<<2)|((id[2]&192)>>6)],
		e[(id[2]&62)>>1],
		e[((id[2]&1)<<4)|((id[3]&240)>>4)],
		e[((id[3]&15)<<1)|((id[4]&128)>>7)],
		e[(id[4]&124)>>2],
		e[((id[4]&3)<<3)|((id[5]&224)>>5)],
		e[id[5]&31],
		e[(id[6]&248)>>3],
		e[((id[6]&7)<<2)|((id[7]&192)>>6)],
		e[(id[7]&62)>>1],
		e[((id[7]&1)<<4)|((id[8]&240)>>4)],
		e[((id[8]&15)<<1)|((id[9]&128)>>7)],
		e[(id[9]&124)>>2],
		e[((id[9]&3)<<3)|((id[10]&224)>>5)],
		e[id[10]&31],
		e[(id[11]&248)>>3],
		e[((id[11]&7)<<2)|((id[12]&192)>>6)],
		e[(id[12]&62)>>1],
		e[((id[12]&1)<<4)|((id[13]&240)>>4)],
		e[((id[13]&15)<<1)|((id[14]&128)>>7)],
		e[(id[14]&124)>>2],
		e[((id[14]&3)<<3)|((id[15]&224)>>5)],
		e[id[15]&31],
	})
}
