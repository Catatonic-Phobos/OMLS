package cache

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Slice is the bytes placed into a remote reservation.
type Slice struct {
	Kind   string
	Source string
	Data   []byte
}

// ReadSlice copies up to n bytes from the local game.
// KindMemory reads resident file pages the game already has in RAM.
// KindGraphics reads the pixels of the game window.
func ReadSlice(kind string, n int) (Slice, error) {
	if n < 1 {
		return Slice{}, fmt.Errorf("fill must be at least 1 byte")
	}
	switch kind {
	case KindMemory:
		return readMemory(n)
	case KindGraphics:
		return readGraphics(n)
	default:
		return Slice{}, fmt.Errorf("kind must be %s or %s", KindMemory, KindGraphics)
	}
}

func readMemory(n int) (Slice, error) {
	pid, err := gamePID()
	if err != nil {
		return Slice{}, err
	}
	if data, src, err := readAnonymous(pid, n); err == nil && len(data) > 0 {
		return Slice{Kind: KindMemory, Source: src, Data: data}, nil
	}
	regions, err := fileRegions(pid)
	if err != nil {
		return Slice{}, err
	}
	var out []byte
	var sources []string
	for _, reg := range regions {
		if len(out) >= n {
			break
		}
		chunk, err := residentRange(reg, n-len(out))
		if err != nil || len(chunk) == 0 {
			continue
		}
		out = append(out, chunk...)
		sources = append(sources, fmt.Sprintf("%s+%d", reg.Path, reg.Offset))
	}
	if len(out) == 0 {
		return Slice{}, fmt.Errorf("no resident file pages for pid %d", pid)
	}
	return Slice{
		Kind:   KindMemory,
		Source: fmt.Sprintf("pid %d resident %s", pid, strings.Join(sources, ", ")),
		Data:   out,
	}, nil
}

func readAnonymous(pid, n int) ([]byte, string, error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/mem", pid))
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	regions, err := anonRegions(pid)
	if err != nil {
		return nil, "", err
	}
	var out []byte
	start := uint64(0)
	for _, reg := range regions {
		if len(out) >= n {
			break
		}
		if start == 0 {
			start = reg.Start
		}
		want := reg.End - reg.Start
		if int(want) > n-len(out) {
			want = uint64(n - len(out))
		}
		buf := make([]byte, want)
		got, err := f.ReadAt(buf, int64(reg.Start))
		if err != nil && got == 0 {
			return nil, "", err
		}
		out = append(out, buf[:got]...)
	}
	if len(out) == 0 {
		return nil, "", fmt.Errorf("empty anonymous read")
	}
	return out, fmt.Sprintf("pid %d anonymous @%x", pid, start), nil
}

func readGraphics(n int) (Slice, error) {
	id, err := gameWindow()
	if err != nil {
		return Slice{}, err
	}
	cmd := exec.Command("xwd", "-silent", "-id", id)
	cmd.Env = displayEnv()
	out, err := cmd.Output()
	if err != nil {
		return Slice{}, fmt.Errorf("xwd %s: %w", id, err)
	}
	if len(out) > n {
		out = out[:n]
	}
	if len(out) == 0 {
		return Slice{}, fmt.Errorf("xwd %s returned no pixels", id)
	}
	return Slice{
		Kind:   KindGraphics,
		Source: fmt.Sprintf("window %s xwd", id),
		Data:   out,
	}, nil
}

func gamePID() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(comm))
		if strings.Contains(name, "TS4_") {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("The Sims 4 process is not running")
}

type region struct {
	Start  uint64
	End    uint64
	Offset int64
	Path   string
	RSS    int
}

func fileRegions(pid int) ([]region, error) {
	all, err := parseMaps(pid)
	if err != nil {
		return nil, err
	}
	var out []region
	for _, reg := range all {
		if reg.Path == "" || strings.HasPrefix(reg.Path, "[") || strings.HasPrefix(reg.Path, "/dev/") {
			continue
		}
		if reg.RSS < 1 {
			continue
		}
		if strings.Contains(reg.Path, "The Sims 4") || strings.Contains(reg.Path, "Electronic Arts") {
			out = append(out, reg)
		}
	}
	if len(out) == 0 {
		for _, reg := range all {
			if reg.RSS > 0 && reg.Path != "" && !strings.HasPrefix(reg.Path, "[") && !strings.HasPrefix(reg.Path, "/dev/") {
				out = append(out, reg)
			}
		}
	}
	// Highest resident mapping first.
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].RSS > out[j-1].RSS {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out, nil
}

