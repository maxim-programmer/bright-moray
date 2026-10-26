FROM golang:1.27-alpine AS builder

WORKDIR /app

RUN apk --no-cache add ca-certificates git

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

FROM scratch

WORKDIR /
COPY --from=builder /app/server .
COPY --from=builder /app/migrations ./migrations

EXPOSE 8080

CMD ["/server"]
