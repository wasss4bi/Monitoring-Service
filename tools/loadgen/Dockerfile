FROM golang:1.26.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/loadgen ./cmd/loadgen && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./cmd/migration

FROM alpine:3.23
COPY --from=build /out/loadgen /loadgen
COPY --from=build /out/migrate /migrate
COPY migrations /migrations
ENTRYPOINT ["/loadgen"]
