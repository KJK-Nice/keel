#!/bin/sh
# Wails requires production or dev. Ubuntu 24.04+ also needs webkit2_41.
tags="desktop,production"
if [ "$(uname -s)" = "Linux" ] && pkg-config --exists webkit2gtk-4.1 2>/dev/null; then
	tags="${tags},webkit2_41"
fi
echo "-tags=${tags}"
