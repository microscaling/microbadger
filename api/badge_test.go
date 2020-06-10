// +build dbrequired

package api

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/microscaling/microbadger/database"
)

func addBadgeThings(db database.PgDB) {

	now := time.Now().UTC()
	earlier := now.Add(-24 * time.Hour)

	db.Exec("INSERT INTO images (name, status, badge_count, created_at, latest, featured, auth_token, badges_installed, is_private, is_automated, pull_count) VALUES('lizrice/nolatest', 'INSPECTED', 2, $1, '30001', True, 'lowercase', 2, false, true, 1000)", now)
	db.Exec("INSERT INTO images (name, status, badge_count, created_at, latest, auth_token, badges_installed, is_private, is_automated, pull_count) VALUES('lizrice/childimage', 'INSPECTED', 2, $1, '10000', 'lowercase', 1, false, false, 2)", now)
	db.Exec("INSERT INTO images (name, status, badge_count, created_at, latest, featured, auth_token, is_private) VALUES('library/official', 'INSPECTED', 2, $1, '20000', True, 'mIxeDcAse', false)", now)
	db.Exec("INSERT INTO images (name, status, badge_count, created_at, latest, auth_token, is_private) VALUES('lizrice/sub', 'SUBMITTED', 2, $1, '30000', 'lowercase', false)", now)
	db.Exec("INSERT INTO images (name, status, badge_count, created_at, latest, auth_token, is_private) VALUES('rossf7/badgerbadgerbadger', 'SITEMAP', 2, $1, '30000', 'lowercase', false)", now)
	db.Exec("INSERT INTO images (name, status, badge_count, created_at, latest, auth_token, is_private) VALUES('myuser/private', 'INSPECTED', 2, $1, '40000', 'latest', True)", now)
	db.Exec("INSERT INTO images (name, status, badge_count, created_at, latest, auth_token, is_private) VALUES('otheruser/private', 'INSPECTED', 2, $1, '50000', 'latest', True)", now)
	db.Exec("INSERT INTO images (name, status, badge_count, created_at, latest, auth_token, is_private) VALUES('rossf7/size', 'SIZE', 2, $1, '60000', 'lowercase', false)", now)
	db.Exec("INSERT INTO images (name, status, badge_count, created_at, latest, auth_token, is_private) VALUES('myuser/size', 'SIZE', 2, $1, '70000', 'lowercase', false)", now)
	db.Exec("INSERT INTO images (name, status, badge_count, latest, is_private) VALUES('another/parentimage', 'INSPECTED', 2, '80000', false)")

	db.Exec("INSERT INTO image_versions (sha, image_name, labels) VALUES('10000', 'lizrice/childimage', '{}')")
	db.Exec("INSERT INTO image_versions (sha, image_name, labels) VALUES('10001', 'lizrice/childimage', '{}')")
	db.Exec("INSERT INTO image_versions (sha, image_name, labels) VALUES('20000', 'library/official', '{}')")
	db.Exec("INSERT INTO image_versions (sha, image_name, labels) VALUES('20001', 'library/official', '{}')")
	db.Exec("INSERT INTO image_versions (sha, image_name, labels, created) VALUES('30000', 'lizrice/nolatest', '{}', $1)", earlier)
	db.Exec("INSERT INTO image_versions (sha, image_name, labels, created) VALUES('30001', 'lizrice/nolatest', '{}', $1)", now)
	db.Exec("INSERT INTO image_versions (sha, image_name, labels) VALUES('40000', 'myuser/private', '{\"org.label-schema.name\":\"private\"}')")
	db.Exec("INSERT INTO image_versions (sha, image_name, labels) VALUES('50000', 'otheruser/private', '{\"org.label-schema.name\":\"otherprivate\"}')")
	db.Exec("INSERT INTO image_versions (sha, image_name, labels, created) VALUES('60000', 'rossf7/size', '{}', $1)", now)
	db.Exec("INSERT INTO image_versions (sha, image_name, labels) VALUES('70000', 'myuser/size', '{}')")
	db.Exec("INSERT INTO image_versions (sha, image_name, labels) VALUES('80000', 'another/parentimage', '{}')")

	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('latest', 'lizrice/childimage', '10000')")
	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('same', 'lizrice/childimage', '10000')")
	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('specific', 'lizrice/childimage', '10001')")

	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('latest', 'library/official', '20000')")
	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('specific', 'library/official', '20001')")

	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('earlier', 'lizrice/nolatest', '30000')")
	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('recent', 'lizrice/nolatest', '30001')")

	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('latest', 'myuser/private', '40000')")
	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('latest', 'otheruser/private', '50000')")
	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('latest', 'rossf7/size', '60000')")
	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('latest', 'myuser/size', '70000')")

	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('latest', 'another/parentimage', '80000')")

}

