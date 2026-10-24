#!/bin/sh
set -eu
case "$1" in
  *Username*) printf '%s\n' 'x-access-token' ;;
  *Password*) cat /run/secrets/go_module_github_token ;;
  *) exit 1 ;;
esac
