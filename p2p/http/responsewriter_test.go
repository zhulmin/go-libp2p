package p2phttp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponseLooksCorrect(t *testing.T) {
	req, err := http.NewRequest("GET", "http://localhost/", bytes.NewReader([]byte("")))
	require.NoError(t, err)
	reqBuf := bytes.Buffer{}
	req.Write(&reqBuf)

	resp := bytes.Buffer{}
	respWriter := bufio.NewWriter(&resp)
	s := bufio.NewReadWriter(bufio.NewReader(&reqBuf), respWriter)

	ServeReadWriter(s, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello world"))
	}))

	respWriter.Flush()
	fmt.Println("Resp is", resp.String())
	parsedResponse, err := http.ReadResponse(bufio.NewReader(&resp), nil)
	require.NoError(t, err)
	fmt.Println("Parsed response is", parsedResponse)
}

func TestMultipleWritesButSmallResponseLooksCorrect(t *testing.T) {
	req, err := http.NewRequest("GET", "http://localhost/", bytes.NewReader([]byte("")))
	require.NoError(t, err)
	reqBuf := bytes.Buffer{}
	req.Write(&reqBuf)

	resp := bytes.Buffer{}
	respWriter := bufio.NewWriter(&resp)
	s := bufio.NewReadWriter(bufio.NewReader(&reqBuf), respWriter)

	ServeReadWriter(s, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello world 1 "))
		w.Write([]byte("2 "))
		w.Write([]byte("3 "))
		w.Write([]byte("4 "))
		w.Write([]byte("5 "))
		w.Write([]byte("6 "))
	}))

	respWriter.Flush()
	fmt.Println("Resp is", resp.String())
	parsedResponse, err := http.ReadResponse(bufio.NewReader(&resp), nil)
	require.NoError(t, err)
	respBody, err := io.ReadAll(parsedResponse.Body)
	require.NoError(t, err)
	require.Equal(t, "Hello world 1 2 3 4 5 6 ", string(respBody))
	require.Equal(t, len("Hello world 1 2 3 4 5 6 "), int(parsedResponse.ContentLength))
	fmt.Println("Parsed response is", parsedResponse)
}