func anonRegions(pid int) ([]region, error) {
	all, err := parseMaps(pid)
	if err != nil {
		return nil, err
	}
	var out []region
	for _, reg := range all {
		if reg.Path == "" && reg.RSS > 0 {
			out = append(out, reg)
		}
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].RSS > out[j-1].RSS {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out, nil
}

func parseMaps(pid int) ([]region, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/smaps", pid))
	if err != nil {
		return nil, err
	}
	var out []region
	cur := -1
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 5 && strings.Contains(fields[0], "-") && !strings.Contains(line, " kB") {
			bounds := strings.SplitN(fields[0], "-", 2)
			if len(bounds) != 2 || len(bounds[0]) < 4 {
				continue
			}
			start, err1 := strconv.ParseUint(bounds[0], 16, 64)
			end, err2 := strconv.ParseUint(bounds[1], 16, 64)
			off, err3 := strconv.ParseInt(fields[2], 16, 64)
			if err1 != nil || err2 != nil || err3 != nil {
				continue
			}
			path := ""
			if len(fields) >= 6 {
				path = strings.TrimSuffix(strings.Join(fields[5:], " "), " (deleted)")
			}
			out = append(out, region{Start: start, End: end, Offset: off, Path: path})
			cur = len(out) - 1
			continue
		}
		if cur >= 0 && strings.HasPrefix(line, "Rss:") && len(fields) >= 2 {
			kb, _ := strconv.Atoi(fields[1])
			out[cur].RSS += kb
		}
	}
	return out, nil
}

func residentRange(reg region, n int) ([]byte, error) {
	if n < 1 || reg.End <= reg.Start {
		return nil, nil
	}
	page := os.Getpagesize()
	length := int(reg.End - reg.Start)
	if length > n {
		// Map a little past n so a partial last page can still be resident.
		length = ((n + page - 1) / page) * page
	}
	if uint64(length) > reg.End-reg.Start {
		length = int(reg.End - reg.Start)
	}
	length = length - (length % page)
	if length < page {
		return nil, nil
	}
	f, err := os.Open(reg.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := unix.Mmap(int(f.Fd()), reg.Offset, length, unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		return nil, err
	}
	defer unix.Munmap(data)
	vec := make([]byte, length/page)
	if err := mincore(data, vec); err != nil {
		return nil, err
	}
	var out []byte
	for i, flags := range vec {
		if flags&1 == 0 || len(out) >= n {
			continue
		}
		start := i * page
		end := start + page
		if end > len(data) {
			end = len(data)
		}
		if end-start > n-len(out) {
			end = start + (n - len(out))
		}
		out = append(out, data[start:end]...)
	}
	return out, nil
}

func gameWindow() (string, error) {
	cmd := exec.Command("xwininfo", "-root", "-tree")
	cmd.Env = displayEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("xwininfo: %w", err)
	}
	var id string
	var area int
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "steam_proton") && !strings.Contains(line, "The Sims") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "0x") {
			continue
		}
		w, h := 0, 0
		for _, f := range fields {
			if strings.Contains(f, "x") && strings.Contains(f, "+") {
				geom := strings.SplitN(f, "+", 2)[0]
				wh := strings.SplitN(geom, "x", 2)
				if len(wh) == 2 {
					w, _ = strconv.Atoi(wh[0])
					h, _ = strconv.Atoi(wh[1])
				}
			}
		}
		if w*h >= area {
			area = w * h
			id = fields[0]
		}
	}
	if id == "" {
		return "", fmt.Errorf("The Sims 4 window is not open")
	}
	return id, nil
}

func displayEnv() []string {
	env := os.Environ()
	if os.Getenv("DISPLAY") == "" {
		env = append(env, "DISPLAY=:0")
	}
	if os.Getenv("XAUTHORITY") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			auth := home + "/.Xauthority"
			if _, err := os.Stat(auth); err == nil {
				env = append(env, "XAUTHORITY="+auth)
			}
		}
	}
	return env
}

// CopyResident returns up to n bytes that are already in the page cache of path.
func CopyResident(path string, n int) ([]byte, error) {
	return residentRange(region{Start: 0, End: uint64(n), Offset: 0, Path: path, RSS: 1}, n)
}

func mincore(data, vec []byte) error {
	if len(data) == 0 || len(vec) == 0 {
		return fmt.Errorf("empty mincore")
	}
	_, _, errno := unix.Syscall(
		unix.SYS_MINCORE,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		uintptr(unsafe.Pointer(&vec[0])),
	)
	if errno != 0 {
		return errno
	}
	return nil
}
