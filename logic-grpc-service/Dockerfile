FROM golang:1.25-bookworm AS builder

WORKDIR /src

COPY go.work go.work.sum ./
COPY shared/go.mod shared/go.sum ./shared/
COPY logic-grpc-service/go.mod logic-grpc-service/go.sum ./logic-grpc-service/
COPY web-gin-service/go.mod web-gin-service/go.sum ./web-gin-service/

WORKDIR /src/logic-grpc-service
RUN go mod download

WORKDIR /src
COPY shared ./shared
COPY logic-grpc-service ./logic-grpc-service

WORKDIR /src/logic-grpc-service
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/logic-server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /out/logic-server /app/logic-server

EXPOSE 9002
USER nonroot:nonroot
ENTRYPOINT ["/app/logic-server"]
