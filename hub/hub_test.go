package hub

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/microscaling/microbadger/registry"
)

func TestNewService(t *testing.T) {
	i := NewService()
	if i.baseURL != "https://hub.docker.com" {
		t.Errorf("Unexpected hub URL %s", i.baseURL)
	}
}

// Check that we can create a mock service and get some fake info from it
func TestInfo(t *testing.T) {
	// Test server that always responds with 200 code and with a set payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"user": "lizrice", "name": "imagetest", "namespace": "lizrice", "status": 1, "description": "", "is_private": false, "is_automated": false, "can_edit": false, "star_count": 0, "pull_count": 17675, "last_updated": "2016-08-26T11:39:56.287301Z", "has_starred": false, "full_description": "blah-di-blah", "permissions": {"read": true, "write": false, "admin": false}}`)
	}))
	defer server.Close()

	// Make a transport that reroutes all traffic to the example server
	transport := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			return url.Parse(server.URL)
		},
	}

	i := registry.Image{
		Name: "org/image",
	}

	hs := NewMockService(transport)
	info, err := hs.Info(i)
	if err != nil {
		t.Errorf("Error getting fake hub info: %v", err)
	}
	t.Logf("Info %v", info)

	if info.PullCount != 17675 {
		t.Errorf("Unexpected pull count %d", info.PullCount)
	}

	if info.LastUpdated.Hour() != 11 {
		t.Errorf("Unexpected time %v", info.LastUpdated)
	}

	if info.FullDescription != "blah-di-blah" {
		t.Errorf("Unexpected full description %s", info.FullDescription)
	}
}

func TestLogin(t *testing.T) {
	transport, server := mockHub(t)
	defer server.Close()

	authToken := "abctokenabc"
	hs := NewMockService(transport)

	token, err := hs.Login("user", "password")
	if err != nil {
		t.Errorf("Unexpected error found logging in to Docker Hub %v", err)
	}

	if token != authToken {
		t.Errorf("Expected auth token to be %s but was %s", authToken, token)
	}

	token, err = hs.Login("user", "incorrect")
	if err == nil {
		t.Errorf("Expected an error logging in but found %v", err)
	}

	if token != "" {
		t.Errorf("Expected token to be blank but was %s", token)
	}
}

func TestUserNamespaces(t *testing.T) {
	transport, server := mockHub(t)
	defer server.Close()

	results := NamespaceList{
		Namespaces: []string{"force12io", "microbadgertest", "microscaling"},
	}

	hs := NewMockService(transport)
	namespaces, err := hs.UserNamespaces("user", "password")
	if err != nil {
		t.Errorf("Error getting fake user namespaces - %v", err)
	}

	if !reflect.DeepEqual(results, namespaces) {
		t.Errorf("Unexpected user namespaces was %v but expected %v", namespaces, results)
	}

	_, err = hs.UserNamespaces("user", "incorrect")
	if err == nil {
		t.Errorf("Expected an error getting namespaces but none was found")
	}
}

func TestUserNamespaceImages(t *testing.T) {
	// TODO!!
	// t.Errorf("Add tests for getting images in a namespace.")
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
				w.WriteHeader(401)
				fmt.Fprintln(w, `{"detail": "Incorrect authentication credentials."}`)
			} else {
				// fmt.Println("We say this password is OK")
				fmt.Fprintln(w, `{"token": "abctokenabc"}`)
			}
		case "http://fakehub/v2/repositories/namespaces/":
			fmt.Fprintf(w, `{"namespaces":["microbadgertest","microscaling"]}`)

		case "http://fakehub/v2/user/orgs/?page_size=250": // Match large page size used by Docker Hub UI
			fmt.Fprintf(w, `{"count": 1, "results": [{"orgname": "microscaling"},{"orgname": "force12io"}]}`)

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
