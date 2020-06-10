// +build dbrequired

package api

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/microscaling/microbadger/database"
	"github.com/microscaling/microbadger/encryption"
	"github.com/microscaling/microbadger/hub"
	"github.com/microscaling/microbadger/queue"
	"github.com/microscaling/microbadger/registry"
)

func TestGetImageList(t *testing.T) {
	db = getDatabase(t)
	emptyDatabase(db)

	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	type test struct {
		name      string
		url       string
		status    int
		body      string
		addThings bool
	}

	var tests = []test{

		// Empty lists
		{name: "empty", url: `/v1/images?query=recent`, status: 200, body: `{"CurrentPage":1}`},
		{name: "empty", url: `/v1/images?query=featured`, status: 200, body: `{"CurrentPage":1}`},
		{name: "empty", url: `/v1/images?query=labelschema`, status: 200, body: `{"CurrentPage":1}`},

		// Lists with entries - these must only be public images under all circumstances
		{name: "list", url: `/v1/images?query=recent`, status: 200, body: `{"CurrentPage":1,"PageCount":1,"ImageCount":2,"Images":["lizrice/childimage","lizrice/featured"]}`, addThings: true},
		{name: "list", url: `/v1/images?query=featured`, status: 200, body: `{"CurrentPage":1,"PageCount":1,"ImageCount":1,"Images":["lizrice/featured"]}`},
		{name: "list", url: `/v1/images?query=labelschema`, status: 200, body: `{"CurrentPage":1,"PageCount":1,"ImageCount":1,"Images":["lizrice/childimage"]}`},
	}

	for id, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log.Debugf("-test %d----------", id)
			if test.addThings {
				addThings(db)
			}

			res, err := http.Get(ts.URL + test.url)
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

			if string(body) != test.body {
				t.Errorf("#%d Body is not as expected, have %s\n expected %s", id, body, test.body)
			}

			ct := res.Header.Get("Content-Type")
			if ct != "application/javascript" {
				t.Errorf("#%d Content type is not as expected, have %s", id, ct)
			}
		})
	}
}

