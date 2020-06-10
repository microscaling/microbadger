// +build dbrequired

package api

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/microscaling/microbadger/encryption"
	"github.com/microscaling/microbadger/hub"
	"github.com/microscaling/microbadger/queue"
	"github.com/microscaling/microbadger/registry"
)

type apiTestCase struct {
	name     string
	url      string
	method   string
	status   int
	body     string
	logIn    bool
	postbody string
}

// ApiTestCall sends the request for a test case and makes sure the result matches what we expect
// TODO!! Move to api_test and use it for a lot more testcases!
func apiTestCall(t *testing.T, ts *httptest.Server, tc apiTestCase) {
	var err error
	var res *http.Response
	var req *http.Request

	switch tc.method {
	case "POST":
		req, err = http.NewRequest(tc.method, ts.URL+tc.url, bytes.NewBuffer([]byte(tc.postbody)))
	case "PUT":
		if tc.postbody != "" {
			req, err = http.NewRequest(tc.method, ts.URL+tc.url, bytes.NewBuffer([]byte(tc.postbody)))
		} else {
			req, err = http.NewRequest(tc.method, ts.URL+tc.url, nil)
		}
	case "GET", "DELETE":
		req, err = http.NewRequest(tc.method, ts.URL+tc.url, nil)
	}

	if err != nil {
		t.Fatalf("Failed to make request #%s (%s) %v", tc.name, tc.url, err)
	}

	if tc.logIn {
		err = logIn(req)
	} else {
		err = logOut(req)
	}

	req.Header.Add("Origin", "http://mydomain")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request #%s (%s) %v", tc.name, tc.url, err)
	}

	body, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Errorf("Error getting body. %v", err)
	}

	if res.StatusCode != tc.status {
		t.Errorf("#%s Status code not as expected, have %d expect %d from %s to %s", tc.name, res.StatusCode, tc.status, tc.method, tc.url)
		if res.StatusCode == 301 {
			loc, _ := res.Location()
			t.Errorf("Redirect to %v", loc)
		}
	}

	if (res.StatusCode != 204) && (res.StatusCode != 202) && strings.TrimSpace(string(body)) != strings.TrimSpace(tc.body) {
		t.Errorf("#%s Body is not as expected, have %s expected %s", tc.name, body, tc.body)
	}

	if (res.StatusCode == 204) && len(body) > 0 {
		t.Errorf("#%s Unexpected body %s", tc.name, body)
	}

	ct := res.Header.Get("Content-Type")
	if tc.status == 200 && ct != "application/javascript" {
		t.Errorf("#%s Content type is not as expected, have %s", tc.name, ct)
	}

	cors := res.Header.Get("Access-Control-Allow-Credentials")
	if tc.status == 200 && cors == "" {
		t.Error("Unexpected Access-Control")
		log.Debugf("%#v", res.Header)
	}
}

func mockHub(t *testing.T) (transport *http.Transport, server *httptest.Server) {
	// Server that fakes responses from Docker Hub
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// fmt.Printf("Request %s\n", r.URL.String())
		switch r.URL.String() {
		case "http://fakehub/v2/users/login/":
			body, err := ioutil.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("Couldn't read from test data")
			}

			if strings.Contains(string(body), "incorrect") {
				// fmt.Println("Password is, literally, incorrect")
				w.WriteHeader(422) // Not sure this is exactly what Docker Hub returns but it doesn't matter so long as it's a failure
			} else {
				// fmt.Println("We say this password is OK")
				fmt.Fprintln(w, `{"token": "abctokenabc"}`)
			}
		case "http://fakehub/v2/repositories/namespaces/":
			fmt.Fprintf(w, `{"namespaces":["microbadgertest"]}`)

		case "http://fakehub/v2/user/orgs/?page_size=250": // Match large page size used by Docker Hub UI
			fmt.Fprintf(w, `{"count": 1, "results": [{"orgname": "microscaling"}]}`)

		case "http://fakehub/v2/repositories/microbadgertest/?page=1":
			fmt.Fprintf(w, `{"count":2, "results":[{"name":"alpine","namespace":"microbadgertest","is_private":true},{"name":"busybox","namespace":"microbadgertest", "is_private":false}]}`)

		case "http://fakehub/v2/repositories/empty/?page=1":
			fmt.Println("Empty")
			fmt.Fprintf(w, `{"count":0, "results":[]}`)

		case "http://fakehub/v2/repositories/notfound/?page=1",
			"http://fakereg/v2/rossf7/windtunnel/tags/list",
			"http://fakeauth/token?service=fakeservice&scope=repository:myuser/private:pull",
			"http://fakeauth/token?service=fakeservice&scope=repository:rossf7/windtunnel:pull":
			// TODO!! Is this the correct response from Docker Hub?
			w.WriteHeader(404)

		case "http://fakeauth/token?service=fakeservice&scope=repository:microbadgertest/alpine:pull":
			fmt.Fprintln(w, `{"token": "abctokenabc"}`)

		case "http://fakereg/v2/myuser/private/tags/list",
			"http://fakereg/v2/microbadgertest/alpine/tags/list":
			fmt.Fprintln(w, `{"tags":["latest"]}`)
		default:
			t.Errorf("Unexpected request to %s", r.URL.String())
		}
	}))

	// Make a transport that reroutes all traffic to the test server
	transport = &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			return url.Parse(server.URL)
		},
	}

	return
}

