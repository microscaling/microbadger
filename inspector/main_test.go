// +build dbrequire

package inspector

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/microscaling/microbadger/database"
)

// Setting up test database for this package
// $ psql -c 'create database microbadger_inspector_test;' -U postgres

func getDatabase(t *testing.T) database.PgDB {
	testdb, err := database.GetPostgres("localhost", "postgres", "microbadger_inspector_test", "", true)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	return testdb
}

func emptyDatabase(db database.PgDB) {
	db.Exec("DELETE FROM tags")
	db.Exec("DELETE FROM image_versions")
	db.Exec("DELETE FROM images")
}

func TestInspect(t *testing.T) {
	var err error

	db := getDatabase(t)
	emptyDatabase(db)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		t.Logf("Request %s", r.URL.String())
		switch r.URL.String() {
		case "http://fakehub/v2/repositories/lizrice/imagetest/":
			fmt.Fprintln(w, `{"user": "lizrice", "name": "imagetest", "namespace": "lizrice", "status": 1, "description": "", "is_private": false, "is_automated": false, "can_edit": false, "star_count": 0, "pull_count": 17675, "last_updated": "2016-08-26T11:39:56.287301Z", "has_starred": false, "full_description": "blah-di-blah", "permissions": {"read": true, "write": false, "admin": false}}`)
		case "http://fakeauth/token?service=fakeservice&scope=repository:lizrice/imagetest:pull":
			fmt.Fprintln(w, `{"token": "abctokenabc"}`)
		case "http://fakereg/v2/lizrice/imagetest/tags/list":
			fmt.Fprintln(w, `{"name": "lizrice/imagetest", "tags": ["tag1", "tag2"]}`)
		case "http://fakereg/v2/lizrice/imagetest/manifests/tag1", "http://fakereg/v2/lizrice/imagetest/manifests/tag2":
			fmt.Fprintln(w, `{"schemaVersion": 1,"name": "lizrice/imagetest","tag": "tag1","architecture": "amd64",
			             "fsLayers": [{"blobSum": "sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"}, {"blobSum": "sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"}, {"blobSum": "sha256:6c123565ed5e79b6c944d6da64bd785ad3ec03c6e853dcb733254aebb215ae55"}],
			             "history": [{"v1Compatibility": "{\"architecture\":\"amd64\",\"author\":\"liz@lizrice.com\",\"config\":{\"Hostname\":\"ae2e58a6294e\",\"Domainname\":\"\",\"User\":\"\",\"AttachStdin\":false,\"AttachStdout\":false,\"AttachStderr\":false,\"Tty\":false,\"OpenStdin\":false,\"StdinOnce\":false,\"Env\":[\"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\"],\"Cmd\":null,\"Image\":\"sha256:af06c2834e821a5900985ae8f64f4c73de9d71a8b08b29019d346adb7f176e9c\",\"Volumes\":null,\"WorkingDir\":\"\",\"Entrypoint\":null,\"OnBuild\":[],\"Labels\":{\"com.lizrice.test\":\"olé\",\"org.label-schema.vcs-ref\":\"2345678\",\"org.label-schema.vcs-url\":\"https://github.com/lizrice/imagetest\"}},\"container\":\"764bfb743437e31ebe8c909020bc9a0c738ed84cbecdc944876226990d71655c\",\"container_config\":{\"Hostname\":\"ae2e58a6294e\",\"Domainname\":\"\",\"User\":\"\",\"AttachStdin\":false,\"AttachStdout\":false,\"AttachStderr\":false,\"Tty\":false,\"OpenStdin\":false,\"StdinOnce\":false,\"Env\":[\"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\"],\"Cmd\":[\"/bin/sh\",\"-c\",\"#(nop) \",\"LABEL com.lizrice.test=olé org.label-schema.vcs-url=https://github.com/lizrice/imagetest org.label-schema.vcs-ref=2345678\"],\"Image\":\"sha256:af06c2834e821a5900985ae8f64f4c73de9d71a8b08b29019d346adb7f176e9c\",\"Volumes\":null,\"WorkingDir\":\"\",\"Entrypoint\":null,\"OnBuild\":[],\"Labels\":{\"com.lizrice.test\":\"olé\",\"org.label-schema.vcs-ref\":\"2345678\",\"org.label-schema.vcs-url\":\"https://github.com/lizrice/imagetest\"}},\"created\":\"2016-08-26T11:39:30.603530099Z\",\"docker_version\":\"1.12.1-rc1\",\"id\":\"7d82ed3f20e8ad8f7515a30cbd6070555d59f81c2fdd9950a5799ef31149c48b\",\"os\":\"linux\",\"parent\":\"c8fd7d51bc1273614a21e4a7f7590331df7102527adfe0dcac7643548f9bf7cf\",\"throwaway\":true}"},
								     {"v1Compatibility": "{\"id\":\"c8fd7d51bc1273614a21e4a7f7590331df7102527adfe0dcac7643548f9bf7cf\",\"parent\":\"415049c7b80053ff6d962bef6d08058abe309c816319b97787e4a212a5d333d0\",\"created\":\"2016-06-30T13:36:14.910427799Z\",\"container_config\":{\"Cmd\":[\"/bin/sh -c #(nop)  MAINTAINER liz@lizrice.com\"]},\"author\":\"liz@lizrice.com\",\"throwaway\":true}"}]}`)
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

	hs := hub.NewMockService(transport)
	rs := registry.NewMockService(transport, "http://fakeauth", "http://fakereg", "fakeservice")
	qs := queue.NewMockService()
	es := encryption.NewMockService()
	err = Inspect("lizrice/imagetest", &db, &rs, &hs, qs, es)
	if err != nil {
		t.Fatalf("Error %v", err)
	}

	// TODO!! Test that this sets up the database as expected
	// TODO!! Test some different cases with different tags, image versions etc
}
