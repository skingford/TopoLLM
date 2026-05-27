# ---- build ----
FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/gateway ./cmd/gateway

# ---- run ----
FROM gcr.io/distroless/static-debian12
COPY --from=builder /out/gateway /gateway
COPY configs/config.example.yaml /configs/config.yaml
EXPOSE 8080
ENTRYPOINT ["/gateway", "-config", "/configs/config.yaml"]
