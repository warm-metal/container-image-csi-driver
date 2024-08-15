#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/utils.sh

set -e

TestBase=$(dirname "${BASH_SOURCE[0]}")

lib::run_test_job "${TestBase}/manifests/ephemeral-volume.yaml"
lib::run_test_job "${TestBase}/manifests/readonly-ephemeral-volume.yaml"
lib::run_test_job "${TestBase}/manifests/pre-provisioned-pv.yaml"

echo "Restart the driver"
kubectl delete po -n kube-system -l=app.kubernetes.io/name=warm-metal-csi-driver
kubectlwait kube-system -l=app.kubernetes.io/name=warm-metal-csi-driver

lib::run_test_job "${TestBase}/manifests/ephemeral-volume.yaml"
lib::run_test_job "${TestBase}/manifests/readonly-ephemeral-volume.yaml"
lib::run_test_job "${TestBase}/manifests/pre-provisioned-pv.yaml"

set +e
