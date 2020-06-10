package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestDecode(t *testing.T) {
	var v1c V1Compatibility

	// thing := `{"id":"a34e7649147cf4f5e2c0d3fb6a4bc51ac56eb1919e886bd71b5046bf2988ea10","parent":"867bcad75309b381a6401d7bcc41127e0c0b70ec22d54f82bd9e9d095189b42a","created":"2016-04-05T09:38:14.14489366Z","container":"19935b629d2aa27829b4cf3b1debce32c46e5c22053b290d5f7e464210277d58","container_config":{"Hostname":"523c4185767e","Domainname":"","User":"","AttachStdin":false,"AttachStdout":false,"AttachStderr":false,"Tty":false,"OpenStdin":false,"StdinOnce":false,"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin","BUILD_PACKAGES=bash curl-dev"],"Cmd":["/bin/sh","-c","#(nop) ENTRYPOINT \u0026{[\"/run.sh\"]}"],"Image":"867bcad75309b381a6401d7bcc41127e0c0b70ec22d54f82bd9e9d095189b42a","Volumes":null,"WorkingDir":"","Entrypoint":["/run.sh"],"OnBuild":[],"Labels":{}},"docker_version":"1.9.1","author":"Ross Fairbanks \"ross@microscaling.com\"","config":{"Hostname":"523c4185767e","Domainname":"","User":"","AttachStdin":false,"AttachStdout":false,"AttachStderr":false,"Tty":false,"OpenStdin":false,"StdinOnce":false,"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin","BUILD_PACKAGES=bash curl-dev"],"Cmd":null,"Image":"867bcad75309b381a6401d7bcc41127e0c0b70ec22d54f82bd9e9d095189b42a","Volumes":null,"WorkingDir":"","Entrypoint":["/run.sh"],"OnBuild":[],"Labels":{}},"architecture":"amd64","os":"linux"}`
	// thing := `{"id":"a34e7649147cf4f5e2c0d3fb6a4bc51ac56eb1919e886bd71b5046bf2988ea10","parent":"867bcad75309b381a6401d7bcc41127e0c0b70ec22d54f82bd9e9d095189b42a","created":"2016-04-05T09:38:14.14489366Z","container_config":{},"docker_version":"1.9.1","architecture":"amd64","os":"linux"}`
	// ,"config":{"Hostname":"523c4185767e","Domainname":"","User":"","AttachStdin":false,"AttachStdout":false,"AttachStderr":false,"Tty":false,"OpenStdin":false,"StdinOnce":false,"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin","BUILD_PACKAGES=bash curl-dev"],"Cmd":null,"Image":"867bcad75309b381a6401d7bcc41127e0c0b70ec22d54f82bd9e9d095189b42a","Volumes":null,"WorkingDir":"","Entrypoint":["/run.sh"],"OnBuild":[],"Labels":{}}
	thing := `{"id":"a34e7649147cf4f5e2c0d3fb6a4bc51ac56eb1919e886bd71b5046bf2988ea10","parent":"867bcad75309b381a6401d7bcc41127e0c0b70ec22d54f82bd9e9d095189b42a","created":"2016-04-05T09:38:14.14489366Z", "author":"Ross Fairbanks \"ross@microscaling.com\"","config":{"Hostname":"523c4185767e", "Labels":{}}}`
	err := json.Unmarshal([]byte(thing), &v1c)
	if err != nil {
		t.Errorf("Failed: %v", err)
	}
}

func TestNewService(t *testing.T) {
	NewService()
}

// Check that we can create a mock service and get some fake info from it
func TestRegistry(t *testing.T) {
	// Test server that always responds with 200 code and with a set payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.String() {
		case "http://fakehub/v2/repositories/org/image/":
			fmt.Fprintln(w, `{"user": "org", "name": "image", "namespace": "org", "status": 1, "description": "", "is_private": false, "is_automated": false, "can_edit": false, "star_count": 0, "pull_count": 17675, "last_updated": "2016-08-26T11:39:56.287301Z", "has_starred": false, "full_description": "blah-di-blah", "permissions": {"read": true, "write": false, "admin": false}}`)
		case "http://fakeauth/token?service=fake.service&scope=repository:org/image:pull":
			fmt.Fprintln(w, `{"token": "abctokenabc"}`)
		case "http://fakereg/v2/org/image/tags/list":
			fmt.Fprintln(w, `{"name": "org/image", "tags": ["tag1", "tag2"]}`)
		default:
			t.Errorf("Unexpected request to %s", r.URL.String())
		}
	}))
	defer server.Close()

	// Make a transport that reroutes all traffic to the example server
	transport := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			return url.Parse(server.URL)
		},
	}

	rs := NewMockService(transport, "http://fakeauth", "http://fakereg", "fake.service")
	i := Image{Name: "org/image"}

	tac, err := NewTokenAuth(i, &rs)
	if err != nil {
		t.Errorf("Unexpectedly failed to get token: %v", err)
	}

	if tac.token != "abctokenabc" {
		t.Errorf("Unexpcted token %s", tac.token)
	}

	tags, err := tac.GetTags()
	if err != nil {
		t.Errorf("Unexpectedly failed to get tags: %v", err)
	}

	if len(tags) != 2 {
		t.Errorf("Unexpected number of tags %d", len(tags))
	}
}