func TestFavourites(t *testing.T) {
	os.Setenv("MB_CORS_ORIGIN", "http://mydomain")

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)
	addUser(db)
	sessionStore = NewTestStore()

	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	var tests = []apiTestCase{
		// Favourites - Logged out user
		{name: "lo", url: `/v1/favourites/`, method: "GET", status: 401, logIn: false},
		{name: "lo", url: `/v1/favourites`, method: "GET", status: 401, logIn: false},
		{name: "lo", url: `/v1/favourites/lizrice/childimage`, method: "GET", status: 401, logIn: false},
		{name: "lo", url: `/v1/favourites/lizrice/childimage`, method: "POST", status: 401, logIn: false},
		{name: "lo", url: `/v1/favourites/`, method: "DELETE", status: 401, logIn: false},
		{name: "lo", url: `/v1/favourites`, method: "DELETE", status: 401, logIn: false},
		{name: "lo", url: `/v1/favourites/lizrice/childimage`, method: "DELETE", status: 401, logIn: false},
		{name: "lo", url: `/v1/favourites/lizrice/childimage`, method: "PUT", status: 401, logIn: false},
		// Favourites - Logged in user. From here on, sequence is important!
		{name: "li-0", url: `/v1/favourites/`, method: "GET", status: 200, body: `{}`, logIn: true},
		{name: "li-1", url: `/v1/favourites/lizrice/childimage`, method: "GET", status: 200, body: `{"IsFavourite":false}`, logIn: true},
		{name: "li-2", url: `/v1/favourites/lizrice/childimage`, method: "POST", body: `{"IsFavourite":true}`, status: 200, logIn: true},
		{name: "li-3", url: `/v1/favourites/`, method: "GET", status: 200, body: `{"Images":["lizrice/childimage"]}`, logIn: true},
		{name: "li-4", url: `/v1/favourites/lizrice/childimage`, method: "GET", status: 200, body: `{"IsFavourite":true}`, logIn: true},
		// Must have both org and repo
		{name: "li-fail", url: `/v1/favourites/lizrice`, method: "GET", status: 404, body: `404 page not found`, logIn: true},
		// Doesn't matter if we duplicate a post
		{name: "li-dupe", url: `/v1/favourites/lizrice/childimage`, method: "POST", body: `{"IsFavourite":true}`, status: 200, logIn: true},
		{name: "li-dupe", url: `/v1/favourites/`, method: "GET", status: 200, body: `{"Images":["lizrice/childimage"]}`, logIn: true},
		// Check we can delete
		{name: "li-del", url: `/v1/favourites/lizrice/childimage`, method: "DELETE", body: `{"IsFavourite":false}`, status: 200, logIn: true},
		{name: "li-del", url: `/v1/favourites/lizrice`, method: "DELETE", status: 404, body: `404 page not found`, logIn: true},
		{name: "li-del", url: `/v1/favourites/`, method: "GET", status: 200, body: `{}`, logIn: true},
		// Can't delete a second time
		{name: "li-del", url: `/v1/favourites/lizrice/childimage`, method: "DELETE", status: 404, body: `404 page not found`, logIn: true},
		// Can only DELETE one at a time
		{name: "li-del", url: `/v1/favourites/`, method: "DELETE", status: 405, body: `405 method not allowed`, logIn: true},
		// Can't favourite a repo that doesn't exist in the database
		{name: "li-badimage", url: `/v1/favourites/org/anotherimage`, method: "POST", status: 404, body: `404 page not found`, logIn: true},
		{name: "li-badimage", url: `/v1/favourites/org/anotherimage`, method: "GET", status: 404, body: `404 page not found`, logIn: true},
		{name: "li-badimage", url: `/v1/favourites/org/anotherimage`, method: "DELETE", status: 404, body: `404 page not found`, logIn: true},
		// PUT not supported for now
		{name: "li-put-0", url: `/v1/favourites/lizrice/childimage`, method: "PUT", status: 405, body: `405 method not allowed`, logIn: true},
		{name: "li-put-1", url: `/v1/favourites/`, method: "PUT", status: 405, body: `405 method not allowed`, logIn: true},

		// TODO!! Check that you can't delete an image that is favourited (or we delete the favourites too - TBD)
		//
		// TODO!! Check what happens if the cookie expires
		// TODO!! Do we need to worry about cookie warnings?
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log.Debugf("----Test %s------", test.name)
			apiTestCall(t, ts, test)
		})
	}
}

