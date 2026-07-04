# syntax=docker/dockerfile:1
FROM golang:1.26-alpine
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


