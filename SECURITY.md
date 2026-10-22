# Security Policy

dtui is an experimental, unofficial, best-effort project. It is read-only by
design (it never writes to a environment) and reads credentials from the local
`dtctl` configuration and credential store without modifying them.

## Reporting a vulnerability

Please **do not open a public issue** for security problems.

Report privately through GitHub's
[private vulnerability reporting](https://github.com/dynatrace-oss/dtui/security/advisories/new)
for this repository. Include the affected version, steps to reproduce and the
impact you observed.

Reports are handled on a best-effort basis; there is no guaranteed response time.

## Scope

In scope: this repository's code and release binaries.
Out of scope: vulnerabilities in Dynatrace products themselves (report those
through Dynatrace's official channels) and in `dtctl` (report to the
[dtctl repository](https://github.com/dynatrace-oss/dtctl)).
