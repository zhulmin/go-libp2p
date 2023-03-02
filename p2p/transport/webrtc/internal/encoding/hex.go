package encoding

import (
	"encoding/hex"
	"strings"
)

// EncodeInterspersedHex encodes a byte slice into a string of hex characters,
// separating each encoded byte with a colon (':').
//
// Example: { 0x01, 0x02, 0x03 } -> "01:02:03"
func EncodeInterspersedHex(src []byte) string {
	var builder strings.Builder
	EncodeInterspersedHexToBuilder(src, &builder)
	return builder.String()
}

// EncodeInterspersedHexToBuilder encodes a byte slice into a of hex characters,
// separating each encoded byte with a colon (':'). String is written to the builder.
//
// Example: { 0x01, 0x02, 0x03 } -> "01:02:03"
func EncodeInterspersedHexToBuilder(src []byte, builder *strings.Builder) {
	if len(src) == 0 {
		return
	}
	builder.Grow(len(src)*3 - 1)
	v := src[0]
	builder.WriteByte(hextable[v>>4])
	builder.WriteByte(hextable[v&0x0f])
	for _, v = range src[1:] {
		builder.WriteByte(':')
		builder.WriteByte(hextable[v>>4])
		builder.WriteByte(hextable[v&0x0f])
	}
}

// DecodeInterspersedHex decodes a byte slice string of hex characters into a byte slice,
// where the hex characters are expected to be separated by a colon (':').
//
// Example: {'0', '1', ':', '0', '2', ':', '0', '3'} -> { 0x01, 0x02, 0x03 }
func DecodeInterspersedHex(src []byte) ([]byte, error) {
	if len(src) == 0 {
		return []byte{}, nil
	}

	dst := make([]byte, (len(src)+1)/3)
	i, j := 0, 1
	for ; j < len(src); j += 3 { // jump one extra byte for the separator (:)
		p := src[j-1]
		q := src[j]

		a := reverseHexTable[p]
		b := reverseHexTable[q]
		if a > 0x0f {
			return nil, hex.InvalidByteError(p)
		}
		if b > 0x0f {
			return nil, hex.InvalidByteError(q)
		}
		dst[i] = (a << 4) | b
		i++
	}
	if (len(src)+1)%3 != 0 {
		// Check for invalid char before reporting bad length,
		// since the invalid char (if present) is an earlier problem.
		if reverseHexTable[src[j-1]] > 0x0f {
			return nil, hex.InvalidByteError(src[j-1])
		}
		return nil, hex.ErrLength
	}
	return dst[:i], nil
}

// DecodeInterpersedHexFromASCIIString decodes an ASCII string of hex characters into a byte slice,
// where the hex characters are expected to be separated by a colon (':').
//
// NOTE that this function returns an error in case the input string contains non-ASCII characters.
//
// Example: "01:02:03" -> { 0x01, 0x02, 0x03 }
func DecodeInterpersedHexFromASCIIString(src string) ([]byte, error) {
	if len(src) == 0 {
		return []byte{}, nil
	}

	dst := make([]byte, (len(src)+1)/3)
	i, j := 0, 1
	for ; j < len(src); j += 3 { // jump one extra byte for the separator (:)
		p := src[j-1]
		q := src[j]

		a := reverseHexTable[p]
		b := reverseHexTable[q]
		if a > 0x0f {
			return nil, hex.InvalidByteError(p)
		}
		if b > 0x0f {
			return nil, hex.InvalidByteError(q)
		}
		dst[i] = (a << 4) | b
		i++
	}
	if (len(src)+1)%3 != 0 {
		// Check for invalid char before reporting bad length,
		// since the invalid char (if present) is an earlier problem.
		if reverseHexTable[src[j-1]] > 0x0f {
			return nil, hex.InvalidByteError(src[j-1])
		}
		return nil, hex.ErrLength
	}
	return dst[:i], nil
}

const (
	hextable        = "0123456789abcdef"
	reverseHexTable = "" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\xff\xff\xff\xff\xff\xff" +
		"\xff\x0a\x0b\x0c\x0d\x0e\x0f\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\x0a\x0b\x0c\x0d\x0e\x0f\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff"
)
