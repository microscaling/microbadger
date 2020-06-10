// +build dbrequired

package database

import (
	"reflect"
	"testing"
)

type searchTestCase struct {
	search string
	images []string
}

type pageURLTestCase struct {
	image string
	url   string
}

func TestImageSearch(t *testing.T) {
	var db PgDB
	var tests = []searchTestCase{
		{search: "liz", images: []string{"lizrice/featured", "lizrice/childimage"}},
		{search: "rice", images: []string{"lizrice/featured", "lizrice/childimage"}},
		{search: "image", images: []string{"lizrice/childimage"}},
		{search: "lizrice/featured", images: []string{"lizrice/featured"}},
		{search: "micro", images: []string{}},
		{search: "myuser/private", images: []string{}},
		{search: "private", images: []string{}},
	}

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	for _, test := range tests {
		images, _ := db.ImageSearch(test.search)

		if len(images) > 0 || len(test.images) > 0 {
			if !reflect.DeepEqual(images, test.images) {
				t.Errorf("Expected search results to be %v but were %v", test.images, images)
			}
		}
	}
}

func TestFeaturedImages(t *testing.T) {
	var db PgDB

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	il := db.GetFeaturedImages()
	if (il.CurrentPage != 1) || (il.PageCount != 1) {
		t.Errorf("ImageList pagination wrong: %v", il)
	}

	if len(il.Images) != il.ImageCount {
		t.Errorf("Wrong image count %d but %d image names included", il.ImageCount, len(il.Images))
	}

	testImages := []string{"lizrice/featured"}
	if !reflect.DeepEqual(il.Images, testImages) {
		t.Errorf("Unexpected featured images: %v\n  expected: %v", il.Images, testImages)
	}
}

func TestRecentImages(t *testing.T) {
	var db PgDB

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	il := db.GetRecentImages()
	if (il.CurrentPage != 1) || (il.PageCount != 1) {
		t.Errorf("ImageList pagination wrong: %v", il)
	}

	if len(il.Images) != il.ImageCount {
		t.Errorf("Wrong image count %d but %d image names included", il.ImageCount, len(il.Images))
	}

	testImages := []string{"lizrice/childimage", "lizrice/featured"}
	if !reflect.DeepEqual(il.Images, testImages) {
		t.Errorf("Unexpected recent images: %v\n  expected: %v", il.Images, testImages)
	}
}

func TestLabelSchemaImages(t *testing.T) {
	var db PgDB

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	il := db.GetLabelSchemaImages(1)
	if (il.CurrentPage != 1) || (il.PageCount != 1) {
		t.Errorf("ImageList pagination wrong: %v", il)
	}

	if len(il.Images) != il.ImageCount {
		t.Errorf("Wrong image count %d but %d image names included", il.ImageCount, len(il.Images))
	}

	testImages := []string{"lizrice/childimage"}
	if !reflect.DeepEqual(il.Images, testImages) {
		t.Errorf("Unexpected label schema images: %v\n  expected: %v", il.Images, testImages)
	}
}

func TestGetImage(t *testing.T) {
	var db PgDB

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	img, err := db.GetImage("lizrice/featured")
	if err != nil {
		t.Errorf("failed to get image")
	}

	if img.Name != "lizrice/featured" {
		t.Errorf("Unexpected image name")
	}
}

func TestGetImageForUser(t *testing.T) {
	var db PgDB

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	img, permission, err := db.GetImageForUser("lizrice/featured", nil)
	if err != nil {
		t.Errorf("failed to get image")
	}

	if !permission {
		t.Errorf("Should have permission to get public image")
	}

	if img.Name != "lizrice/featured" {
		t.Errorf("Unexpected image name")
	}

	// Shouldn't be able to get a private image unless we have permissions for it	t.Logf("At the database level we are allowing access to private images without checking permissions?")
	img, permission, err = db.GetImageForUser("myuser/private", nil)
	if permission {
		t.Errorf("Shouldn't be able to get private image")
	}

	// User Image Permission tests in user_test.go check that we can only get images we have permission for
}

func TestFeatureImage(t *testing.T) {
	var db PgDB

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	// Can feature and unfeature an image
	err := db.FeatureImage("lizrice/childimage", true)
	if err != nil {
		t.Errorf("Failed to feature image")
	}

	// Can feature and unfeature an image
	err = db.FeatureImage("lizrice/childimage", false)
	if err != nil {
		t.Errorf("Failed to unfeature image")
	}

	// But not if it's private
	err = db.FeatureImage("myuser/private", true)
	if err == nil {
		t.Errorf("Shouldn't be able to feature private image")
	}
}

func TestGetPageURL(t *testing.T) {
	var db PgDB
	var tests = []pageURLTestCase{
		{image: "microbadgertest/alpine", url: "https://microbadger.com/registry/docker/images/microbadgertest/alpine"},
		{image: "microscaling/microscaling", url: "https://microbadger.com/images/microscaling/microscaling"},
		{image: "library/alpine", url: "https://microbadger.com/images/alpine"},
	}

	db = getDatabase(t)
	emptyDatabase(db)

	db.Exec("INSERT INTO images (name, status, is_private) VALUES('microbadgertest/alpine', 'INSPECTED', true)")
	db.Exec("INSERT INTO images (name, status, is_private) VALUES('microscaling/microscaling', 'INSPECTED', false)")
	db.Exec("INSERT INTO images (name, status, is_private) VALUES('library/alpine', 'INSPECTED', false)")

	for _, test := range tests {
		img, _ := db.GetImage(test.image)
		url := db.GetPageURL(img)

		if url != test.url {
			t.Errorf("Unexpected image URL. Expected %s but was %s", test.url, url)
		}
	}
}

func TestDeleteImage(t *testing.T) {
	var d PgDB
	var rows int

	d = getDatabase(t)
	emptyDatabase(d)
	addThings(d)

	var images = []string{"lizrice/childimage", "lizrice/featured", "myuser/private", "public/sitemap"}

	for _, image := range images {
		err := d.DeleteImage(image)
		if err != nil {
			t.Errorf("Unexpected error deleting image %s - %v", image, err)
		}
	}

	d.db.Table("images").Count(&rows)
	if rows != 0 {
		t.Errorf("Found %d unexpected rows in images", rows)
	}

	d.db.Table("image_versions").Count(&rows)
	if rows != 0 {
		t.Errorf("Found %d unexpected rows in image_versions", rows)
	}

	d.db.Table("tags").Count(&rows)
	if rows != 0 {
		t.Errorf("Found %d unexpected rows in tags", rows)
	}
}
