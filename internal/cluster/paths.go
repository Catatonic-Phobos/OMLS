package cluster

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultDataDir is where a user-launched daemon stores identity and profiles.
func DefaultDataDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "share", "omls")
	}
	return "omls-data"
}

// DefaultSocketPath is the unix socket a user-launched daemon creates.
func DefaultSocketPath() string {
	if p := os.Getenv("OMLS_SOCKET"); p != "" {
		return p
	}
	if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
		return filepath.Join(r, "omls", "omlsd.sock")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "share", "omls", "omlsd.sock")
	}
	return filepath.Join(os.TempDir(), "omlsd.sock")
}

// ResolveSocket finds a running daemon socket.
// explicit, then OMLS_SOCKET, then the usual user and system paths.
func ResolveSocket(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", errStopped(explicit)
		}
		return explicit, nil
	}
	if p := os.Getenv("OMLS_SOCKET"); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", errStopped(p)
		}
		return p, nil
	}
	var candidates []string
	if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
		candidates = append(candidates, filepath.Join(r, "omls", "omlsd.sock"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".local", "share", "omls", "omlsd.sock"))
	}
	candidates = append(candidates, "/run/omls/omlsd.sock")
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	hint := DefaultSocketPath()
	if len(candidates) > 0 {
		hint = candidates[0]
	}
	return "", errStopped(hint)
}

func errStopped(path string) error {
	return fmt.Errorf("%w (looked for %s)", ErrStopped, path)
}

// ErrStopped means the local daemon socket is not there.
var ErrStopped = fmt.Errorf("omlsd is not running")
