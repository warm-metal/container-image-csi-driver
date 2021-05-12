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
  wait kubectl get po -n $@ -o=custom-columns=:metadata.name --no-headers
  local pods=$(kubectl get po -n $@ -o=custom-columns=:metadata.name,:metadata.deletionTimestamp --no-headers | grep '<none>' | awk '{ print $1 }')
  while IFS= read -r pod; do
    kubectl wait -n $1 --timeout=5m --for=condition=ready po $pod
  done <<< "$pods"
}

function lib::run_test_job() {
  local manifest=$1
  echo "Start job $(basename $manifest)"

  kubectl delete --ignore-not-found -f "$manifest"
  kubectl apply -f "$manifest"
  local job=$(kubectl get -f "$manifest" --no-headers -o=custom-columns=:metadata.name,:.kind | grep Job | awk '{ print $1 }')
  while [ "$job" == "" ]; do
    sleep 1
    job=$(kubectl get -f "$manifest" --no-headers -o=custom-columns=:metadata.name,:.kind | grep Job | awk '{ print $1 }')
  done

  kubectl wait --timeout=5m --for=condition=complete job/$job
  succeeded=$(kubectl get --no-headers -ocustom-columns=:.status.succeeded job/$job)
  [[ succeeded -eq 1 ]] && kubectl delete -f "$manifest"
}

function lib::run_failed_test_job() {
  local manifest=$1
  echo "Start job $(basename $manifest)"
  kubectl delete --ignore-not-found -f "$manifest"
  kubectl delete ev --all > /dev/null
  kubectl apply -f "$manifest"
  local jobUID=$(kubectl get --no-headers -o=custom-columns=:.metadata.uid,:.kind -f "$manifest" | grep Job | awk '{ print $1 }')
  while [ "$jobUID" == "" ]; do
    sleep 1
    jobUID=$(kubectl get --no-headers -o=custom-columns=:.metadata.uid,:.kind -f "$manifest" | grep Job | awk '{ print $1 }')
  done

  local pod=$(kubectl get po -l controller-uid=${jobUID} --no-headers -o=custom-columns=:.metadata.name)
  while [ "$pod" == "" ]; do
    sleep 1
    pod=$(kubectl get po -l controller-uid=${jobUID} --no-headers -o=custom-columns=:.metadata.name)
  done

  local evCode=$(kubectl get ev --field-selector=reason==FailedMount,involvedObject.name=${pod} --no-headers -o=custom-columns=:.message)
  while [ "$evCode" == "" ]; do
    sleep 1
    evCode=$(kubectl get ev --field-selector=reason==FailedMount,involvedObject.name=${pod} --no-headers -o=custom-columns=:.message)
  done

  [[ "${evCode}" == *"code = InvalidArgument desc = AccessMode of PV can be only ReadOnlyMany or ReadOnlyOnce" ]] && kubectl delete -f "$manifest"
}