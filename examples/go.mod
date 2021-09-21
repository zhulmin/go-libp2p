module github.com/libp2p/go-libp2p/examples

go 1.16

require (
	github.com/gogo/protobuf v1.3.2
	github.com/google/uuid v1.3.0
	github.com/ipfs/go-datastore v0.4.6
	github.com/ipfs/go-log/v2 v2.3.0
	github.com/libp2p/go-libp2p v0.14.4
	github.com/libp2p/go-libp2p-connmgr v0.2.4
	github.com/libp2p/go-libp2p-core v0.10.1-0.20210921170543-f829c09c1ca0
	github.com/libp2p/go-libp2p-discovery v0.5.1
	github.com/libp2p/go-libp2p-kad-dht v0.13.0
	github.com/libp2p/go-libp2p-noise v0.3.1-0.20210921174126-fa31d59f9697
	github.com/libp2p/go-libp2p-swarm v0.6.1-0.20210922105329-6a837b036140
	github.com/libp2p/go-libp2p-tls v0.3.1-0.20210921173952-a113dc2e87ca
	github.com/multiformats/go-multiaddr v0.4.1
)

// Ensure that examples always use the go-libp2p version in the same git checkout.
replace github.com/libp2p/go-libp2p => ../
