FROM docker.io/library/golang:1.16

WORKDIR /go/src/container-image-csi-driver
COPY go.mod go.sum ./

RUN go mod download

COPY sanity_test.go .

CMD go test -v ./... -ginkgo.skip='.*\[Controller\s+Server\].*'
