#!/usr/bin/env bash

set -x

source $(dirname "$0")/../../hack/lib/cluster.sh

echo "Testing on docker"
lib::start_cluster_docker

set -e

lib::install_driver
source $(dirname "$0")/cases.sh

set +e
set +x