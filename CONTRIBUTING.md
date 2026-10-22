# Contributing to chronicle

Thank you for your interest in chronicle. Bug reports, docs fixes, features and reviews are all welcome.

- For a bug or a feature, open an issue first, so that we can agree on the approach before you write code.
- For a security problem, do not open an issue. Follow [SECURITY.md](SECURITY.md).
- All participation follows the [code of conduct](CODE_OF_CONDUCT.md).

## Set up

You need Go 1.27 or later, Docker, Node.js with pnpm (for the UI), [uv](https://docs.astral.sh/uv/) (for the Python
SDK) and Cargo (for the Rust SDK). The `Makefile` downloads the other tools into `bin/`, pinned and checked with
SHA-256.

```sh
git clone https://github.com/Thinnesen/chronicle && cd chronicle
make test lint
```

| Task | Command |
|---|---|
| Unit and envtest tests | `make test` |
| Tests that need PostgreSQL | `make postgres-up`, then `export CHRONICLE_TEST_POSTGRES=...` (the target prints it), then `make test` |
| Lint | `make lint` |
| End-to-end tests on Kind | `make test-e2e` (creates and deletes the Kind cluster `chronicle-e2e`) |
| Helm chart test on Kind | `make test-chart` |
| UI | `pnpm install`, `pnpm dev`, `pnpm lint`, `pnpm typecheck`, `pnpm test` in `ui/`; Playwright with `pnpm exec playwright test` |
| Python SDK | `uv run pytest` in `sdk/python/` |
| Rust SDK | `cargo test` and `cargo clippy` in `sdk/rust/` |

## Project layout

| Path | Content |
|---|---|
| `api/v1alpha1/` | The custom resources (Workflow, WorkflowTemplate, CronWorkflow) |
| `cmd/` | The binaries: the controller (`cmd/main.go`), the API server, the CLI and the step executor |
| `internal/engine/` | The workflow engine: a pure package without I/O, tested with table tests and fuzzing |
| `internal/controller/` | The reconcilers that apply the decisions of the engine to Kubernetes |
| `internal/server/` | The Connect-RPC API, the login and the web UI server |
| `internal/` (other) | Validation, the Argo converter, the archive, artifacts, telemetry and more, one package per concern |
| `proto/` | The API definition. `gen/` and `ui/src/gen/` are generated from it |
| `ui/` | The web UI: TypeScript, React and Vite |
| `sdk/` | The SDKs for Go, Python and Rust |
| `dist/chart/` | The Helm chart |
| `docs/` | The user docs, the design and the decisions |

chronicle is a [Kubebuilder](https://book.kubebuilder.io) v4 project. Use the `kubebuilder` CLI to scaffold, and keep
the `+kubebuilder:scaffold` markers.

### Generated files

Do not edit generated files: `zz_generated.*`, `config/crd/bases/*`, `config/rbac/role.yaml`, `PROJECT`, `gen/` and
`ui/src/gen/`. Regenerate them:

- After a change in `api/`: `make manifests generate helm-crds`.
- After a change in `proto/`: `make proto ui-proto`.

## Make a change

1. Write the test first where a test protects real behavior: the engine, validation, security and the main user flows.
   Use table tests for pure logic, envtest for the controller and Playwright for the UI. Do not add tests for trivial
   code.
2. Keep the change small and focused. Keep files below 400 lines and functions short.
3. Return wrapped errors: `fmt.Errorf("...: %w", err)`. Never log a token, a cookie or a URL that contains a token.
4. Record a design change in [`docs/DECISIONS.md`](docs/DECISIONS.md) and update
   [`docs/DESIGN.md`](docs/DESIGN.md) in the same pull request.
5. Update the user docs in `docs/` when a behavior changes.
6. After a change to the controller, the server or the chart, run `make test-e2e`.

### Style

- Docs, comments and commit messages use [ASD-STE100 Simplified Technical English](https://www.asd-ste100.org): short
  sentences, the active voice, one instruction per sentence.
- Comments say why, not what. Start a doc comment with the name of the item, as in
  [Effective Go](https://go.dev/doc/effective_go).
- Go code follows `gofmt` and the linters in `.golangci.yml`.

### Dependencies

- Pin exact versions and commit the lockfiles (`go.sum`, `ui/pnpm-lock.yaml`, `sdk/python/uv.lock`,
  `sdk/rust/Cargo.lock`).
- Pin images by digest and GitHub Actions by commit SHA.
- Prefer the standard library. Add a module only when it clearly pays off.

## Commits and pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org): `feat:`, `fix:`, `docs:`, `test:`, `refactor:`,
  `perf:`, `build:`, `ci:`, `chore:`. The release notes come from them.
- Sign off each commit to certify the [Developer Certificate of Origin](https://developercertificate.org): add the
  `--signoff` flag to `git commit`.
- Open the pull request against `main`. CI must pass before a review.

## Releases

A maintainer tags `vX.Y.Z` on `main`. The release workflow then publishes the image
(`ghcr.io/thinnesen/chronicle`), the Helm chart (`oci://ghcr.io/thinnesen/charts/chronicle`) and the CLI binaries, all
signed with Sigstore cosign. chronicle follows [Semantic Versioning](https://semver.org); in the 0.x series a minor
version can contain breaking changes.

## License

By contributing, you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).
