.PHONY: build test run tidy fmt vet clean install uninstall \
	install-claudecode uninstall-claudecode \
	install-codex uninstall-codex install-codex-skills uninstall-codex-skills

BIN := bin/exq
PKG := ./cmd/exq

export CGO_ENABLED ?= 0

# Override with `make install PREFIX=$HOME/.local` to avoid sudo.
PREFIX ?= /usr/local
INSTALL_DIR := $(DESTDIR)$(PREFIX)/bin

# The skills are bundled as a single plugin (plugin/) shared by both
# runtimes, and invoked by name:
#   Claude Code: ~/.claude/skills/exq  (skills-dir plugin; auto-loads as
#                                       exq@skills-dir; /exq:exq-new)
#   Codex CLI:   local marketplace (.agents/plugins/marketplace.json ->
#                                       ./plugin; $exq-new)
# A symlink under ~/.claude/plugins/ is NOT auto-loaded (it needs marketplace
# registration); ~/.claude/skills/<name>/ with a plugin.json auto-loads instead.
# Override the dir with `make install-claudecode CLAUDE_SKILLS_DIR=/path`.
CLAUDE_SKILLS_DIR ?= $(HOME)/.claude/skills
PLUGIN_NAME := exq
PLUGIN_SRC := $(CURDIR)/plugin

AGENTS_HOME ?= $(HOME)/.agents
CODEX_SKILLS_DIR := $(AGENTS_HOME)/skills
CODEX_SKILLS_SRC := $(CURDIR)/plugin/skills

build:
	@mkdir -p bin
	go build -o $(BIN) $(PKG)

test:
	go test ./...

run: build
	@$(BIN) $(ARGS)

tidy:
	go mod tidy

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -rf bin/

install: build
	install -d $(INSTALL_DIR)
	install -m 0755 $(BIN) $(INSTALL_DIR)/exq
	@echo "Installed exq to $(INSTALL_DIR)/exq"

uninstall:
	rm -f $(INSTALL_DIR)/exq
	@echo "Removed $(INSTALL_DIR)/exq"

# Symlink the whole plugin/ directory into ~/.claude/skills/exq so Claude
# Code auto-loads it as the exq@skills-dir plugin. Skills are then invoked
# as /exq:<skill>. Restart Claude Code to pick it up; verify with
# `claude plugin list`.
install-claudecode:
	@mkdir -p $(CLAUDE_SKILLS_DIR)
	@target=$(CLAUDE_SKILLS_DIR)/$(PLUGIN_NAME); \
	if [ -L $$target ]; then \
		rm -f $$target; \
	elif [ -e $$target ]; then \
		echo "skip: $$target already exists (not a symlink)"; \
		exit 0; \
	fi; \
	ln -s $(PLUGIN_SRC) $$target; \
	echo "Linked $$target -> $(PLUGIN_SRC)"; \
	echo "Restart Claude Code, then verify with: claude plugin list"

uninstall-claudecode:
	@target=$(CLAUDE_SKILLS_DIR)/$(PLUGIN_NAME); \
	if [ -L $$target ]; then \
		rm -f $$target; \
		echo "Removed $$target"; \
	fi

# Register the same plugin/ with Codex via the local marketplace
# (.agents/plugins/marketplace.json). Codex COPIES the plugin into
# ~/.codex/plugins/cache/ (no symlink), so re-run after editing skills.
# Exact `codex plugin` subcommands depend on your codex version.
install-codex:
	codex plugin marketplace add $(CURDIR)
	@echo "Added marketplace '$(PLUGIN_NAME)'. Verify/enable with 'codex plugin', then restart codex."

uninstall-codex:
	-codex plugin marketplace remove $(PLUGIN_NAME)

# Fallback for codex versions without plugin support: per-skill DIRECTORY
# symlinks into ~/.agents/skills ($exq-new). Codex's skill loader follows
# directory symlinks but drops symlinked SKILL.md files (openai/codex#15756).
# Live-edits apply immediately, unlike the marketplace copy.
install-codex-skills:
	@mkdir -p $(CODEX_SKILLS_DIR)
	@for src in $(CODEX_SKILLS_SRC)/*/; do \
		name=$$(basename $$src); \
		target=$(CODEX_SKILLS_DIR)/$$name; \
		if [ -L $$target ]; then \
			rm -f $$target; \
		elif [ -e $$target ]; then \
			echo "skip: $$target already exists (not a symlink)"; \
			continue; \
		fi; \
		ln -s $${src%/} $$target; \
		echo "Linked $$target -> $${src%/}"; \
	done
	@echo "Restart Codex to pick up the new skills."

uninstall-codex-skills:
	@for src in $(CODEX_SKILLS_SRC)/*/; do \
		name=$$(basename $$src); \
		target=$(CODEX_SKILLS_DIR)/$$name; \
		if [ -L $$target ]; then \
			rm -f $$target; \
			echo "Removed $$target"; \
		fi; \
	done
