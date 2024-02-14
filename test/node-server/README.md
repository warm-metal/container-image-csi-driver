## What is this?
This directory contains two files:
1. `docker-compose.yaml`: used to mount workspace on image built using `Dockerfile.containerd`
2. `Dockerfile.containerd`: used to run containerd

## Why?
This is to run container-image-csi-driver code in a container where containerd is running

## How to use?
Build the image
```shell
$ docker build . -f Dockerfile.containerd  -t <image-name>:<tag>
```
For example
```shell
$ docker build . -f Dockerfile.containerd  -t containerd-on-mac:0.6
```

Start using the image:
```
$ docker-compose up
```

For example:
```shell
$ docker-compose up
[+] Building 0.0s (0/0)                                                                                                              docker:desktop-linux
[+] Running 1/0
 ✔ Container node-server-containerd-workspace-1  Recreated                                                                                           0.0s
Attaching to node-server-containerd-workspace-1
node-server-containerd-workspace-1  | time="2023-11-08T09:03:00Z" level=warning msg="containerd config version `1` has been deprecated and will be removed in containerd v2.0, please switch to version `2`, see https://github.com/containerd/containerd/blob/main/docs/PLUGINS.md#version-header"
node-server-containerd-workspace-1  | time="2023-11-08T09:03:00.484125750Z" level=info msg="starting containerd" revision=61f9fd88f79f081d64d6fa3bb1a0dc71ec870523 version=1.6.24
node-server-containerd-workspace-1  | time="2023-11-08T09:03:00.491186708Z" level=info msg="loading plugin \"io.containerd.content.v1.content\"..." type=io.containerd.content.v1
...
```

`exec` into the docker container:
```
$ docker ps
```
For example:
```shell
$ docker ps
CONTAINER ID   IMAGE                   COMMAND                  CREATED          STATUS          PORTS                      NAMES
7769b9e621f1   containerd-on-mac:0.6   "containerd"             24 minutes ago   Up 24 minutes                              node-server-containerd-workspace-1
```

Get the `CONTAINER ID` and `exec` into it using:
```shell
$ docker exec -it <CONTAINER ID> bash
```
For example:
```shell
$ docker exec -it 7769b9e621f1 bash                                                                                    ~
root@7769b9e621f1:/go#
```
