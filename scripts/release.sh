#!/bin/sh
# Builds release archives into dist/. The release workflow runs it on each
# runner (.github/workflows/release.yml); it also runs locally.
#
#   sh scripts/release.sh v0.1.0 app     Keel for this machine: on macOS
#                                        Keel-<v>-macos-universal.zip (arm64 +
#                                        x86_64), on Linux
#                                        keel-<v>-linux-<x86_64|arm64>.tar.gz
#   sh scripts/release.sh v0.1.0 signal  keel-signal for every OS (pure Go)
#
# LICENSE and THIRD_PARTY_NOTICES (scripts/notices.sh) go into every archive
# when they exist at the repo root. No KEEL_SIGNAL is baked in: Share stays
# off until the user enters a keel-signal in Settings · Sharing
# (docs/sharing.md).
set -eu

tag=${1:?usage: scripts/release.sh vX.Y.Z app|signal}
what=${2:?usage: scripts/release.sh vX.Y.Z app|signal}
case "$tag" in v[0-9]*) ;; *) echo "release: version must look like v0.1.0, got $tag" >&2; exit 1 ;; esac
v=${tag#v}
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
mkdir -p dist

# archname GOARCH: the CPU name in archive names (x86_64, arm64).
archname() {
	case "$1" in amd64) echo x86_64 ;; *) echo "$1" ;; esac
}

# docs FOLDER: the license files that ship beside a binary.
docs() {
	for f in LICENSE THIRD_PARTY_NOTICES; do
		if [ -f "$f" ]; then cp "$f" "$1/"; fi
	done
}

case "$what" in
app)
	make ui
	tags=$(sh scripts/go-tags.sh)
	ld="-X main.version=$tag"
	case "$(uname -s)" in
	Darwin)
		# One universal Keel.app: cgo cross-builds x86_64 on Apple Silicon.
		for arch in arm64 amd64; do
			GOARCH=$arch sh scripts/go.sh build $tags -trimpath -ldflags "$ld" -o "bin/keel-$arch" ./cmd/keel
		done
		lipo -create -output bin/keel bin/keel-arm64 bin/keel-amd64
		notices=
		if [ -f THIRD_PARTY_NOTICES ]; then notices=THIRD_PARTY_NOTICES; fi
		make macos-app KEEL_VERSION="$tag" KEEL_NOTICES="$notices"
		codesign --verify --deep --strict bin/Keel.app
		ditto -c -k --keepParent bin/Keel.app "dist/Keel-$v-macos-universal.zip"
		;;
	Linux)
		name="keel-$v-linux-$(archname "$(go env GOARCH)")"
		sh scripts/go.sh build $tags -trimpath -ldflags "$ld" -o bin/keel ./cmd/keel
		rm -rf "dist/$name" && mkdir "dist/$name"
		cp bin/keel README.md "dist/$name/"
		docs "dist/$name"
		tar -C dist -czf "dist/$name.tar.gz" "$name"
		rm -rf "dist/$name"
		;;
	*)
		echo "release: no app build for $(uname -s)" >&2
		exit 1
		;;
	esac
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
	echo "release: unknown target $what (app or signal)" >&2
	exit 1
	;;
esac
ls -l dist
