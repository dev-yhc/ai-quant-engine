FROM golang:1.24-alpine AS build

WORKDIR /src

COPY apps/alert-dispatcher/go.mod apps/alert-dispatcher/go.sum ./apps/alert-dispatcher/
RUN cd apps/alert-dispatcher && GOWORK=off go mod download

COPY apps/alert-dispatcher ./apps/alert-dispatcher
RUN cd apps/alert-dispatcher && GOWORK=off CGO_ENABLED=0 go build -trimpath -o /out/alert-dispatcher ./cmd/alert-dispatcher

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/alert-dispatcher /usr/local/bin/alert-dispatcher
USER nobody
ENTRYPOINT ["/usr/local/bin/alert-dispatcher"]
