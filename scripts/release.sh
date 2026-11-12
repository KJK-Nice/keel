#!/bin/sh
# Builds the release files into dist/. The release workflow runs it
# (.github/workflows/release.yml); it also runs locally, and changes nothing outside the worktree: it never uploads.
#
#   sh scripts/release.sh v0.1.0 macos   Keel for macOS, universal (arm64 + x86_64), packaged by mygo build:
#                                        keel-<mygo.json version>.dmg (Keel.app inside)
#   sh scripts/release.sh v0.1.0 linux   Keel for Linux, x86_64 and arm64 (mygo build builds both on any machine):
#                                        keel-<v>-linux-<amd64|arm64>.tar.gz and keel_<v>_<amd64|arm64>.deb
#   sh scripts/release.sh v0.1.0 signal  keel-signal for every OS (plain go build, not a MyGo app)
#
# The version of the packages is "version" in mygo.json and must equal the tag (a v0.0.0-pr<N> dry run of the
# workflow is the one exception). `keel version` prints the tag (MYGO_LDFLAGS -X main.version).
# LICENSE and THIRD_PARTY_NOTICES (scripts/notices.sh) go into every package and archive.
# KEEL_SIGN_IDENTITY (a Developer ID Application name) signs the macOS app and notarizes its disk image with the
# keychain profile "notary"; without it the app is signed ad hoc and not notarized, and the log says so.
# No KEEL_SIGNAL is baked in: Share stays off until the user enters a keel-signal in Settings · Sharing
# (docs/sharing.md).
set -eu

tag=${1:?usage: scripts/release.sh vX.Y.Z macos|linux|signal}
what=${2:?usage: scripts/release.sh vX.Y.Z macos|linux|signal}
case "$tag" in v[0-9]*) ;; *) echo "release: version must look like v0.1.0, got $tag" >&2; exit 1 ;; esac
v=${tag#v}
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
mkdir -p dist

# archname GOARCH: the CPU name in the keel-signal archive names (x86_64, arm64).
archname() {
	case "$1" in amd64) echo x86_64 ;; *) echo "$1" ;; esac
}

# docs FOLDER: the license files that ship beside a binary.
docs() {
	for f in LICENSE THIRD_PARTY_NOTICES; do
		if [ -f "$f" ]; then cp "$f" "$1/"; fi
	done
}

# fail MESSAGE
fail() {
	echo "release: $1" >&2
	exit 1
}

# package PLATFORMS: mygo build, with the version of the tag in `keel version`. Sets cv, the version of mygo.json,
# which names the files.
package() {
	cv=$(sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' mygo.json)
	[ -n "$cv" ] || fail "no version in mygo.json"
	case "$tag" in
	v0.0.0-pr*) ;; # a dry run of the workflow on a pull request
	*) [ "$cv" = "$v" ] || fail "mygo.json says version $cv but the tag is $tag: set \"version\" in mygo.json to $v and tag that commit" ;;
	esac
	[ -f THIRD_PARTY_NOTICES ] || sh scripts/notices.sh THIRD_PARTY_NOTICES
	rm -rf build
	platforms=$1
	if [ -n "${KEEL_SIGN_IDENTITY:-}" ]; then
		echo "release: signing with the Developer ID in KEEL_SIGN_IDENTITY and notarizing (keychain profile notary)"
		set -- -sign "$KEEL_SIGN_IDENTITY"
	else
		echo "release: no KEEL_SIGN_IDENTITY: the macOS app is signed ad hoc and not notarized (Gatekeeper blocks it on other Macs until the user allows it)"
		set -- -skip-notarize
	fi
	MYGO_LDFLAGS="-X main.version=$tag" go tool mygo build -platform "$platforms" "$@"
}

case "$what" in
macos)
	[ "$(uname -s)" = Darwin ] || fail "the macOS app is signed and put in a disk image on macOS only"
	package darwin/universal
	app=build/darwin-universal/Keel.app
	exe=$app/Contents/MacOS/Keel
	for f in "$exe" "$app/Contents/Resources/libghostty-vt.dylib"; do
		archs=$(lipo -archs "$f")
		case "$archs" in *arm64*x86_64* | *x86_64*arm64*) ;; *) fail "$f is $archs, want arm64 and x86_64" ;; esac
	done
	# The universal binary runs here (arm64 or Rosetta).
	[ "$("$exe" version)" = "keel $tag" ] || fail "keel version is not 'keel $tag'"
	[ "$(plutil -extract CFBundleIdentifier raw "$app/Contents/Info.plist")" = app.keel.desktop ] || fail "the bundle identifier changed"
	[ "$(plutil -extract CFBundleShortVersionString raw "$app/Contents/Info.plist")" = "$cv" ] || fail "Info.plist version is not $cv"
	for f in LICENSE THIRD_PARTY_NOTICES; do
		[ -f "$app/Contents/Resources/$f" ] || fail "$f is not in the app"
	done
	codesign --verify --deep --strict "$app"
	cp build/darwin-universal/keel-"$cv".dmg dist/
	;;
linux)
	package linux/amd64,linux/arm64
	for arch in amd64 arm64; do
		dir=build/linux-$arch
		listing=$(tar -tzf "$dir/keel-$cv-linux-$arch.tar.gz")
		for f in keel libghostty-vt.so LICENSE THIRD_PARTY_NOTICES; do
			echo "$listing" | grep -qx "$f" || fail "$f is not in the $arch archive"
		done
		case "$arch" in amd64) want='x86-64' ;; arm64) want='ARM aarch64' ;; esac
		file "$dir/keel" | grep -q "$want" || fail "$dir/keel is not $want"
		# The binary of this machine's CPU runs here.
		if [ "$(uname -s)" = Linux ] && [ "$(go env GOARCH)" = "$arch" ]; then
			[ "$("$dir/keel" version)" = "keel $tag" ] || fail "keel version is not 'keel $tag'"
		fi
		if command -v dpkg-deb >/dev/null 2>&1; then
			dpkg-deb -c "$dir/keel_${cv}_$arch.deb" | grep -q 'opt/keel/libghostty-vt.so' || fail "libghostty-vt.so is not in the $arch .deb"
		fi
		cp "$dir/keel-$cv-linux-$arch.tar.gz" "$dir/keel_${cv}_$arch.deb" dist/
	done
	;;
signal)
	for target in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64 windows/amd64; do
		os=${target%/*}
		arch=${target#*/}
		name="keel-signal-$v-$os-$(archname "$arch")"
		exe=keel-signal
		if [ "$os" = windows ]; then exe=keel-signal.exe; fi
		rm -rf "dist/$name" && mkdir "dist/$name"
		CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -o "dist/$name/$exe" ./cmd/keel-signal
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