func TestNotifications(t *testing.T) {
	os.Setenv("MB_CORS_ORIGIN", "http://mydomain")

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)
	addUser(db)
	err := limitUserNotifications(db)
	if err != nil {
		t.Errorf("failed to set user notifications limit: %v", err)
	}
	sessionStore = NewTestStore()

	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	var tests = []apiTestCase{

		// Notifications - Logged out user
		{name: "lo", url: `/v1/notifications/`, method: "GET", status: 401, logIn: false},
		{name: "lo", url: `/v1/notifications`, method: "GET", status: 401, logIn: false},
		{name: "lo", url: `/v1/notifications/`, method: "POST", status: 401, logIn: false},
		{name: "lo", url: `/v1/notifications`, method: "POST", status: 401, logIn: false},
		{name: "lo", url: `/v1/notifications/images/library/official`, method: "GET", status: 401, logIn: false},
		{name: "lo", url: `/v1/notifications/images/lizrice/childimage`, method: "GET", status: 401, logIn: false},

		// Notifications - Logged in user. Sequence is important!
		{name: "lin-0", url: `/v1/notifications/`, method: "GET", status: 200, body: `{"NotificationCount":0,"NotificationLimit":1,"Notifications":[]}`, logIn: true},
		{name: "lin-1", url: `/v1/notifications/`, method: "POST", postbody: `{"ImageName":"lizrice/childimage","WebhookURL":"http://hooks.example.com/test"}`,
			body: `{"ID":1,"ImageName":"lizrice/childimage","WebhookURL":"http://hooks.example.com/test"}`, status: 200, logIn: true},
		{name: "lin-2", url: `/v1/notifications/1`, method: "GET", body: `{"ID":1,"ImageName":"lizrice/childimage","WebhookURL":"http://hooks.example.com/test","PageURL":"https://microbadger.com/images/lizrice/childimage"}`, status: 200, logIn: true},
		{name: "lin-3", url: `/v1/notifications/1`, method: "PUT", postbody: `{"ID":1,"ImageName":"lizrice/childimage","WebhookURL":"http://hooks.example.com/another-hook"}`,
			body: `{"ID":1,"ImageName":"lizrice/childimage","WebhookURL":"http://hooks.example.com/another-hook","PageURL":"https://microbadger.com/images/lizrice/childimage"}`, status: 200, logIn: true},
		{name: "lin-4", url: `/v1/notifications/`, method: "POST", postbody: `{"ImageName":"lizrice/childimage","WebhookURL":"example/test"}`,
			body: "Invalid webhook URL", status: 422, logIn: true},
		{name: "lin-5", url: `/v1/notifications/`, method: "POST", postbody: `{"ImageName":"lizrice/childimage","WebhookURL":"http://hooks.example.com/another-hook"}`, body: "Failed to create notification", status: 500, logIn: true},
		{name: "lin-6", url: `/v1/notifications/images/lizrice/childimage`, method: "GET", status: 200, body: `{"NotificationCount":1,"NotificationLimit":1,"Notification":{"ID":1,"ImageName":"lizrice/childimage","WebhookURL":"http://hooks.example.com/another-hook","PageURL":"https://microbadger.com/images/lizrice/childimage"}}`, logIn: true},
		{name: "lin-7", url: `/v1/notifications/images/lizrice/featured`, method: "GET", status: 200, body: `{"NotificationCount":1,"NotificationLimit":1,"Notification":{}}`, logIn: true},
		{name: "lin-8", url: `/v1/notifications/1`, method: "DELETE", body: ``, status: 204, logIn: true},
		{name: "lin-9", url: `/v1/notifications/`, method: "GET", status: 200, body: `{"NotificationCount":0,"NotificationLimit":1,"Notifications":[]}`, logIn: true},

		// Can't create notification for a missing image
		{name: "lin-10", url: `/v1/notifications/`, method: "POST", postbody: `{"ImageName":"notfound/image","WebhookURL":"http://example.com"}`, status: 404, logIn: true},

		// Can't create notification for a private image we don't have access to
		{name: "lin-11", url: `/v1/notifications/`, method: "POST", postbody: `{"ImageName":"microbadgertest/alpine","WebhookURL":"http://example.com"}`, status: 404, logIn: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log.Debugf("----Test %s------", test.name)
			apiTestCall(t, ts, test)
		})
	}
}

