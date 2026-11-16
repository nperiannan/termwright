# Contributing to Termwright

Thanks for helping. Bug reports, ideas, documentation fixes and pull requests are all welcome.

## Reporting a bug or asking for a feature

Use [GitHub Issues](https://github.com/nperiannan/termwright/issues/new/choose); the forms ask for what's needed. For a security problem, follow the [security policy](SECURITY.md) instead.

## Setting up

The [README](README.md#building-from-source) lists the requirements (Go, Node.js, the Wails CLI) and the platform packages. Then:

```bash
make dev     # the app with live reload
make test    # the checks below
```

## Tests

`make test` runs:

- `go vet` on the Go packages;
- the `internal/` tests with Go's race detector;
- the `app/` tests (the ones that need a KVM host are skipped unless `TW_EMU_SSH` and `TW_EMU_SSH_KEY` point to one);
- the Python client's tests in `scripts/python`.

The same checks run on every pull request. If you change the local API, regenerate its description with `go test -run TestOpenAPIDoc ./app/ -update` and commit `docs/api/openapi.yaml`.

## Pull requests

- Open them against the `develop` branch. Releases are tagged on `main`.
- Keep one topic per pull request, with tests for new behaviour.
- For a change people will notice, update:
  - the [User Guide](docs/USER_GUIDE.md) (and the [Emulated Lab Guide](docs/EMULATOR_GUIDE.md) for the lab);
  - the [feature list](docs/FEATURES.md);
  - the *Unreleased* section of the [CHANGELOG](CHANGELOG.md);
  - the Russian translation in `frontend/src/app/i18n/ru.ts` for new interface text (a translation you aren't sure of is fine; it can be improved later);
  - the [API](docs/API.md) and [Python](docs/PYTHON.md) docs if the API changes.
- Screenshots in the docs use the dark theme.
- Write commit messages that say what changed for the user, for example `Lab: cables between hosts survive a host restart`.

Changes made with the help of AI tools are welcome. You're responsible for them as for any other change: understand them, test them, and make sure they can be published under the project's license.

## License

By contributing, you agree that your contribution is licensed under the [Apache License 2.0](LICENSE), like the rest of the project.

Everyone taking part is expected to follow the [code of conduct](CODE_OF_CONDUCT.md).
