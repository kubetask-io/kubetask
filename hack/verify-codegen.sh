#!/bin/bash

# Copyright Contributors to the CodeSweep project

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
if [ -d "${SCRIPT_ROOT}/client" ]; then
  cp -a "${SCRIPT_ROOT}/client" "${TMP_DIR}/"
fi

# Regenerate files
"${SCRIPT_ROOT}/hack/update-codegen.sh"

# Check if anything changed (only if client directory exists)
if [ -d "${SCRIPT_ROOT}/client" ] || [ -d "${TMP_DIR}/client" ]; then
  ret=0
  diff -Naupr "${TMP_DIR}/client" "${SCRIPT_ROOT}/client" || ret=$?

  if [[ $ret -ne 0 ]]; then
    echo "Generated client code is out of date. Please run 'make update-scripts'" >&2
    exit 1
  fi

  echo "Generated client code is up to date."
else
  echo "Skipping client code verification (client directory not used)"
fi
