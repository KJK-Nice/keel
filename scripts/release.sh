#!/bin/sh
# Builds the release files into dist/. The release workflow runs it
# (.github/workflows/release.yml); it also runs locally, and changes nothing outside the worktree: it never uploads.
#
#   sh scripts/release.sh vX.Y.Z macos  Slime for macOS, universal (arm64 + x86_64), packaged by mygo build:
#                                        slime-<mygo.json version>.dmg (Slime.app inside)
#   sh scripts/release.sh vX.Y.Z linux  Slime for Linux, x86_64 and arm64 (mygo build builds both on any machine):
#                                        slime-<v>-linux-<amd64|arm64>.tar.gz and slime_<v>_<amd64|arm64>.deb
#   sh scripts/release.sh vX.Y.Z signal slime-signal for every OS (plain go build, not a MyGo app)
#
# The version of the packages is "version" in mygo.json and must equal the tag (a v0.0.0-pr<N> dry run of the
# workflow is the one exception). `slime version` prints the tag (MYGO_LDFLAGS -X main.version).
# LICENSE and THIRD_PARTY_NOTICES (scripts/notices.sh) go into every package and archive. The list is the variable
# $shipped below; mygo.json ("resources") has to repeat it, since MyGo reads that file and not this script.
# SLIME_SIGN_IDENTITY (a Developer ID Application name) signs the macOS app and notarizes its disk image with the
# keychain profile "notary"; without it the app is signed ad hoc and not notarized, and the log says so.
# MYGO_UPDATER_PRIVATE_KEY (the secret of mygo keygen; mygo.json holds its public key) signs the in-app updates:
# mygo build then writes update-<platform>.json, the app archive and deltas, which the release must carry for
# "Check for Updates…" (docs/release.md). A tagged release fails without it; a pull request dry run goes on without.
# No SLIME_SIGNAL is baked in: Share stays off until the user enters a slime-signal in Settings · Sharing
# (docs/sharing.md).
set -eu

tag=${1:?usage: scripts/release.sh vX.Y.Z macos|linux|signal}
what=${2:?usage: scripts/release.sh vX.Y.Z macos|linux|signal}
case "$tag" in v[0-9]*) ;; *) echo "release: version must look like vX.Y.Z, got $tag" >&2; exit 1 ;; esac
v=${tag#v}
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
mkdir -p dist
shipped="LICENSE THIRD_PARTY_NOTICES"

# archname GOARCH: the CPU name in the slime-signal archive names (x86_64, arm64).
archname() {
	case "$1" in amd64) echo x86_64 ;; *) echo "$1" ;; esac
}

