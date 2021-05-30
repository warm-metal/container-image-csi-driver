#!/usr/bin/env bash

set -e
IMG=docker.io/warmmetal/csi-image:$(git rev-parse --short HEAD)
docker build -t ${IMG} .
minikube image -p csi-image-test load ${IMG}
make install-util
set +e