FROM golang:1.25.2-alpine AS builder
ENV GOTOOLCHAIN=auto
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV SF6_COOKIE=$SF6_COOKIE
ENV DATABASE_URL=$DATABASE_URL
ENV KAFKA_BROKER=$KAFKA_BROKER
ENV PORT=8080
RUN CGO_ENABLED=0 GOOS=linux go build -o neo-shadaloo .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
ENV SF6_COOKIE=$SF6_COOKIE
ENV DATABASE_URL=$DATABASE_URL
ENV KAFKA_BROKER=$KAFKA_BROKER
ENV PORT=8080
WORKDIR /app
COPY --from=builder /app/neo-shadaloo .
EXPOSE 8080
CMD ["./neo-shadaloo"]
