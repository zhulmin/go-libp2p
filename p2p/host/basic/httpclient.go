package basichost

import (
	"net/http"
	"net/url"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/net/gostream"
)

func newHTTPClientViaStreams(s gostream.Streamer, p peer.ID) (*http.Client, error) {
	transport := gostream.NewTransport(s)
	u, err := url.Parse("libp2p://" + p.String())
	if err != nil {
		return nil, err
	}
	rt := roundTripperWithBaseURL{transport, u}

	return &http.Client{Transport: rt}, nil
}

type roundTripperWithBaseURL struct {
	rt      http.RoundTripper
	baseURL *url.URL
}

func (rt roundTripperWithBaseURL) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := rt.baseURL.JoinPath(req.URL.Path)
	newURL.RawQuery = req.URL.RawQuery
	req.URL = newURL
	return rt.rt.RoundTrip(req)
}
