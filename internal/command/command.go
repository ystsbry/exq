// Package command defines the on-disk format of an exq command and loads
// its metadata.
//
// A command is a directory under .exq/commands/:
//
//	.exq/commands/<name>/
//	  command.toml  # metadata (description = "...")
//	  run.sh        # executable entrypoint (any language via shebang)
package command

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const (
	// MetaFile is the metadata file name inside a command directory.
	MetaFile = "command.toml"
	// RunFile is the entrypoint file name inside a command directory.
	RunFile = "run.sh"
)

// Arg is one declared argument of a command. The [[args]] order in
// command.toml is the positional order the values are passed in ($1, $2, …),
// so Args must preserve file order.
type Arg struct {
	Key         string `toml:"key"`
	Description string `toml:"description"`
}

// Command is a single exq command discovered on disk.
type Command struct {
	Name        string
	Description string
	Args        []Arg
	Dir         string // absolute path to .exq/commands/<name>
}

// RunPath returns the absolute path of the command's entrypoint.
func (c Command) RunPath() string {
	return filepath.Join(c.Dir, RunFile)
}

// meta mirrors command.toml.
type meta struct {
	Description string `toml:"description"`
	Args        []Arg  `toml:"args"`
}

// Load reads the command stored at dir. A missing or malformed command.toml
// is tolerated (the command is still usable, just without a description) so
// that one broken file never hides a runnable command from the listing.
func Load(dir string) Command {
	c := Command{
		Name: filepath.Base(dir),
		Dir:  dir,
	}
	var m meta
	if _, err := toml.DecodeFile(filepath.Join(dir, MetaFile), &m); err == nil {
		c.Description = m.Description
		c.Args = m.Args
	}
	return c
}

// ValidateName rejects names that would escape the commands directory or
// collide with filesystem special entries.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("command name is empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid command name %q", name)
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("invalid command name %q: must not contain path separators", name)
	}
	return nil
}

// Runnable reports whether the entrypoint exists and is executable.
func (c Command) Runnable() error {
	info, err := os.Stat(c.RunPath())
	if err != nil {
		return fmt.Errorf("%s: %w", c.Name, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s: %s is a directory", c.Name, RunFile)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s: %s is not executable (try: chmod +x %s)", c.Name, RunFile, c.RunPath())
	}
	return nil
}
