// +build dbrequired

package api

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/sessions"
	"github.com/markbates/goth"
	"github.com/op/go-logging"

	"github.com/microscaling/microbadger/database"
	"github.com/microscaling/microbadger/queue"
)

// Setting up test database for this package
// $ psql -c 'create database microbadger_api_test;' -U postgres

func getDatabase(t *testing.T) database.PgDB {
	dbLogLevel := logging.GetLevel("mmdata")

	testdb, err := database.GetPostgres("localhost", "postgres", "microbadger_api_test", "", (dbLogLevel == logging.DEBUG))
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	return testdb
}

func emptyDatabase(db database.PgDB) {
	db.Exec("DELETE FROM tags")
	db.Exec("DELETE FROM image_versions")
	db.Exec("DELETE FROM images")
	db.Exec("DELETE FROM favourites")
	db.Exec("DELETE FROM notifications")
	db.Exec("DELETE FROM notification_messages")
	db.Exec("DELETE FROM users")
	db.Exec("DELETE FROM user_auths")
	db.Exec("DELETE from user_image_permissions")
	db.Exec("DELETE FROM user_registry_credentials")
	db.Exec("DELETE FROM sessions")
	db.Exec("SELECT setval('users_id_seq', 1, false)")
	db.Exec("SELECT setval('notifications_id_seq', 1, false)")
	db.Exec("SELECT setval('notification_messages_id_seq', 1, false)")
}

func addThings(db database.PgDB) {
	now := time.Now().UTC()

	db.Exec("INSERT INTO images (name, status, badge_count, created_at, latest, auth_token, is_private) VALUES('lizrice/childimage', 'INSPECTED', 2, $1, '10000', 'lowercase', false)", now)
	db.Exec("INSERT INTO images (name, status, badge_count, created_at, latest, auth_token, is_private, featured) VALUES('lizrice/featured', 'INSPECTED', 2, $1, '15000', 'mIxeDcAse', false, true)", now)
	db.Exec("INSERT INTO images (name, status, badge_count, created_at, latest, is_private) VALUES('myuser/private', 'INSPECTED', 2, $1, '20000', True)", now)
	db.Exec("INSERT INTO images (name, status, badge_count, created_at, latest, auth_token, is_private, featured) VALUES('microbadgertest/alpine', 'INSPECTED', 2, $1, '30000', 'mIxeDcAse', true, false)", now)

	db.Exec("INSERT INTO image_versions (image_name, labels, sha) VALUES('lizrice/childimage', '{\"org.label-schema.name\":\"childimage\"}', '10000')")
	db.Exec("INSERT INTO image_versions (image_name, labels, sha) VALUES('lizrice/featured', '{\"blah\":\"blah\"}', '15000')")
	db.Exec("INSERT INTO image_versions (image_name, labels, sha) VALUES('myuser/private', '{\"org.label-schema.name\":\"private\"}', '20000')")
	db.Exec("INSERT INTO image_versions (image_name, labels, sha) VALUES('microbadgertest/alpine', '', '30000')")
}

func addUser(db database.PgDB) {
	db.GetOrCreateUser(database.User{}, goth.User{Provider: "github", UserID: "12345", Name: "myuser", Email: "myname@myaddress.com"})
}

func limitUserNotifications(db database.PgDB) error {
	u, err := db.GetOrCreateUser(database.User{}, goth.User{Provider: "github", UserID: "12345", Name: "myuser", Email: "myname@myaddress.com"})
	if err != nil {
		return fmt.Errorf("failed to get user: %v", err)
	}

	us, err := db.GetUserSetting(u)
	if err != nil {
		return fmt.Errorf("failed to get user settings: %v", err)
	}

	us.NotificationLimit = 1
	return db.PutUserSetting(us)
}

type TestStore struct {
	session *sessions.Session
}

func NewTestStore() *TestStore {
	ts := TestStore{}
	ts.session = sessions.NewSession(&ts, "test-session-name")
	return &ts
}

func (s *TestStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return s.session, nil
}

func (s *TestStore) New(r *http.Request, name string) (*sessions.Session, error) {
	return s.session, nil
}

