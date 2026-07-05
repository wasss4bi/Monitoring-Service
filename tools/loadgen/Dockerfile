# syntax=docker/dockerfile:1
FROM golang:1.26.4-alpine AS build
WORKDIR /src
ENV GOPRIVATE=github.com/wasss4bi/values-from-body-fetcher
RUN apk add --no-cache git
COPY docker/go/git-askpass.sh /usr/local/bin/go-private-askpass
RUN sed -i 's/\r$//' /usr/local/bin/go-private-askpass && chmod 700 /usr/local/bin/go-private-askpass
COPY go.mod go.sum ./
RUN --mount=type=secret,id=go_module_github_token,required=true \
    GIT_ASKPASS=/usr/local/bin/go-private-askpass GIT_TERMINAL_PROMPT=0 go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/loadgen ./cmd/loadgen && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./cmd/migration

FROM alpine:3.23
COPY --from=build /out/loadgen /loadgen
COPY --from=build /out/migrate /migrate
COPY migrations /migrations
ENTRYPOINT ["/loadgen"]
