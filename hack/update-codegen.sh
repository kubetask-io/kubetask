#!/bin/bash

# Copyright Contributors to the KubeOpenCode project

set -o errexit
set -o nounset
set -o pipefail

# Client generation is not currently used.
# If typed clients are needed in the future, use controller-gen or code-generator.
echo "Skipping client generation (not currently used)"
