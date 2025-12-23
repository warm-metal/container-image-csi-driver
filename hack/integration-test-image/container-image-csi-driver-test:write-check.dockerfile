FROM docker.io/library/golang:1.23-alpine3.21 as builder
WORKDIR /go/src/container-image-csi-driver-test
COPY write_check.go .
RUN GO111MODULE=off go build -o write_check

FROM alpine:3.23.0
# Ensure we have the latest packages and remove cache
RUN apk update && apk upgrade && rm -rf /var/cache/apk/*

ENV TARGET=""
WORKDIR /
COPY --from=builder /go/src/container-image-csi-driver-test/write_check .
ENTRYPOINT ["/write_check"]
