#!/bin/sh
# Assemble THIRD-PARTY-LICENSES for the container image from the Go module
# licenses (via go-licenses, which also reproduces Apache-2.0 NOTICE files) and
# the pre-collected frontend npm notices. Runs in the Docker build stage; the
# result is copied into the distroless runtime image.
#
# Usage: build-notices.sh <npm-notice-file> <output-file>
set -eu

NPM_NOTICES="${1:?npm notice file required}"
OUT="${2:?output file required}"
SAVE_DIR="$(mktemp -d)"

# Reproduce each dependency's license (and Apache-2.0 NOTICE) into SAVE_DIR.
# Ignore our own module: its MIT license is the project license, not a notice.
go-licenses save ./cmd/atrium \
	--save_path="$SAVE_DIR" \
	--ignore github.com/atrium-secureshare/atrium-core \
	--force

{
	echo "Atrium Core — Third-Party License Notices"
	echo "========================================="
	echo
	echo "Atrium Core is licensed under the MIT License (see /LICENSE). It bundles"
	echo "the third-party components below, each under its own license."
	echo
	echo "################################################################################"
	echo "# Go dependencies"
	echo "################################################################################"
	echo
	find "$SAVE_DIR" -type f \
		\( -iname 'LICENSE*' -o -iname 'NOTICE*' -o -iname 'COPYING*' \) |
		sort |
		while IFS= read -r f; do
			rel=${f#"$SAVE_DIR"/}
			echo "================================================================================"
			echo "$(dirname "$rel") — $(basename "$f")"
			echo "================================================================================"
			echo
			cat "$f"
			echo
		done
	echo
	echo "################################################################################"
	echo "# npm dependencies (frontend, runtime only)"
	echo "################################################################################"
	echo
	cat "$NPM_NOTICES"
} >"$OUT"

rm -rf "$SAVE_DIR"
echo "wrote $OUT"
