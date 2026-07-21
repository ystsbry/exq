package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/ystsbry/exq/internal/command"
	"github.com/ystsbry/exq/internal/runner"
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

			res, err := tui.Run(st)
			if err != nil {
				return err
			}
			if res == nil {
				return nil
			}
			if res.Command.Kind == command.KindWorkflow {
				return executeWorkflow(st, res.Command, res.Values)
			}
			code, err := runner.Run(res.Command, st.Root, res.Values)
			if err != nil {
				return err
			}
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&empty, "empty", false, "start with no commands (empty state)")
	cmd.Flags().BoolVar(&snapshot, "snapshot", false, "render all UI states to stdout and exit")
	return cmd
}

// newDemoStore builds a store in a fresh temp directory, populated with
// sampleCommands unless empty. No git repository is needed: the demo skips
// Init and creates the directories directly.
func newDemoStore(empty bool) (*store.Store, func(), error) {
	tmp, err := os.MkdirTemp("", "exq-demo-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	st, err := store.Open(tmp)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := os.MkdirAll(st.ScriptsDir(), 0o755); err != nil {
		cleanup()
		return nil, nil, err
	}
	if !empty {
		for _, s := range sampleCommands {
			dir := filepath.Join(st.ScriptsDir(), s.name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				cleanup()
				return nil, nil, err
			}
			if err := os.WriteFile(filepath.Join(dir, command.MetaFile), []byte(s.meta), 0o644); err != nil {
				cleanup()
				return nil, nil, err
			}
			if err := os.WriteFile(filepath.Join(dir, command.RunFile), []byte(s.script), 0o755); err != nil {
				cleanup()
				return nil, nil, err
			}
		}
		for _, w := range sampleWorkflows {
			dir := filepath.Join(st.WorkflowsDir(), w.name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				cleanup()
				return nil, nil, err
			}
			if err := os.WriteFile(filepath.Join(dir, command.MetaFile), []byte(w.meta), 0o644); err != nil {
				cleanup()
				return nil, nil, err
			}
		}
	}
	return st, cleanup, nil
}
