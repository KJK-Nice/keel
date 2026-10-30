#!/bin/sh
# Wails v3 Linux builds against GTK4 + WebKitGTK 6.0 (no webkit2_41 tag).
# production compiles out the inspector; desktop selects the native window path.
# private_mac_apis lets Wails clear the WKWebView background on macOS, so the
# translucent backdrop shows through; without it the webview stays opaque. It
# has no effect elsewhere.
echo "-tags=desktop,production,private_mac_apis"
