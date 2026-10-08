package update_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Catatonic-Phobos/OMLS/internal/update"
)

func TestNormalizeAndVersionsEqual(t *testing.T) {
	if update.NormalizeVersion("v1.1.0") != "1.1.0" {
		t.Fatal(update.NormalizeVersion("v1.1.0"))
	}
	if !update.VersionsEqual("v1.1.0", "1.1.0") {
		t.Fatal("expected equal")
	}
	if update.VersionsEqual("1.0.0", "1.1.0") {
		t.Fatal("expected unequal")
	}
}

func TestAssetNameFor(t *testing.T) {
	if update.AssetNameFor("linux", "amd64") != "omls-linux-amd64.tar.gz" {
		t.Fatal(update.AssetNameFor("linux", "amd64"))
	}
}

func TestSelectAsset(t *testing.T) {
	rel := update.Release{
		TagName: "v1.1.0",
		Assets: []update.Asset{
			{Name: "notes.md", BrowserDownloadURL: "http://example/notes"},
			{Name: "omls-linux-amd64.tar.gz", BrowserDownloadURL: "http://example/pack"},
		},
	}
	a, err := update.SelectAsset(rel, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "omls-linux-amd64.tar.gz" {
		t.Fatalf("got %s", a.Name)
	}
	if _, err := update.SelectAsset(rel, "linux", "arm64"); err == nil {
		t.Fatal("expected missing arch error")
	}
	empty := update.Release{TagName: "v1.0.0"}
	if _, err := update.SelectAsset(empty, "linux", "amd64"); err == nil || !strings.Contains(err.Error(), "no downloadable packs") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseReleaseJSON(t *testing.T) {
	raw := []byte(`{"tag_name":"v1.1.0","name":"OMLS 1.1","assets":[{"name":"omls-linux-amd64.tar.gz","browser_download_url":"https://example/x","size":12}]}`)
	rel, err := update.ParseReleaseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v1.1.0" || len(rel.Assets) != 1 {
		t.Fatalf("%+v", rel)
	}
	if _, err := update.ParseReleaseJSON([]byte(`{}`)); err == nil {
		t.Fatal("expected missing tag")
	}
}

func TestPlanAndRunCheckOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/Catatonic-Phobos/OMLS/releases/latest" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(update.Release{
			TagName: "v1.2.0",
			Assets: []update.Asset{{
				Name:               "omls-linux-amd64.tar.gz",
				BrowserDownloadURL: "https://example.invalid/pack",
			}},
		})
	}))
	defer srv.Close()

	var out bytes.Buffer
	res, err := update.Plan(update.Options{
		CurrentVersion: "1.1.0",
		OS:             "linux",
		Arch:           "amd64",
		APIBase:        srv.URL,
		HTTPClient:     srv.Client(),
		Stdout:         &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.UpToDate || res.LatestVer != "1.2.0" {
		t.Fatalf("%+v", res)
	}

	res, err = update.Run(update.Options{
		CurrentVersion: "1.2.0",
		OS:             "linux",
		Arch:           "amd64",
		CheckOnly:      true,
		APIBase:        srv.URL,
		HTTPClient:     srv.Client(),
		Stdout:         &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.UpToDate || res.Installed {
		t.Fatalf("%+v out=%s", res, out.String())
	}
}

func TestDownloadExtractAndInstall(t *testing.T) {
	packBytes := buildTestPack(t)
	var srvURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/Catatonic-Phobos/OMLS/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(update.Release{
			TagName: "v1.1.1",
			Assets: []update.Asset{{
				Name:               "omls-linux-amd64.tar.gz",
				BrowserDownloadURL: srvURL + "/omls-linux-amd64.tar.gz",
			}},
		})
	})
	mux.HandleFunc("/omls-linux-amd64.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(packBytes)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL

	var installed string
	var out bytes.Buffer
	res, err := update.Run(update.Options{
		CurrentVersion: "1.1.0",
		OS:             "linux",
		Arch:           "amd64",
		APIBase:        srv.URL,
		HTTPClient:     srv.Client(),
		Stdout:         &out,
		Stderr:         &out,
		InstallFunc: func(packDir string) error {
			installed = packDir
			if _, err := os.Stat(filepath.Join(packDir, "install.sh")); err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(packDir, "omls")); err != nil {
				return err
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Installed || installed == "" {
		t.Fatalf("res=%+v installed=%q out=%s", res, installed, out.String())
	}
}

func buildTestPack(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string]string{
		"omls-linux-amd64/install.sh":      "#!/bin/sh\necho ok\n",
		"omls-linux-amd64/omls":            "#!/bin/sh\necho omls 1.1.1\n",
		"omls-linux-amd64/omls.service.in": "[Unit]\nDescription=OMLS\n",
		"omls-linux-amd64/PACK.txt":        "version=1.1.1\n",
	}
	for name, body := range files {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}
		if strings.HasSuffix(name, ".in") || strings.HasSuffix(name, ".txt") {
			hdr.Mode = 0o644
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
