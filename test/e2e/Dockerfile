FROM docker.io/library/golang:1.16

WORKDIR /go/src/csi-driver-image
COPY go.mod go.sum ./

RUN go mod download

RUN curl -skL https://dl.k8s.io/v1.20.5/kubernetes-client-linux-amd64.tar.gz | \
    tar -C /usr/bin --strip-components 3 -zxf - kubernetes/client/bin/kubectl
COPY id_rsa* /root/.ssh/

COPY *.go .

ENTRYPOINT go run .