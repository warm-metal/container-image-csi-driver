FROM golang:1.27.1-alpine3.24

WORKDIR /go/src/git-go
COPY go.mod go.sum ./

RUN go mod download

COPY . ./

CMD ["go", "test", "-v", "./..."]
