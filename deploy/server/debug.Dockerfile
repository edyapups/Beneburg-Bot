FROM golang:1.19.2-buster AS build

WORKDIR /go/src/github.com/edyapups/beneburg/

COPY . .
RUN go mod download && go mod verify
RUN go install github.com/go-delve/delve/cmd/dlv@latest

RUN go build -gcflags="all=-N -l" -o beneburg .

## Deploy
FROM gcr.io/distroless/base-debian10

WORKDIR /go/src/github.com/edyapups/beneburg/

COPY templates ./templates
COPY assets ./assets

COPY --from=build /go/src/github.com/edyapups/beneburg/beneburg ./beneburg
COPY --from=build /go/bin/dlv /dlv

EXPOSE 8080
EXPOSE 2345

ENTRYPOINT ["/dlv", "--listen=:2345", "--headless=true", "--api-version=2", "--accept-multiclient", "exec", "./beneburg"]
