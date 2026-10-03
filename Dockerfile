FROM golang:1.27.1 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /server ./cmd/server

FROM debian:bookworm-slim
COPY --from=build /server /server
COPY config/config.json /config/config.json
EXPOSE 8080
ENTRYPOINT ["/server", "-config", "/config/config.json"]
