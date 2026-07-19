// Package store manages the .exq directory in the current working
// directory: initialization (including the .git/info/exclude entry),
// command discovery, and command removal.
package store

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ystsbry/exq/internal/command"
)

const (
	// DirName is the exq data directory created in the working directory.
	DirName = ".exq"
	// commandsSubdir holds one directory per command under DirName.
	commandsSubdir = "commands"
	// excludePattern is the line appended to .git/info/exclude. The
	// trailing slash restricts the match to directories.
	excludePattern = ".exq/"
)

// Store operates on the .exq directory under Root.
type Store struct {
	// Root is the directory exq operates in (normally the cwd).
	Root string
}

// Open returns a Store rooted at dir. The .exq directory does not need to
// exist yet; Init creates it.
func Open(dir string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return &Store{Root: abs}, nil
}

// Dir returns the absolute path of the .exq directory.
func (s *Store) Dir() string {
	return filepath.Join(s.Root, DirName)
}

// CommandsDir returns the absolute path of the commands directory.
func (s *Store) CommandsDir() string {
	return filepath.Join(s.Dir(), commandsSubdir)
}

// Exists reports whether the .exq directory has been initialized.
func (s *Store) Exists() bool {
	info, err := os.Stat(s.Dir())
	return err == nil && info.IsDir()
}

// InitResult reports what Init actually changed, for user-facing output.
type InitResult struct {
	CreatedDir     bool
	UpdatedExclude bool
	ExcludePath    string
}

// Init creates .exq/commands/ and ensures .git/info/exclude contains the
// exclude pattern. It is idempotent: re-running never duplicates the
// exclude line or fails on existing directories.
func (s *Store) Init() (*InitResult, error) {
	res := &InitResult{}

	if !s.Exists() {
		res.CreatedDir = true
	}
	if err := os.MkdirAll(s.CommandsDir(), 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", s.CommandsDir(), err)
	}

	excludePath, err := gitExcludePath(s.Root)
	if err != nil {
		return nil, err
	}
	res.ExcludePath = excludePath
	updated, err := ensureLine(excludePath, excludePattern)
	if err != nil {
		return nil, err
	}
	res.UpdatedExclude = updated
	return res, nil
}

// List returns the commands under .exq/commands/, sorted by name.
// A missing commands directory yields an empty list, not an error.
func (s *Store) List() ([]command.Command, error) {
	entries, err := os.ReadDir(s.CommandsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cmds []command.Command
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cmds = append(cmds, command.Load(filepath.Join(s.CommandsDir(), e.Name())))
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	return cmds, nil
}

// Get returns the command with the given name.
func (s *Store) Get(name string) (command.Command, error) {
	if err := command.ValidateName(name); err != nil {
		return command.Command{}, err
	}
	dir := filepath.Join(s.CommandsDir(), name)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return command.Command{}, fmt.Errorf("command %q not found under %s", name, s.CommandsDir())
	}
	return command.Load(dir), nil
}

// Remove deletes the command directory for name.
func (s *Store) Remove(name string) error {
	c, err := s.Get(name)
	if err != nil {
		return err
	}
	return os.RemoveAll(c.Dir)
}

// gitExcludePath resolves <git-common-dir>/info/exclude for the repository
// containing root. Using the common dir keeps worktrees sharing one
// exclude file, matching git's own behavior.
func gitExcludePath(root string) (string, error) {
	cmd := exec.Command("git", "-C", root, "rev-parse", "--git-common-dir")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("not inside a git repository? git rev-parse: %w: %s",
			err, strings.TrimSpace(stderr.String()))
	}
	gitDir := strings.TrimSpace(stdout.String())
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	return filepath.Join(gitDir, "info", "exclude"), nil
}

// ensureLine appends line to the file at path unless an identical line is
// already present. Returns true when the file was modified.
func ensureLine(path, line string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) == line {
			return false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, err
	}
	prefix := ""
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		prefix = "\n"
	}
	if _, err := fmt.Fprintf(f, "%s%s\n", prefix, line); err != nil {
		_ = f.Close()
		return false, err
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	return true, nil
}
