FROM golang:1.10

WORKDIR /go/src/helloworld
COPY main.go .
RUN go get -d -v ./...
RUN go build

RUN mkdir static
COPY static/. static/.

CMD ["/go/src/helloworld/helloworld"]
