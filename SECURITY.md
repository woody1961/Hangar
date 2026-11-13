# Security

Hangar runs on your own computer and keeps your hangar there: it has no server, no account and no telemetry. Its
security design is described under "Privacy and security" in the [README](README.md).

## What is checked

Before each release:

- `govulncheck ./...` (for macOS and Windows) finds no known vulnerability in Go or the libraries Hangar uses. Hangar
  is built with the Go version named by `toolchain` in `go.mod`; keep it on the latest patch release.
- `go vet`, `staticcheck` and `gosec` are run and their findings reviewed.
- `npm test` passes, including the checks for oversized pictures and spreadsheet formulas in exports.

## Things to know

- Hangar's RSI window is a browser controlled through its debugging connection, which listens on this computer only
  while the window is open. Other programs running as you could connect to it meanwhile, so close the window on a
  shared computer (Settings can close it after every import).
- For the bookmark, Hangar listens on `127.0.0.1` (port 8742 by default). It answers only requests addressed to
  `localhost`, and accepts imports only from RSI's own site or Hangar itself.
- Nothing is encrypted at rest. Hangar's folder is made private to your account on macOS and Linux; anyone who can
  sign in as you can read it. Exports and backups contain everything, including gift recipients.
- The release programs are not signed by a recognised publisher (the Mac app is signed locally, not notarised), so
  macOS and Windows warn the first time you open them. Download them only from this repository.
