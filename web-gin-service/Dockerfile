FROM golang:1.25-bookworm AS builder

ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=sum.golang.google.cn

ENV GOPROXY=${GOPROXY} \
    GOSUMDB=${GOSUMDB}

WORKDIR /src

COPY go.work go.work.sum ./
COPY shared/go.mod shared/go.sum ./shared/
COPY logic-grpc-service/go.mod logic-grpc-service/go.sum ./logic-grpc-service/
COPY web-gin-service/go.mod web-gin-service/go.sum ./web-gin-service/

WORKDIR /src/web-gin-service
RUN for attempt in 1 2 3; do \
      go mod download && break; \
      if [ "$attempt" = "3" ]; then exit 1; fi; \
      sleep 5; \
    done

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
