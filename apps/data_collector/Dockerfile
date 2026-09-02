FROM golang:1.24-alpine AS build

WORKDIR /src

COPY apps/data_collector/go.mod apps/data_collector/go.sum ./apps/data_collector/
COPY domains/marketdata/go.mod ./domains/marketdata/
RUN cd apps/data_collector && GOWORK=off go mod download

COPY apps/data_collector ./apps/data_collector
COPY domains/marketdata ./domains/marketdata
RUN cd apps/data_collector && \
    GOWORK=off CGO_ENABLED=0 go build -trimpath -o /out/data-collector ./cmd/data-collector && \
    GOWORK=off CGO_ENABLED=0 go build -trimpath -o /out/schedule-data-collector ./cmd/schedule-data-collector

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/data-collector /usr/local/bin/data-collector
COPY --from=build /out/schedule-data-collector /usr/local/bin/schedule-data-collector

USER nobody
ENTRYPOINT ["/usr/local/bin/data-collector"]
