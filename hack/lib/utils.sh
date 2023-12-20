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
    trap "kubectl -n $1 describe po $pod; docker exec kind-${GITHUB_RUN_ID}-control-plane crictl images" ERR
    kubectl wait -n $1 --timeout=3m --for=condition=ready po $pod
  done <<< "$pods"
}

function lib::run_test_job() {
  local manifest=$1
  echo "Start job $(basename $manifest)"

  local total_failed=0
  kubectl delete --ignore-not-found -f "$manifest"
  kubectl apply -f "$manifest"
  jobs="$(kubectl get -f "$manifest" --no-headers -o=custom-columns=:metadata.name,:.kind | grep Job | awk '{ print $1 }')"
  while [ "$jobs" == "" ]; do
    sleep 1
    jobs="$(kubectl get -f "$manifest" --no-headers -o=custom-columns=:metadata.name,:.kind | grep Job | awk '{ print $1 }')"
  done


  while read job
  do 
    kubectl wait --timeout=5m --for=condition=complete job/$job
    succeeded=$(kubectl get --no-headers -ocustom-columns=:.status.succeeded job/$job)

    if [[ "${succeeded}" != "1" ]]; then
        echo "Test failed for job/$job"
        total_failed=$(($total_failed+1))
    fi
  done <<< "$jobs"  # this is done because `<code> | while read job` creates a subshell 
  # because of which increment in `total_failed` is never reflected outside the loop 
  # more info: https://unix.stackexchange.com/questions/402750/modify-global-variable-in-while-loop

  [[ total_failed -eq 0 ]] && kubectl delete -f "$manifest"
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