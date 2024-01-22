#!/usr/bin/env bash

source $(dirname "$0")/../lib/cluster.sh

set -e
echo "Install a private registry"
lib::install_private_registry
docker pull docker.io/warmmetal/container-image-csi-driver-test:simple-fs
docker tag docker.io/warmmetal/container-image-csi-driver-test:simple-fs localhost:5000/warmmetal/container-image-csi-driver-test:simple-fs
docker login -u warmmetal -p warmmetal localhost:5000
docker push localhost:5000/warmmetal/container-image-csi-driver-test:simple-fs
set +e
