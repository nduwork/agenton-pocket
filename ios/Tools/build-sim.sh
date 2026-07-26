#!/usr/bin/env bash
# Compile the iOS app for the Simulator, unsigned. This is the iOS equivalent of
# `go test ./...` — the check that says the Swift still builds. It produces a
# .app in DerivedData; it does not install or launch anything.
#
#   ios/Tools/build-sim.sh              # quiet: errors and warnings only
#   ios/Tools/build-sim.sh -verbose     # extra args pass through to xcodebuild
#
# The .xcodeproj is generated from project.yml (source of truth) and is not
# committed. This regenerates it every build via xcodegen (brew install xcodegen).
set -euo pipefail

# A machine with only the Command Line Tools selected has no iOS SDK; point at
# the full Xcode unless the caller already chose one.
export DEVELOPER_DIR="${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}"

cd "$(dirname "$0")/.."

# project.yml is the source of truth; regenerate Agenton.xcodeproj from it.
xcodegen generate

# -quiet prints warnings and errors only — and also swallows "BUILD SUCCEEDED",
# hence the closing line (set -e means we only reach it on success).
xcodebuild \
  -project Agenton.xcodeproj \
  -scheme Agenton \
  -destination 'generic/platform=iOS Simulator' \
  -quiet \
  build CODE_SIGNING_ALLOWED=NO "$@"

echo "iOS simulator build OK"