func TestGetImageJSON(t *testing.T) {
	db = getDatabase(t)
	emptyDatabase(db)
	addBadgeThings(db)
	addImageVersionDetails(db)
	expectedImages := badgeThings()

	qs = queue.NewMockService()
	rs = registry.NewService()
	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	type test struct {
		name       string
		url        string
		status     int
		imageName  string
		tag        string
		layercount int
	}

	var tests = []test{
		// Good images
		// If there is a version tagged latest, and it also has other tag(s) we use the longest other tag
		{url: `/v1/images/lizrice/childimage`, status: 200, imageName: "lizrice/childimage", tag: "same", layercount: 4},
		{url: `/v1/images/official`, status: 200, imageName: "official", tag: "latest"},

		// Image still under size inspection
		{url: `/v1/images/rossf7/size`, status: 200, imageName: "rossf7/size", tag: "latest"},

		// If URL has a tag return the matching version
		{url: `/v1/images/lizrice/childimage:latest`, status: 200, imageName: "lizrice/childimage", tag: "same", layercount: 4},
		{url: `/v1/images/lizrice/childimage:specific`, status: 200, imageName: "lizrice/childimage", tag: "specific", layercount: 4},
		{url: `/v1/images/lizrice/childimage:blah`, status: 404},
		{url: `/v1/images/official:latest`, status: 200, imageName: "official", tag: "latest"},
		{url: `/v1/images/official:blah`, status: 404},

		// Missing images
		{url: `/v1/images/blah`, status: 404},
		{url: `/v1/images/blah/blah`, status: 404},
		{url: `/v1/images/lizrice/blah`, status: 404},

		// Private images that we shouldn't have access to
		{url: `/v1/images/youruser/private`, status: 404},
	}

	for id, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			log.Debugf("-test %d----------", id)

			res, err := http.Get(ts.URL + test.url)
			if err != nil {
				t.Fatalf("Failed to send request #%d (%s) %v", id, test.url, err)
			}

			body, err := ioutil.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Errorf("Error getting body. %v", err)
			}

			if res.StatusCode != test.status {
				t.Errorf("#%d Unexpected status code %d, expected %d", id, res.StatusCode, test.status)
			}

			if test.status == 200 {
				imageName := fmt.Sprintf(`"ImageName":"%s"`, test.imageName)
				if !strings.Contains(string(body), imageName) {
					t.Errorf("#%d Didn't find expected ImageName %s in %s", id, imageName, body)
				}

				latest := fmt.Sprintf(`"LatestVersion":"%s"`, test.tag)
				if !strings.Contains(string(body), latest) {
					t.Errorf("#%d Didn't find expected LatestVersion %s in %s", id, latest, body)
				}

				layercount := fmt.Sprintf(`"LayerCount":%d`, test.layercount)
				if !strings.Contains(string(body), layercount) {
					t.Errorf("#%d Didn't find expected LayerCount %s in %s", id, layercount, body)
				}

				if !strings.Contains(string(body), "Versions") {
					t.Errorf("#%d Didn't find any versions in %s", id, body)
				}

				// check that library/ gets sripped out of any image names
				if strings.Contains(string(body), `"ImageName":"library`) {
					t.Errorf("#%d Didn't expect to find library in %s", id, body)
				}

				// Check that the versions & tags are what we expected
				var imageResp database.Image
				var found, tagFound bool
				err = json.Unmarshal(body, &imageResp)
				if err != nil {
					t.Fatalf("Couldn't unmarshal body to image: %v", err)
				}

				exp := expectedImages[test.imageName]
				for _, iv := range imageResp.Versions {
					found = false
					for _, ev := range exp.Versions {
						if iv.SHA == ev.SHA {
							found = true
						}
					}

					if !found {
						t.Errorf("Unexpected version %s / %s", test.imageName, iv.SHA)
					}
				}

				for _, ev := range exp.Versions {
					found = false
					for _, iv := range imageResp.Versions {
						if iv.SHA == ev.SHA {
							found = true

							for _, et := range ev.Tags {
								tagFound = false
								for _, tt := range iv.Tags {
									if et.Tag == tt.Tag {
										tagFound = true
										if et != tt {
											t.Errorf("Tags clash exp %v got %v", et, tt)
										}
									}
								}

								if !tagFound {
									t.Errorf("Missing tag %s from image %s", et.Tag, test.imageName)
								}
							}
						}
					}

					if !found {
						t.Errorf("Missing expected version %s / %s", test.imageName, ev.SHA)
					}
				}

				// t.Log(string(body))
			}

			ct := res.Header.Get("Content-Type")
			if ct != "application/javascript" {
				t.Errorf("#%d Content type is not as expected, have %s", id, ct)
			}
		})
	}
}

func TestGetIdentical(t *testing.T) {
	db = getDatabase(t)
	emptyDatabase(db)
	addBadgeThings(db)
	addImageVersionDetails(db)

	iv, err := db.GetImageVersionBySHA("80000", "another/parentimage", false)
	if err != nil {
		t.Errorf("Failed to get imageversion: %v", err)
	}

	if iv.LayerCount != 2 {
		t.Logf("%#v", iv)
		t.Errorf("Layer count is %d, expected 2", iv.LayerCount)
	}

	layers := make([]database.ImageLayer, 2)
	err = json.Unmarshal([]byte(iv.Layers), &layers)
	if err != nil {
		t.Errorf("Error unmarshalling layers %s: %v", iv.Layers, err)
	}

	// We're passing in a different SHA & image name so we can check we match ourselves
	ivList := getIdenticalImageVersions(layers, "blah", "blah", nil)
	if len(ivList) != 1 {
		t.Logf("%d Identical\n%v", len(ivList), ivList)
		t.Errorf("Expected image version to match itself")
	}
}

