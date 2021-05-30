FROM docker.io/library/golang:1.16-alpine3.13 as builder

WORKDIR /go/src/csi-driver-image
COPY go.mod go.sum ./

RUN go mod download

COPY cmd ./cmd
COPY pkg ./pkg

RUN CGO_ENABLED=0 go build -o csi-image-plugin ./cmd/plugin

FROM alpine:3.13
WORKDIR /
COPY --from=builder /go/src/csi-driver-image/csi-image-plugin /usr/bin/
ENTRYPOINT ["csi-image-plugin"]
