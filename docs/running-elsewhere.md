# Running Athenaeum on another machine

Athenaeum is a single executable with the frontend embedded. Copying that one
file to a machine is the whole installation: no Node.js, no npm, no SQLite CLI,
no database process, no browser extension (constitution C6, requirement N4).

## 1. Get the archives

Released versions are built and published automatically. Pushing a version tag
runs `.github/workflows/release.yml`, which re-runs the full gate against the
tagged commit, cross-compiles every target, verifies the binary reports the
expected version, and publishes the release with checksums. Download from the
repository's Releases page.

Authentication uses the `GITHUB_TOKEN` that Actions mints per run. No personal
access token is required, and none should be stored in the repository or its
secrets: a workflow-scoped token expires when the run ends, which a PAT does
not.

To cut a release:

```bash
git tag -a v0.1.0-alpha.4 -m "release notes go here; the workflow uses them"
git push origin v0.1.0-alpha.4
```

A tag that is not a bare `x.y.z` is published as a pre-release.

To build the artifacts for a tag without publishing anything, run the Release
workflow manually from the Actions tab and give it the tag name; it uploads the
archives as run artifacts and stops before creating a release.

### Building locally instead

```bash
make package
```

This cross-compiles from any host, because the build sets `CGO_ENABLED=0`:

```text
dist/athenaeum-<version>-darwin-amd64.tar.gz    Intel Mac
dist/athenaeum-<version>-darwin-arm64.tar.gz    Apple Silicon Mac
dist/athenaeum-<version>-linux-amd64.tar.gz
dist/athenaeum-<version>-linux-arm64.tar.gz
```

Each archive contains the executable, `LICENSE`, and `README.md`.

## 2. Copy it over

Over Tailscale:

```bash
tailscale file cp dist/athenaeum-0.1.0-dev-darwin-arm64.tar.gz <machine>:
```

Or with scp:

```bash
scp dist/athenaeum-0.1.0-dev-darwin-arm64.tar.gz <machine>:~/
```

## 3. Install and run

```bash
tar -xzf athenaeum-0.1.0-dev-darwin-arm64.tar.gz
chmod +x athenaeum-darwin-arm64
mv athenaeum-darwin-arm64 ~/.local/bin/athenaeum     # anywhere on PATH

athenaeum open /path/to/workspace/athenaeum.toml
```

On macOS, Gatekeeper will refuse an unsigned binary downloaded from elsewhere.
Clear the quarantine attribute:

```bash
xattr -d com.apple.quarantine ~/.local/bin/athenaeum
```

Signing and notarisation are release-engineering work, not yet done.

## 4. The workspace travels separately

The executable carries no documents. Point it at an `athenaeum.toml` on the
target machine — a cloned repository, a synced folder, whatever holds the
Markdown. Files stay authoritative and stay where they are (constitution C2).

Shared annotations live in `.athenaeum/shared/` inside the workspace and travel
with it. Personal annotations, session state, and the search index live in the
user's own directories and deliberately do not (spec 03 section 1).

## Serving to other machines instead of installing

If you would rather run one instance and reach it from everywhere, use remote
mode rather than copying the binary around.

```bash
# Generate a token once, readable only by you.
mkdir -p ~/.config/athenaeum && chmod 700 ~/.config/athenaeum
python3 -c "import secrets;print(secrets.token_urlsafe(32))" \
  > ~/.config/athenaeum/remote-token
chmod 600 ~/.config/athenaeum/remote-token

# Bind to the Tailscale interface specifically, never 0.0.0.0.
athenaeum serve athenaeum.toml \
  --remote \
  --bind "$(tailscale ip -4)" \
  --port 7777 \
  --auth-token-file ~/.config/athenaeum/remote-token \
  --no-open
```

Then open `http://<tailscale-ip>:7777/bootstrap?t=<token>` on any device signed
into the tailnet.

Why bind to the Tailscale address rather than `0.0.0.0`: `0.0.0.0` publishes the
workspace on every interface, including the local network and any Docker
bridges. Binding to the Tailscale address alone means only the tailnet can
reach it.

Athenaeum provides no TLS (spec 03 section 11). Tailscale supplies WireGuard
encryption end to end, which is why it is the documented deployment. Do not
expose remote mode to the open internet without a trusted reverse proxy
terminating TLS in front of it.

### Running it as a service

`systemd` user unit, so it survives logout and restarts on failure:

```ini
# ~/.config/systemd/user/athenaeum.service
[Unit]
Description=Athenaeum
After=network-online.target tailscaled.service

[Service]
ExecStart=/home/%u/.local/bin/athenaeum serve /home/%u/dev/athenaeum/athenaeum.toml \
  --remote --bind 100.79.67.51 --port 7777 \
  --auth-token-file /home/%u/.config/athenaeum/remote-token --no-open
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
```

```bash
systemctl --user daemon-reload
systemctl --user enable --now athenaeum
loginctl enable-linger "$USER"   # keep it running after logout
```

## Windows

Windows is a portability target, not a v0.1 release platform (D-021). The code
does cross-compile today:

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o athenaeum.exe ./cmd/athenaeum
```

That binary is untested: no acceptance run covers Windows, and the path
semantics that differ there — case-insensitive comparison, drive letters,
reserved device names, atomic replace behaviour — are exactly the areas where
correctness matters most. Treat it as unsupported until Windows joins the
acceptance matrix.

Reaching a Linux or macOS instance from Windows over remote mode works fine and
is the recommended route today.
