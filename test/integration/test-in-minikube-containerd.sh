#!/usr/bin/env bash

set -e
set -x

source $(dirname "$0")/../../hack/lib/cluster.sh

echo "Testing on containerd"
lib::start_cluster_containerd ${K8S_VERSION}
lib::install_driver
source $(dirname "$0")/cases.sh

set +x
set +e