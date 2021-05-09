#!/usr/bin/env bash

source $(dirname "$0")/../lib/cluster.sh

set -e
lib::start_cluster_containerd $@
set +e