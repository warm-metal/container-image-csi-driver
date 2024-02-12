# Kind

This is a temporary dump which can be reconciled with local dev docs as they mature.

```
make local-kind-flush
make local-kind-load
make local-kind-install
sleep 1
PODNAME=$(k get po -n kube-system -l=component=nodeplugin --no-headers | cut -f 1 -d " ")
CONTAINERLOG='/tmp/container-image-csi-plugin.log'
echo "" > ${CONTAINERLOG}
sleep 1
k logs -n kube-system -f ${PODNAME} -c csi-plugin | tee ${CONTAINERLOG}&
k apply -f sample/ephemeral-volume-set.yaml
k wait --for=condition=complete --timeout=10s job/ephemeral-volume-1
k wait --for=condition=complete --timeout=1s job/ephemeral-volume-2
k wait --for=condition=complete --timeout=1s job/ephemeral-volume-3
k wait --for=condition=complete --timeout=1s job/ephemeral-volume-4
k wait --for=condition=complete --timeout=1s job/ephemeral-volume-5
sleep 1
k delete -f sample/ephemeral-volume-set.yaml
sleep 3
echo "**********************************"
echo "** ctrl+C to kill log streaming **"
echo "**********************************"
fg
```