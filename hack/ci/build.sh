#!/usr/bin/env bash

set -e
docker build -t docker.io/warmmetal/csi-image:$(git rev-parse --short HEAD) .
set +e