func TestGetImageParents(t *testing.T) {
	db = getDatabase(t)
	emptyDatabase(db)
	addBadgeThings(db)
	addImageVersionDetails(db)

	iv, err := db.GetImageVersionBySHA("10000", "lizrice/childimage", false)
	if err != nil {
		t.Errorf("Failed to get imageversion: %v", err)
	}

	if iv.LayerCount != 4 {
		t.Logf("%#v", iv)
		t.Errorf("Layer count is %d, expected 2", iv.LayerCount)
	}

	layers := make([]database.ImageLayer, 4)
	err = json.Unmarshal([]byte(iv.Layers), &layers)
	if err != nil {
		t.Errorf("Error unmarshalling layers %s: %v", iv.Layers, err)
	}
	keepLayers := layers

	// Get a matching parent
	ivParents := getParentsFromLayers(&layers, "10000", "lizrice/childimage", nil)
	if len(ivParents) != 1 {
		t.Logf("%d Parents\n%v", len(ivParents), ivParents)
		t.Errorf("Expected a matching parent (base image)")
	}
	if ivParents[0].ImageName != "another/parentimage" {
		t.Errorf("Unexpected parent image %s, expected another/parentimage", ivParents[0].ImageName)
	}

	// Get more than one matching parent
	db.Exec("UPDATE image_versions SET hash='9b11b2be7a73196d5f017d37d8cee8e2529495bda10eaa79f47ccf19eefd1513' where image_name='lizrice/nolatest' and sha='30000'")
	layers = keepLayers
	ivParents = getParentsFromLayers(&layers, "10000", "lizrice/childimage", nil)
	if len(ivParents) != 2 {
		t.Logf("%d Parents\n%v", len(ivParents), ivParents)
		t.Errorf("Expected two matching parents")
	}

	// Don't return matching parent if user doesn't have permission for it
	db.Exec("UPDATE image_versions SET hash='9b11b2be7a73196d5f017d37d8cee8e2529495bda10eaa79f47ccf19eefd1513' where image_name='otheruser/private' and sha='50000'")
	layers = keepLayers
	ivParents = getParentsFromLayers(&layers, "10000", "lizrice/childimage", nil)
	if len(ivParents) != 2 {
		t.Logf("%d Parents\n%v", len(ivParents), ivParents)
		t.Errorf("Still expected two matching parents")
	}

	// Only get official match even if there are other candidates
	db.Exec("UPDATE image_versions SET hash='9b11b2be7a73196d5f017d37d8cee8e2529495bda10eaa79f47ccf19eefd1513' where image_name='library/official' and sha='20000'")
	layers = keepLayers
	ivParents = getParentsFromLayers(&layers, "10000", "lizrice/childimage", nil)
	if len(ivParents) != 1 {
		t.Logf("%d Parents\n%v", len(ivParents), ivParents)
		t.Errorf("Only expected one official image match")
	}
	if ivParents[0].ImageName != "library/official" {
		t.Errorf("Unexpected parent image %s, expected library/official", ivParents[0].ImageName)
	}

}