func badgeThings() map[string]database.Image {
	// Note this doesn't have everything we set up above! Just the things we're testing for specifically
	var images = make(map[string]database.Image, 4)
	images["lizrice/childimage"] = database.Image{
		Versions: []database.ImageVersion{
			{SHA: "10000", Tags: []database.Tag{{Tag: "latest"}, {Tag: "same"}}},
			{SHA: "10001", Tags: []database.Tag{{Tag: "specific"}}},
		}}

	images["official"] = database.Image{
		Versions: []database.ImageVersion{
			{SHA: "20000", Tags: []database.Tag{{Tag: "latest"}}},
			{SHA: "20001", Tags: []database.Tag{{Tag: "specific"}}},
		}}

	images["lizrice/nolatest"] = database.Image{
		Versions: []database.ImageVersion{
			{SHA: "30000", Tags: []database.Tag{{Tag: "earlier"}}},
			{SHA: "30001", Tags: []database.Tag{{Tag: "recent"}}},
		}}

	images["rossf7/size"] = database.Image{
		Versions: []database.ImageVersion{
			{SHA: "60000", Tags: []database.Tag{{Tag: "latest"}}},
		}}

	return images
}

func TestGetBadges(t *testing.T) {

	type test struct {
		url     string
		status  int
		tag     string
		missing bool
	}

	// URL is /badges/<badgeType>/<org>/<repo>.svg for the latest tag
	// or     /badges/<badgeType>/<image>:<tag>.svg for a specific tag
	// or     /badges/<badgeType>/<image>.svg for library images

	// <badgeType> is one of image, commit, version, license

	// At the command line Docker insists that the image name is lower case
	// However, tags are case sensitive, e.g
	//
	// $ docker images lizrice/lowercase
	// REPOSITORY          TAG                 IMAGE ID            CREATED             SIZE
	// lizrice/lowercase   Mixed               af06c2834e82        7 weeks ago         4.797 MB
	// lizrice/lowercase   latest              af06c2834e82        7 weeks ago         4.797 MB
	// lizrice/lowercase   mixed               af06c2834e82        7 weeks ago         4.797 MB

	var tests = []test{
		// Good badges
		// If you don't specify a tag, and there is a latest, we prefer to use the longest other tag instead of "latest"
		{url: `/badges/<badgeType>/lizrice/childimage.svg`, status: 200, tag: "same"},
		{url: `/badges/<badgeType>/lizrice/childimage:specific.svg`, status: 200, tag: "specific"},
		{url: `/badges/<badgeType>/lizrice/childimage:latest.svg`, status: 200, tag: "latest"},
		{url: `/badges/<badgeType>/lizrice/childimage:same.svg`, status: 200, tag: "same"},
		{url: `/badges/<badgeType>/official.svg`, status: 200, tag: "latest"},
		{url: `/badges/<badgeType>/official:specific.svg`, status: 200, tag: "specific"},
		{url: `/badges/<badgeType>/lizrice/nolatest.svg`, status: 200, tag: "recent"},

		// Missing badges (i.e. where we serve a 'missing' badge)
		{url: `/badges/<badgeType>/lizrice/blah.svg`, status: 200, missing: true},

		// Tags are case sensitive so this is effectively missing
		{url: `/badges/<badgeType>/lizrice/childimage:Specific.svg`, status: 200, missing: true},

		// More missing badges
		{url: `/badges/<badgeType>/lizrice/childimage:blah.svg`, status: 200, missing: true},
		{url: `/badges/<badgeType>/blah.svg`, status: 200, missing: true},
		{url: `/badges/<badgeType>/official:blah.svg`, status: 200, missing: true},

		// Bad badge type
		{url: `/badges/blah/official.svg`, status: 400},
		{url: `/badges/blah/lizrice/childimage.svg`, status: 400},

		// Private image
		{url: `/badges/<badgeType>/microbadgertest/alpine.svg`, status: 200, missing: true},
	}

	db = getDatabase(t)
	emptyDatabase(db)
	addBadgeThings(db)

	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	for id, test := range tests {
		log.Debugf("-test %d----------", id)

		for _, bt := range []string{"image", "commit", "version", "license"} {
			log.Debugf("-- badge type %s----------", bt)
			url := strings.Replace(test.url, "<badgeType>", bt, 1)
			res, err := http.Get(ts.URL + url)
			if err != nil {
				t.Fatalf("Failed to send request #%d (%s) %v", id, url, err)
			}

			body, err := ioutil.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Fatalf("Error getting body. %v", err)
			}

			if res.StatusCode != test.status {
				t.Fatalf("#%d Bad things have happened. %d", id, res.StatusCode)
			}

			if test.status == 200 {
				ct := res.Header.Get("Content-Type")
				if ct != "image/svg+xml" {
					t.Errorf("#%d Content type is not as expected, have %s", id, ct)
				}

				if test.missing {
					if !strings.Contains(string(body), "not found") {
						t.Errorf("#%d image badge doesn't show not found (%s)", id, body)
					}
				} else {
					switch bt {
					case "image":
						// Image badge shows size and number of layers e.g. "18.6MB 9 layers"
						// TODO! Check the size is in there correctly
						if !strings.Contains(string(body), "layers") {
							t.Errorf("#%d image badge doesn't show layers: (%s)", id, body)
						}

					case "commit":
						// Commit badge shows git commit e.g. "commit eebf408"
						// TODO! Check commit is correct
						if !strings.Contains(string(body), "commit") {
							t.Errorf("#%d commit badge doesn't show commit: (%s)", id, body)
						}

					case "version":
						// Version badge shows version and tag name e.g. "version latest"
						if !strings.Contains(string(body), "version") {
							t.Errorf("#%d version badge doesn't show version: (%s)", id, body)
						}
						if !strings.Contains(string(body), test.tag) {
							t.Errorf("#%d version badge doesn't show correct tag: (%s)", id, body)
						}

					case "license":
						// License badge shows e.g. "license MIT"
						// TODO! Check license is correct
						if !strings.Contains(string(body), "license") {
							t.Errorf("#%d license badge doesn't show license: (%s)", id, body)
						}
					}
				}
			}
		}
	}
}

func TestGetBadgeLoggedIn(t *testing.T) {
	// TODO!!
	// t.Errorf("Add test to make sure badges aren't visible even if you're logged in and have access to that image")
}

func TestGetBadgeCounts(t *testing.T) {
	db = getDatabase(t)
	emptyDatabase(db)
	addBadgeThings(db)

	ts := httptest.NewServer(muxRoutes())
	defer ts.Close()

	type test struct {
		url    string
		status int
		badges int
		images int
	}

	var tests = []test{
		{url: `/v1/badges/counts`, status: 200, badges: 5, images: 2},
	}

	for id, test := range tests {
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
			t.Errorf("#%d Bad things have happened. %d", id, res.StatusCode)
		}

		if test.status == 200 {
			if !strings.Contains(string(body), fmt.Sprintf(`"badges":%d`, test.badges)) {
				t.Errorf("#%d Unexpected badge count in %s", id, body)
			}

			if !strings.Contains(string(body), fmt.Sprintf(`"images":%d`, test.images)) {
				t.Errorf("#%d Unexpected image count in %s", id, body)
			}

			ct := res.Header.Get("Content-Type")
			if ct != "application/javascript" {
				t.Errorf("#%d Content type is not as expected, have %s", id, ct)
			}
		}
	}
}
