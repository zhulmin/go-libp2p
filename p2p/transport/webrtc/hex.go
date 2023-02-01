package libp2pwebrtc

import (
	"encoding/hex"
	"strings"
)

func encodeInterpersedHex(src []byte) string {
	var builder strings.Builder
	encodeInterpersedHexToBuilder(src, &builder)
	return builder.String()
}

func encodeInterpersedHexToBuilder(src []byte, builder *strings.Builder) {
	if src == nil {
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

func decodeInterpersedHex(src []byte) ([]byte, error) {
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

func decodeInterpersedHexFromASCIIString(src string) ([]byte, error) {
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
