#!/bin/bash
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" <<-EOSQL
    CREATE USER microbadger;
    ALTER USER microbadger WITH PASSWORD 'microbadger';
    CREATE DATABASE microbadger;
    GRANT ALL PRIVILEGES ON DATABASE microbadger TO microbadger;
EOSQL