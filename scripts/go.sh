#!/bin/sh
# go wrapper: Darwin CGO flags Wails needs but `wails build` would have set.
export CGO_ENABLED=1
if [ "$(go env GOOS 2>/dev/null)" = darwin ]; then
	case " ${CGO_LDFLAGS:-} " in
	*" -framework UniformTypeIdentifiers "*) ;;
	*) export CGO_LDFLAGS="${CGO_LDFLAGS:+$CGO_LDFLAGS }-framework UniformTypeIdentifiers" ;;
	esac
	case " ${CGO_CFLAGS:-} " in
	*" -Wno-deprecated-declarations "*) ;;
	*) export CGO_CFLAGS="${CGO_CFLAGS:+$CGO_CFLAGS }-Wno-deprecated-declarations" ;;
	esac
fi
exec go "$@"
