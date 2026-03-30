FROM golang:1.23-alpine AS builder

WORKDIR /src

COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download

COPY backend ./backend
RUN cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/api-server ./cmd/api-server

FROM alpine:3.20

RUN adduser -D -g '' appuser

WORKDIR /app/backend

COPY --from=builder /out/api-server /app/backend/api-server
COPY --from=builder /src/backend/migrations /app/backend/migrations

RUN chown -R appuser:appuser /app
USER appuser

EXPOSE 8080

ENTRYPOINT ["/app/backend/api-server"]