# updates DIR: the in-app update files of one mygo build platform (manifest, app archive, deltas) into dist/, when
# the build signed them. Without MYGO_UPDATER_PRIVATE_KEY there are none (package stops a tagged release first).
updates() {
	[ -n "${MYGO_UPDATER_PRIVATE_KEY:-}" ] || return 0
	ls "$1"/update-*.json >/dev/null 2>&1 || fail "mygo build wrote no update manifest in $1"
	for f in "$1"/update-*.json "$1"/*.tar.gz "$1"/*.delta; do
		if [ -f "$f" ]; then cp "$f" dist/; fi
	done
}

# docs FOLDER: the license files that ship beside a binary.
docs() {
	for f in $shipped; do
		if [ -f "$f" ]; then cp "$f" "$1/"; fi
	done
}

# fail MESSAGE
fail() {
	echo "release: $1" >&2
	exit 1
}

# package PLATFORMS: mygo build, with the version of the tag in `slime version`. Sets cv, the version of mygo.json,
# which names the files.
package() {
	cv=$(sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' mygo.json)
	[ -n "$cv" ] || fail "no version in mygo.json"
	case "$tag" in
	v0.0.0-pr*) ;; # a dry run of the workflow on a pull request
	*) [ "$cv" = "$v" ] || fail "mygo.json says version $cv but the tag is $tag: set \"version\" in mygo.json to $v and tag that commit" ;;
	esac
	if [ -z "${MYGO_UPDATER_PRIVATE_KEY:-}" ]; then
		case "$tag" in
		v0.0.0-pr*) echo "release: no MYGO_UPDATER_PRIVATE_KEY: no update files (a dry run)" ;;
		*) fail "no MYGO_UPDATER_PRIVATE_KEY: installed apps could not update to this release (docs/release.md)" ;;
		esac
	fi
	[ -f THIRD_PARTY_NOTICES ] || sh scripts/notices.sh THIRD_PARTY_NOTICES
	rm -rf build
	platforms=$1
	SLIME_SIGN_IDENTITY=${SLIME_SIGN_IDENTITY:-${KEEL_SIGN_IDENTITY:-}} # its name before the rename still works
	if [ -n "${SLIME_SIGN_IDENTITY:-}" ]; then
		echo "release: signing with the Developer ID in SLIME_SIGN_IDENTITY and notarizing (keychain profile notary)"
		set -- -sign "$SLIME_SIGN_IDENTITY"
	else
		echo "release: no SLIME_SIGN_IDENTITY: the macOS app is signed ad hoc and not notarized (Gatekeeper blocks it on other Macs until the user allows it)"
		set -- -skip-notarize
	fi
	MYGO_LDFLAGS="-X main.version=$tag" go tool mygo build -platform "$platforms" "$@"
}

case "$what" in
macos)
	[ "$(uname -s)" = Darwin ] || fail "the macOS app is signed and put in a disk image on macOS only"
	package darwin/universal
	app=build/darwin-universal/Slime.app
	exe=$app/Contents/MacOS/Slime
	for f in "$exe" "$app/Contents/Resources/libghostty-vt.dylib"; do
		archs=$(lipo -archs "$f")
		case "$archs" in *arm64*x86_64* | *x86_64*arm64*) ;; *) fail "$f is $archs, want arm64 and x86_64" ;; esac
	done
	# The universal binary runs here (arm64 or Rosetta).
	[ "$("$exe" version)" = "slime $tag" ] || fail "slime version is not 'slime $tag'"
	[ "$(plutil -extract CFBundleIdentifier raw "$app/Contents/Info.plist")" = app.keel.desktop ] || fail "the bundle identifier changed"
	[ "$(plutil -extract CFBundleShortVersionString raw "$app/Contents/Info.plist")" = "$cv" ] || fail "Info.plist version is not $cv"
	for f in $shipped; do
		[ -f "$app/Contents/Resources/$f" ] || fail "$f is not in the app"
	done
	codesign --verify --deep --strict "$app"
	cp build/darwin-universal/slime-"$cv".dmg dist/
	updates build/darwin-universal
	;;
linux)
	package linux/amd64,linux/arm64
	for arch in amd64 arm64; do
		dir=build/linux-$arch
		listing=$(tar -tzf "$dir/slime-$cv-linux-$arch.tar.gz")
		for f in slime libghostty-vt.so $shipped; do
			echo "$listing" | grep -qx "$f" || fail "$f is not in the $arch archive"
		done
		case "$arch" in amd64) want='x86-64' ;; arm64) want='ARM aarch64' ;; esac
		file "$dir/slime" | grep -q "$want" || fail "$dir/slime is not $want"
		# The binary of this machine's CPU runs here.
		if [ "$(uname -s)" = Linux ] && [ "$(go env GOARCH)" = "$arch" ]; then
			[ "$("$dir/slime" version)" = "slime $tag" ] || fail "slime version is not 'slime $tag'"
		fi
		if command -v dpkg-deb >/dev/null 2>&1; then
			dpkg-deb -c "$dir/slime_${cv}_$arch.deb" | grep -q 'opt/slime/libghostty-vt.so' || fail "libghostty-vt.so is not in the $arch .deb"
		fi
		cp "$dir/slime-$cv-linux-$arch.tar.gz" "$dir/slime_${cv}_$arch.deb" dist/
		updates "$dir"
	done
	;;
signal)
	for target in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64 windows/amd64; do
		os=${target%/*}
		arch=${target#*/}
		name="slime-signal-$v-$os-$(archname "$arch")"
		exe=slime-signal
		if [ "$os" = windows ]; then exe=slime-signal.exe; fi
		rm -rf "dist/$name" && mkdir "dist/$name"
		CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -o "dist/$name/$exe" ./cmd/slime-signal
		docs "dist/$name"
		if [ "$os" = windows ]; then
			(cd dist && zip -qr "$name.zip" "$name")
		else
			tar -C dist -czf "dist/$name.tar.gz" "$name"
		fi
		rm -rf "dist/$name"
	done
	;;
*)
	echo "release: unknown target $what (macos, linux or signal)" >&2
	exit 1
	;;
esac
ls -l dist
