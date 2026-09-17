# Contributing

Contributions are welcome through GitHub issues and pull requests.

## Development

This module requires Go 1.23 or later and has no third-party runtime dependencies.

Before opening a pull request, run:

```sh
go test ./...
go vet ./...
go test -race ./...
```

CI also scans reachable code for known vulnerabilities with `govulncheck` on the current stable Go release.

## Commits

Use [Conventional Commits](https://www.conventionalcommits.org/). The release workflow uses commit types to determine semantic versions:

- `fix:` produces a patch release.
- `feat:` produces a minor release.
- `!` or a `BREAKING CHANGE:` footer produces a major release.

Optional local validation is available through [Lefthook](https://lefthook.dev/):

```sh
brew install lefthook
lefthook install
```

Keep pull request titles in the same Conventional Commit format. The repository uses squash merges, so the pull request title becomes the commit subject on `main`.
