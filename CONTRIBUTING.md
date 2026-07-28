# Contributing

Thanks for helping improve Agent Usage. This project reads data produced by
several agent harnesses and presents it inside Herdr, so seemingly small changes
can affect session matching, displayed limits, local state, or compatibility
with existing installations.

## Before you start

Small bug fixes and documentation improvements are welcome as direct pull
requests.

Please open an issue before investing in any of the following:

- a new feature or supported provider
- a change to a configuration or persisted-state format
- a significant UI or behavior change

Use the issue to describe the problem, the proposed behavior, and any
compatibility implications. This gives maintainers a chance to confirm that the
approach fits the project before substantial work begins.

## Development

Agent Usage requires Go 1.25 or later. The executable entry point is
`cmd/usagebar`. Shared behavior lives under `internal`, including provider
registration in `internal/providers`, provider-specific session extraction and
resolution in `internal/providers/<agent>`, limit collection and presentation
in `internal/limits`, and plugin setup in `internal/setup`.

Build and test the project from the repository root:

```sh
make build
make test
```
Before opening a pull request, run the same core checks used by CI
(`gofmt -l .` should produce no output):

```sh
gofmt -l .
go vet ./...
go test ./...
go build ./...
```

CI runs these checks on both Linux and macOS and also runs golangci-lint and
govulncheck.

### Commit messages

Commit messages and pull request titles follow the
[Conventional Commits](https://www.conventionalcommits.org/) specification.
The repository already uses this style in its history (for example
`0dd0492` `fix: normalize config_dir matching and cover the default
account`); the rules below formalise that existing style.

```text
<type>(optional scope): <short summary>

<optional body>

<optional footer(s)>
```

Allowed `<type>` values:

| Type       | Use for                                                   |
| ---------- | --------------------------------------------------------- |
| `feat`     | A new user-visible feature                                |
| `fix`      | A bug fix                                                 |
| `docs`     | Documentation only changes                                |
| `style`    | Formatting changes that do not affect meaning             |
| `refactor` | A code change that neither fixes a bug nor adds a feature |
| `perf`     | A code change that improves performance                   |
| `test`     | Adding or fixing tests                                    |
| `build`    | Build system or dependency changes                        |
| `ci`       | CI configuration changes                                  |
| `chore`    | Tooling, release commits, or other maintenance            |
| `revert`   | A revert of a previous commit                             |

Use a lowercase scope in parentheses to identify the affected package when
it is obvious (for example `chore(deps): bump actions/checkout to v7`).
Keep the summary line under 72 characters and use the imperative mood
("add", not "added" or "adds").

PR titles must follow the same format. They are validated by the
`Lint PR title` GitHub Actions workflow
(`.github/workflows/lint-pr-title.yml`), which uses the same type list as
the local hook.

To lint commits locally before pushing, install the commit-msg hook:

```sh
make install-hooks
```

This installs
[`conventionalcommit/commitlint`](https://github.com/conventionalcommit/commitlint)
(a Go implementation, no Node.js required) and configures it against
`.commitlint.yaml`. After installation, a non-conforming message such as
`git commit -m "fix typo"` will be rejected; rewrite it as
`fix: correct typo in setup output` and try again.

### Maintainer notes (enforcing the rule)

The local hook and the `Lint PR title` workflow only *report* violations.
To actually block merges, a maintainer with admin access to the repository
must apply both of the following GitHub repository settings once this
commit lands on `main`:

1. **Required status check.** Under *Settings → Branches → Branch
   protection rules*, mark `Lint PR title / Validate PR title` as a
   required check for `main`. `synchronize` is included in the workflow
   trigger so the check re-runs on every push and stays current as
   required checks require it.
2. **Squash merge defaults to the PR title.** Under
   *Settings → General → Pull Requests*, enable *Allow squash merging* and
   tick *Default to PR title for squash merge commits*. Without this,
   GitHub suggests the underlying commit message for the squash commit,
   which may not be conventional even when the PR title is.

These settings live in GitHub, not in the repository, so they are not
versioned here. Re-apply them after creating a new repository or
re import.

## Pull requests

Keep each pull request focused on one purpose. Explain what changed, why it is
needed, how it was tested, and any user-visible or compatibility impact. Avoid
unrelated refactoring or formatting changes.

Passing tests does not guarantee that a change will be accepted. Maintainers
will also consider backward compatibility, ongoing maintenance cost, and fit
with the project's direction.

## AI-assisted contributions

AI-assisted contributions are welcome, but the submitter remains responsible
for the result. You must understand the change, review it for correctness, and
run the relevant verification yourself. Disclose meaningful AI assistance when
it would help reviewers evaluate the contribution or when a maintainer asks.
