package libp2phttp

import (
	"net/url"

	ma "github.com/multiformats/go-multiaddr"
)

const P_HTTPPATH = 0x400000 // Temporary private use code

func httpPathStoB(s string) ([]byte, error) {
	b, err := url.PathUnescape(s)
	if err != nil {
		return nil, err
	}
	return []byte(b), nil
}

func httpPathBtoS(b []byte) (string, error) {
	s := url.PathEscape(string(b))
	return s, nil
}

func init() {
	protoHttpPath := ma.Protocol{
		Name:       "httppath",
		Code:       P_HTTPPATH,
		VCode:      ma.CodeToVarint(P_HTTPPATH),
		Size:       ma.LengthPrefixedVarSize,
		Transcoder: ma.NewTranscoderFromFunctions(httpPathStoB, httpPathBtoS, nil),
	}

	err := ma.AddProtocol(protoHttpPath)
	if err != nil {
		panic(err)
	}
}
