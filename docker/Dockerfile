# ─────────────────────────────────────────────────────────────
# MetaCI — Production Dockerfile (multi-stage)
# ─────────────────────────────────────────────────────────────

# Stage 1: Build backend
FROM golang:1.25-alpine AS backend

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /metaci \
    ./cmd/metaci

# Stage 2: Build frontend
FROM node:22-alpine AS frontend

WORKDIR /app
COPY ui/package.json ui/package-lock.json* ./
RUN npm install --silent
COPY ui/ ./
RUN npx vite build

# Stage 3: Runtime
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=backend /metaci /metaci
COPY --from=frontend /app/dist /app/ui/dist
COPY ci /app/ci
COPY docs /app/docs

WORKDIR /app

EXPOSE 8080

ENTRYPOINT ["/metaci"]
CMD ["serve", "--addr", ":8080", "--dir", "./ci", "--static", "./ui/dist"]
