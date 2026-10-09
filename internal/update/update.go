// Package update downloads the latest OMLS release pack from GitHub and installs it.
package update

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// DefaultRepo is the GitHub repository that publishes OMLS packs.
	DefaultRepo = "Catatonic-Phobos/OMLS"
	// AssetPattern is omls-<os>-<arch>.tar.gz
	apiBase = "https://api.github.com"
)

// Options controls an update run.
type Options struct {
	Repo           string // owner/name
	CurrentVersion string // e.g. 1.1.0
	Arch           string // GOARCH; empty = runtime.GOARCH
	OS             string // GOOS; empty = runtime.GOOS
	CheckOnly      bool
	Force          bool
	APIBase        string // default https://api.github.com
	HTTPClient     *http.Client
	InstallRoot    string // override extract+install working dir (tests)
	Stdout         io.Writer
	Stderr         io.Writer
	// InstallFunc replaces the default pack installer (tests).
	InstallFunc func(packDir string) error
}

// Release is the subset of the GitHub Releases API used by omls update.
type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Assets  []Asset `json:"assets"`
}

// Asset is a downloadable release file.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Result is the outcome of Check or Apply.
type Result struct {
	Current     string
	LatestTag   string
	LatestVer   string
	AssetName   string
	DownloadURL string
	UpToDate    bool
	Ahead       bool // local build is newer than the published release
	Installed   bool
}

// AssetNameFor returns the expected pack filename for os/arch.
func AssetNameFor(goos, arch string) string {
	return fmt.Sprintf("omls-%s-%s.tar.gz", goos, arch)
}

// NormalizeVersion strips a leading "v" and surrounding space.
func NormalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	return strings.TrimPrefix(v, "v")
}

// VersionsEqual compares release tags / version strings without a leading v.
func VersionsEqual(a, b string) bool {
	return CompareVersions(a, b) == 0
}

