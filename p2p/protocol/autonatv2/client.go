package autonatv2

import "github.com/libp2p/go-libp2p/core/host"

type Client struct {
	host host.Host
}

func NewClient(h host.Host) *Client {
	return &Client{host: h}
}

func (c *Client) CheckReachability() bool {
	return true
}
