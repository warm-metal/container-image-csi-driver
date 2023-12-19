#!/usr/bin/env bash

set -e
set -x
VERSION=$(git rev-parse --short HEAD)
IMG=docker.io/warmmetal/csi-image:${VERSION}
BUILDER=$(docker buildx ls | grep ci-builderx || true)
[ "${BUILDER}" != "" ] || docker buildx create \
    --name ci-builderx --driver docker-container \
    --bootstrap \
    --driver-opt image=moby/buildkit:master,network=host
docker buildx use ci-builderx
docker buildx build -t ${IMG} -o "type=oci,dest=csi-image.tar" .
kind load image-archive csi-image.tar -n kind-${GITHUB_RUN_ID}
docker buildx build --target install-util -o "type=local,dest=_output/" .
set +e