FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o telesrv ./cmd/telesrv

FROM alpine:latest
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/telesrv .
EXPOSE 2398
CMD ["./telesrv"]