func TestGetImageVersionJSON(t *testing.T) {
	db = getDatabase(t)
	emptyDatabase(db)
	addBadgeThings(db)
	addImageVersionDetails(db)

	qs = queue.NewMockService()
	rs = registry.NewService()
	es = encryption.NewService()
	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	type test struct {
		name      string
		url       string
		status    int
		imageName string
		labels    string
	}

	var tests = []test{
		// Good images
		{url: `/v1/images/lizrice/childimage/version/10001`, status: 200, imageName: "lizrice/childimage", labels: `{"com.label-schema.license":"Apache2.0","com.lizrice.test":"olé"}`},
		{url: `/v1/images/official/version/20001`, status: 200, imageName: "official"},
		{url: `/v1/images/rossf7/size/version/60000`, status: 202, imageName: "rossf7/size"},

		// Missing images
		{url: `/v1/images/blah/version/`, status: 404},
		{url: `/v1/images/blah/version`, status: 404},
		{url: `/v1/images/blah/blah/version/`, status: 404},
		{url: `/v1/images/blah/blah/version`, status: 404},
		{url: `/v1/images/lizrice/blah/version`, status: 404},
		{url: `/v1/images/lizrice/blah/version/`, status: 404},

		// Missing image versions
		{url: `/v1/images/lizrice/childimage/version/abcde`, status: 404},
		{url: `/v1/images/lizrice/childimage/version/1000000`, status: 404},
		{url: `/v1/images/lizrice/childimage/version/20001`, status: 404},
	}

	for id, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			log.Debugf("-test %d----------", id)

			res, err := http.Get(ts.URL + test.url)
			if err != nil {
				t.Fatalf("Failed to send request #%d (%s) %v", id, test.url, err)
			}

			body, err := ioutil.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Errorf("Error getting body. %v", err)
			}

			if res.StatusCode != test.status {
				t.Errorf("#%d Unexpected status code %d, wanted %d", id, res.StatusCode, test.status)
			}

			if test.status == 200 {
				imageName := fmt.Sprintf(`"ImageName":"%s"`, test.imageName)
				if !strings.Contains(string(body), imageName) {
					t.Errorf("#%d Didn't find expected ImageName %s in %s", id, imageName, body)
				}

				if len(test.labels) > 0 {
					labels := fmt.Sprintf(`"Labels":%s`, test.labels)
					if !strings.Contains(string(body), labels) {
						t.Errorf("#%d Didn't find expected labels %s in %s", id, labels, body)
					}
				}

				// check that library/ gets sripped out of any image names
				if strings.Contains(string(body), `"ImageName":"library`) {
					t.Errorf("#%d Didn't expect to find library in %s", id, body)
				}

				ct := res.Header.Get("Content-Type")
				if ct != "application/javascript" {
					t.Errorf("#%d Content type is not as expected, have %s", id, ct)
				}
			}
		})
	}
}

func TestImageSearch(t *testing.T) {
	os.Setenv("MB_CORS_ORIGIN", "http://mydomain")

	db = getDatabase(t)
	emptyDatabase(db)
	addBadgeThings(db)
	addUser(db)
	sessionStore = NewTestStore()

	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	var tests = []apiTestCase{
		{name: "s-0", url: `/v1/images/search/official`, method: "GET", status: 200, body: `{"Images":["library/official"]}`, logIn: false},
		{name: "s-1", url: `/v1/images/search/rice`, method: "GET", status: 200, body: `{"Images":["lizrice/nolatest","lizrice/childimage"]}`, logIn: false},
		{name: "s-2", url: `/v1/images/search/image`, method: "GET", status: 200, body: `{"Images":["another/parentimage","lizrice/childimage"]}`, logIn: false},
		{name: "s-3", url: `/v1/images/search/lizrice/nolatest`, method: "GET", status: 200, body: `{"Images":["lizrice/nolatest"]}`, logIn: true},
		// Result is empty (but successful) if there are no results
		{name: "s-4", url: `/v1/images/search/micro`, method: "GET", status: 200, body: `{}`, logIn: true},
		{name: "s-5", url: `/v1/images/search/rossf7/badger`, method: "GET", status: 200, body: `{"Images":["rossf7/badgerbadgerbadger"]}`, logIn: true},
		// Private images should not be visible through search unless the logged in user has permissions
		{name: "s-7", url: `/v1/images/search/myuser/priv`, method: "GET", status: 200, body: `{}`, logIn: false},
		// TODO!! For now, we never return private images in search even if the user should have permissions
		// {name: "s-8", url: `/v1/images/search/myuser/priv`, method: "GET", status: 200, body: `{"Images":["myuser/private"]}`, logIn: true},
		{name: "s-9", url: `/v1/images/search/otheruser/priv`, method: "GET", status: 200, body: `{}`, logIn: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log.Debugf("----Test %s------", test.name)
			apiTestCall(t, ts, test)
		})
	}
}

