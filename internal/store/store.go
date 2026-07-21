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
	// scriptsSubdir holds one directory per script under DirName.
	scriptsSubdir = "scripts"
	// workflowsSubdir holds one directory per workflow under DirName.
	workflowsSubdir = "workflows"
	// legacySubdir is the pre-scripts/workflows layout; Init migrates it.
	legacySubdir = "commands"
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

// ScriptsDir returns the absolute path of the scripts directory.
func (s *Store) ScriptsDir() string {
	return filepath.Join(s.Dir(), scriptsSubdir)
}

// WorkflowsDir returns the absolute path of the workflows directory.
func (s *Store) WorkflowsDir() string {
	return filepath.Join(s.Dir(), workflowsSubdir)
}

// legacyDir is the pre-migration commands directory.
func (s *Store) legacyDir() string {
	return filepath.Join(s.Dir(), legacySubdir)
}

// kindDir pairs a discovery directory with the Kind of its entries.
type kindDir struct {
	dir  string
	kind command.Kind
}

// kindDirs returns the discovery locations in deterministic order.
func (s *Store) kindDirs() []kindDir {
	return []kindDir{
		{s.ScriptsDir(), command.KindScript},
		{s.WorkflowsDir(), command.KindWorkflow},
	}
}

// checkLegacy fails when the old .exq/commands layout is still present, so
// list/run surface a migration hint instead of silently showing nothing.
func (s *Store) checkLegacy() error {
	if info, err := os.Stat(s.legacyDir()); err == nil && info.IsDir() {
		return fmt.Errorf("legacy layout %s detected — run `exq init` to migrate to %s",
			s.legacyDir(), s.ScriptsDir())
	}
	return nil
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
	Migrated       []string // entry names moved from the legacy commands/ dir
}

// Init creates .exq/scripts/ and .exq/workflows/, migrates a legacy
// .exq/commands/ layout into scripts/, and ensures .git/info/exclude
// contains the exclude pattern. It is idempotent: re-running never
// duplicates the exclude line or fails on existing directories.
func (s *Store) Init() (*InitResult, error) {
	res := &InitResult{}

	if !s.Exists() {
		res.CreatedDir = true
	}
	for _, dir := range []string{s.ScriptsDir(), s.WorkflowsDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", dir, err)
		}
	}

	migrated, err := s.migrateLegacy()
	if err != nil {
		return nil, err
	}
	res.Migrated = migrated

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

// migrateLegacy moves every entry of a pre-scripts/workflows commands/
// directory into scripts/ and removes the emptied commands/ directory.
// Returns the migrated entry names; a missing legacy dir is a no-op.
func (s *Store) migrateLegacy() ([]string, error) {
	entries, err := os.ReadDir(s.legacyDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var migrated []string
	for _, e := range entries {
		src := filepath.Join(s.legacyDir(), e.Name())
		dst := filepath.Join(s.ScriptsDir(), e.Name())
		if _, err := os.Stat(dst); err == nil {
			return nil, fmt.Errorf("cannot migrate %s: %s already exists", src, dst)
		}
		if err := os.Rename(src, dst); err != nil {
			return nil, fmt.Errorf("migrate %s: %w", src, err)
		}
		migrated = append(migrated, e.Name())
	}
	if err := os.Remove(s.legacyDir()); err != nil {
		return nil, fmt.Errorf("remove legacy dir %s: %w", s.legacyDir(), err)
	}
	return migrated, nil
}

// List returns the scripts and workflows under .exq/, sorted by name.
// Missing subdirectories yield an empty list, not an error. Names must be
// unique across both kinds; a duplicate is an error.
func (s *Store) List() ([]command.Command, error) {
	if err := s.checkLegacy(); err != nil {
		return nil, err
	}
	var cmds []command.Command
	seen := map[string]string{}
	for _, loc := range s.kindDirs() {
		dir, kind := loc.dir, loc.kind
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if prev, dup := seen[e.Name()]; dup {
				return nil, fmt.Errorf("name %q exists in both %s and %s — names must be unique across scripts and workflows",
					e.Name(), prev, dir)
			}
			seen[e.Name()] = dir
			c := command.Load(filepath.Join(dir, e.Name()))
			c.Kind = kind
			cmds = append(cmds, c)
		}
	}
	// Kind-major order so the UIs can render a scripts section followed
	// by a workflows section without re-sorting.
	sort.Slice(cmds, func(i, j int) bool {
		if cmds[i].Kind != cmds[j].Kind {
			return cmds[i].Kind < cmds[j].Kind
		}
		return cmds[i].Name < cmds[j].Name
	})
	return cmds, nil
}

// Get returns the script or workflow with the given name. A name present
// in both subdirectories is an error.
func (s *Store) Get(name string) (command.Command, error) {
	if err := command.ValidateName(name); err != nil {
		return command.Command{}, err
	}
	if err := s.checkLegacy(); err != nil {
		return command.Command{}, err
	}
	var found []command.Command
	for _, loc := range s.kindDirs() {
		path := filepath.Join(loc.dir, name)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			c := command.Load(path)
			c.Kind = loc.kind
			found = append(found, c)
		}
	}
	switch len(found) {
	case 0:
		return command.Command{}, fmt.Errorf("command %q not found under %s or %s",
			name, s.ScriptsDir(), s.WorkflowsDir())
	case 1:
		return found[0], nil
	default:
		return command.Command{}, fmt.Errorf("name %q exists in both %s and %s — names must be unique across scripts and workflows",
			name, s.ScriptsDir(), s.WorkflowsDir())
	}
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
