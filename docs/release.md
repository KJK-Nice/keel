# Releasing Keel: signing and notarizing

`.github/workflows/release.yml` builds the release (see CONTRIBUTING.md, "Releasing"). The macOS app is signed ad hoc and not notarized unless the repository has all six secrets below; with any of them missing the job log says "The signing secrets are not all set" and the release still builds, as it did before MyGo's packager. Pull requests from forks never see the secrets, so their dry runs are ad hoc too.

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

The job puts the certificate and the notary credentials (the keychain profile `notary`, which `mygo.json` names) in a keychain that lives as long as the job, passes the identity to `scripts/release.sh` as `KEEL_SIGN_IDENTITY`, and MyGo signs `Keel.app` and its libghostty-vt with the hardened runtime and the entitlements of `packaging/macos/entitlements.plist`, submits the disk image to Apple's notary service, waits and staples the ticket. An App Store Connect API key works in place of the Apple ID (`notarytool store-credentials --key --key-id --issuer`); the workflow step would change with it.

Windows has no Keel build, so MyGo's Windows secrets (`MYGO_WINDOWS_CERTIFICATE_PASSWORD`) are not used.

## Auto-updates (not enabled)

MyGo can make Keel update itself (`updates` in `mygo.json`, the updater plugin in the app). It is not on: Keel has no updater. Turning it on would take:

- `go tool mygo keygen`, the public key into `mygo.json` (`updates.publicKey`), and the secret key as a repository secret `MYGO_UPDATER_PRIVATE_KEY`;
- `updates.github` (`KJK-Nice/keel`) in `mygo.json`, and `mygo build` signing an update archive per platform with that key (the workflow would pass the secret, and publish those files and the update manifests with the release);
- the updater plugin and its user interface in the app (`cmd/keel`, `internal/native`), with the agent invariants of `docs/ui-redesign.md` §6 in mind;
- notarized macOS builds first, since an update replaces the app Gatekeeper already accepted.

See MyGo's `docs/updates.md`.
