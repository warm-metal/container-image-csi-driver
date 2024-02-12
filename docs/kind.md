# Kind

This is a temporary dump which can be reconciled with local dev docs as they mature.

```
make local-kind-flush
make local-kind-load
make local-kind-install
PODNAME=$(k get po -n kube-system -l=component=nodeplugin --no-headers | cut -f 1 -d " ")
k logs -n kube-system -f $PODNAME -c csi-plugin | tee /tmp/container-image-csi-plugin.log&
k apply -f sample/ephemeral-volume-set.yaml
sleep 10
k delete -f sample/ephemeral-volume-set.yaml
sleep 1
echo "ctrl+C to kill log streaming"
fg
```