#!/bin/bash

# Copyright Contributors to the KubeOpenCode project

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
TMP_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "${TMP_DIR}"
}
trap "cleanup" EXIT SIGINT

# Save current state
cp -a "${SCRIPT_ROOT}/api" "${TMP_DIR}/"

# Regenerate files
"${SCRIPT_ROOT}/hack/update-deepcopy.sh"

# Check if anything changed
ret=0
diff -Naupr "${TMP_DIR}/api" "${SCRIPT_ROOT}/api" || ret=$?

if [[ $ret -ne 0 ]]; then
  echo "Generated deepcopy code is out of date. Please run 'make update-scripts'" >&2
  exit 1
fi

echo "Generated deepcopy code is up to date."
