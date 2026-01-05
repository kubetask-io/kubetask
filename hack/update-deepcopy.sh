#!/bin/bash

# Copyright Contributors to the KubeOpenCode project

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE}")/..
cd "${SCRIPT_ROOT}"

# Use controller-gen for deepcopy generation (simpler and more compatible)
LOCALBIN="${SCRIPT_ROOT}/bin"
CONTROLLER_GEN="${LOCALBIN}/controller-gen"

# Ensure controller-gen is installed
if [[ ! -x "${CONTROLLER_GEN}" ]]; then
    echo "Installing controller-gen..."
    mkdir -p "${LOCALBIN}"
    GOBIN="${LOCALBIN}" go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.5
fi

echo "Generating deepcopy functions..."
"${CONTROLLER_GEN}" object:headerFile="${SCRIPT_ROOT}/hack/boilerplate.txt" paths="./api/..."

echo "Deepcopy generation complete"
