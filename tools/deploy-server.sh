#!/usr/bin/env bash
set -euo pipefail

export MONITORING_IMAGE="${1:?image is required}"
registry_user="${2:?registry username is required}"
test -f .env || { echo 'Server .env is missing' >&2; exit 1; }
read -r registry_token
test -n "$registry_token"
export DOCKER_CONFIG
DOCKER_CONFIG="$(mktemp -d)"
trap 'rm -rf "$DOCKER_CONFIG"' EXIT
printf '%s' "$registry_token" | docker login ghcr.io -u "$registry_user" --password-stdin
unset registry_token

compose=(docker compose -f docker-compose.server.yml)
"${compose[@]}" config --quiet
"${compose[@]}" pull
"${compose[@]}" up -d --wait postgres rabbitmq redis
"${compose[@]}" stop monitoring_service outbox_publisher monitor_scheduler endpoint_fetch_worker endpoint_fetch_html_worker notification_worker monitoring_cleanup dependency_health_worker
"${compose[@]}" run --rm --no-deps migration
"${compose[@]}" up -d --wait --wait-timeout 300 --remove-orphans
