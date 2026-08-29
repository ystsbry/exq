package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/herdr"
	"github.com/ystsbry/exq/internal/store"
	"github.com/ystsbry/exq/internal/tui"
	"github.com/ystsbry/exq/internal/workflow"
)

// sampleCommands is the fixture set the demo store is populated with.
// One entry has a long description to check wrapping/truncation visually,
// and one declares [[args]] so the argument form can be exercised.
var sampleCommands = []struct {
	name, meta, script string
}{
	{
		name: "deploy-local",
		meta: `description = "ローカル環境にビルドしてデプロイする"

[[args]]
key = "env"
description = "デプロイ先環境 (dev / prod)"

[[args]]
key = "service"
description = "対象サービス名（空なら全サービス）"
`,
		script: "#!/bin/sh\necho \"[demo] deploy-local: env=${1:-} service=${2:-} (pretending to deploy...)\"\n",
	},
	{
		name: "reset-db",
		meta: `description = "テスト DB を初期化してシードデータを投入する（この説明は折り返し・省略の見え方を確認するために意図的に長くしてある）"
`,
		script: "#!/bin/sh\necho \"[demo] reset-db: pretending to reset the database...\"\n",
	},
	{
		name: "tail-logs",
		meta: `description = "開発サーバのログを追いかける"
`,
		script: "#!/bin/sh\necho \"[demo] tail-logs: pretending to tail logs...\"\n",
	},
	{
		name: "broken-step",
		meta: `description = "常に失敗するステップ（ワークフローの失敗例用）"
`,
		script: "#!/bin/sh\necho \"[demo] broken-step: failing on purpose\" >&2\nexit 1\n",
	},
}

// sampleWorkflows exercises the workflow path: one that succeeds and one
// that fails mid-way so the summary shows ✓ / ✗ / skipped at once.
var sampleWorkflows = []struct {
	name, meta string
}{
	{
		name: "morning-setup",
		meta: `description = "朝の開発開始ルーチン（成功する例）"
steps = ["reset-db", "tail-logs"]
`,
	},
	{
		name: "failing-flow",
		meta: `description = "途中で失敗するワークフローの例"
steps = ["reset-db", "broken-step", "tail-logs"]
`,
	},
	{
		name: "deploy-flow",
		meta: `description = "DB リセット後にデプロイする（引数付きワークフローの例）"
steps = ["reset-db", "deploy-local ${env}"]

[[args]]
key = "env"
description = "デプロイ先環境 (dev / prod)"
`,
	},
}

func newDemoCmd() *cobra.Command {
	var (
		empty    bool
		snapshot bool
	)
	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Launch the TUI with sample data to check the UI (storybook-like)",
		Long: `Populate a throwaway temporary directory with sample commands and open
the normal TUI against it, so the UI can be exercised without touching any
real .exq directory (deleting and running affect only the temp fixtures).

With --snapshot, every UI state (browse / empty / confirm-delete / error)
is rendered to stdout instead — no TTY or key input needed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, cleanup, err := newDemoStore(empty)
			if err != nil {
				return err
			}
			defer cleanup()

			if snapshot {
				items, err := st.List()
				if err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				for _, s := range tui.Snapshots(st, items) {
					fmt.Fprintf(out, "=== %s ===\n%s\n", s.Name, s.View)
				}
				// The post-run summary is terminal output, not a TUI
				// state, so it is rendered here from a fixed fixture.
				sum := workflow.Summary(&workflow.Result{Steps: []workflow.StepResult{
					{Name: "reset-db", Status: workflow.StatusSuccess, Duration: 300 * time.Millisecond},
					{Name: "broken-step", Status: workflow.StatusFailed, ExitCode: 1, Duration: 2100 * time.Millisecond},
					{Name: "tail-logs", Status: workflow.StatusSkipped},
				}})
				fmt.Fprintf(out, "=== workflow-summary ===\n%s\n", sum)
				return nil
			}

			rep := herdr.New()
			rep.Report(herdr.StateIdle, "", "")
			res, err := tui.Run(st)
			if err != nil {
				rep.Release()
				return err
			}
			if res == nil {
				rep.Release()
				return nil
			}
			return execute(cmd.OutOrStdout(), cmd.ErrOrStderr(), st, res.Command, res.Values, rep)
		},
	}
	cmd.Flags().BoolVar(&empty, "empty", false, "start with no commands (empty state)")
	cmd.Flags().BoolVar(&snapshot, "snapshot", false, "render all UI states to stdout and exit")
	return cmd
}

// newDemoStore builds a store in a fresh temp directory, populated with
// sampleCommands unless empty. No git repository is needed: the demo skips
// Init and creates the directories directly.
func newDemoStore(empty bool) (st *store.Store, cleanup func(), err error) {
	tmp, err := os.MkdirTemp("", "exq-demo-*")
	if err != nil {
		return nil, nil, err
	}
	// Anything that fails from here on leaves a half-built fixture, which
	// is of no use to anyone — take the whole temp directory with it.
	defer func() {
		if err != nil {
			_ = os.RemoveAll(tmp)
		}
	}()

	st, err = store.Open(tmp)
	if err != nil {
		return nil, nil, err
	}
	if err = os.MkdirAll(st.ScriptsDir(), 0o755); err != nil {
		return nil, nil, err
	}
	if !empty {
		for _, s := range sampleCommands {
			if err = writeFixture(filepath.Join(st.ScriptsDir(), s.name), s.meta, s.script); err != nil {
				return nil, nil, err
			}
		}
		for _, w := range sampleWorkflows {
			if err = writeFixture(filepath.Join(st.WorkflowsDir(), w.name), w.meta, ""); err != nil {
				return nil, nil, err
			}
		}
	}
	return st, func() { _ = os.RemoveAll(tmp) }, nil
}

// writeFixture creates one demo command directory. An empty script marks a
// workflow, which composes scripts instead of having an entrypoint of its
// own.
func writeFixture(dir, meta, script string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, command.MetaFile), []byte(meta), 0o644); err != nil {
		return err
	}
	if script == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(dir, command.RunFile), []byte(script), 0o755)
}
