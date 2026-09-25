# Contributing to Release Planner

Pull requests are welcome.

## Check your change

Run the checks from the repository root before you push:

```sh
gofmt -l . && go vet ./... && go test ./...
```

`gofmt -l` lists files that need formatting, so it should print nothing.

## Work on the documentation

The documentation site lives in `docs/`. [docs/README.md](docs/README.md) explains how to run, check, and build it. When a command, config key, or generated file changes, update the page that describes it in the same pull request.
