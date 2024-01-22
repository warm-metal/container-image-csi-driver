#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh

set -e
set -x

CONTAINERD_PREFIX="containerd://"
CRIO_PREFIX="cri-o://"

PodRestartCount=$1
TestBase=$(dirname "${BASH_SOURCE[0]}")

# Save kubelet, containerd and cri-o configuration for restarting
minikube ssh -p csi-image-test -n csi-image-test-m02 -- sudo mkdir -p /var/lib/boot2docker/etc/kubernetes
minikube ssh -p csi-image-test -n csi-image-test-m02 -- sudo cp -rpf /etc/kubernetes/* /var/lib/boot2docker/etc/kubernetes/ || true
# urcloud is a hacked minikube binary which can be downloaded from "".
urcloud cp -p csi-image-test "${TestBase}/minikube-bootlocal.sh" "csi-image-test-m02:/var/lib/boot2docker/bootlocal.sh"

manifest=${TestBase}/manual-manifests/write-check.yaml
kubectl delete --ignore-not-found -f "$manifest"
kubectl apply -f "$manifest"
jobUID=$(kubectl get --no-headers -o=custom-columns=:.metadata.uid,:.kind -f "$manifest" | grep Job | awk '{ print $1 }')
while [ "$jobUID" == "" ]; do
  sleep 1
  jobUID=$(kubectl get --no-headers -o=custom-columns=:.metadata.uid,:.kind -f "$manifest" | grep Job | awk '{ print $1 }')
done

TargetPodName=$(kubectl get po -l controller-uid=${jobUID} --no-headers -o=custom-columns=:.metadata.name)
while [ "$TargetPodName" == "" ]; do
  sleep 1
  TargetPodName=$(kubectl get po -l controller-uid=${jobUID} --no-headers -o=custom-columns=:.metadata.name)
done

kubectl wait --timeout=5m --for=condition=ready po $TargetPodName
containers=( "$(kubectl get po ${TargetPodName} -o=jsonpath='{.status.containerStatuses[*].containerID}')" )
Runtime=
pidsToBeTerminated=()
for c in ${containers[@]}; do
  if [[ $c == "${CONTAINERD_PREFIX}"* ]]; then
    Runtime=containerd
    c=${c#"${CONTAINERD_PREFIX}"}
  fi

  if [[ $c == "${CRIO_PREFIX}"* ]]; then
    Runtime=crio
    c=${c#"${CRIO_PREFIX}"}
  fi

  containerPID=$(minikube ssh -p csi-image-test -n csi-image-test-m02 -- sudo crictl inspect -o go-template --template '{{.info.pid}}' ${c} | tr -d '\r\n' | awk '{$1=$1};1')
  minikube ssh -p csi-image-test -n csi-image-test-m02 -- sudo kill -USR1 ${containerPID}
  pidsToBeTerminated+=( ${containerPID} )
done

echo "Wait 1sec for file writing"
sleep 1

minikube -p csi-image-test node stop csi-image-test-m02
urcloud -p csi-image-test node start csi-image-test-m02 --rejoin=false --preload=false
minikube -p csi-image-test image load k8s.gcr.io/pause:3.2
minikube -p csi-image-test image load k8s.gcr.io/kube-proxy:v1.20.2

nodeStatus=$(kubectl get no csi-image-test-m02 --no-headers | awk '{ print $2 }')
while [ "$nodeStatus" != "Ready" ]; do
  sleep 1
  nodeStatus=$(kubectl get no csi-image-test-m02 --no-headers | awk '{ print $2 }')
done

echo "Wait 5secs for pod status updating"
sleep 5

restartCnt=$(kubectl get po $TargetPodName -o jsonpath='{.status.containerStatuses[0].restartCount}')
while [ $restartCnt -lt ${PodRestartCount} ]; do
  sleep 1
  restartCnt=$(kubectl get po $TargetPodName -o jsonpath='{.status.containerStatuses[0].restartCount}')
done

podStatus=$( kubectl get po $TargetPodName --no-headers | awk '{ print $3 }')
while [ "$podStatus" != "Running" ]; do
    sleep 1
    podStatus=$( kubectl get po $TargetPodName --no-headers | awk '{ print $3 }')
done

kubectl wait --timeout=5m --for=condition=ready po $TargetPodName

containers=($(kubectl get po ${TargetPodName} -o=jsonpath="{.status.containerStatuses[*].containerID}"))
for c in ${containers[@]}; do
  if [[ $c == "${CONTAINERD_PREFIX}"* ]]; then
    Runtime=containerd
    c=${c#"${CONTAINERD_PREFIX}"}
  fi

  if [[ $c == "${CRIO_PREFIX}"* ]]; then
    Runtime=crio
    c=${c#"${CRIO_PREFIX}"}
  fi

  containerPID=$(minikube ssh -p csi-image-test -n csi-image-test-m02 -- sudo crictl inspect -o go-template --template '{{.info.pid}}' ${c} | tr -d '\r\n' | awk '{$1=$1};1')
  minikube ssh -p csi-image-test -n csi-image-test-m02 -- sudo kill -USR2 ${containerPID}
done

kubectl wait --timeout=5m --for=condition=complete -f "${manifest}"

set +x
set +e