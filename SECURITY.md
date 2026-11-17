# Security Policy

## Supported versions

Only the latest release is supported. Fixes land on `dev` and ship in the next
tag; there are no backports.

| Version | Supported |
| ------- | --------- |
| latest release | ✅ |
| `dev` | ⚠️ pre-release, no guarantees |
| anything older | ❌ |

## Reporting a vulnerability

Open a [private security advisory](https://github.com/neotoxicfr/neofreebox/security/advisories/new)
rather than a public issue. Please include the version, the configuration
involved, and a way to reproduce.

## Security model

neofreebox holds a Freebox OS app token with every right on the box, so it is
meant to sit behind an authenticating reverse proxy (Authelia, Authentik…) and
also has its own login:

- A single account, created on first run with a one-time setup code that is
  only printed in the container logs, so reaching the page is not enough. The password is hashed with bcrypt
  in `account.json` in the data directory; failed sign-ins are limited to 10 per 15 minutes for the whole app (it sits
  behind a proxy, so client IPs mean nothing). Someone who gets past the proxy
  could use that to lock you out for 15 minutes: an accepted trade-off.
- Sessions are `HttpOnly`, `Secure`, `SameSite=Lax` cookies kept in memory: a
  restart signs you out.
- Every state-changing request is a POST checked by Go's cross-origin
  protection.
- The app token is read from a file (`NEOFREEBOX_APP_TOKEN_FILE`), never sent
  to the browser and never logged. Treat that file as a secret: it gives full
  control of the box.
- The box is reached over HTTPS, with Free's root certificates embedded.

## Container hardening

The image is `distroless/static` (CA bundle and time zones, no shell, no
package manager) running as a non-root user. The example compose adds
`read_only: true`, `cap_drop: [ALL]` and `no-new-privileges:true`.

## Supply chain

- Every GitHub Action is pinned to a full commit SHA, and the repository
  *enforces* SHA pinning, so a hijacked tag cannot alter a workflow.
- CI runs `govulncheck` and a Trivy filesystem scan (vulnerabilities, secrets, misconfigurations)
  on every push to `dev`, every pull request and every release tag, and fails on a fixable
  `CRITICAL`/`HIGH`. No image is published unless it passes.
- Dependabot alerts and security updates are enabled.
