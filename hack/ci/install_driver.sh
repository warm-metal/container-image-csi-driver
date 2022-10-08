#!/usr/bin/env bash

source $(dirname "$0")/../lib/cluster.sh

set -e

IMG=docker.io/warmmetal/csi-image:$(git rev-parse --short HEAD)
lib::install_driver ${IMG}

set +e
