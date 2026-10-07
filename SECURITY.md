# Security Policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub:
[Report a vulnerability](https://github.com/stefang-kahoot/terraform-provider-nodeping/security/advisories/new)
(the **Security** tab → **Report a vulnerability**).

Do not open a public issue, pull request or discussion for a suspected
vulnerability.

A useful report includes the affected version, the provider configuration or
steps that trigger the problem, and what an attacker gains. Leave out real
NodePing API tokens and contact data. Redact them from logs and state files.

This provider has one maintainer, so there is no guaranteed response time.
Reports are handled in the private advisory, and a fix ships as a new release
together with the published advisory.

## Supported versions

Only the latest release gets security fixes. Upgrade to it before reporting,
if you can.

## Scope

In scope: this provider's code, its release artifacts and the workflows that
build them.

Out of scope: the NodePing service and API itself. Report those to NodePing.
Vulnerabilities in Terraform or in a Go dependency belong upstream, unless this
provider uses the dependency in a way that makes it exploitable.
