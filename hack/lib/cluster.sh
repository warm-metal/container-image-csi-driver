#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/utils.sh

function lib::start_cluster_containerd() {
  local version=${1:-stable}
  minikube start -p container-image-csi-driver-test \
    --kubernetes-version=${version} \
    --container-runtime=containerd \
    --insecure-registry=localhost:31000
}

function lib::start_cluster_crio() {
  local version=${1:-stable}
  minikube start -p container-image-csi-driver-test \
    --kubernetes-version=${version} \
    --container-runtime=cri-o \
    --insecure-registry=localhost:31000
}

function lib::start_cluster_docker() {
  local version=${1:-stable}
  minikube start -p container-image-csi-driver-test \
    --kubernetes-version=${version} \
    --container-runtime=docker \
    --insecure-registry=localhost:31000

  kubectl apply -f $(dirname "${BASH_SOURCE[0]}")/kube-systemd.yaml
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

function lib::gen_manifests() {
  local secret=$1
  local disableCache=$2
  docker cp _output/container-image-csi-driver-install kind-${GITHUB_RUN_ID}-control-plane:/usr/bin/container-image-csi-driver-install
  if [[ "${secret}" != "" ]]; then
    if [[ "${disableCache}" != "" ]]; then
      docker exec kind-${GITHUB_RUN_ID}-control-plane \
        container-image-csi-driver-install \
        --enable-daemon-image-credential-cache=false \
        --pull-image-secret-for-daemonset=${secret} 2>/dev/null
    else
      docker exec kind-${GITHUB_RUN_ID}-control-plane \
        container-image-csi-driver-install --pull-image-secret-for-daemonset=${secret} 2>/dev/null
    fi
  else
    docker exec kind-${GITHUB_RUN_ID}-control-plane container-image-csi-driver-install 2>/dev/null
  fi
}

function lib::uninstall_driver() {
  local manifest=$(lib::gen_manifests)
  echo "${manifest}" | kubectl delete -f -
}

function lib::install_driver_from_manifest_file() {
  local manifest=$1
  kubectl delete --ignore-not-found -f ${manifest}
  kubectl apply -f ${manifest}
}

function lib::install_driver() {
  local image=$1
  local manifest=$(lib::gen_manifests $2)
  echo "${manifest}" | kubectl delete --ignore-not-found -f -
  if [ "${image}" != "" ]; then
    echo "${manifest}" | sed "s|image: docker.io/warmmetal/container-image-csi-driver.*|image: ${image}|" | kubectl apply -f -
  else
    echo "${manifest}" | kubectl apply -f -
  fi
  kubectlwait kube-system -l=app=container-image-csi-driver
}

function lib::install_private_registry() {
  docker run -d \
    -p 127.0.0.1:5000:5000 \
    --restart=always \
    --name private-registry \
    --mount type=bind,source=$(realpath $(dirname "${BASH_SOURCE[0]}")/htpasswd),target=/auth/htpasswd \
    -e "REGISTRY_AUTH=htpasswd" \
    -e "REGISTRY_AUTH_HTPASSWD_REALM=Registry Realm" \
    -e REGISTRY_AUTH_HTPASSWD_PATH=/auth/htpasswd \
    registry:2

  if [ "$(docker inspect -f='{{json .NetworkSettings.Networks.kind}}' 'private-registry')" = 'null' ]; then
    docker network connect "kind" "private-registry"
  fi

  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "private-registry:5000"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF
}
