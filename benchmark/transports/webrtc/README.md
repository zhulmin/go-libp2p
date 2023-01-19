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
go ./benchmark/transports/webrtc/main.go -l 9999 -t webrtc
```

This should output a multiaddr which can be used by the client to connect.
Other transport values supported instead of `webrtc` are: `tcp`, `quic`, `websocket` and `webtransport`.

### Client

Run:

```
go ./benchmark/transports/webrtc/main.go -d <multiaddr> -c <number of conns> -s <number of streams>
```

> TODO: why does it not exit by itself?! On what is it stuck?

> TODO: how to pprof this?!

> TODO: how to get some decent graphs that could be investigated easily?
