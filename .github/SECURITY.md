# Security policy

## Supported versions

Only the latest release of Moontop gets fixes. Please update before reporting.

## Reporting a vulnerability

Please **don't open a public issue** for a security problem. Report it
privately through GitHub instead:
[Security → Report a vulnerability](https://github.com/justinh-rahb/moontop/security/advisories/new).

Include what you found, how to reproduce it, and what Moontop version, OS and
Moonraker version you saw it on. You should hear back within a week.

## Scope

Moontop is a client: it talks to a Moonraker instance you point it at and can
do anything that Moonraker's API allows, including moving the printer, heating
it, editing its config and rebooting the host. Things in scope include:

- Moontop sending a command, or editing or uploading a file, that you didn't ask for.
- Moontop leaking an API key or other credential (to logs, the terminal title,
  another host, or a file with loose permissions).
- The install script or release artifacts being tampered with or not verified.

Securing Moonraker itself (authorization, trusted clients, exposing it to the
internet) is out of scope here; see
[Moonraker's documentation](https://moonraker.readthedocs.io/en/latest/configuration/#authorization).
