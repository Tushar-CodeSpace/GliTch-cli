FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o tui-client .

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/tui-client .
CMD ["./tui-client"]
