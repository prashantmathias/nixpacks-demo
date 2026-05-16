FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY main.go .
RUN go mod init target-app && go mod tidy && CGO_ENABLED=0 go build -o server .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server .
EXPOSE 8080
CMD ["./server"]
