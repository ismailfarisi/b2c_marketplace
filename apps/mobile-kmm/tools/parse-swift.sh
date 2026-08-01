#!/usr/bin/env bash
#
# Parse every Swift file in iosApp, without Xcode.
#
# `swiftc -parse` stops after parsing: it never resolves an import, so SwiftUI,
# UIKit, AVFoundation and the Shared framework not existing on this machine
# costs nothing. What it checks is that the file is syntactically Swift.
#
# That is a narrow guarantee and it is worth stating precisely, because the
# temptation is to read a clean run as "the iOS app builds":
#
#   caught   — unbalanced braces, a malformed generic, a bad string
#              interpolation, `guard` with no `else`, a stray comma in an
#              argument list, anything the grammar rejects
#   NOT      — a wrong type, a missing SwiftUI modifier, a misspelled Kotlin
#   caught     symbol, an argument label that does not match, `some View`
#              returning two different shapes
#
# The Kotlin symbols and argument labels are the job of tools/interopcheck.
# Between the two, what is left for a Mac is real type checking and SwiftUI
# semantics — which is the right place for it, but is no longer the *first*
# place a missing brace gets noticed.
#
# Skips cleanly with status 0 when there is no toolchain, because most machines
# touching this repository will not have one and this runs from `test-mobile`.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
app="$here/../iosApp/iosApp"

find_swiftc() {
    if command -v swiftc >/dev/null 2>&1; then
        command -v swiftc
        return
    fi
    # Windows: winget installs to a versioned directory and the compiler needs
    # the separately-installed runtime on PATH, or it exits 53 with no message.
    #
    # $USER is not set in Git Bash, and under `set -u` referencing it aborted
    # the search — which the caller then reported as "no toolchain", the one
    # outcome that looks like success. A skip that can be reached by accident
    # is worse than no skip at all.
    local local_appdata="${LOCALAPPDATA:-$HOME/AppData/Local}"
    for c in \
        "$local_appdata"/Programs/Swift/Toolchains/*/usr/bin/swiftc.exe \
        "/c/Program Files/Swift/Toolchains"/*/usr/bin/swiftc.exe; do
        if [ -x "$c" ]; then
            echo "$c"
            return
        fi
    done
}

swiftc_bin="$(find_swiftc)"
if [ -z "$swiftc_bin" ]; then
    echo "parse-swift: no Swift toolchain — skipping (this is not a failure)"
    exit 0
fi

# The Windows compiler needs the separately-installed runtime on PATH or it
# dies with "error while loading shared libraries" and no library named.
#
# Set here rather than inside find_swiftc, which runs in a command-substitution
# subshell — the export went with it and the compiler was invoked without it.
case "$swiftc_bin" in
*.exe)
    swift_root="${swiftc_bin%/Toolchains/*}"
    for rt in "$swift_root"/Runtimes/*/usr/bin; do
        [ -d "$rt" ] && export PATH="$rt:$PATH"
    done
    ;;
esac

shopt -s nullglob
files=("$app"/*.swift)
if [ ${#files[@]} -eq 0 ]; then
    echo "parse-swift: no Swift files at $app" >&2
    exit 1
fi

# Hand a Windows compiler Windows paths, explicitly.
#
# Git Bash usually rewrites POSIX paths on the way to a native binary, so this
# worked by accident — until a caller with MSYS_NO_PATHCONV=1 in the
# environment, which is a perfectly ordinary thing to set, got seventeen
# "error opening input file '\c\Users\...'" lines and a failure that looks like
# broken Swift rather than a broken script. Converting here does not care
# either way.
if command -v cygpath >/dev/null 2>&1; then
    for i in "${!files[@]}"; do
        files[$i]="$(cygpath -w "${files[$i]}")"
    done
fi

if "$swiftc_bin" -parse "${files[@]}"; then
    echo "parse-swift: ${#files[@]} files parse"
else
    echo "parse-swift: FAILED" >&2
    exit 1
fi
