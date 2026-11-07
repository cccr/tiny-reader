#!/bin/sh
# Copy Tiny Reader to the server and (re)start it with docker-compose.
# Usage: ./deploy.sh   (HOST and DIR come from deploy.env or the environment)
set -eu

cd "$(dirname "$0")"

if [ -f deploy.env ]; then
  . ./deploy.env
fi
HOST="${HOST:?set HOST=user@host in deploy.env (see deploy.env.example)}"
DIR="${DIR:-tiny-reader}"

FILES="go.mod main.go index.html Dockerfile docker-compose.yml .env.example README.md"
if [ -f .env ]; then
  FILES="$FILES .env"
fi

ssh "$HOST" "mkdir -p $DIR"
scp $FILES "$HOST:$DIR/"

ssh "$HOST" "cd $DIR && test -f .env || { echo 'No .env on server: create $DIR/.env with ANTHROPIC_API_KEY' >&2; exit 1; }"
ssh "$HOST" "cd $DIR && docker-compose up -d --build && docker-compose ps"

echo "Open http://${HOST#*@}:3040"
