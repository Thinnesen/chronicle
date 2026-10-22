# Security policy

chronicle runs workloads with the credentials of your teams, so we treat security reports with priority.

## Report a vulnerability

Do not open a public issue for a security problem. Report it privately with a
[GitHub security advisory](https://github.com/Thinnesen/chronicle/security/advisories/new).

Tell us:

- the affected version or commit,
- the steps to reproduce the problem, or a proof of concept,
- the impact that you expect.

We confirm the report within 7 days. We keep you informed while we work on a fix, and we credit you in the advisory
unless you prefer to stay anonymous.

## Supported versions

chronicle is in the 0.x series. We fix security problems in the latest release only. Upgrade to the latest release to
get the fixes.

## Disclosure

We publish a fixed release first, then the GitHub security advisory with the details and a CVE where it applies.

## Secure your installation

- The threat model and the security design are in [`docs/DESIGN.md`](docs/DESIGN.md) (section 6).
- Production settings (login, network policies, images by digest) are in [`docs/operations.md`](docs/operations.md).
- Release images and checksums are signed with Sigstore cosign. See the release notes for how to verify them.