func TestGetPrivateImage(t *testing.T) {
	os.Setenv("MB_CORS_ORIGIN", "http://mydomain")

	db = getDatabase(t)
	emptyDatabase(db)
	addBadgeThings(db)
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
		// Can't get private images if not logged in
		{name: "pi-0", url: `/v1/registry/docker/images/myuser/private`, method: "GET", status: 401, logIn: false},
		{name: "pi-1", url: `/v1/registry/docker`, method: "PUT", status: 401, postbody: `{"User":"microbadgertest","Password":"ckKgLL5YQvn-BpORAdPMM6VB8GtfqO"}`, logIn: false},

		// Log in and add credentials
		{name: "pi-2prep", url: `/v1/registry/docker`, method: "PUT", status: 204, postbody: `{"User":"microbadgertest","Password":"ckKgLL5YQvn-BpORAdPMM6VB8GtfqO"}`, logIn: true},

		// Now we can see a private image
		{name: "pi-2", url: `/v1/registry/docker/images/myuser/private`, method: "PUT", status: 204, logIn: true},
		{name: "pi-3", url: `/v1/registry/docker/images/myuser/private`, method: "GET", status: 200, logIn: true, body: `{"LatestSHA":"40000","UpdatedAt":"0001-01-01T00:00:00Z","LastUpdated":"0001-01-01T00:00:00Z","PullCount":0,"StarCount":0,"Versions":[{"SHA":"40000","Tags":[{"tag":"latest"}],"ImageName":"myuser/private","Author":"","Labels":{"org.label-schema.name":"private"},"LayerCount":0,"DownloadSize":0,"Created":"0001-01-01T00:00:00Z"}],"Id":"40000","ImageName":"myuser/private","ImageURL":"https://hub.docker.com/r/myuser/private/","Labels":{"org.label-schema.name":"private"},"LatestVersion":"latest"}`},

		// Can't see private images that don't exist
		{name: "pi-4", url: `/v1/registry/docker/images/otheruser/private`, method: "GET", status: 404, body: `404 page not found`, logIn: true},
		{name: "pi-5", url: `/v1/registry/docker/images/notfound/alpine`, method: "GET", status: 404, body: `404 page not found`, logIn: true},

		// See a private image that we haven't started inspecting yet
		{name: "pi-6", url: `/v1/registry/docker/images/microbadgertest/alpine`, method: "GET", status: 404, body: `404 page not found`, logIn: true},
		{name: "pi-7prep", url: `/v1/registry/docker/images/microbadgertest/alpine`, method: "PUT", status: 204, logIn: true},
		{name: "pi-7", url: `/v1/registry/docker/images/microbadgertest/alpine`, method: "GET", status: 202, logIn: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log.Debugf("----Test %s------", test.name)
			apiTestCall(t, ts, test)
		})
	}
}

func BenchmarkGetImageJSON(b *testing.B) {
	// Current result on LR laptop
	// BenchmarkGetImageJSON-4       	     300	   5650160 ns/op
	var err error

	// TODO!! Make it so you can call the common getDatabase call (current requires a T not a B)
	db, err = database.GetPostgres("localhost", "postgres", "microbadger_api_test", "", false)
	if err != nil {
		b.Fatalf("Failed to open test database: %v", err)
	}

	emptyDatabase(db)
	addBadgeThings(db)
	addImageVersionDetails(db)
	qs = queue.NewMockService()
	rs = registry.NewService()
	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	for n := 0; n < b.N; n++ {
		http.Get(ts.URL + `/v1/images/lizrice/childimage`)
	}
}

func BenchmarkGetImageVersionJSON(b *testing.B) {
	// Current result on LR laptop
	// BenchmarkGetImageVersionJSON-4	     200	   5712086 ns/op
	var err error

	// TODO!! Make it so you can call the common getDatabase call (current requires a T not a B)
	db, err = database.GetPostgres("localhost", "postgres", "microbadger_api_test", "", false)
	if err != nil {
		b.Fatalf("Failed to open test database: %v", err)
	}

	emptyDatabase(db)
	addBadgeThings(db)
	addImageVersionDetails(db)

	qs = queue.NewMockService()
	rs = registry.NewService()
	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	for n := 0; n < b.N; n++ {
		http.Get(ts.URL + `/v1/images/lizrice/childimage/version/10001`)
	}
}

