FROM golang:1.24-alpine AS build

WORKDIR /src

COPY apps/valuation-engine/go.mod apps/valuation-engine/go.sum ./apps/valuation-engine/
COPY domains/valuation/go.mod ./domains/valuation/
RUN cd apps/valuation-engine && GOWORK=off go mod download

COPY apps/valuation-engine ./apps/valuation-engine
COPY domains/valuation ./domains/valuation
RUN cd apps/valuation-engine && GOWORK=off CGO_ENABLED=0 go build -trimpath -o /out/valuation-engine ./cmd/valuation-engine

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/valuation-engine /usr/local/bin/valuation-engine
USER nobody
ENTRYPOINT ["/usr/local/bin/valuation-engine"]