// CompareVersions returns -1 when a is older than b, 0 when equal, and 1 when a is newer.
// Missing numeric parts count as zero. A leading v is ignored.
func CompareVersions(a, b string) int {
	as := versionParts(a)
	bs := versionParts(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func versionParts(v string) []int {
	v = NormalizeVersion(v)
	if v == "" {
		return nil
	}
	chunks := strings.Split(v, ".")
	out := make([]int, 0, len(chunks))
	for _, c := range chunks {
		n := 0
		for _, r := range c {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}

// SelectAsset finds the pack asset for goos/arch on a release.
func SelectAsset(rel Release, goos, arch string) (Asset, error) {
	want := AssetNameFor(goos, arch)
	for _, a := range rel.Assets {
		if a.Name == want && a.BrowserDownloadURL != "" {
			return a, nil
		}
	}
	var names []string
	for _, a := range rel.Assets {
		names = append(names, a.Name)
	}
	if len(names) == 0 {
		return Asset{}, fmt.Errorf("release %s has no downloadable packs; attach %s to the GitHub Release", rel.TagName, want)
	}
	return Asset{}, fmt.Errorf("release %s has no asset %q (found: %s)", rel.TagName, want, strings.Join(names, ", "))
}

// ParseReleaseJSON unmarshals a GitHub release payload.
func ParseReleaseJSON(raw []byte) (Release, error) {
	var rel Release
	if err := json.Unmarshal(raw, &rel); err != nil {
		return Release{}, err
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return Release{}, fmt.Errorf("release missing tag_name")
	}
	return rel, nil
}

func (o Options) client() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (o Options) stdout() io.Writer {
	if o.Stdout != nil {
		return o.Stdout
	}
	return os.Stdout
}

func (o Options) stderr() io.Writer {
	if o.Stderr != nil {
		return o.Stderr
	}
	return os.Stderr
}

func (o Options) repo() string {
	if o.Repo != "" {
		return o.Repo
	}
	return DefaultRepo
}

func (o Options) goos() string {
	if o.OS != "" {
		return o.OS
	}
	return runtime.GOOS
}

func (o Options) arch() string {
	if o.Arch != "" {
		return o.Arch
	}
	return runtime.GOARCH
}

func (o Options) api() string {
	if o.APIBase != "" {
		return strings.TrimRight(o.APIBase, "/")
	}
	return apiBase
}

// FetchLatest loads the latest GitHub release for the configured repo.
func FetchLatest(o Options) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", o.api(), o.repo())
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "omls-update")
	resp, err := o.client().Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("github releases: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Release{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github releases: HTTP %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return ParseReleaseJSON(body)
}

// Plan resolves what would be installed without downloading.
func Plan(o Options) (Result, error) {
	rel, err := FetchLatest(o)
	if err != nil {
		return Result{}, err
	}
	asset, err := SelectAsset(rel, o.goos(), o.arch())
	if err != nil {
		return Result{}, err
	}
	cur := NormalizeVersion(o.CurrentVersion)
	latest := NormalizeVersion(rel.TagName)
	cmp := CompareVersions(cur, latest)
	res := Result{
		Current:     cur,
		LatestTag:   rel.TagName,
		LatestVer:   latest,
		AssetName:   asset.Name,
		DownloadURL: asset.BrowserDownloadURL,
		UpToDate:    cmp == 0 && !o.Force,
		Ahead:       cmp > 0 && !o.Force,
	}
	return res, nil
}

// Run checks for a newer pack and optionally installs it.
func Run(o Options) (Result, error) {
	res, err := Plan(o)
	if err != nil {
		return Result{}, err
	}
	fmt.Fprintf(o.stdout(), "current=%s latest=%s asset=%s\n", res.Current, res.LatestTag, res.AssetName)
	if res.Ahead {
		fmt.Fprintf(o.stdout(), "local %s is newer than published release %s\n", res.Current, res.LatestTag)
		return res, nil
	}
	if res.UpToDate {
		fmt.Fprintln(o.stdout(), "already up to date")
		return res, nil
	}
	if o.CheckOnly {
		fmt.Fprintf(o.stdout(), "update available: %s → %s\n", res.Current, res.LatestTag)
		return res, nil
	}
	fmt.Fprintf(o.stdout(), "downloading %s…\n", res.DownloadURL)
	packDir, cleanup, err := DownloadAndExtract(o, res.DownloadURL)
	if err != nil {
		return res, err
	}
	defer cleanup()
	fmt.Fprintf(o.stdout(), "installing from %s…\n", packDir)
	if err := installPack(o, packDir); err != nil {
		return res, err
	}
	res.Installed = true
	fmt.Fprintf(o.stdout(), "updated %s → %s\n", res.Current, res.LatestTag)
	return res, nil
}

func installPack(o Options, packDir string) error {
	if o.InstallFunc != nil {
		return o.InstallFunc(packDir)
	}
	install := filepath.Join(packDir, "install.sh")
	if _, err := os.Stat(install); err != nil {
		return fmt.Errorf("pack missing install.sh: %w", err)
	}
	cmd := exec.Command("bash", install)
	cmd.Stdout = o.stdout()
	cmd.Stderr = o.stderr()
	cmd.Dir = packDir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pack install: %w", err)
	}
	return nil
}

// DownloadAndExtract fetches a tarball and unpacks it to a temp directory.
// The returned cleanup removes the temp tree.
func DownloadAndExtract(o Options, url string) (packDir string, cleanup func(), err error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "omls-update")
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := o.client().Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", nil, fmt.Errorf("download: HTTP %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	base := o.InstallRoot
	if base == "" {
		base, err = os.MkdirTemp("", "omls-update-*")
		if err != nil {
			return "", nil, err
		}
	} else {
		if err := os.MkdirAll(base, 0o755); err != nil {
			return "", nil, err
		}
	}
	cleanup = func() {
		if o.InstallRoot == "" {
			_ = os.RemoveAll(base)
		}
	}

	if err := extractTarGz(resp.Body, base); err != nil {
		cleanup()
		return "", nil, err
	}
	packDir, err = findPackRoot(base)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return packDir, cleanup, nil
}

func extractTarGz(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := writeTarEntry(dest, hdr, tr); err != nil {
			return err
		}
	}
}

func writeTarEntry(dest string, hdr *tar.Header, r io.Reader) error {
	name := filepath.Clean(hdr.Name)
	if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
		return fmt.Errorf("refusing unsafe path %q", hdr.Name)
	}
	target := filepath.Join(dest, name)
	rel, err := filepath.Rel(dest, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("refusing path escape %q", hdr.Name)
	}
	switch hdr.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, 0o755)
	case tar.TypeReg, tar.TypeRegA:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(hdr.Mode) & 0o777
		if mode == 0 {
			mode = 0o644
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(f, r)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	case tar.TypeSymlink:
		// Packs do not need symlinks; skip for safety.
		return nil
	default:
		return nil
	}
}

func findPackRoot(base string) (string, error) {
	if _, err := os.Stat(filepath.Join(base, "install.sh")); err == nil {
		return base, nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cand := filepath.Join(base, e.Name())
		if _, err := os.Stat(filepath.Join(cand, "install.sh")); err == nil {
			return cand, nil
		}
	}
	return "", fmt.Errorf("extracted pack has no install.sh under %s", base)
}
