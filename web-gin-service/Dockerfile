FROM golang:1.25-bookworm AS builder

WORKDIR /src

COPY go.work go.work.sum ./
COPY shared/go.mod shared/go.sum ./shared/
COPY logic-grpc-service/go.mod logic-grpc-service/go.sum ./logic-grpc-service/
COPY web-gin-service/go.mod web-gin-service/go.sum ./web-gin-service/

WORKDIR /src/web-gin-service
RUN go mod download

WORKDIR /src
COPY shared ./shared
COPY web-gin-service ./web-gin-service

WORKDIR /src/web-gin-service
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/web-server ./cmd/server

FROM gcr.io/distroless/static-debian12

WORKDIR /app
COPY --from=builder /out/web-server /app/web-server

EXPOSE 8080
ENTRYPOINT ["/app/web-server"]
