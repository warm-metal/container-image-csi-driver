#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/utils.sh

function lib::start_cluster_containerd() {
  local version=${1:-stable}
  minikube start -p csi-image-test \
    --kubernetes-version=${version} \
    --container-runtime=containerd \
    --insecure-registry=localhost:31000
}

function lib::start_cluster_docker() {
  minikube start -p csi-image-test \
    --container-runtime=docker \
    --insecure-registry=localhost:31000

  kubectl apply -f https://raw.githubusercontent.com/warm-metal/kube-systemd/master/config/samples/install.yaml
  kubectlwait kube-systemd-system -l=control-plane=controller-manager

  kubectl apply -f $(dirname "${BASH_SOURCE[0]}")/kube-systemd-containerd.yaml
  kubectlwait kube-system

  executed=$(kubectl get unit systemd-containerd.service -o custom-columns=:status.execTimestamp --no-headers)
  while [ $? -ne 0 ] || [ "$executed" == "" ]; do
    sleep 1
    executed=$(kubectl get unit systemd-containerd.service -o custom-columns=:status.execTimestamp --no-headers)
  done

  error=$(kubectl get unit systemd-containerd.service -o custom-columns=:status.error --no-headers)
  if [ "${error}" != "<none>" ]; then
    echo "${error}"
    exit 1
  fi
}

function lib::install_driver() {
  local image=$1
  kubectl delete --ignore-not-found -f $(dirname "${BASH_SOURCE[0]}")/../../install/cri-containerd.yaml
  if [ "${image}" != "" ]; then
    sed "s|image: docker.io/warmmetal/csi-image.*|image: ${image}|" $(dirname "${BASH_SOURCE[0]}")/../../install/cri-containerd.yaml | kubectl apply -f -
  else
    kubectl apply -f $(dirname "${BASH_SOURCE[0]}")/../../install/cri-containerd.yaml
  fi
  kubectlwait kube-system -l=app=csi-image-warm-metal
}