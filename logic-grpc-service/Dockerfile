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

WORKDIR /src/logic-grpc-service
RUN for attempt in 1 2 3; do \
      go mod download && break; \
      if [ "$attempt" = "3" ]; then exit 1; fi; \
      sleep 5; \
    done

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
