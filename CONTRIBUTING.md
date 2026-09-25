# Contributing to gitcal

Thanks for considering a contribution. gitcal is a small, read-only local tool,
so changes should preserve predictable output and avoid modifying repositories.

## Set up the project

Install the Go version declared in `go.mod` and make sure Git is available on
`PATH`, then clone the repository:

```sh
git clone https://github.com/aghogwarts/gitcal.git
cd gitcal
go test ./...
```

## Before opening a pull request

Run the same checks as CI:

```sh
gofmt -w .
go vet ./...
go test -count=1 ./...
go build ./...
```

Please include tests for behaviour changes. Tests must use temporary Git
repositories and temporary configuration files rather than a contributor's real
repositories or gitcal settings.

Keep pull requests focused and explain the user-visible behaviour, relevant edge
cases, and platforms tested. For larger features, open an issue first so the
design can be discussed before implementation.

## Reporting bugs

Open a GitHub issue with your operating system, terminal, Go and Git versions,
the command you ran, expected behaviour, and actual output. Remove private paths,
email addresses, commit messages, or repository information before posting logs.
