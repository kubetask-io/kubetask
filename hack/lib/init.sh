#!/bin/bash

# Copyright Contributors to the KubeOpenCode project

set -o errexit
set -o nounset
set -o pipefail

# The root of the build/dist directory
KUBEOPENCODE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
