FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY cloudflare.go cloudflare.go
COPY main.go main.go
COPY prometheus.go prometheus.go
COPY go.mod go.mod
COPY go.sum go.sum

RUN go get -d -v
RUN CGO_ENABLED=0 GOOS=linux go build --ldflags '-w -s -extldflags "-static"' -o cloudflare_exporter .

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /app/cloudflare_exporter cloudflare_exporter

ENTRYPOINT [ "./cloudflare_exporter" ]
