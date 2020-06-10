package main

import (
	"testing"

	"github.com/microscaling/microbadger/database"
)

func getTestDB(t *testing.T) database.PgDB {
	host := "microbadger-dev.c5iapezpnud7.us-east-1.rds.amazonaws.com"
	user := "microbadger"
	password := "cupremotebox"
	dbname := "microbadger"
	db, err := database.GetPostgres(host, user, dbname, password, false)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	return db
}