func TestGetNotificationHistory(t *testing.T) {
	os.Setenv("MB_CORS_ORIGIN", "http://mydomain")

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)
	addUser(db)
	sessionStore = NewTestStore()

	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	// Add a notification
	apiTestCall(t, ts, apiTestCase{name: "lin-1", url: `/v1/notifications/`, method: "POST",
		postbody: `{"ImageName":"lizrice/childimage","WebhookURL":"http://hooks.example.com/test"}`,
		body:     `{"ID":1,"ImageName":"lizrice/childimage","WebhookURL":"http://hooks.example.com/test"}`,
		status:   200, logIn: true})

	// Create some notification message history
	db.Exec("INSERT INTO notification_messages (notification_id, image_name, message) VALUES(1, 'lizrice/childimage', '{\"ImageName\":\"lizrice/childimage\"}')")

	var tests = []apiTestCase{
		{url: `/v1/notifications/1`, method: "GET", status: 200, body: `{"ID":1,"ImageName":"lizrice/childimage","WebhookURL":"http://hooks.example.com/test","PageURL":"https://microbadger.com/images/lizrice/childimage","History":[{"WebhookURL":"","Message":{"ImageName":"lizrice/childimage"},"Attempts":0,"StatusCode":0,"Response":"","SentAt":"0001-01-01T00:00:00Z"}]}`, logIn: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log.Debugf("----Test %s------", test.name)
			apiTestCall(t, ts, test)
		})
	}
}

