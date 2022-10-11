FROM docker.io/library/golang:1.19.2-alpine3.16 as builder

WORKDIR /go/src/csi-driver-image
COPY go.mod go.sum ./

RUN go mod download

COPY cmd ./cmd
COPY pkg ./pkg

RUN CGO_ENABLED=0 go build -o csi-image-plugin ./cmd/plugin

FROM alpine:3.16
WORKDIR /
COPY --from=builder /go/src/csi-driver-image/csi-image-plugin /usr/bin/
ENTRYPOINT ["csi-image-plugin"]
