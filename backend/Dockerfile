FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY . .
# go.sum нужно один раз сгенерировать локально (`go mod tidy`) и закоммитить —
# тогда сборка воспроизводима, а tidy здесь ничего не изменит.
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /server .

FROM alpine:3.20

RUN apk --no-cache add ca-certificates \
    && adduser -D -u 10001 app \
    && mkdir -p /data/uploads && chown app:app /data/uploads

USER app
COPY --from=builder /server /server

ENV UPLOAD_DIR=/data/uploads
EXPOSE 8080
CMD ["/server"]