func TestUserRegistryCredentials(t *testing.T) {
	os.Setenv("MB_CORS_ORIGIN", "http://mydomain")

	var noCredsJSON = `{"UserID":1,"EnabledImageCount":0,"Registries":[{"ID":"docker","Name":"Docker Hub","Url":"https://hub.docker.com","CredentialsName":""}]}`
	var hasCredsJSON = `{"UserID":1,"EnabledImageCount":0,"Registries":[{"ID":"docker","Name":"Docker Hub","Url":"https://hub.docker.com","CredentialsName":"microbadgertest"}]}`
	var hasImageJSON = `{"UserID":1,"EnabledImageCount":1,"Registries":[{"ID":"docker","Name":"Docker Hub","Url":"https://hub.docker.com","CredentialsName":"microbadgertest"}]}`

	db = getDatabase(t)
	emptyDatabase(db)

	addUser(db)
	sessionStore = NewTestStore()
	transport, server := mockHub(t)
	defer server.Close()

	es = encryption.NewMockService()
	hs = hub.NewMockService(transport)
	rs = registry.NewMockService(transport, "http://fakeauth", "http://fakereg", "fakeservice")
	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	var tests = []apiTestCase{
		{name: "rc-00", url: `/v1/registries/`, method: "GET", status: 401, logIn: false},
		{name: "rc-01", url: `/v1/registries/`, method: "GET", status: 200, body: noCredsJSON, logIn: true},
		{name: "rc-02", url: `/v1/registry/docker`, method: "PUT", status: 401, postbody: `{"User":"microbadgertest","Password":"Q_Qesb1Z2hA7H94iXu3_buJeQ7416"}`, logIn: false},
		{name: "rc-03", url: `/v1/registry/notfound`, method: "PUT", status: 404, body: constStatusNotFound, postbody: `{"User":"microbadgertest","Password":"Q_Qesb1Z2hA7H94iXu3_buJeQ7416"}`, logIn: true},
		{name: "rc-04", url: `/v1/registry/docker`, method: "PUT", status: 422, postbody: `{"User":"microbadgertest","Password":"incorrect"}`, logIn: true},
		{name: "rc-05", url: `/v1/registry/docker`, method: "PUT", status: 204, postbody: `{"User":"microbadgertest","Password":"ckKgLL5YQvn-BpORAdPMM6VB8GtfqO"}`, logIn: true},
		{name: "rc-06", url: `/v1/registries/`, method: "GET", status: 200, body: hasCredsJSON, logIn: true},

		// Enable image and check count is updated
		{name: "rc-07pre", url: `/v1/registry/docker/images/microbadgertest/alpine`, method: "PUT", status: 204, logIn: true},
		{name: "rc-07", url: `/v1/registries/`, method: "GET", status: 200, body: hasImageJSON, logIn: true},

		{name: "rc-08", url: `/v1/registry/docker`, method: "DELETE", status: 401, logIn: false},
		{name: "rc-09", url: `/v1/registry/notfound`, method: "DELETE", status: 404, logIn: true, body: constStatusNotFound},
		{name: "rc-10", url: `/v1/registry/docker`, method: "DELETE", status: 204, logIn: true},
		{name: "rc-11", url: `/v1/registries/`, method: "GET", status: 200, body: noCredsJSON, logIn: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log.Debugf("----Test %s------", test.name)
			apiTestCall(t, ts, test)
		})
	}
}

func TestUserNamespaces(t *testing.T) {
	os.Setenv("MB_CORS_ORIGIN", "http://mydomain")

	db = getDatabase(t)
	emptyDatabase(db)

	addUser(db)
	sessionStore = NewTestStore()

	transport, server := mockHub(t)
	defer server.Close()

	es = encryption.NewMockService()
	hs = hub.NewMockService(transport)
	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	var tests = []apiTestCase{
		{name: "un-0", url: `/v1/registry/docker/namespaces`, method: "GET", status: 401, logIn: false},
		{name: "un-1", url: `/v1/registry/notfound/namespaces/`, method: "GET", status: 404, logIn: true, body: `404 page not found`},
		{name: "un-2", url: `/v1/registry/docker/namespaces`, method: "GET", status: 422, logIn: true},
		{name: "un-3", url: `/v1/registry/docker`, method: "PUT", status: 204, postbody: `{"User":"microbadgertest","Password":"ckKgLL5YQvn-BpORAdPMM6VB8GtfqO"}`, logIn: true},
		{name: "un-4", url: `/v1/registry/docker/namespaces/`, method: "GET", status: 200, body: `{"Namespaces":["microbadgertest","microscaling"]}`, logIn: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log.Debugf("----Test %s------", test.name)
			apiTestCall(t, ts, test)
		})
	}
}

