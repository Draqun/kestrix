#!/bin/bash
# PostgreSQL init script — creates the compatibility metaci_test database alongside metaci.
# Runs only on first volume initialization (when /docker-entrypoint-initdb.d/ executes).
set -e
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
	CREATE DATABASE metaci_test;
EOSQL
