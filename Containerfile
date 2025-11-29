FROM docker.io/golang:1.23-alpine AS builder
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY src/ .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o dist/bot .

FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/dist/bot /bot
EXPOSE 8080
ENTRYPOINT ["/bot"]