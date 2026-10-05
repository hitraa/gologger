# Contributing to gologger

Bug reports, documentation improvements, and pull requests are welcome. For a
bug report, include a small reproduction, expected behavior, actual behavior,
and the Go version and operating system involved.

## Checks

Run these checks before opening a pull request:

```sh
go test ./...
go test -race ./...
go vet ./...
```

Keep changes focused and add tests for behavior changes. The CI workflow tests
Linux, macOS, and Windows.

## Versioning and releases

gologger follows Semantic Versioning. The version in `version.go`, the entry in
`CHANGELOG.md`, and the release tag must agree. Release tags use the form
`vMAJOR.MINOR.PATCH`.

1. Update `Version` and add release notes to `CHANGELOG.md`.
2. Merge the release changes to the default branch.
3. Create and push the matching tag, for example `git tag v1.0.0 && git push origin v1.0.0`.

The tag workflow verifies the version and changelog, runs tests and vet, then
creates a GitHub Release with generated notes. Go consumers can install a
specific release with `go get github.com/hitraa/gologger@v1.0.0`.

Contributions intentionally submitted for inclusion are licensed under
Apache-2.0, as described in section 5 of `LICENSE`, unless a separate written
agreement says otherwise.
