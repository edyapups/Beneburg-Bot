FROM golang:1.23-bookworm AS build

WORKDIR /usr/src/app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY pkg ./pkg
COPY cmd ./cmd

RUN CGO_ENABLED=0 go build -o /beneburg ./cmd/beneburg


## Deploy
FROM alpine:3.20

RUN apk add --no-cache ca-certificates postgresql16-client tzdata

WORKDIR /


COPY templates /templates
COPY assets /assets
COPY --from=build /beneburg /beneburg

EXPOSE 8080

ENTRYPOINT ["/beneburg"]
