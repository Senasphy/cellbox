FROM golang:1.23-bookworm AS build

ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/cellbox ./cmd/cellbox

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/cellbox /cellbox
USER 0:0
EXPOSE 8080
ENTRYPOINT ["/cellbox"]
CMD ["serve", "--backend", "docker", "--listen", "0.0.0.0:8080", "--data-dir", "/var/lib/cellbox", "--allow-remote"]
