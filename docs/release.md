# Releasing Slime

## Cutting a release

Releases are cut from `main` by pushing a tag; `.github/workflows/release.yml` does the rest. First set `"version"` in `mygo.json` (the one place the version lives) to the release, without the `v`, commit it, then tag that commit:

```bash
git tag vX.Y.Z && git push origin vX.Y.Z
```

The workflow runs `scripts/release.sh` for each target; the script is the source of what is built, and it fails, before anything is published, when the tag and `mygo.json` disagree. A tag with a hyphen (`vX.Y.Z-rc.1`) is a pre-release. Release builds bake in no `SLIME_SIGNAL`. The release is published with the install notes of `.github/release-notes.md` (its "What's new" section is the release's own: rewrite it for each tag) and notes generated from the merged PRs. A PR that changes the release tooling (`mygo.json`, `packaging/`, the Makefile, the scripts, the workflow) runs the same builds as a dry run and keeps the files as workflow artifacts. Locally, `sh scripts/release.sh vX.Y.Z macos|linux|signal` builds into `dist/`; it never uploads.

## Release files

| Target | Files |
|---|---|
| macOS, universal (arm64 and x86_64) | `slime-<version>.dmg`, with `Slime.app` inside; for updates, `slime-<version>-darwin-universal.tar.gz`, `update-darwin-universal.json` and deltas |
| Linux, `amd64` and `arm64` (one Ubuntu runner builds both) | `slime-<version>-linux-<arch>.tar.gz` and `slime_<version>_<arch>.deb`; for updates, `update-linux-<arch>.json` and deltas |
| slime-signal, a plain `go build`, for macOS, Linux and Windows | `slime-signal-<version>-<os>-<cpu>` archives |

`LICENSE` and `THIRD_PARTY_NOTICES` (`scripts/notices.sh`) are in each, and `SHA256SUMS` lists every file. The packages keep MyGo's file names. Windows has no Slime app, only slime-signal.

## Signing and notarizing

The macOS app is signed ad hoc and not notarized unless the repository has all six secrets below; with any of them missing the job log says "The signing secrets are not all set" and the release still builds, as it did before MyGo's packager. Pull requests from forks never see the secrets, so their dry runs are ad hoc too.

## Secrets to add

Repository secrets (Settings, Secrets and variables, Actions). Names only here; never commit or print their values.

| Secret | What it is |
|---|---|
| `MACOS_CERTIFICATE` | the "Developer ID Application" certificate with its private key, exported from Keychain Access as a `.p12`, base64 encoded (`base64 -i certificate.p12`) |
| `MACOS_CERTIFICATE_PASSWORD` | the password the `.p12` was exported with |
| `MACOS_SIGNING_IDENTITY` | the certificate's name as codesign knows it, `Developer ID Application: Name (TEAMID)` |
| `APPLE_ID` | the Apple ID of the developer account |
| `APPLE_TEAM_ID` | the team ID, the `TEAMID` of the identity |
| `APPLE_APP_PASSWORD` | an app-specific password of that Apple ID (account.apple.com, Sign-In and Security) |
| `MYGO_UPDATER_PRIVATE_KEY` | the secret key of `mygo keygen` (`mygo-update.key`) that signs the in-app updates; required for a tagged release (see Auto-updates) |

The job puts the certificate and the notary credentials (the keychain profile `notary`, which `mygo.json` names) in a keychain that lives as long as the job, passes the identity to `scripts/release.sh` as `SLIME_SIGN_IDENTITY`, and MyGo signs `Slime.app` and its libghostty-vt with the hardened runtime and the entitlements of `packaging/macos/entitlements.plist`, submits the disk image to Apple's notary service, waits and staples the ticket. An App Store Connect API key works in place of the Apple ID (`notarytool store-credentials --key --key-id --issuer`); the workflow step would change with it.

Windows has no Slime build, so MyGo's Windows secrets (`MYGO_WINDOWS_CERTIFICATE_PASSWORD`) are not used.

## Auto-updates

Slime updates itself with MyGo's updater (`docs/updates.md` in the MyGo module): "Check for Updates…" is in the Slime menu and the menu bar item, and the app checks in the background once a day. The update window is MyGo's, drawn in native UI (`plugins/updater/native`). Builds that cannot update (`bin/slime`, the `.deb` in `/opt`) never check and say why when asked.

- `updates` in `mygo.json` holds the public key of `mygo keygen` and `github: KJK-Nice/slime`: installed apps read `update-<platform>.json` from the **latest** release (`releases/latest/download/`), so a pre-release never reaches them.
- The secret key is the repository secret `MYGO_UPDATER_PRIVATE_KEY` (the contents of `mygo-update.key`). Keep a copy in a password manager: installed apps accept only updates signed with it, so losing it strands them, and anyone with it can ship code to them. Changing it means a new public key in `mygo.json`, which only apps built after the change trust.
- `scripts/release.sh` fails a tagged release without the key, and adds what `mygo build` signed to `dist/`: `update-<platform>.json` (darwin-universal, linux-amd64, linux-arm64), the app archive (`slime-<version>-darwin-universal.tar.gz` on macOS) and deltas from earlier versions (`*.delta`, none for the first release that has updates).
- The release notes of the update window come from `CHANGELOG.md` (`## X.Y.Z`) when the file exists; Slime has none, so the window shows none.
