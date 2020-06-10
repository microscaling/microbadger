// +build dbrequired

package database

import (
	"testing"
	"time"

	logging "github.com/op/go-logging"
)

// Setting up test database for this package
// $ psql -c 'create database microbadger_database_test;' -U postgres

func getDatabase(t *testing.T) PgDB {
	dbLogLevel := logging.GetLevel("mmdata")

	testdb, err := GetPostgres("localhost", "postgres", "microbadger_database_test", "", (dbLogLevel == logging.DEBUG))
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	return testdb
}

func emptyDatabase(db PgDB) {
	db.Exec("DELETE FROM tags")
	db.Exec("DELETE FROM image_versions")
	db.Exec("DELETE FROM images")
	db.Exec("DELETE FROM favourites")
	db.Exec("DELETE FROM notification_messages")
	db.Exec("SELECT setval('notifications_id_seq', 1, false)")
	db.Exec("DELETE FROM notifications")
	db.Exec("DELETE FROM users")
	db.Exec("SELECT setval('users_id_seq', 1, false)")
	db.Exec("DELETE from user_auths")
	db.Exec("DELETE from user_image_permissions")
	db.Exec("DELETE from user_registry_credentials")
	db.Exec("DELETE from user_settings")
	db.Exec("DELETE FROM sessions")
	db.Exec("DELETE FROM registries WHERE id <> 'docker'") // don't delete the pre-installed Docker registry
}

func addThings(db PgDB) {
	now := time.Now().UTC()

	db.Exec("INSERT INTO images (name, status, badge_count, created_at, auth_token, pull_count, latest) VALUES('lizrice/childimage', 'INSPECTED', 2, $1, 'lowercase', 10, '10000')", now)
	// We wouldn't expect to have any image versions yet for an image in SITEMAP status
	db.Exec("INSERT INTO images (name, status, badge_count, created_at, auth_token, pull_count, latest) VALUES('public/sitemap', 'SITEMAP', 2, $1, 'lowercase', 10, '12000')", now)
	db.Exec("INSERT INTO images (name, status, badge_count, created_at, featured, auth_token, pull_count, latest) VALUES('lizrice/featured', 'INSPECTED', 2, $1, True, 'mIxeDcAse', 1000, '15000')", now)
	db.Exec("INSERT INTO images (name, status, badge_count, created_at, featured, auth_token, pull_count, latest, is_private) VALUES('myuser/private', 'INSPECTED', 2, $1, True, 'mIxeDcAse', 1000, '20000', True)", now)

	db.Exec("INSERT INTO image_versions (image_name, labels, sha) VALUES('lizrice/childimage', 'org.label-schema.name=childimage', '10000')")
	db.Exec("INSERT INTO image_versions (image_name, sha) VALUES('lizrice/featured', '15000')")
	db.Exec("INSERT INTO image_versions (image_name, labels, sha) VALUES('myuser/private', 'org.label-schema.name=private', '20000')")
}
