// Package tlsconfig builds mTLS credentials for the OMLS fabric.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// LoadServer loads a server TLS config that requires and verifies client certs.
func LoadServer(caFile, certFile, keyFile string) (*tls.Config, error) {
	cert, pool, err := load(caFile, certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// LoadClient loads a client TLS config that verifies the server against the CA.
func LoadClient(caFile, certFile, keyFile, serverName string) (*tls.Config, error) {
	cert, pool, err := load(caFile, certFile, keyFile)
	if err != nil {
		return nil, err
	}
	if serverName == "" {
		serverName = "localhost"
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func load(caFile, certFile, keyFile string) (tls.Certificate, *x509.CertPool, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("load key pair: %w", err)
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("read CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return tls.Certificate{}, nil, fmt.Errorf("parse CA: no certificates found in %s", caFile)
	}
	return cert, pool, nil
}
