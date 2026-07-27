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
COPY llm-gateway-service/go.mod llm-gateway-service/go.sum* ./llm-gateway-service/

WORKDIR /src/llm-gateway-service
RUN for attempt in 1 2 3; do \
      go mod download && break; \
      if [ "$attempt" = "3" ]; then exit 1; fi; \
      sleep 5; \
    done

WORKDIR /src
COPY llm-gateway-service ./llm-gateway-service

WORKDIR /src/llm-gateway-service
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/llm-gateway ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /out/llm-gateway /app/llm-gateway

EXPOSE 8090
USER nonroot:nonroot
ENTRYPOINT ["/app/llm-gateway"]
