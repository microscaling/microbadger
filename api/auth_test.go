// +build dbrequired

package api

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/microscaling/microbadger/database"
)

func TestMe(t *testing.T) {
	var err error
	var res *http.Response
	var req *http.Request
	var user database.User

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)
	addUser(db)
	sessionStore = NewTestStore()

	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	// If not logged in, /v1/me should return nothing
	res, err = http.Get(ts.URL + "/v1/me")
	if err != nil {
		t.Fatalf("Failed to send request")
	}

	body, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Errorf("Error getting body. %v", err)
	}

	if res.StatusCode != 200 {
		t.Errorf("Failed with status code %d", res.StatusCode)
	}

	if string(body) != "{}" {
		t.Errorf("Body unexpectedly not empty: %s", body)
	}

	// Now try again but this time log in
	req, err = http.NewRequest("GET", ts.URL+"/v1/me", nil)
	logIn(req)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request %v", err)
	}

	body, err = ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Errorf("Error getting body. %v", err)
	}

	err = json.Unmarshal(body, &user)
	if err != nil {
		t.Errorf("Error unmarshalling user. %v", err)
	}

	if (user.Email != "myname@myaddress.com") || (user.Name != "myuser") {
		t.Errorf("Didn't get the user we expected %#v", user)
	}

	// Now log out again
	req, err = http.NewRequest("GET", ts.URL+"/v1/me", nil)
	logOut(req)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request %v", err)
	}

	body, err = ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Errorf("Error getting body. %v", err)
	}

	if res.StatusCode != 200 {
		t.Errorf("Failed with status code %d", res.StatusCode)
	}

	if string(body) != "{}" {
		t.Errorf("Body unexpectedly not empty: %s", body)
	}
}
