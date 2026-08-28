FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o owpengram-server .

FROM alpine:latest
WORKDIR /app
RUN apk --no-libc-dev add ca-certificates
COPY --from=builder /app/owpengram-server .
EXPOSE 2398
CMD ["./owpengram-server"]
  
