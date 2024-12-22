#!/usr/bin/env bash

set -eux

go run -ldflags "-X nyiyui.ca/seekback-server/server.vcsInfo=go_run_$(git rev-parse @)" -tags 'fts5' cmd/server/main.go -tokens-path tokens.json
