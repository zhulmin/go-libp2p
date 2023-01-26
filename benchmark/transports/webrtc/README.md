# WebRTC Transport Benchmarks

This directory contains a benchmarking tool and instructions how to use it,
to measure the performance of the WebRTC transport.

- [1. Instructions](#1-instructions)
  - [1.1. Listener](#11-listener)
    - [1.1.1. Metrics](#111-metrics)
  - [1.2. Client](#12-client)
  - [1.3. Profile](#13-profile)
- [2. Benchmarks](#2-benchmarks)
    - [2.1. Scenario 1](#21-scenario-1)
        - [2.1.1. Results](#211-results)
    - [2.2. Scenario 2](#22-scenario-2)
        - [2.2.1. Results](#221-results)

## 1. Instructions

In this section we'll show you how to run this benchmarking tool on your local (development) machine.

1. Run a listener
2. Run a client

What you do next to this depends on what you're after.

- Are you using it to get metrics from a standard and well defined cloud run?
- Are you using it to get metrics from your local machine?
- Are you using it to (Go) profile one or multiple things?

With that in mind, we'll show you how to do all of the above.

### 1.1. Listener

Run:

```
go run ./benchmark/transports/webrtc -c 2 -s 8 -metrics csv listen
```

This should output a multiaddr which can be used by the client to connect.
Other transport values supported instead of `webrtc` are: `tcp`, `quic`, `websocket` and `webtransport`.

The listener will continue to run until you kill it.

#### 1.1.1. Metrics

The metrics can be summarized using the `report` command:

```
go run ./benchmark/transports/webrtc report -s 16 metrics_listen_webrtc_c2_s8_e1_p0.csv
```

Which will print the result to the stdout of your terminal.
Or you can visualize them using the bundled python script:

```
./benchmark/transports/webrtc/scripts/visualise/visualise.py metrics_listen_webrtc_c2_s8_e1_p0.csv -s 16
```

Which will open a new window with your graph in it.

More useful is however to save it to a file so we can share it. For the WebRTC results of Scenario 1
we might for example use the following command:

```
 ./benchmark/transports/webrtc/scripts/visualise/visualise.py \
    -s 10000 \
    -o ./benchmark/transports/webrtc/images/s1_webrtc.png \
    ./benchmark/transports/webrtc/results/metrics_dial_webrtc_c10_s100_e1_p0.csv \
    ./benchmark/transports/webrtc/results/metrics_listen_webrtc_c10_s100_e1_p0.csv
```

### 1.2. Client

Run:

```
go run ./benchmark/transports/webrtc dial <multiaddr>
```

You can configure the number of streams and connections opened by the dialer using opt-in flags.

The client will continue to run until you kill it.

> Tip:
> 
> similar to the `listen` command you can also use the `-metrics <path>.csv` flag to output the metrics to a file.

### 1.3. Profile

Profiling the benchmark tool is supported using the Golang std pprof tool.

E.g. you can start your listener (or client) with the `-profile 6060` flag to enable profiling over http.

With your listener/client running you can then profile using te std golang tool, e.g.:

```
# get cpu profile
go tool pprof http://localhost:6060/debug/pprof/profile

# get memory (heap) profile
go tool pprof http://localhost:6060/debug/pprof/heap

# check contended mutexes
go tool pprof http://localhost:6060/debug/pprof/mutex

# check why threads block
go tool pprof http://localhost:6060/debug/pprof/block

# check the amount of created goroutines
go tool pprof http://localhost:6060/debug/pprof/goroutine
```

It will open an interactive window allowing you to inspect the heap/cpu profile, e.g. to see te top offenders
of your own code by focussing on the relevant module (e.g. `top github.com/libp2p/go-libp2p/p2p/transport/webrtc`).

And of course you can also use the `-pdf` flag to output it to a file instead that you can view in your browser or
any other capable pdf viewer.

## 2. Benchmarks

The goal of this tooling was to be able to benchmark how the WebRTC transport performs on its own
as well as compared to other transports such as QUIC and WebTransport. Not all scenarios which are benchmarked
are compatible with the different transports, but WebRTC is tested on all benchmarked scenarios.

The scenarios described below and the results you'll find at the end are ran on / come from two c5 large EC2 instances.
Each instance has 8 vCPUs and 16GB RAM. More information can be found at:
https://aws.amazon.com/ec2/instance-types/c5/

**Scenario 1:**

1. Server, on EC2 instance A, listens on a generated multi address.
2. Client, on EC2 instance B, dials 10 connections, with 1000 streams per connection to the server.

**Scenario 2:**

1. Server, on EC2 instance A, listens on a generated multi address.
2. Client, on EC2 instance B, dials 100 connections, with 100 streams per connection to the server.

For both scenarios the following holds true:

- Connections are ramped up at the rate of 1 connection/sec. 
- Streams are created at the rate of 10 streams/sec.
- This is done to ensure the webrtc transport's inflight request limiting does not start rejecting connections.
- The client opens streams to the server and runs the echo protocol writing 2KiB/s per stream (1 KiB every 500ms).
- We let the tests run for about 5 minute each.

The instances are running each scenario variation one by one, as such there at any given moment only one benchmark script running.

### 2.1. Scenario 1

Server:

```
# TCP
go run ./benchmark/transports/webrtc -metrics metrics_s1_tcp_server.csv -t tcp listen
# copy addressA

# WebSocket
go run ./benchmark/transports/webrtc -metrics csv -t websocket listen
# copy addressB

# WebRTC
go run ./benchmark/transports/webrtc -metrics csv -c listen
# copy addressC
```

Client:

```
# TCP
go run ./benchmark/transports/webrtc -metrics csv -t tcp -c 10 -s 1000 dial <addressA>

# WebSocket
go run ./benchmark/transports/webrtc -metrics csv -t websocket -c 10 -s 1000 dial <addressB>

# WebRTC
go run ./benchmark/transports/webrtc -metrics csv -c 10 -s 1000 dial <addressC>
```

#### 2.1.1. Results

**TCP**

![Scenario 1 — TCP](./images/s1_tcp.png)

**WebSocket (WS)**

![Scenario 1 — WebSocket](./images/s1_WebSocket.png)

**WebRTC**

![Scenario 1 — WebRTC](./images/s1_webrtc.png)

### 2.2. Scenario 2

Server:

```
# TCP
go run ./benchmark/transports/webrtc -metrics csv -t tcp listen
# copy addressA

# WebSocket
go run ./benchmark/transports/webrtc -metrics csv -t websocket listen
# copy addressB

# WebRTC
go run ./benchmark/transports/webrtc -metrics csv -c listen
# copy addressC

# QUIC
go run ./benchmark/transports/webrtc -metrics csv -c listen
# copy addressD

# WebTransport
go run ./benchmark/transports/webrtc -metrics csv -c listen
# copy addressE
```

Client:

```
# TCP
go run ./benchmark/transports/webrtc -metrics csv -t tcp -c 100 -s 100 dial <addressA>

# WebSocket
go run ./benchmark/transports/webrtc -metrics csv -t websocket -c 100 -s 100 dial <addressB>

# WebRTC
go run ./benchmark/transports/webrtc -metrics csv -c 100 -s 100 dial <addressC>

# QUIC
go run ./benchmark/transports/webrtc -metrics csv -t quic -c 100 -s 100 dial <addressD>

# WebTransport
go run ./benchmark/transports/webrtc -metrics csv -t webtransport -c 100 -s 100 dial <addressE>
```

#### 2.2.1. Results

**TCP**

![Scenario 2 — TCP](./images/s2_tcp.png)

**WebSocket (WS)**

![Scenario 2 — WebSocket](./images/s2_WebSocket.png)

**WebRTC**

![Scenario 2 — WebRTC](./images/s2_webrtc.png)

**QUIC**

![Scenario 2 — QUIC](./images/s2_quic.png)

**WebTransport**

![Scenario 2 — WebTransport](./images/s2_webtransport.png)
