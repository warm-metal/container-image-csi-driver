set -x
mkdir -p /etc/kubernetes /mnt/vda1/var/lib/boot2docker/etc/kubernetes
mount --bind /mnt/vda1/var/lib/boot2docker/etc/kubernetes /etc/kubernetes
mkdir -p /etc/containerd /mnt/vda1/var/lib/boot2docker/etc/containerd
mount --bind /mnt/vda1/var/lib/boot2docker/etc/containerd /etc/containerd
mkdir -p /etc/crio /mnt/vda1/var/lib/boot2docker/etc/crio
mount --bind /mnt/vda1/var/lib/boot2docker/etc/crio /etc/crio
set +x