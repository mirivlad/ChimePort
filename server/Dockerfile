FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN go mod tidy && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/chimeport ./cmd/chimeport

FROM debian:bookworm-slim
RUN groupadd --system --gid 10001 chimeport && useradd --system --uid 10001 --gid 10001 --home-dir /nonexistent chimeport \
  && mkdir -p /data && chown 10001:10001 /data
COPY --from=build /out/chimeport /usr/local/bin/chimeport
USER 10001:10001
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/chimeport"]
CMD ["serve"]
