#!/bin/sh
# Builds the Hangar desktop app for Linux (x86-64).
#   On Linux:       needs Go 1.25+, a C compiler and the window libraries, e.g. on Debian/Ubuntu:
#                     sudo apt install gcc pkg-config libwayland-dev libx11-dev libx11-xcb-dev libxkbcommon-x11-dev \
#                       libgles2-mesa-dev libegl1-mesa-dev libffi-dev libxcursor-dev libvulkan-dev
#   Anywhere else:  with Docker running, the same happens inside a Linux container.
# Usage: scripts/build-linux.sh [output file] [version]
set -e
cd "$(dirname "$0")/.."
OUT="${1:-release/Hangar-Linux-x64}"
VERSION="${2:-dev}"
if [ "$(uname -s)" = "Linux" ]; then
  CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$OUT" .
  exit 0
fi
mkdir -p "$(dirname "$OUT")"
docker run --rm --platform linux/amd64 -v "$PWD":/src -w /src golang:1.25-bookworm sh -c "
  apt-get update -qq && apt-get install -y -qq pkg-config libwayland-dev libx11-dev libx11-xcb-dev libxkbcommon-x11-dev \
    libgles2-mesa-dev libegl1-mesa-dev libffi-dev libxcursor-dev libvulkan-dev >/dev/null &&
  CGO_ENABLED=1 GOTOOLCHAIN=auto go build -buildvcs=false -trimpath -ldflags '-s -w -X main.version=$VERSION' -o /src/$OUT ."
