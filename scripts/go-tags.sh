#!/bin/sh
# Wails v3 Linux builds against GTK4 + WebKitGTK 6.0 (no webkit2_41 tag).
# production compiles out the inspector; desktop selects the native window path.
echo "-tags=desktop,production"