func TestUserNamespaceImages(t *testing.T) {
	os.Setenv("MB_CORS_ORIGIN", "http://mydomain")

	var imagesJSON = `{"CurrentPage":1,"PageCount":1,"ImageCount":2,"Images":[`
	imagesJSON += `{"ImageName":"microbadgertest/alpine","IsInspected":false,"IsPrivate":true},`
	imagesJSON += `{"ImageName":"microbadgertest/busybox","IsInspected":true,"IsPrivate":false}]}`

	db = getDatabase(t)
	emptyDatabase(db)

	addUser(db)
	sessionStore = NewTestStore()

	transport, server := mockHub(t)
	defer server.Close()

	es = encryption.NewMockService()
	qs = queue.NewMockService()
	hs = hub.NewMockService(transport)
	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	var tests = []apiTestCase{
		// Can't get registry image info if not logged in to microbadger
		{name: "uni-0", url: `/v1/registry/docker/namespaces/microbadgertest/images/`, method: "GET", status: 401, logIn: false},

		// Can't get registry image info from a nonexistant registry
		{name: "uni-1", url: `/v1/registry/notfound/namespaces/microbadgertest/images/`, method: "GET", status: 404, logIn: true, body: `404 page not found`},

		// Can't get registry image info until we have sign-in credentials for that registry
		{name: "uni-2", url: `/v1/registry/docker/namespaces/microbadgertest/images/`, method: "GET", status: 422, logIn: true},
		{name: "uni-3", url: `/v1/registry/docker`, method: "PUT", status: 204, postbody: `{"User":"microbadgertest","Password":"ckKgLL5YQvn-BpORAdPMM6VB8GtfqO"}`, logIn: true},
		{name: "uni-4", url: `/v1/registry/docker/namespaces/microbadgertest/images/`, method: "GET", status: 200, body: imagesJSON, logIn: true},
		{name: "uni-5", url: `/v1/registry/docker/namespaces/microbadgertest/images/?page=1`, method: "GET", status: 200, body: imagesJSON, logIn: true},

		// Cant't get registry image info from a nonexistant namespace on a good registry
		{name: "uni-6", url: `/v1/registry/docker/namespaces/notfound/images/`, method: "GET", status: 404, logIn: true, body: `404 page not found`},

		// Namespace with no images
		{name: "uni-7", url: `/v1/registry/docker/namespaces/empty/images/?page=1`, method: "GET", status: 200, body: `{"CurrentPage":1,"PageCount":0,"ImageCount":0,"Images":[]}`, logIn: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log.Debugf("----Test %s------", test.name)
			apiTestCall(t, ts, test)
		})
	}
}

func TestUserImagePermissions(t *testing.T) {
	os.Setenv("MB_CORS_ORIGIN", "http://mydomain")

	db = getDatabase(t)
	emptyDatabase(db)

	addUser(db)
	sessionStore = NewTestStore()

	transport, server := mockHub(t)
	defer server.Close()

	qs = queue.NewMockService()

	rs = registry.NewMockService(transport, "http://fakeauth", "http://fakereg", "fakeservice")
	es = encryption.NewMockService()
	hs = hub.NewMockService(transport)
	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	var imagesJSON = `{"CurrentPage":1,"PageCount":1,"ImageCount":2,"Images":[`
	imagesJSON += `{"ImageName":"microbadgertest/alpine","IsInspected":false,"IsPrivate":true},`
	imagesJSON += `{"ImageName":"microbadgertest/busybox","IsInspected":true,"IsPrivate":false}]}`

	var imagesJSON2 = `{"CurrentPage":1,"PageCount":1,"ImageCount":2,"Images":[`
	imagesJSON2 += `{"ImageName":"microbadgertest/alpine","IsInspected":true,"IsPrivate":true},`
	imagesJSON2 += `{"ImageName":"microbadgertest/busybox","IsInspected":true,"IsPrivate":false}]}`

	var tests = []apiTestCase{
		// Can't do permissions if not logged in
		{name: "uip-0", url: `/v1/registry/docker/images/microbadgertest/alpine`, method: "PUT", status: 401, logIn: false},
		{name: "uip-1", url: `/v1/registry/docker/images/microbadgertest/alpine`, method: "DELETE", status: 401, logIn: false},

		// Can't do permissions until we add credentials for this registry
		{name: "uip-2", url: `/v1/registry/docker/images/microbadgertest/alpine`, method: "PUT", status: 422, logIn: true},
		{name: "uip-3", url: `/v1/registry/docker/images/microbadgertest/alpine`, method: "DELETE", status: 404, logIn: true},
		{name: "uip-4", url: `/v1/registry/docker`, method: "PUT", status: 204, postbody: `{"User":"microbadgertest","Password":"ckKgLL5YQvn-BpORAdPMM6VB8GtfqO"}`, logIn: true},
		// Check that we now see the private image as not inspected (TODO!! Bad name)
		{name: "uip-ns", url: `/v1/registry/docker/namespaces/microbadgertest/images/`, method: "GET", status: 200, body: imagesJSON, logIn: true},
		{name: "uip-5", url: `/v1/registry/docker/images/microbadgertest/alpine`, method: "PUT", status: 204, logIn: true},

		// Check that we now see the private image as inspected (TODO!! Bad name)
		{name: "uip-ns2", url: `/v1/registry/docker/namespaces/microbadgertest/images/`, method: "GET", status: 200, body: imagesJSON2, logIn: true},

		// Can't delete permission if there is a notification
		{name: "uip-6pre", url: `/v1/notifications/`, method: "POST", postbody: `{"ImageName":"microbadgertest/alpine","WebhookURL":"http://example.com"}`,
			body: `{"ID":1,"ImageName":"microbadgertest/alpine","WebhookURL":"http://example.com"}`, status: 200, logIn: true},
		{name: "uip-6", url: `/v1/registry/docker/images/microbadgertest/alpine`, method: "DELETE", status: 422, logIn: true},

		// Can delete the permission once the notification is deleted
		{name: "uip-7pre", url: `/v1/notifications/1`, method: "DELETE", status: 204, logIn: true},
		{name: "uip-7", url: `/v1/registry/docker/images/microbadgertest/alpine`, method: "DELETE", status: 204, logIn: true},

		// Can't delete the same permission twice (TODO!! Think about whether this is better or poss just 204 as end result is the same)
		{name: "uip-8", url: `/v1/registry/docker/images/microbadgertest/alpine`, method: "DELETE", status: 404, logIn: true},

		// Can't add permissions for an image that we don't have permissions for
		{name: "uip-9", url: `/v1/registry/docker/images/rossf7/windtunnel`, method: "PUT", status: 404, logIn: true, body: `404 page not found`},

		// Can't add permissions if not logged in
		{name: "uip-10", url: `/v1/registry/docker`, method: "PUT", status: 401, postbody: `{"User":"microbadgertest","Password":"ckKgLL5YQvn-BpORAdPMM6VB8GtfqO"}`, logIn: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log.Debugf("----Test %s------", test.name)
			apiTestCall(t, ts, test)
		})
	}
}

