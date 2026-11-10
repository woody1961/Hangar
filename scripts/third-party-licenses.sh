#!/bin/sh
# Writes THIRD_PARTY_LICENSES.md: the licence of Go itself and of every library built into the Hangar programs
# (for macOS, Windows and Linux). Run it after changing go.mod.
set -e
cd "$(dirname "$0")/.."
OUT=THIRD_PARTY_LICENSES.md
mods=$(for os in darwin windows linux; do
  GOOS=$os CGO_ENABLED=1 go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}@{{.Version}}{{end}}{{end}}' . 2>/dev/null
done | sort -u)
{
  echo "# Third-party licences"
  echo
  echo "The Hangar programs are built with Go and include the libraries below. Each is used under its own licence,"
  echo "reproduced here in full."
  echo
  echo "## Go (the standard library and runtime)"
  echo
  echo '```'
  cat "$(go env GOROOT)/LICENSE"
  echo '```'
  for m in $mods; do
    dir=$(go mod download -json "$m" | sed -n 's/.*"Dir": "\(.*\)".*/\1/p')
    lic=$(ls "$dir" 2>/dev/null | grep -iE '^(LICEN[CS]E|COPYING|UNLICENSE)' | head -1)
    echo
    echo "## ${m%@*} (${m#*@})"
    echo
    if [ -n "$lic" ]; then
      echo '```'
      cat "$dir/$lic"
      echo '```'
    else
      echo "No licence file found in the module; see its source repository."
    fi
  done
} > "$OUT"
echo "Wrote $OUT ($(echo "$mods" | wc -l | tr -d ' ') libraries)"
