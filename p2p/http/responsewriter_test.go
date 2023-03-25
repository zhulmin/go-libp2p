package p2phttp

import (
	"bufio"
	"bytes"
	"fmt"
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
