package encoding

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeInterspersedHex(t *testing.T) {
	b, err := hex.DecodeString("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
	require.NoError(t, err)
	require.Equal(t, "ba:78:16:bf:8f:01:cf:ea:41:41:40:de:5d:ae:22:23:b0:03:61:a3:96:17:7a:9c:b4:10:ff:61:f2:00:15:ad", EncodeInterspersedHex(b))
}

func TestEncodeInterspersedHexToBuilder(t *testing.T) {
	b, err := hex.DecodeString("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
	require.NoError(t, err)
	var builder strings.Builder
	EncodeInterspersedHexToBuilder(b, &builder)
	require.Equal(t, "ba:78:16:bf:8f:01:cf:ea:41:41:40:de:5d:ae:22:23:b0:03:61:a3:96:17:7a:9c:b4:10:ff:61:f2:00:15:ad", builder.String())
}

func TestDecodeInterpersedHexStringLowerCase(t *testing.T) {
	b, err := DecodeInterpersedHexFromASCIIString("ba:78:16:bf:8f:01:cf:ea:41:41:40:de:5d:ae:22:23:b0:03:61:a3:96:17:7a:9c:b4:10:ff:61:f2:00:15:ad")
	require.NoError(t, err)
	require.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", hex.EncodeToString(b))
}

func TestDecodeInterpersedHexStringMixedCase(t *testing.T) {
	b, err := DecodeInterpersedHexFromASCIIString("Ba:78:16:BF:8F:01:cf:ea:41:41:40:De:5d:ae:22:23:b0:03:61:a3:96:17:7a:9c:b4:10:FF:61:f2:00:15:ad")
	require.NoError(t, err)
	require.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", hex.EncodeToString(b))
}

func TestDecodeInterpersedHexStringOneByte(t *testing.T) {
	b, err := DecodeInterpersedHexFromASCIIString("ba")
	require.NoError(t, err)
	require.Equal(t, "ba", hex.EncodeToString(b))
}

func TestDecodeInterpersedHexBytesLowerCase(t *testing.T) {
	b, err := DecodeInterspersedHex([]byte("ba:78:16:bf:8f:01:cf:ea:41:41:40:de:5d:ae:22:23:b0:03:61:a3:96:17:7a:9c:b4:10:ff:61:f2:00:15:ad"))
	require.NoError(t, err)
	require.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", hex.EncodeToString(b))
}

func TestDecodeInterpersedHexBytesMixedCase(t *testing.T) {
	b, err := DecodeInterspersedHex([]byte("Ba:78:16:BF:8F:01:cf:ea:41:41:40:De:5d:ae:22:23:b0:03:61:a3:96:17:7a:9c:b4:10:FF:61:f2:00:15:ad"))
	require.NoError(t, err)
	require.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", hex.EncodeToString(b))
}

func TestDecodeInterpersedHexBytesOneByte(t *testing.T) {
	b, err := DecodeInterspersedHex([]byte("ba"))
	require.NoError(t, err)
	require.Equal(t, "ba", hex.EncodeToString(b))
}
