#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/utils.sh

set -e

TestBase=$(dirname "${BASH_SOURCE[0]}")
kubectl apply -f "${TestBase}/metrics-manifests/error-ephemeral-volume.yaml"
lib::run_test_job "${TestBase}/metrics-manifests/no-error-ephemeral-volume.yaml"


ip="$(kubectl get po -n kube-system -l=component=nodeplugin -ojsonpath='{.items[*].status.podIP}')"
cat "${TestBase}/metrics-manifests/test.yaml" | sed "s|%IP|$ip|g" > "${TestBase}/metrics-manifests/rendered-test.yaml"

lib::run_test_job "${TestBase}/metrics-manifests/rendered-test.yaml"

kubectl delete -f "${TestBase}/metrics-manifests/error-ephemeral-volume.yaml"

set +e