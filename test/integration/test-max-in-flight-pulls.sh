#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/utils.sh

set -e

TestBase=$(dirname "${BASH_SOURCE[0]}")

echo "Restart the driver"

kubectl delete po -l=app.kubernetes.io/name=warm-metal-csi-driver -nkube-system
kubectlwait kube-system -l=app.kubernetes.io/name=warm-metal-csi-driver

lib::run_test_job "${TestBase}/max-in-flight-pulls-manifests"

warmmetalpod="$(kubectl get po -nkube-system -l=component=nodeplugin --no-headers | awk '{print $1}')"
t1="$(kubectl logs -nkube-system $warmmetalpod csi-plugin | grep 'pull image "ubuntu:latest"' | awk '{print $2}' | xargs -I{} date -d '{}' +'%s')"
t2="$(kubectl logs -nkube-system $warmmetalpod csi-plugin | grep 'image is ready for use: "ubuntu:latest"' | awk '{print $2}' | xargs -I{} date -d '{}' +'%s')"
t3="$(kubectl logs -nkube-system $warmmetalpod csi-plugin | grep 'pull image "debian:latest"' | awk '{print $2}' | xargs -I{} date -d '{}' +'%s')"
t4="$(kubectl logs -nkube-system $warmmetalpod csi-plugin | grep 'image is ready for use: "debian:latest"' | awk '{print $2}' | xargs -I{} date -d '{}' +'%s')"

kubectl delete --ignore-not-found -f "${TestBase}/compatible-manifests/pre-provisioned-pv.yaml"

if [ "${t1}" -lt "${t2}" -a "${t2}" -le "${t3}" -a "${t3}" -lt "${t4}" ]; then
    echo 'Test succeeded'
else
    echo 'Test failed'
    exit 1
fi


set +e