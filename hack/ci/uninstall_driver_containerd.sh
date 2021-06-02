#!/usr/bin/env bash

source $(dirname "$0")/../lib/cluster.sh

set -x
set -e

lib::uninstall_driver

set +e
set +x