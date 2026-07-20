package command

import (
	"os"
	"path/filepath"
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
