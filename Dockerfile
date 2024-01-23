FROM docker.io/library/golang:1.22.5-alpine3.19 as builder
RUN apk add --no-cache btrfs-progs-dev lvm2-dev make build-base
WORKDIR /go/src/container-image-csi-driver
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY pkg ./pkg
COPY Makefile ./
RUN make build
RUN make install-util

FROM scratch as install-util
COPY --from=builder /go/src/container-image-csi-driver/_output/container-image-csi-driver-install /

FROM alpine:3.19
RUN apk add --no-cache btrfs-progs-dev lvm2-dev
WORKDIR /
COPY --from=builder /go/src/container-image-csi-driver/_output/container-image-csi-driver /usr/bin/
ENTRYPOINT ["container-image-csi-driver"]
