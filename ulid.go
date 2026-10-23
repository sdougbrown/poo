package poo

import (
	"crypto/rand"
	"io"
	"time"
)

// crockford is the Crockford base32 alphabet a ULID is written in.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewULID returns a new ULID: 48 bits of milliseconds since the Unix
// epoch followed by 80 random bits, written as 26 characters of Crockford
// base32.
//
// Ids made in different milliseconds sort in time order. Ids made in the
// same millisecond are unique but not ordered.
func NewULID() string {
	return newULID(time.Now(), rand.Reader)
}

// newULID builds a ULID from a time and an entropy source.
//
// A source that fails leaves the entropy it did not fill as zero: the
// system source does not fail.
func newULID(t time.Time, entropy io.Reader) string {
	var raw [16]byte
	ms := uint64(t.UnixMilli()) & (1<<48 - 1)
	for i := 0; i < 6; i++ {
		raw[i] = byte(ms >> (8 * (5 - i)))
	}
	_, _ = io.ReadFull(entropy, raw[6:])

	// 128 bits make 26 groups of 5 bits, the first one holding the top 3.
	var out [26]byte
	for i := 0; i < 26; i++ {
		// Bit offset of the group's low end, counted from the end of
		// the 128 bits.
		shift := 5 * (25 - i)
		out[i] = crockford[groupAt(raw, shift)]
	}
	return string(out[:])
}

// groupAt returns the 5 bits that end shift bits from the low end of the
// 128-bit value.
func groupAt(raw [16]byte, shift int) byte {
	var v uint16
	byteIdx := 15 - shift/8
	v = uint16(raw[byteIdx])
	if byteIdx > 0 {
		v |= uint16(raw[byteIdx-1]) << 8
	}
	return byte(v>>(shift%8)) & 0x1f
}