func TestPrivateNotification(t *testing.T) {
	os.Setenv("MB_CORS_ORIGIN", "http://mydomain")

	db = getDatabase(t)
	emptyDatabase(db)

	addThings(db)
	addUser(db)
	sessionStore = NewTestStore()

	transport, server := mockHub(t)
	defer server.Close()

	qs = queue.NewMockService()

	rs = registry.NewMockService(transport, "http://fakeauth", "http://fakereg", "fakeservice")
	es = encryption.NewMockService()
	hs = hub.NewMockService(transport)
	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	var tests = []apiTestCase{
		// Save credentials
		{name: "pn-0", url: `/v1/registry/docker`, method: "PUT", status: 204, postbody: `{"User":"microbadgertest","Password":"ckKgLL5YQvn-BpORAdPMM6VB8GtfqO"}`, logIn: true},

		// Can't create notification till we have permission
		{name: "pn-1", url: `/v1/notifications/`, method: "POST", postbody: `{"ImageName":"microbadgertest/alpine","WebhookURL":"http://example.com"}`, status: 404, logIn: true},

		{name: "pn-2pre", url: `/v1/registry/docker/images/microbadgertest/alpine`, method: "PUT", status: 204, logIn: true},
		{name: "pn-2", url: `/v1/notifications/`, method: "POST", postbody: `{"ImageName":"microbadgertest/alpine","WebhookURL":"http://example.com"}`,
			body: `{"ID":1,"ImageName":"microbadgertest/alpine","WebhookURL":"http://example.com"}`, status: 200, logIn: true},
		{name: "pn-3", url: `/v1/notifications/1`, method: "GET", body: `{"ID":1,"ImageName":"microbadgertest/alpine","WebhookURL":"http://example.com","PageURL":"https://microbadger.com/registry/docker/images/microbadgertest/alpine"}`, status: 200, logIn: true},

		// Can't update notification to an image we don't have permission to access
		{name: "pn-4", url: `/v1/notifications/1`, method: "PUT", postbody: `{"ID":1,"ImageName":"rossf7/windtunnel","WebhookURL":"http://example.com"}`, status: 404, logIn: true},

		// Can delete private notification
		{name: "pn-6", url: `/v1/notifications/1`, method: "DELETE", body: ``, status: 204, logIn: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log.Debugf("----Test %s------", test.name)
			apiTestCall(t, ts, test)
		})
	}
}
