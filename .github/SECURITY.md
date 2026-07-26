# Security Policy

## Supported versions

Agenton Pocket is pre-1.0. Security fixes are applied to the latest release on
`main`; older tagged releases are not maintained.

| Version | Supported |
|---|---|
| latest `main` / newest release | ✅ |
| older tags | ❌ |

## Reporting a vulnerability

**Please do not open a public issue for security problems.**

Report privately through GitHub Security Advisories:
**Security → Report a vulnerability** on this repository
(`https://github.com/nduwork/agenton-pocket/security/advisories/new`).

Include:

- A description of the issue and its impact
- Steps to reproduce (or a proof of concept)
- Affected version / commit

You will get an acknowledgement within a few days. Once a fix is available we
will coordinate a disclosure timeline with you.

## Scope notes

The daemon listens on a Unix socket only and never opens a public port; remote
access is expected to ride a private tailnet, and the web bridge binds the
machine's tailnet IP rather than `0.0.0.0`. Reports that assume the bridge is
exposed directly to the public internet should say so explicitly.
