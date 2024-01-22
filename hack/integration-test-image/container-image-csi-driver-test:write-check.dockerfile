FROM docker.io/library/golang:1.16-alpine as builder
WORKDIR /go/src/docker.io/warmmetal/container-image-csi-driver-test
COPY write_check.go .
RUN GO111MODULE=off go build -o write_check

FROM alpine:3
ENV TARGET=""
WORKDIR /
COPY --from=builder /go/src/docker.io/warmmetal/container-image-csi-driver-test/write_check .
ENTRYPOINT ["/write_check"]
