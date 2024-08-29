#!/usr/bin/env bash

set -e
set -x
VERSION=$(git rev-parse --short HEAD)
IMG=docker.io/warmmetal/container-image-csi-driver:${VERSION}
BUILDER=$(docker buildx ls | grep ci-builderx || true)

if [ "${BUILDER}" != "" ]; then
    docker buildx rm ci-builderx
fi

docker buildx create \
    --name ci-builderx --driver docker-container \
    --bootstrap \
    --driver-opt image=moby/buildkit:master,network=host

docker buildx use ci-builderx
docker buildx build -t ${IMG} -o "type=oci,dest=container-image-csi-driver.tar" .
kind load image-archive container-image-csi-driver.tar -n kind-${GITHUB_RUN_ID}
docker buildx build --target install-util -o "type=local,dest=_output/" .

# Cleanup builder to avoid caching issues
docker buildx rm ci-builderx
set +e
