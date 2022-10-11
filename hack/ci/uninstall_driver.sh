#!/usr/bin/env bash

source $(dirname "$0")/../lib/cluster.sh

set -e

lib::uninstall_driver

set +e
