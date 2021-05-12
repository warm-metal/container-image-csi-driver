#!/usr/bin/env bash

source $(dirname "$0")/../lib/cluster.sh

set -x
set -e

lib::uninstall_driver_for_containerd

set +e
set +x