# WebRTC Transport Benchmarks

This directory contains a benchmarking tool and instructions how to use it,
to measure the performance of the WebRTC transport.

## Instructions

In this section we'll show you how to run this benchmarking tool on your local (development) machine.

1. Run a listener
2. Run a client

... And then?!

### Listener

Run:

```
go run ./benchmark/transports/webrtc listen
```

This should output a multiaddr which can be used by the client to connect.
Other transport values supported instead of `webrtc` are: `tcp`, `quic`, `websocket` and `webtransport`.

The listener will continue to run until you kill it.

### Client

Run:

```
go run ./benchmark/transports/webrtc dial <multiaddr>
```

You can configure the number of streams and connections opened by the dialer using opt-in flags.

The client will continue to run until you kill it.

> TODO: how to pprof this?!

> TODO: how to get some decent graphs that could be investigated easily?
