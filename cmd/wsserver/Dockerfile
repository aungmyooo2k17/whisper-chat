# Stage 1: Build
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /wsserver ./cmd/wsserver

# Stage 2: Run
FROM scratch
COPY --from=builder /wsserver /wsserver
COPY --from=builder /app/migrations /migrations
EXPOSE 8080
ENTRYPOINT ["/wsserver"]
