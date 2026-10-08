package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// GetJSON calls the local daemon and decodes a JSON body.
func GetJSON(ctx context.Context, socketPath, path string, dest any) error {
	body, err := do(ctx, socketPath, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(body, dest)
}

// PostJSON calls the local daemon with a JSON body and decodes the response.
func PostJSON(ctx context.Context, socketPath, path string, payload, dest any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	body, err := do(ctx, socketPath, http.MethodPost, path, raw)
	if err != nil {
		return err
	}
	if dest == nil || len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, dest)
}

// GetBytes calls the local daemon and returns the raw body.
func GetBytes(ctx context.Context, socketPath, path string) ([]byte, error) {
	return do(ctx, socketPath, http.MethodGet, path, nil)
}

func do(ctx context.Context, socketPath, method, path string, payload []byte) ([]byte, error) {
	if socketPath == "" {
		return nil, ErrStopped
	}
	client := &http.Client{
		Timeout: 3 * time.Minute,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://omlsd"+path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := stringsTrim(raw)
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("omlsd: %s", msg)
	}
	return raw, nil
}

func stringsTrim(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
