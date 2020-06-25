FROM golang:1.13

WORKDIR /go/src/helloworld
COPY . .
RUN go get -d -v ./...
RUN go build

CMD ["/go/src/helloworld/helloworld"]
