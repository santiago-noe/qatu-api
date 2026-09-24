#!/bin/sh
set -e
/app/migrations up
exec /app/server
