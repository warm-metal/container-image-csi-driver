FROM docker.io/library/golang:1.16-alpine as builder
WORKDIR /go/src/csi-image-test
COPY write_check.go .
RUN GO111MODULE=off go build -o write_check

FROM alpine:3
ENV TARGET=""
WORKDIR /
COPY --from=builder /go/src/csi-image-test/write_check .
ENTRYPOINT ["/write_check"]