func (s *TestStore) Save(r *http.Request, w http.ResponseWriter, sess *sessions.Session) error {
	return nil
}

func logIn(r *http.Request) error {
	u, err := db.GetOrCreateUser(database.User{}, goth.User{Provider: "github", UserID: "12345", Name: "myuser", Email: "myname@myaddress.com"})

	session, err := sessionStore.Get(r, "test-session-name")
	session.Values["user"] = u
	session.Save(r, nil)

	return err
}

func logOut(r *http.Request) error {
	session, err := sessionStore.Get(r, "test-session-name")
	session.Values["user"] = nil
	session.Save(r, nil)

	return err
}

func TestAPIBadUrls(t *testing.T) {
	var err error

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	qs = queue.NewMockService()
	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	type test struct {
		url    string
		status int
		post   bool
	}

	var tests = []test{
		// Bad URLs
		{url: `/v2/images`, status: 404},
		{url: `/blah`, status: 404},
		{url: `/v2/images`, status: 404, post: true},
		{url: `/blah`, status: 404, post: true},

		// Bad methods
		// TODO!! This currently fails because we're not good at parsing the URLs, and we think
		// this is an auth token 'test' for library/lizrice
		// {url: `/images/lizrice/childimage`, status: 405, post: true},
	}

	for id, test := range tests {
		var res *http.Response
		if test.post {
			res, err = http.Post(ts.URL+test.url, "application/x-www-form-urlencoded", nil)
		} else {
			res, err = http.Get(ts.URL + test.url)
		}

		if err != nil {
			t.Fatalf("Failed to send request #%d (%s) %v", id, test.url, err)
		}

		if res.StatusCode != test.status {
			t.Errorf("#%d Unexpected status code: %d", id, res.StatusCode)
		}

		ct := res.Header.Get("Content-Type")
		if ct != "text/plain; charset=utf-8" {
			t.Errorf("#%d Content type is not as expected, have %s", id, ct)
		}
	}

}

func TestAPIHealthCheck(t *testing.T) {
	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/healthcheck.txt")
	if err != nil {
		t.Fatalf("Failed to send request %v", err)
	}

	body, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Errorf("Error getting body. %v", err)
	}

	if res.StatusCode != 200 {
		t.Errorf("Bad things have happened. %d", res.StatusCode)
	}

	if string(body) != "HEALTH OK" {
		t.Errorf("Body is not as expected, have %s", body)
	}

	ct := res.Header.Get("Content-Type")
	if ct != "text/plain; charset=utf-8" {
		t.Errorf("Content type is not as expected, have %s", ct)
	}

}

func TestAPIWebHook(t *testing.T) {
	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	type test struct {
		url    string
		status int
		body   string
	}

	var tests = []test{
		// Images that don't exist
		{url: `/images/blah`, status: 404, body: "Image not found"},
		{url: `/images/lizrice/blah`, status: 404, body: "Image not found"},

		// Correct image & authtoken
		{url: `/images/lizrice/childimage/lowercase`, status: 200, body: `OK`},
		{url: `/images/lizrice/featured/mIxeDcAse`, status: 200, body: `OK`},

		// Correct image, wrong authtoken
		{url: `/images/lizrice/featured/wrong`, status: 403, body: `Bad token`},
		{url: `/images/lizrice/featured/mixedcase`, status: 403, body: `Bad token`},
	}

	for id, test := range tests {
		res, err := http.Post(ts.URL+test.url, "application/x-www-form-urlencoded", nil)
		if err != nil {
			t.Fatalf("Failed to send request #%d (%s) %v", id, test.url, err)
		}

		body, err := ioutil.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Errorf("Error getting body. %v", err)
		}

		if res.StatusCode != test.status {
			t.Errorf("#%d Bad things have happened. %d", id, res.StatusCode)
		}

		if test.status == 200 {
			if string(body) != test.body {
				t.Errorf("#%d Body is not as expected, have %s", id, body)
			}
		}

		ct := res.Header.Get("Content-Type")
		if ct != "text/plain; charset=utf-8" {
			t.Errorf("#%d Content type is not as expected, have %s", id, ct)
		}
	}
}
