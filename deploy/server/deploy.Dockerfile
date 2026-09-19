FROM golang:1.23-bookworm AS build

WORKDIR /usr/src/app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY pkg ./pkg
COPY cmd ./cmd

RUN go build -o /beneburg ./cmd/beneburg


## Deploy
FROM gcr.io/distroless/base-debian12

WORKDIR /


COPY templates /templates
COPY assets /assets
COPY --from=build /beneburg /beneburg

EXPOSE 8080

ENTRYPOINT ["/beneburg"]