func addImageVersionDetails(db database.PgDB) {
	db.Exec("UPDATE image_versions SET manifest = $1 WHERE image_name='lizrice/childimage'",
		`{"schemaVersion":1,"name":"lizrice/childimage","history":[{"v1Compatibility":"{\"architecture\":\"amd64\",\"author\":\"liz@l`+
			`izrice.com\",\"config\":{\"Hostname\":\"ae2e58a6294e\",\"Domainname\":\"\",\"User\":\"\",\"AttachStdin\":false,\"AttachStdout`+
			`\":false,\"AttachStderr\":false,\"Tty\":false,\"OpenStdin\":false,\"StdinOnce\":false,\"Env\":[\"PATH=/usr/local/sbin:/usr/lo`+
			`cal/bin:/usr/sbin:/usr/bin:/sbin:/bin\"],\"Cmd\":null,\"Image\":\"sha256:24f0f43ec118b3551cbd25ae3431dfd9068ac9851558444084bd`+
			`5fef62651048\",\"Volumes\":null,\"WorkingDir\":\"\",\"Entrypoint\":null,\"OnBuild\":[],\"Labels\":{\"com.label-schema.license`+
			`\":\"Apache2.0\",\"com.lizrice.test\":\"olé\"}},\"container\":\"323ce17968b7fb5f7a9f0b865ba7fe655dc5d692c17bff0b751b5e99b03f9`+
			`d7d\",\"container_config\":{\"Hostname\":\"ae2e58a6294e\",\"Domainname\":\"\",\"User\":\"\",\"AttachStdin\":false,\"AttachStd`+
			`out\":false,\"AttachStderr\":false,\"Tty\":false,\"OpenStdin\":false,\"StdinOnce\":false,\"Env\":[\"PATH=/usr/local/sbin:/usr`+
			`/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\"],\"Cmd\":[\"/bin/sh\",\"-c\",\"#(nop) \",\"LABEL com.label-schema.license=Apache2.`+
			`0\"],\"Image\":\"sha256:24f0f43ec118b3551cbd25ae3431dfd9068ac9851558444084bd5fef62651048\",\"Volumes\":null,\"WorkingDir\":\"`+
			`\",\"Entrypoint\":null,\"OnBuild\":[],\"Labels\":{\"com.label-schema.license\":\"Apache2.0\",\"com.lizrice.test\":\"olé\"}},\`+
			`"created\":\"2016-07-14T17:58:11.874925612Z\",\"docker_version\":\"1.12.0-rc3\",\"id\":\"bc8b686e7eedf25e0d013722c0d3eff33be7`+
			`524b9d01b9896d8c22a432ead2be\",\"os\":\"linux\",\"parent\":\"2137fcb24a8ab54f0cc890029fd46fb205e177862026da931ebb693533b5393d`+
			`\",\"throwaway\":true}"},{"v1Compatibility":"{\"id\":\"2137fcb24a8ab54f0cc890029fd46fb205e177862026da931ebb693533b5393d\",\"p`+
			`arent\":\"b62e85337e42ce41d4bdffaf31ff4602ca362cfbb0ea56e94b8f47d8553fe18c\",\"created\":\"2016-07-14T17:58:11.521533058Z\",\`+
			`"container_config\":{\"Cmd\":[\"/bin/sh -c #(nop)  MAINTAINER liz@lizrice.com\"]},\"author\":\"liz@lizrice.com\",\"throwaway\`+
			`":true}"},{"v1Compatibility":"{\"id\":\"b62e85337e42ce41d4bdffaf31ff4602ca362cfbb0ea56e94b8f47d8553fe18c\",\"parent\":\"c8fd7`+
			`d51bc1273614a21e4a7f7590331df7102527adfe0dcac7643548f9bf7cf\",\"created\":\"2016-07-12T11:37:25.854227555Z\",\"container_conf`+
			`ig\":{\"Cmd\":[\"/bin/sh -c #(nop)  LABEL com.lizrice.test=olé\"]},\"author\":\"liz@lizrice.com\",\"throwaway\":true}"},{"v1C`+
			`ompatibility":"{\"id\":\"c8fd7d51bc1273614a21e4a7f7590331df7102527adfe0dcac7643548f9bf7cf\",\"parent\":\"415049c7b80053ff6d96`+
			`2bef6d08058abe309c816319b97787e4a212a5d333d0\",\"created\":\"2016-06-30T13:36:14.910427799Z\",\"container_config\":{\"Cmd\":[`+
			`\"/bin/sh -c #(nop)  MAINTAINER liz@lizrice.com\"]},\"author\":\"liz@lizrice.com\",\"throwaway\":true}"},{"v1Compatibility":"`+
			`{\"id\":\"415049c7b80053ff6d962bef6d08058abe309c816319b97787e4a212a5d333d0\",\"created\":\"2016-06-23T19:55:12.671901355Z\",\`+
			`"container_config\":{\"Cmd\":[\"/bin/sh -c #(nop) ADD file:86864edb9037700501e6e016262c29922e0c67762b4525901ca5a8194a078bfb i`+
			`n /\"]}}"}],"fsLayers":[{"blobSum":"sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"},{"blobSum":"sha`+
			`256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"},{"blobSum":"sha256:a3ed95caeb02ffe68cdd9fd84406680ae93`+
			`d633cb16422d00e8a7c22955b46d4"},{"blobSum":"sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"},{"blobS`+
			`um":"sha256:6c123565ed5e79b6c944d6da64bd785ad3ec03c6e853dcb733254aebb215ae55"}]}`)
	db.Exec("UPDATE image_versions SET labels = $1 WHERE image_name='lizrice/childimage'",
		`{"com.label-schema.license":"Apache2.0","com.lizrice.test":"olé"}`)

	db.Exec("UPDATE image_versions SET layers = $1 WHERE image_name='lizrice/childimage'",
		`[{"BlobSum":"sha256:6c123565ed5e79b6c944d6da64bd785ad3ec03c6e853dcb733254aebb215ae55","Command":"ADD file:86864edb9037700501`+
			`e6e016262c29922e0c67762b4525901ca5a8194a078bfb in /","DownloadSize":2320188},{"BlobSum":"sha256:a3ed95caeb02ffe68cdd9fd844066`+
			`80ae93d633cb16422d00e8a7c22955b46d4","Command":"MAINTAINER liz@lizrice.com","DownloadSize":0},{"BlobSum":"sha256:a3ed95caeb02`+
			`ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4","Command":"LABEL com.lizrice.test=olé","DownloadSize":0},{"BlobSum":"sh`+
			`a256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4","Command":"MAINTAINER liz@lizrice.com","DownloadSize":`+
			`0},{"BlobSum":"sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4","Command":"LABEL com.label-schema.lic`+
			`ense=Apache2.0","DownloadSize":32}]`)

	// TODO!! For some reason the tests are set up for a layer_count of 4 even though there seem to be 5 layers in the list
	db.Exec("UPDATE image_versions SET layer_count = 4 where image_name='lizrice/childimage'")
	db.Exec("UPDATE image_versions SET download_size = 2320220 where image_name='lizrice/childimage'")

	// Parent image matches top two layers of childimage
	db.Exec("UPDATE image_versions SET layers = $1 WHERE image_name='another/parentimage' and sha='80000'",
		`[{"BlobSum":"sha256:6c123565ed5e79b6c944d6da64bd785ad3ec03c6e853dcb733254aebb215ae55","Command":"ADD file:86864edb9037700501`+
			`e6e016262c29922e0c67762b4525901ca5a8194a078bfb in /","DownloadSize":2320188},{"BlobSum":"sha256:a3ed95caeb02ffe68cdd9fd844066`+
			`80ae93d633cb16422d00e8a7c22955b46d4","Command":"MAINTAINER liz@lizrice.com","DownloadSize":0}]`)
	db.Exec("UPDATE image_versions SET layer_count = 2 where image_name='another/parentimage' and sha='80000'")

	// fmt.Println(inspector.GetHashFromLayers(layers))
	db.Exec("UPDATE image_versions SET hash='9b11b2be7a73196d5f017d37d8cee8e2529495bda10eaa79f47ccf19eefd1513' where image_name='another/parentimage' and sha='80000'")
}
