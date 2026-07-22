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

Add or update tests for the behavior you change. Prefer focused tests beside
the affected package, and include regression coverage for bug fixes when
practical.

Format changed Go files with `gofmt`. Before opening a pull request, run the
same core checks used by CI (`gofmt -l .` should produce no output):

```sh
gofmt -l .
go vet ./...
go test ./...
go build ./...
```

CI runs these checks on both Linux and macOS and also runs golangci-lint and
govulncheck.

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
