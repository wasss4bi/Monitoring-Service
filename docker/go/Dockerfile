# syntax=docker/dockerfile:1.10.0@sha256:865e5dd094beca432e8c0a1d5e1c465db5f998dca4e439981029b3b81fb39ed5
FROM golang:1.26.4-alpine3.22@sha256:727cfc3c40be55cd1bc9a4a059406b28a059857e3be752aa9d09531e12c20c56
WORKDIR /app
ENV GOPRIVATE=github.com/wasss4bi/values-from-body-fetcher
RUN apk add --no-cache git
COPY docker/go/git-askpass.sh /usr/local/bin/go-private-askpass
RUN sed -i 's/\r$//' /usr/local/bin/go-private-askpass && chmod 700 /usr/local/bin/go-private-askpass
COPY ./go.mod ./go.sum ./
RUN --mount=type=secret,id=go_module_github_token,required=true \
    GIT_ASKPASS=/usr/local/bin/go-private-askpass GIT_TERMINAL_PROMPT=0 go mod download
RUN go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1
COPY . .
RUN go build -o /usr/local/bin/monitoring-service ./cmd/monitoring_service
RUN go build -o /usr/local/bin/migration ./cmd/migration
RUN go build -o /usr/local/bin/outbox-publisher ./cmd/outbox_publisher
RUN go build -o /usr/local/bin/idempotency-cleanup ./cmd/idempotency_cleanup
RUN go build -o /usr/local/bin/monitor-scheduler ./cmd/monitor_scheduler
RUN go build -o /usr/local/bin/endpoint-fetch-worker ./cmd/endpoint_fetch_worker
RUN go build -o /usr/local/bin/endpoint-fetch-html-worker ./cmd/endpoint_fetch_html_worker
RUN go build -o /usr/local/bin/endpoint-fetch-queue-migrator ./cmd/endpoint_fetch_queue_migrator
RUN go build -o /usr/local/bin/notification-worker ./cmd/notification_worker
RUN go build -o /usr/local/bin/monitoring-cleanup ./cmd/monitoring_cleanup
RUN go build -o /usr/local/bin/dependency-health-worker ./cmd/dependency_health_worker

CMD ["monitoring-service"]


