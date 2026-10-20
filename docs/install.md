# Install the CLI and the SDKs

chronicle has no release yet, so you install the CLI and the SDKs from this repository. Pin each one to a commit, so that a change on a branch cannot reach your build. The repository is private now. Give Git access to it with an SSH key or a token. For Go, also set `GOPRIVATE=github.com/Thinnesen/chronicle`.

The CLI and the SDKs do not need each other. The CLI is a tool for people. The SDKs are libraries for your own programs: workflows as code, and task workers.

## The `chronicle` CLI

The CLI is one Go binary. Build it with Go 1.27 or later:

```sh
git clone https://github.com/Thinnesen/chronicle && cd chronicle
git checkout <commit>
go build -o ~/.local/bin/chronicle ./cmd/chronicle
```

Log in once. With an OIDC login on the server, the CLI uses the device flow:

```sh
chronicle login --server https://chronicle.example.com -n team-a
```

In CI, log in with a service account token that has the audience `chronicle`:

```sh
kubectl create token ci -n team-a --audience=chronicle | chronicle login --server https://chronicle.example.com --token-file -
```

Then, for example: `chronicle submit --from etl -p day=2026-10-19 --log`, `chronicle list` (`-A` for all your namespaces), `chronicle approve <run>`. Run `chronicle --help` for all commands.

`chronicle lint` and `chronicle convert` need no login. Check your workflow files in CI before a merge:

```sh
chronicle lint workflows/
```

## The Go SDK

The Go SDK is a package of the main module:

```sh
go get github.com/Thinnesen/chronicle@<commit>
```

```go
import "github.com/Thinnesen/chronicle/sdk/go/chronicle"
```

See [sdk/go/README.md](../sdk/go/README.md).

## The Python SDK

The Python SDK (`chronicle-sdk`) needs Python 3.12 or later and only the standard library. With uv:

```sh
uv add "chronicle-sdk @ git+https://github.com/Thinnesen/chronicle@<commit>#subdirectory=sdk/python"
```

With pip: `pip install "chronicle-sdk @ git+https://github.com/Thinnesen/chronicle@<commit>#subdirectory=sdk/python"`. The import name is `chronicle`. See [sdk/python/README.md](../sdk/python/README.md).

## The Rust SDK

The Rust SDK is the package `chronicle-sdk`; its library name is `chronicle`. In `Cargo.toml`:

```toml
[dependencies]
chronicle-sdk = { git = "https://github.com/Thinnesen/chronicle", rev = "<commit>" }
```

```rust
use chronicle::{Client, Workflow};
```

See [sdk/rust/README.md](../sdk/rust/README.md).

## Task workers

A task worker is your own program that uses an SDK. Run it where you like, for example as a Deployment in the team namespace. It needs the URL of the chronicle server and a service account token with the audience `chronicle`. The token must allow `update` on `workflows/tasks` in the namespace. See [task-workers.md](task-workers.md).
