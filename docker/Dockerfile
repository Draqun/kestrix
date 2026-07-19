# ─────────────────────────────────────────────────────────────
# MetaCI — Production Dockerfile (multi-stage)
# ─────────────────────────────────────────────────────────────

# Stage 1: Build
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /metaci \
    ./cmd/metaci

# Stage 2: Runtime (distroless)
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /metaci /metaci
COPY --from=builder /app/examples /app/examples
COPY --from=builder /app/docs /app/docs

WORKDIR /app

EXPOSE 8080

ENTRYPOINT ["/metaci"]
CMD ["serve", "--addr", ":8080", "--dir", "./examples"]
