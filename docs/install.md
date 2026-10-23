# Install chronicle

This guide installs the server with Helm, the `chronicle` CLI and the SDKs. Replace `0.1.0` with the release that you
want; the [releases](https://github.com/Thinnesen/chronicle/releases) list them.

The CLI and the SDKs do not need each other. The CLI is a tool for people. The SDKs are libraries for your own
programs: workflows as code, and task workers.

## The server

chronicle is tested with Kubernetes 1.37. It needs [cert-manager](https://cert-manager.io) for the certificate of its
admission webhook.

```sh
helm repo add jetstack https://charts.jetstack.io
helm install cert-manager jetstack/cert-manager --version v1.21.2 \
  --namespace cert-manager --create-namespace --set crds.enabled=true --wait

helm install chronicle oci://ghcr.io/thinnesen/charts/chronicle --version 0.1.0 \
  --namespace chronicle-system --create-namespace --wait
```

The chart installs the custom resources, the controller and the API server with the web UI. The values in
[`dist/chart/values.yaml`](../dist/chart/values.yaml) configure the login (OIDC or Microsoft Entra ID), the teams, the
archive database and more. [`operations.md`](operations.md) describes a production setup.

The image `ghcr.io/thinnesen/chronicle` is signed with Sigstore cosign. Verify it before you install:

```sh
cosign verify ghcr.io/thinnesen/chronicle:0.1.0 \
  --certificate-identity-regexp 'https://github.com/Thinnesen/chronicle/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## The `chronicle` CLI

Download the archive for your system from the [releases](https://github.com/Thinnesen/chronicle/releases), check it
and put the binary on your `PATH`. For example, on Linux with amd64:

```sh
curl -LO https://github.com/Thinnesen/chronicle/releases/download/v0.1.0/chronicle_0.1.0_linux_amd64.tar.gz
curl -LO https://github.com/Thinnesen/chronicle/releases/download/v0.1.0/checksums.txt
sha256sum --check --ignore-missing checksums.txt
tar -xzf chronicle_0.1.0_linux_amd64.tar.gz chronicle && mv chronicle ~/.local/bin/
chronicle --version
```

With Go 1.27 or later, you can also build it: `go install github.com/Thinnesen/chronicle/cmd/chronicle@v0.1.0`.

Log in once. With an OIDC login on the server, the CLI uses the device flow:

```sh
chronicle login --server https://chronicle.example.com --namespace team-a
```

In CI, log in with a service account token that has the audience `chronicle`:

```sh
kubectl create token ci --namespace team-a --audience=chronicle | chronicle login --server https://chronicle.example.com --token-file -
```

Then, for example: `chronicle submit --from etl -p day=2026-10-19 --log`, `chronicle list` (`-A` for all your
namespaces), `chronicle approve <run>`. Run `chronicle --help` for all commands.

`chronicle lint` needs no login. Check your workflow files in CI before a merge:

```sh
chronicle lint workflows/
```

## The Go SDK

The Go SDK is a package of the main module:

```sh
go get github.com/Thinnesen/chronicle@v0.1.0
```

```go
import "github.com/Thinnesen/chronicle/sdk/go/chronicle"
```

See [sdk/go/README.md](../sdk/go/README.md).

## The Python SDK

The Python SDK (`chronicle-sdk`) needs Python 3.12 or later and only the standard library. With uv:

```sh
uv add "chronicle-sdk @ git+https://github.com/Thinnesen/chronicle@v0.1.0#subdirectory=sdk/python"
```

With pip: `pip install "chronicle-sdk @ git+https://github.com/Thinnesen/chronicle@v0.1.0#subdirectory=sdk/python"`.
The import name is `chronicle`. See [sdk/python/README.md](../sdk/python/README.md).

## The Rust SDK

The Rust SDK is the package `chronicle-sdk`; its library name is `chronicle`. In `Cargo.toml`:

```toml
[dependencies]
chronicle-sdk = { git = "https://github.com/Thinnesen/chronicle", tag = "v0.1.0" }
```

```rust
use chronicle::{Client, Workflow};
```

See [sdk/rust/README.md](../sdk/rust/README.md).

## Task workers

A task worker is your own program that uses an SDK. Run it where you like, for example as a Deployment in the team
namespace. It needs the URL of the chronicle server and a service account token with the audience `chronicle`. The
token must allow `update` on `workflows/tasks` in the namespace. See [task-workers.md](task-workers.md).
