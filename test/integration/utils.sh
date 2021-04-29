#!/usr/bin/env bash

function wait() {
  local out=$($@)
  while [ "$out" == "" ]; do
    sleep 1
    out=$($@)
  done
}

# kubectl wait doesn't exit if labels are provided instead a pod name.
# I have to write a substitution.
# kubectlwait namespace pod-selector(can be name, -l label selector, or --all)
function kubectlwait() {
  set +e
  wait kubectl get po -n $1 $2 -o=custom-columns=:metadata.name --no-headers
  local pods=$(kubectl get po -n $1 $2 -o=custom-columns=:metadata.name --no-headers)
  while IFS= read -r pod; do
    kubectl wait -n $1 --timeout=30m --for=condition=ready po $pod
  done <<< "$pods"
  set -e
}

function runTestJob() {
  local job=$1
  local manifest=$2
  echo "Start job $job: $manifest"

  kubectl delete --ignore-not-found -f "$manifest"
  kubectl apply -f "$manifest"
  kubectl wait --timeout=30m --for=condition=complete job/$job
  succeeded=$(kubectl get job/$job --no-headers -ocustom-columns=:.status.succeeded)
  [[ succeeded -eq 1 ]]
}