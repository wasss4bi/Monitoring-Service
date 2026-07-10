# syntax=docker/dockerfile:1.10.0@sha256:865e5dd094beca432e8c0a1d5e1c465db5f998dca4e439981029b3b81fb39ed5
FROM golang:1.26.4-alpine3.22@sha256:727cfc3c40be55cd1bc9a4a059406b28a059857e3be752aa9d09531e12c20c56 AS build
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

FROM alpine:3.23.4@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
COPY --from=build /out/loadgen /loadgen
COPY --from=build /out/migrate /migrate
COPY migrations /migrations
ENTRYPOINT ["/loadgen"]
