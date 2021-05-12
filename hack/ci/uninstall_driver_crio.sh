#!/usr/bin/env bash

source $(dirname "$0")/../lib/cluster.sh

set -x
set -e

lib::install_driver_for_crio

set +e
set +x