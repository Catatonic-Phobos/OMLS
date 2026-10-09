package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Serve listens for reserve, write, read, and stat until ctx is cancelled.
func Serve(ctx context.Context, addr string, st *Store) error {
	if addr == "" {
		addr = DefaultListen
	}
	if st == nil {
		st = NewStore()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/reserve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Bytes  int    `json:"bytes"`
			Kind   string `json:"kind"`
			Source string `json:"source"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		stat, err := st.Reserve(req.Bytes, req.Kind, req.Source)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, stat)
	})
	mux.HandleFunc("/v1/write", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil || offset < 0 {
			http.Error(w, "offset is required", http.StatusBadRequest)
			return
		}
		body := http.MaxBytesReader(w, r.Body, Chunk+1024)
		buf, err := io.ReadAll(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := st.WriteAt(offset, buf); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, st.Stat())
	})
	mux.HandleFunc("/v1/read", func(w http.ResponseWriter, r *http.Request) {
		offset, err1 := strconv.Atoi(r.URL.Query().Get("offset"))
		length, err2 := strconv.Atoi(r.URL.Query().Get("length"))
		if err1 != nil || err2 != nil || offset < 0 || length < 0 || length > Chunk {
			http.Error(w, "offset and length are required", http.StatusBadRequest)
			return
		}
		buf, err := st.ReadAt(offset, length)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(buf)
	})
	mux.HandleFunc("/v1/stat", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, st.Stat())
	})
	mux.HandleFunc("/v1/release", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, st.Release())
	})

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	err = srv.Serve(ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Place reserves capacity on addr and copies slice.Data into it, then reads it back.
func Place(ctx context.Context, addr string, capacity int, sl Slice) (Stat, error) {
	if len(sl.Data) > capacity {
		return Stat{}, fmt.Errorf("slice %d is larger than reservation %d", len(sl.Data), capacity)
	}
	if err := postJSON(ctx, addr, "/v1/reserve", map[string]any{
		"bytes":  capacity,
		"kind":   sl.Kind,
		"source": sl.Source,
	}, nil); err != nil {
		return Stat{}, err
	}
	for off := 0; off < len(sl.Data); {
		end := off + Chunk
		if end > len(sl.Data) {
			end = len(sl.Data)
		}
		if err := postRaw(ctx, addr, fmt.Sprintf("/v1/write?offset=%d", off), sl.Data[off:end]); err != nil {
			return Stat{}, err
		}
		off = end
	}
	got := make([]byte, 0, len(sl.Data))
	for off := 0; off < len(sl.Data); {
		end := off + Chunk
		if end > len(sl.Data) {
			end = len(sl.Data)
		}
		part, err := getRaw(ctx, addr, fmt.Sprintf("/v1/read?offset=%d&length=%d", off, end-off))
		if err != nil {
			return Stat{}, err
		}
		got = append(got, part...)
		off = end
	}
	if !bytes.Equal(got, sl.Data) {
		return Stat{}, fmt.Errorf("remote reservation does not match the local slice")
	}
	var st Stat
	if err := getJSON(ctx, addr, "/v1/stat", &st); err != nil {
		return Stat{}, err
	}
	return st, nil
}

// FetchStat reads the reservation on addr.
func FetchStat(ctx context.Context, addr string) (Stat, error) {
	var st Stat
	err := getJSON(ctx, addr, "/v1/stat", &st)
	return st, err
}

func postJSON(ctx context.Context, addr, path string, payload any, dest any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return do(req, dest)
}

func postRaw(ctx context.Context, addr, path string, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	return do(req, nil)
}

func getJSON(ctx context.Context, addr, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+path, nil)
	if err != nil {
		return err
	}
	return do(req, dest)
}

func getRaw(ctx context.Context, addr, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", addr, stringsTrim(body))
	}
	return body, nil
}

func do(req *http.Request, dest any) error {
	resp, err := client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s", stringsTrim(body))
	}
	if dest == nil || len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, dest)
}

func client() *http.Client {
	return &http.Client{Timeout: 3 * time.Minute}
}

func writeJSON(w http.ResponseWriter, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func stringsTrim(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

// CachePort rewrites a fabric address host:7443 into the cache port.
func CachePort(fabricAddr string) string {
	host, _, err := net.SplitHostPort(fabricAddr)
	if err != nil {
		return fabricAddr
	}
	_, port, err := net.SplitHostPort(DefaultListen)
	if err != nil {
		port = "7444"
	}
	return net.JoinHostPort(host, port)
}
