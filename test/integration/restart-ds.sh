#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/utils.sh

set -e

TestBase=$(dirname "${BASH_SOURCE[0]}")
kubectl apply -f "${TestBase}/compatible-manifests/ephemeral-volume.yaml"
kubectlwait default compatible-ephemeral-volume

kubectl apply -f "${TestBase}/compatible-manifests/pre-provisioned-pv.yaml"
kubectlwait default compatible-pre-provisioned-pv

echo "Restart the driver"
kubectl delete po -n kube-system -l=app.kubernetes.io/name=warm-metal-csi-driver
kubectlwait kube-system -l=app.kubernetes.io/name=warm-metal-csi-driver

lib::run_test_job "${TestBase}/manifests/ephemeral-volume.yaml"
lib::run_test_job "${TestBase}/manifests/readonly-ephemeral-volume.yaml"
lib::run_test_job "${TestBase}/manifests/pre-provisioned-pv.yaml"

kubectl delete -f "${TestBase}/compatible-manifests/ephemeral-volume.yaml"
kubectl delete --ignore-not-found -f "${TestBase}/compatible-manifests/pre-provisioned-pv.yaml"

set +e