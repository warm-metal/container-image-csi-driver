FROM golang:1-buster as builder

WORKDIR /go/src/git-go
COPY go.mod go.sum ./

RUN go mod download

COPY . ./

RUN CGO_ENABLED=0 go build -o csi-image-plugin ./cmd/plugin

FROM alpine:3
WORKDIR /
COPY --from=builder /go/src/git-go/csi-image-plugin ./
ENTRYPOINT ["/csi-image-plugin"]
