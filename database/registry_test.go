// +build dbrequired

package database

import (
	"testing"
)

func TestRegistry(t *testing.T) {
	var err error
	var db PgDB

	reg := Registry{
		ID:   "test",
		Name: "My test registry",
		Url:  "http://test",
	}

	db = getDatabase(t)

	// Before we empty the database, check that we are setting up the default Docker Hub entry correctly
	_, err = db.GetRegistry("docker")
	if err != nil {
		t.Errorf("Didn't initialize the Docker Registry entry: %v", err)
	}

	emptyDatabase(db)

	err = db.PutRegistry(&reg)
	if err != nil {
		t.Errorf("Error creating registry %v", err)
	}

	r, err := db.GetRegistry("test")
	if err != nil {
		t.Errorf("Error getting registry: %v", err)
	}

	if r.Name != reg.Name || r.ID != reg.ID {
		t.Errorf("Unexpected: %v ", r)
	}

	_, err = db.GetRegistry("notthere")
	if err == nil {
		t.Errorf("Shouldn't be able to get missing registry")
	}
}
