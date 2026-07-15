FROM golang:1.26.5-alpine3.24

WORKDIR /go/src/git-go
COPY go.mod go.sum ./

RUN go mod download

COPY . ./

CMD ["go", "test", "-v", "./..."]
