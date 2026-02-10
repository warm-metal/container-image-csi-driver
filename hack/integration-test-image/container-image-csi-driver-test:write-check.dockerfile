FROM docker.io/library/golang:1.25-alpine3.23 as builder
WORKDIR /go/src/container-image-csi-driver-test
COPY write_check.go .
RUN GO111MODULE=off go build -o write_check

FROM alpine:3.23.3
# Ensure we have the latest packages including libssl and remove cache
RUN apk update && \
    apk upgrade && \
    apk add --no-cache libssl3 libcrypto3 && \
    rm -rf /var/cache/apk/*

ENV TARGET=""
WORKDIR /
COPY --from=builder /go/src/container-image-csi-driver-test/write_check .
ENTRYPOINT ["/write_check"]
