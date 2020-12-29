FROM golang:1-buster

WORKDIR /go/src/git-go
COPY go.mod go.sum ./

RUN go mod download

COPY . ./

CMD ["go", "test", "-v", "./..."]
