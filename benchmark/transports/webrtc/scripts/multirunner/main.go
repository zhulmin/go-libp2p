// runner used to run the multiple tests in series for the
// different transports for one of the described benchmark scenarios
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
	"time"
)

var (
	// listen flags
	flagListenRunDuration time.Duration
	flagListenPort        int

	// dial flags
	flagDialAddress          string
	flagDialScenario         int
	flagDialCooldownDuration time.Duration
)

const (
	DEFAULT_RUN_DURATION      = 5 * time.Minute
	DEFAULT_COOLDOWN_DURATION = 5 * time.Second
)

var (
	scenarios = []struct {
		Transports  []string
		Connections int
		Streams     int
	}{
		{
			Transports:  []string{"tcp", "websocket", "webrtc"},
			Connections: 10,
			Streams:     1000,
		},
		{
			Transports:  []string{"tcp", "websocket", "webrtc", "webtransport", "quic"},
			Connections: 100,
			Streams:     100,
		},
	}
)

func init() {
	// listen flags
	flag.IntVar(&flagListenPort, "l", 9080, "port to listen to, used for listen cmd")
	flag.DurationVar(&flagListenRunDuration, "d", DEFAULT_RUN_DURATION, "run duration")

	// dial flags
	flag.StringVar(&flagDialAddress, "a", "127.0.0.1:9080", "address to dial to")
	flag.IntVar(&flagDialScenario, "s", 0, "scenario to run")
	flag.DurationVar(&flagDialCooldownDuration, "w", DEFAULT_COOLDOWN_DURATION, "cooldown duration")

	flag.Parse()
}

func main() {
	cmd := strings.ToLower(strings.TrimSpace(flag.Arg(0)))

	switch cmd {
	case "listen":
		listen()

	case "dial":
		dial()
	}
}

type (
	MessageStartListener struct {
		Transport       *string `json:"t"`
		MetricsFileName *string `json:"m"`
	}

	MessageStartListenerResponse struct {
		Address string `json:"a"`
	}
)

func dial() {
	conn, err := net.Dial("tcp", flagDialAddress)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	buf := bufio.NewReader(conn)

	testScenario := scenarios[flagDialScenario]
	for _, transport := range testScenario.Transports {
		log.Printf("dialer: starting test for transport %s\n", transport)
		clientMetricsFileName := fmt.Sprintf("s%d_%s_dial.csv", flagDialScenario+1, transport)
		serverMetricsFileName := fmt.Sprintf("s%d_%s_listen.csv", flagDialScenario+1, transport)

		request, err := json.Marshal(MessageStartListener{
			Transport:       &transport,
			MetricsFileName: &serverMetricsFileName,
		})
		if err != nil {
			panic(err)
		}
		if _, err = conn.Write(append(request, '\n')); err != nil {
			panic(err)
		}

		response, err := buf.ReadBytes('\n')
		if err != nil {
			panic(err)
		}

		var responseMsg MessageStartListenerResponse
		if err = json.Unmarshal(response, &responseMsg); err != nil {
			panic(err)
		}

		runDialProcess(responseMsg.Address, transport, clientMetricsFileName, testScenario.Connections, testScenario.Streams)
		log.Printf("dialer: test for transport %s finished\n", transport)
		<-time.After(flagDialCooldownDuration)
		log.Println("dialer: cooldown finished")
	}
}

func runDialProcess(address string, transport string, metricsFileName string, connections int, streams int) {
	exec.Command(
		"go",
		"run",
		"./benchmark/transports/webrtc",
		"-t", transport,
		"-metrics", metricsFileName,
		"-c", fmt.Sprintf("%d", connections),
		"-s", fmt.Sprintf("%d", streams),
		"dial",
		address,
	).Run()
	// above cmd will shutdown with an error when server stopts listening, so no need to stop :)
}

func listen() {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", flagListenPort))
	if err != nil {
		panic(err)
	}

	for {
		log.Println("listener: waiting for connection...")
		conn, err := listener.Accept()
		if err != nil {
			panic(err)
		}

		log.Println("listener: handle connection...")
		go handleIncomingConn(conn)
	}
}

func handleIncomingConn(conn net.Conn) {
	defer conn.Close()

	buf := bufio.NewReader(conn)

	msg, err := buf.ReadBytes('\n')
	if err != nil {
		panic(err)
	}
	log.Printf("listener: received msg: %s\n", msg)

	var msgBody MessageStartListener
	if err = json.Unmarshal(msg, &msgBody); err != nil {
		panic(err)
	}

	transport := "webrtc"
	if msgBody.Transport != nil {
		transport = *msgBody.Transport
	}

	metrics := "csv"
	if msgBody.MetricsFileName != nil {
		metrics = *msgBody.MetricsFileName
	}

	osCmd := exec.Command(
		"go",
		"run",
		"./benchmark/transports/webrtc",
		"-t", transport,
		"-metrics", metrics,
		"listen",
	)

	cmdOut, err := osCmd.StdoutPipe()
	if err != nil {
		panic(err)
	}
	defer osCmd.Process.Kill()

	osCmd.Stderr = osCmd.Stdout
	if err = osCmd.Start(); err != nil {
		panic(err)
	}

	cmdOutBuf := bufio.NewReader(cmdOut)
	for {
		line, err := cmdOutBuf.ReadString('\n')
		if err != nil {
			panic(err)
		}
		fmt.Println(line)
		if strings.Contains(line, "listener: my address:") {
			address := strings.TrimSpace(strings.SplitN(line, "my address: ", 2)[1])
			response, err := json.Marshal(MessageStartListenerResponse{Address: address})
			if err != nil {
				panic(err)
			}
			if _, err = conn.Write(append(response, '\n')); err != nil {
				panic(err)
			}
			break
		}
	}

	// keep it running until duration finished :)
	log.Printf("listener: running server: waiting for test to finish (duration: %s)...\n", flagListenRunDuration)
	<-time.After(flagListenRunDuration)
	log.Println("listener: test finished")
}
