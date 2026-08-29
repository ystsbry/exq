package command

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMeta(t *testing.T, dir, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, MetaFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadArgsPreservesOrder(t *testing.T) {
	dir := writeMeta(t, filepath.Join(t.TempDir(), "deploy"), `description = "deploy"

[[args]]
key = "env"
description = "target environment"

[[args]]
key = "service"
description = "service name"
`)
	c := Load(dir)
	if c.Description != "deploy" {
		t.Errorf("description = %q", c.Description)
	}
	if len(c.Args) != 2 || c.Args[0].Key != "env" || c.Args[1].Key != "service" {
		t.Fatalf("args order not preserved: %+v", c.Args)
	}
	if c.Args[0].Description != "target environment" {
		t.Errorf("arg description = %q", c.Args[0].Description)
	}
}

func TestLoadWithoutArgs(t *testing.T) {
	dir := writeMeta(t, filepath.Join(t.TempDir(), "plain"), `description = "plain"
`)
	c := Load(dir)
	if len(c.Args) != 0 {
		t.Errorf("expected no args, got %+v", c.Args)
	}
}

func TestLoadBrokenMetaTolerated(t *testing.T) {
	dir := writeMeta(t, filepath.Join(t.TempDir(), "broken"), "not toml [")
	c := Load(dir)
	if c.Name != "broken" || c.Description != "" || len(c.Args) != 0 {
		t.Errorf("broken meta should yield zero-value metadata: %+v", c)
	}
}

func TestLoadSteps(t *testing.T) {
	dir := writeMeta(t, filepath.Join(t.TempDir(), "pre-pr"), `description = "before opening a PR"
steps = ["fmt", "test"]
`)
	c := Load(dir)
	if len(c.Steps) != 2 || c.Steps[0] != "fmt" || c.Steps[1] != "test" {
		t.Errorf("steps not loaded in order: %+v", c.Steps)
	}
}

func TestLoadMissingMetaTolerated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bare")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	c := Load(dir)
	if c.Name != "bare" || c.Description != "" {
		t.Errorf("missing meta should still yield a named command: %+v", c)
	}
}

func TestRunPath(t *testing.T) {
	c := Load(filepath.Join(t.TempDir(), "deploy"))
	if got, want := c.RunPath(), filepath.Join(c.Dir, RunFile); got != want {
		t.Errorf("RunPath() = %q, want %q", got, want)
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{name: "deploy"},
		{name: "deploy-local"},
		{name: ".hidden"},
		{name: "", wantErr: true},
		{name: ".", wantErr: true},
		{name: "..", wantErr: true},
		{name: "a/b", wantErr: true},
		{name: "../evil", wantErr: true},
		{name: "trailing/", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.name)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateName(%q) = nil, want error", tt.name)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateName(%q) = %v, want nil", tt.name, err)
			}
		})
	}
}

// writeRun creates a command directory whose run.sh has the given mode.
func writeRun(t *testing.T, name string, mode os.FileMode) Command {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, RunFile), []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	return Load(dir)
}

func TestRunnableAcceptsExecutable(t *testing.T) {
	c := writeRun(t, "ok", 0o755)
	if err := c.Runnable(); err != nil {
		t.Errorf("Runnable() = %v, want nil", err)
	}
}

func TestRunnableRejectsMissingRunFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	err := Load(dir).Runnable()
	if err == nil {
		t.Fatal("Runnable() = nil, want error for a missing run.sh")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Runnable() = %v, want an fs.ErrNotExist", err)
	}
}

func TestRunnableRejectsNonExecutable(t *testing.T) {
	c := writeRun(t, "plain", 0o644)
	err := c.Runnable()
	if err == nil {
		t.Fatal("Runnable() = nil, want error for a non-executable run.sh")
	}
	// The message has to tell the user how to fix it.
	if !strings.Contains(err.Error(), "chmod +x") {
		t.Errorf("Runnable() = %v, want a chmod hint", err)
	}
}

func TestRunnableRejectsDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dirrun")
	if err := os.MkdirAll(filepath.Join(dir, RunFile), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Load(dir).Runnable()
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("Runnable() = %v, want a directory error", err)
	}
}
