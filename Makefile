.PHONY: test build tidy install-plugin install-hooks

test:
	go test ./...

# --build always compiles from source and fails hard, so a broken tree is
# reported rather than papered over with a prebuilt release download. The
# herdr-plugin.toml [[build]] hook calls the same script with --in-tree.
build:
	bash bin/ensure-binary.sh --build
	chmod +x bin/*.sh

tidy:
	go mod tidy

# Link this tree into Herdr as the usagebar plugin (after make build).
install-plugin: build
	herdr plugin link .

# Install the local commit-msg hook so `git commit` is linted by
# conventionalcommit/commitlint before the commit is created. Requires Go
# 1.25+ on PATH. Pinned to v0.12.0 (the version audited against
# .commitlint.yaml); bump deliberately and re-run the audit.
#
# Side effect: `commitlint init` sets `core.hooksPath` to
# `.commitlint/hooks`, so any hooks already in `.git/hooks/` (pre-commit,
# pre-push, ...) stop running until you copy them under
# `.commitlint/hooks/` or unset `core.hooksPath`.
install-hooks:
	@CL=commitlint; \
	if ! command -v commitlint >/dev/null 2>&1 || ! commitlint --version 2>/dev/null | grep -q '0\.12\.0'; then \
	  echo "installing commitlint v0.12.0..."; \
	  go install github.com/conventionalcommit/commitlint@v0.12.0; \
	  CL="$$(go env GOPATH)/bin/commitlint"; \
	fi; \
	"$$CL" init; \
	echo "commit-msg hook installed. Try: git commit -m 'foo: bad'"
