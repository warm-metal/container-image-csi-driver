## What is this?
This directory contains two files:
1. `docker-compose.yaml`: used to mount workspace on image built using `Dockerfile.containerd`
2. `Dockerfile.containerd`: used to run containerd

## Why?
This is to test `cmd/plugin/node_server_test.go` and `pkg/remoteimage/pull_test.go`

## How to run the tests?
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

**To run `TestNodePublishVolume`**:

`cd` into `/code/cmd/plugin` and run `go test -run 'TestNodePublishVolume'` (note that `TestNodePublishVolume` is a regex)
```
root@7769b9e621f1:/go# cd /code/cmd/plugin
root@7769b9e621f1:/code/cmd/plugin# go test -run 'TestNodePublishVolume'
I1108 10:27:03.784182   19936 mounter.go:45] load 0 snapshots from runtime
I1108 10:27:03.787347   19936 server.go:108] Listening for connections on address: &net.UnixAddr{Name:"//csi/csi.sock", Net:"unix"}
...
I1108 10:27:13.222601   19936 node_server_test.go:94] server was stopped
I1108 10:27:13.225907   19936 mounter.go:45] load 0 snapshots from runtime
I1108 10:27:13.235697   19936 server.go:108] Listening for connections on address: &net.UnixAddr{Name:"//csi/csi.sock", Net:"unix"}
...
PASS
ok  	github.com/warm-metal/container-image-csi-driver/cmd/plugin	46.711s
```

**To test `TestPull`**:
`cd` into `/code/pkg/remoteimage` and run `go test -run 'TestPull'` (note that `TestPull` is a regex)
```
root@cdf7ee254501:~# cd /code/pkg/remoteimage
root@cdf7ee254501:/code/pkg/remoteimage# go test -run 'TestPull'
PASS
ok  	github.com/warm-metal/container-image-csi-driver/pkg/remoteimage	2.247s
root@cdf7ee254501:/code/pkg/remoteimage#
```
