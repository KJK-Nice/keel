#!/bin/sh
# go wrapper: Darwin CGO flags Wails needs but `wails build` would have set.
export CGO_ENABLED=1
if [ "$(go env GOOS 2>/dev/null)" = darwin ]; then
	# Go 1.27's runtime object is macOS 13. Unset target lets ld use 11.0
	# while clang tags CGO objects as the SDK (15+). Same min everywhere.
	export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-13.0}"
	min="-mmacosx-version-min=${MACOSX_DEPLOYMENT_TARGET}"
	case " ${CGO_LDFLAGS:-} " in
	*" -framework UniformTypeIdentifiers "*) ;;
	*) export CGO_LDFLAGS="${CGO_LDFLAGS:+$CGO_LDFLAGS }-framework UniformTypeIdentifiers" ;;
	esac
	case " ${CGO_LDFLAGS:-} " in
	*" -mmacosx-version-min="*) ;;
	*) export CGO_LDFLAGS="${CGO_LDFLAGS:+$CGO_LDFLAGS }${min}" ;;
	esac
	case " ${CGO_CFLAGS:-} " in
	*" -Wno-deprecated-declarations "*) ;;
	*) export CGO_CFLAGS="${CGO_CFLAGS:+$CGO_CFLAGS }-Wno-deprecated-declarations" ;;
	esac
	case " ${CGO_CFLAGS:-} " in
	*" -mmacosx-version-min="*) ;;
	*) export CGO_CFLAGS="${CGO_CFLAGS:+$CGO_CFLAGS }${min}" ;;
	esac
fi
exec go "$@"
