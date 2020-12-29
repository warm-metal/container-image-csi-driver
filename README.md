# csi-driver-image

This repo contains a CSI driver for mounting images. 
The driver uses snapshot service of container runtime instead of calling CRI interfaces.
So, it doesn't start a container before mounting.
