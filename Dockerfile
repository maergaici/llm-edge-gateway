# -------编译-------
FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server .

#-------运行-------



FROM alpine:3.20

WORKDIR /

COPY --from=builder /app/server /server

EXPOSE 8080

ENTRYPOINT ["/server"]
