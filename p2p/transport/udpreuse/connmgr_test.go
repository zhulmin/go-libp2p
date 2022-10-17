package udpreuse

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/lucas-clemente/quic-go"

	"github.com/stretchr/testify/require"
)

func getTLSConf(t *testing.T, domain string) *tls.Config {
	t.Helper()
	cert, priv := generateCert(t, domain)
	return &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{cert.Raw},
			PrivateKey:  priv,
			Leaf:        cert,
		}},
	}
}

func generateCert(t *testing.T, domain string) (*x509.Certificate, *ecdsa.PrivateKey) {
	certTempl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: domain},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caPrivateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caBytes, err := x509.CreateCertificate(rand.Reader, certTempl, certTempl, &caPrivateKey.PublicKey, caPrivateKey)
	require.NoError(t, err)
	ca, err := x509.ParseCertificate(caBytes)
	require.NoError(t, err)
	return ca, caPrivateKey
}

func TestListening(t *testing.T) {
	cm, err := NewConnManager(true)
	require.NoError(t, err)

	tlsConf1 := getTLSConf(t, "proto1.io")
	tlsConf1.NextProtos = []string{"proto1"}
	ln1, err := cm.ListenQUIC("udp4", &net.UDPAddr{}, "proto1", tlsConf1)
	require.NoError(t, err)
	defer ln1.Close()
	tlsConf2 := getTLSConf(t, "proto2.io")
	tlsConf2.NextProtos = []string{"proto2"}
	ln2, err := cm.ListenQUIC("udp4", &net.UDPAddr{}, "proto2", tlsConf2)
	require.NoError(t, err)
	defer ln2.Close()
	addr := ln1.Addr()
	require.Equal(t, addr, ln2.Addr())

	connChan1 := make(chan quic.Connection)
	connChan2 := make(chan quic.Connection)
	go func() {
		conn, err := ln1.Accept(context.Background())
		require.NoError(t, err)
		connChan1 <- conn
	}()
	go func() {
		conn, err := ln2.Accept(context.Background())
		require.NoError(t, err)
		connChan2 <- conn
	}()

	udpConn, err := net.ListenUDP("udp4", nil)
	require.NoError(t, err)

	conn1, err := quic.Dial(udpConn, addr, "proto1.io", &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"proto1"}}, nil)
	require.NoError(t, err)
	require.Equal(t, "proto1.io", conn1.ConnectionState().TLS.ServerName)
	require.Equal(t, "proto1", conn1.ConnectionState().TLS.NegotiatedProtocol)
	select {
	case sconn1 := <-connChan1:
		sconn1.CloseWithError(0, "")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	conn2, err := quic.Dial(udpConn, addr, "proto2.io", &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"proto2"}}, nil)
	require.NoError(t, err)
	require.Equal(t, "proto2.io", conn2.ConnectionState().TLS.ServerName)
	require.Equal(t, "proto2", conn2.ConnectionState().TLS.NegotiatedProtocol)
	select {
	case sconn2 := <-connChan2:
		sconn2.CloseWithError(0, "")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestListenOnSameProto(t *testing.T) {
	cm, err := NewConnManager(true)
	require.NoError(t, err)

	tlsConf := getTLSConf(t, "proto1.io")
	tlsConf.NextProtos = []string{"proto1"}
	ln1, err := cm.ListenQUIC("udp4", &net.UDPAddr{}, "proto1", tlsConf)
	require.NoError(t, err)
	defer ln1.Close()

	// listening on the same address doesn't work
	_, err = cm.ListenQUIC("udp4", &net.UDPAddr{}, "proto1", tlsConf)
	require.EqualError(t, err, "already listening for protocol proto1")

	// listening on a different address is fine
	ln2, err := cm.ListenQUIC("udp6", &net.UDPAddr{}, "proto1", tlsConf)
	require.NoError(t, err)
	defer ln2.Close()
}

func TestDistinctListeners(t *testing.T) {
	cm, err := NewConnManager(true)
	require.NoError(t, err)

	tlsConf1 := getTLSConf(t, "proto1.io")
	tlsConf1.NextProtos = []string{"proto1"}
	ln1, err := cm.ListenQUIC("udp4", &net.UDPAddr{}, "proto1", tlsConf1)
	require.NoError(t, err)
	defer ln1.Close()
	tlsConf2 := getTLSConf(t, "proto2.io")
	tlsConf2.NextProtos = []string{"proto2"}
	ln2, err := cm.ListenQUIC("udp6", &net.UDPAddr{}, "proto2", tlsConf2)
	require.NoError(t, err)
	defer ln2.Close()

	udpConn4, err := net.ListenUDP("udp4", nil)
	require.NoError(t, err)
	conn1, err := quic.Dial(udpConn4, ln1.Addr(), "proto1.io", &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"proto1"}}, nil)
	require.NoError(t, err)
	defer conn1.CloseWithError(0, "")
	require.Equal(t, "proto1", conn1.ConnectionState().TLS.NegotiatedProtocol)
	_, err = quic.Dial(udpConn4, ln1.Addr(), "proto2.io", &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"proto2"}}, nil)
	require.Error(t, err)

	udpConn6, err := net.ListenUDP("udp6", nil)
	require.NoError(t, err)
	conn2, err := quic.Dial(udpConn6, ln2.Addr(), "proto1.io", &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"proto2"}}, nil)
	require.NoError(t, err)
	defer conn2.CloseWithError(0, "")
	require.Equal(t, "proto1", conn1.ConnectionState().TLS.NegotiatedProtocol)
	_, err = quic.Dial(udpConn6, ln2.Addr(), "proto2.io", &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"proto1"}}, nil)
	require.Error(t, err)
}
