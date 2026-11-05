// Package storage transfers real files through authenticated SSH or SMB.
package storage

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Profile struct {
	Mode, Host, User, Key, Root, Share, Password, Domain string
	Port                                                 int
}

var nodeName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
var hostName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*$`)
var userName = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$`)

func Init(file, home string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "# Literal values; this file is never sourced as shell code. Keep mode 0600.\n# Fill in the remote host, login user, and an absolute directory owned by that user.\nOMLS_STUDIO_MODE=ssh\nOMLS_STUDIO_HOST=\nOMLS_STUDIO_PORT=22\nOMLS_STUDIO_USER=\nOMLS_STUDIO_KEY=%s/.ssh/omls_ed25519\nOMLS_STUDIO_ROOT=\n# For MODE=smb, set SHARE and credentials below. ROOT is relative to SHARE.\nOMLS_STUDIO_SHARE=\nOMLS_STUDIO_PASSWORD=\nOMLS_STUDIO_DOMAIN=\n", home)
	if err == nil {
		fmt.Println("Created private connection template:", file)
	}
	return err
}

func Load(file, node string) (Profile, error) {
	var p Profile
	node = strings.ToUpper(node)
	if !nodeName.MatchString(node) {
		return p, fmt.Errorf("invalid node name")
	}
	f, err := os.Open(file)
	if err != nil {
		return p, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return p, err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return p, fmt.Errorf("connection file must be a regular private file (chmod 600)")
	}
	values := map[string]string{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return p, fmt.Errorf("invalid connection configuration line")
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && ((v[0] == '\'' && v[len(v)-1] == '\'') || (v[0] == '"' && v[len(v)-1] == '"')) {
			v = v[1 : len(v)-1]
		}
		if _, ok := values[k]; ok {
			return p, fmt.Errorf("duplicate connection key %s", k)
		}
		values[k] = v
	}
	if err := scan.Err(); err != nil {
		return p, err
	}
	get := func(k string) string { return values["OMLS_"+node+"_"+k] }
	p = Profile{Mode: get("MODE"), Host: get("HOST"), User: get("USER"), Key: get("KEY"), Root: get("ROOT"), Share: get("SHARE"), Password: get("PASSWORD"), Domain: get("DOMAIN"), Port: 22}
	if s := get("PORT"); s != "" {
		p.Port, err = strconv.Atoi(s)
		if err != nil || p.Port < 1 || p.Port > 65535 {
			return p, fmt.Errorf("invalid port")
		}
	}
	if !hostName.MatchString(p.Host) || !userName.MatchString(p.User) {
		return p, fmt.Errorf("invalid host or login user")
	}
	switch p.Mode {
	case "ssh":
		if !filepath.IsAbs(p.Root) || !filepath.IsAbs(p.Key) {
			return p, fmt.Errorf("SSH ROOT and KEY must be absolute paths")
		}
	case "smb":
		if pathErr := validSMBRoot(p.Root); pathErr != nil {
			return p, pathErr
		}
		if !hostName.MatchString(p.Share) {
			return p, fmt.Errorf("SMB SHARE must be configured")
		}
		if strings.ContainsAny(p.Password+p.Domain, "\r\n") {
			return p, fmt.Errorf("SMB credentials cannot contain newlines")
		}
	default:
		return p, fmt.Errorf("MODE must be ssh or smb")
	}
	return p, nil
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (p Profile) ssh(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, "ssh", "-p", strconv.Itoa(p.Port), "-i", p.Key, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=8", p.User+"@"+p.Host, command)
}

// Exec constructs an authenticated remote command with each argument quoted.
// Callers own stdin/stdout and execute it; credentials never enter argv.
func (p Profile) Exec(ctx context.Context, args ...string) (*exec.Cmd, error) {
	if p.Mode != "ssh" {
		return nil, fmt.Errorf("remote task execution requires SSH")
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("remote command is required")
	}
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = quote(arg)
	}
	return p.ssh(ctx, strings.Join(quoted, " ")), nil
}
func (p Profile) smb(ctx context.Context, command string) (*exec.Cmd, func(), error) {
	f, err := os.CreateTemp("", "omls-smb-credentials-*")
	if err != nil {
		return nil, nil, err
	}
	clean := func() { os.Remove(f.Name()) }
	if _, err = fmt.Fprintf(f, "username = %s\npassword = %s\ndomain = %s\n", p.User, p.Password, p.Domain); err != nil {
		f.Close()
		clean()
		return nil, nil, err
	}
	if err = f.Close(); err != nil {
		clean()
		return nil, nil, err
	}
	return exec.CommandContext(ctx, "smbclient", "//"+p.Host+"/"+p.Share, "-A", f.Name(), "-c", command), clean, nil
}
func (p Profile) Check(ctx context.Context) error {
	var c *exec.Cmd
	if p.Mode == "ssh" {
		c = p.ssh(ctx, "test -d "+quote(p.Root)+" && test -r "+quote(p.Root)+" && test -w "+quote(p.Root))
	} else {
		var clean func()
		var err error
		root, quoteErr := smbQuote(p.Root)
		if quoteErr != nil {
			return quoteErr
		}
		c, clean, err = p.smb(ctx, "cd "+root+"; ls")
		if err != nil {
			return err
		}
		defer clean()
	}
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s connection/root check failed: %w; run storage login for SSH enrollment", p.Mode, err)
	}
	fmt.Println("Authenticated connection and remote root checked:", p.Host)
	return nil
}

func validSMBRoot(root string) error {
	if strings.HasPrefix(root, "/") || strings.ContainsAny(root, "\\\r\n\x00") {
		return fmt.Errorf("SMB ROOT must be relative to SHARE")
	}
	for _, part := range strings.Split(root, "/") {
		if part == ".." {
			return fmt.Errorf("SMB ROOT cannot traverse parents")
		}
	}
	_, err := smbQuote(root)
	return err
}
func (p Profile) Login(ctx context.Context) error {
	if p.Mode != "ssh" {
		return fmt.Errorf("SMB uses PASSWORD in the private profile; use storage check")
	}
	if _, err := os.Stat(p.Key); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(p.Key), 0700); err != nil {
			return err
		}
		c := exec.CommandContext(ctx, "ssh-keygen", "-t", "ed25519", "-N", "", "-f", p.Key)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	c := exec.CommandContext(ctx, "ssh-copy-id", "-i", p.Key+".pub", "-p", strconv.Itoa(p.Port), p.User+"@"+p.Host)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return err
	}
	c = p.ssh(ctx, "umask 077; mkdir -p -- "+quote(p.Root))
	c.Stderr = os.Stderr
	return c.Run()
}
