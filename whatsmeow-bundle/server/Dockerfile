# Build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

RUN go mod tidy && CGO_ENABLED=1 GOOS=linux go build -o whatsmeow-server main.go

# Runtime stage
FROM alpine:latest

RUN apk add --no-cache sqlite-libs ca-certificates tzdata ffmpeg

WORKDIR /app

COPY --from=builder /app/whatsmeow-server .

# Crear directorio para datos
RUN mkdir -p /data/sessions

ENV WHATSMEOW_DATA_DIR=/data/sessions
ENV WHATSMEOW_PORT=3100

EXPOSE 3100

CMD ["./whatsmeow-server"]
