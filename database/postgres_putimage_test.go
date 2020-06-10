// +build dbrequired

package database

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"
)

func addTagThings(db PgDB) {
	db.Exec("INSERT INTO images (name, status, latest) VALUES('lizrice/childimage', 'INSPECTED', '10000')")

	db.Exec("INSERT INTO image_versions (sha, image_name, labels) VALUES('10000', 'lizrice/childimage', '{}')")
	db.Exec("INSERT INTO image_versions (sha, image_name, labels) VALUES('10001', 'lizrice/childimage', '{}')")

	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('latest', 'lizrice/childimage', '10000')")
	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('same', 'lizrice/childimage', '10000')")
	db.Exec("INSERT INTO tags (tag, image_name, sha) VALUES('specific', 'lizrice/childimage', '10001')")
}

// changeTags is a test function that takes the current image state from the DB and modifies it according to what the
// changes are supposed to be. This is only changing the data structure, where what we're doing in the real code is
// changing the database.  In the tests we read the result back out of the database and check that it matches what this
// function built.
func changeTags(db PgDB, newTags []Tag, changedTags []Tag, deletedTags []Tag) (Image, error) {
	imageName := "lizrice/childimage"
	img, err := db.GetImage(imageName)
	if err != nil {
		log.Errorf("Failed to get image %s: %v", imageName, err)
		return img, err
	}

	img.Versions, err = db.GetImageVersions(img)
	if err != nil {
		log.Errorf("Failed to get image versions: %v", err)
		return img, err
	}

	// Get the current set of tags for this version
	for i, iv := range img.Versions {
		img.Versions[i].Tags, err = db.GetTags(&iv)
		if err != nil && err.Error() != "record not found" {
			log.Errorf("Failed to get tags: %v", err)
			return img, err
		}
	}

	// Delete any versions tags we're suppose to be deleting
	for _, dt := range deletedTags {
		for i, v := range img.Versions {
			if v.SHA == dt.SHA {
				// Use a download loop because we're deleting from it
				for j := len(v.Tags) - 1; j >= 0; j-- {
					tt := v.Tags[j]
					if tt.Tag == dt.Tag {
						log.Debugf("[Test] Deleting tag %s from version %s ", dt.Tag, dt.SHA)
						v.Tags = append(v.Tags[:j], v.Tags[j+1:]...)
						img.Versions[i] = v
					}
				}
			}
		}
	}

	// Change any tags we're supposed to be changing
	for _, ct := range changedTags {
		found := false
		for i, v := range img.Versions {
			// Use a download loop because we're deleting from it
			for j := len(v.Tags) - 1; j >= 0; j-- {
				tt := v.Tags[j]
				if tt.Tag == ct.Tag {
					log.Debugf("[Test] Deleting changed tag %s from version %s ", ct.Tag, tt.SHA)
					v.Tags = append(v.Tags[:j], v.Tags[j+1:]...)
					img.Versions[i] = v
				}
			}

			// Add the tag to its new version
			if ct.SHA == v.SHA {
				found = true
				log.Debugf("[Test] moving tag %s to version %s", ct.Tag, v.SHA)
				v.Tags = append(v.Tags, ct)
				img.Versions[i] = v
			}
		}

		if !found {
			// We may need to add the new version with the hew tag
			log.Debugf("[Test] Adding new version %s with tag %s", ct.SHA, ct.Tag)

			v := ImageVersion{SHA: ct.SHA, ImageName: ct.ImageName}
			v.Tags = append(v.Tags, ct)
			img.Versions = append(img.Versions, v)
		}
	}

	// Add any new tags
	for _, at := range newTags {
		found := false
		for i, v := range img.Versions {
			if v.SHA == at.SHA {
				found = true
				log.Debugf("[Test] Adding new tag %s to version %s ", at.Tag, at.SHA)
				v.Tags = append(v.Tags, at)
				img.Versions[i] = v
			}
		}
		// If we get here this must be a new version
		if !found {
			log.Debugf("[Test] Adding new version %s with tag %s", at.SHA, at.Tag)
			v := ImageVersion{SHA: at.SHA, ImageName: at.ImageName}
			v.Tags = append(v.Tags, at)
			img.Versions = append(img.Versions, v)
		}
	}

	// If there are any versions left with no tags, remove them from what we would have got from inspection
	for i := len(img.Versions) - 1; i >= 0; i-- {
		v := img.Versions[i]
		if len(v.Tags) == 0 {
			log.Debugf("[Test] Deleting version %s", v.SHA)
			img.Versions = append(img.Versions[:i], img.Versions[i+1:]...)
		}
	}

	// We should now have an image that reflects what we might have got from a reinspection
	return img, nil
}

func initialImage(db PgDB, newTags []Tag, changedTags []Tag, deletedTags []Tag) (Image, error) {
	img, err := db.GetImage("lizrice/childimage")
	if err != nil {
		return img, fmt.Errorf("Failed getting image: %v", err)
	}
	img.Versions, err = db.GetAllImageVersions(img)
	if err != nil {
		return img, fmt.Errorf("Failed getting image versions: %v", err)
	}
	for i, iv := range img.Versions {
		img.Versions[i].Tags, err = db.GetTags(&iv)
		if err != nil {
			return img, fmt.Errorf("Failed getting tags for image versions: %v", err)
		}
	}
	return img, nil
}

func TestPutImage(t *testing.T) {
	var db PgDB

	db = getDatabase(t)
	emptyDatabase(db)
	addTagThings(db)

	testCases := []struct {
		name        string
		updateImage func(PgDB, []Tag, []Tag, []Tag) (Image, error)
		newTags     []Tag
		changedTags []Tag
		deletedTags []Tag
		result      []ImageVersion
	}{
		// Check it's all ok to start with
		{name: "init", updateImage: initialImage,
			newTags: []Tag{}, changedTags: []Tag{}, deletedTags: []Tag{},
			result: []ImageVersion{
				{SHA: "10000", Tags: []Tag{{Tag: "latest"}, {Tag: "same"}}},
				{SHA: "10001", Tags: []Tag{{Tag: "specific"}}},
			},
		},

		// Add a new version with one tag - change is an added tag
		{name: "Add", updateImage: changeTags,
			newTags:     []Tag{{Tag: "Tag1", ImageName: "lizrice/childimage", SHA: "10003"}},
			changedTags: []Tag{},
			deletedTags: []Tag{},
			result: []ImageVersion{
				{SHA: "10000", Tags: []Tag{{Tag: "latest"}, {Tag: "same"}}},
				{SHA: "10001", Tags: []Tag{{Tag: "specific"}}},
				{SHA: "10003", Tags: []Tag{{Tag: "Tag1"}}},
			}},

		// Add a new version with two tags - two changes
		{name: "Add-two", updateImage: changeTags,
			newTags: []Tag{
				{Tag: "Tag2", ImageName: "lizrice/childimage", SHA: "10004"},
				{Tag: "Tag3", ImageName: "lizrice/childimage", SHA: "10004"}},
			changedTags: []Tag{},
			deletedTags: []Tag{},
			result: []ImageVersion{
				{SHA: "10000", Tags: []Tag{{Tag: "latest"}, {Tag: "same"}}},
				{SHA: "10001", Tags: []Tag{{Tag: "specific"}}},
				{SHA: "10003", Tags: []Tag{{Tag: "Tag1"}}},
				{SHA: "10004", Tags: []Tag{{Tag: "Tag2"}, {Tag: "Tag3"}}},
			}},

		// Add a new tag to an existing version
		{name: "Add-existing", updateImage: changeTags,
			newTags: []Tag{
				{Tag: "Tag4", ImageName: "lizrice/childimage", SHA: "10001"}},
			changedTags: []Tag{},
			deletedTags: []Tag{},
			result: []ImageVersion{
				{SHA: "10000", Tags: []Tag{{Tag: "latest"}, {Tag: "same"}}},
				{SHA: "10001", Tags: []Tag{{Tag: "specific"}, {Tag: "Tag4"}}},
				{SHA: "10003", Tags: []Tag{{Tag: "Tag1"}}},
				{SHA: "10004", Tags: []Tag{{Tag: "Tag2"}, {Tag: "Tag3"}}},
			}},

		// Delete an existing tag
		{name: "Del-existing", updateImage: changeTags,
			newTags:     []Tag{},
			changedTags: []Tag{},
			deletedTags: []Tag{
				{Tag: "Tag3", ImageName: "lizrice/childimage", SHA: "10004"}},
			result: []ImageVersion{
				{SHA: "10000", Tags: []Tag{{Tag: "latest"}, {Tag: "same"}}},
				{SHA: "10001", Tags: []Tag{{Tag: "specific"}, {Tag: "Tag4"}}},
				{SHA: "10003", Tags: []Tag{{Tag: "Tag1"}}},
				{SHA: "10004", Tags: []Tag{{Tag: "Tag2"}}},
			}},

		// Delete tags from two versions
		{name: "Del-two", updateImage: changeTags,
			newTags:     []Tag{},
			changedTags: []Tag{},
			deletedTags: []Tag{
				{Tag: "same", ImageName: "lizrice/childimage", SHA: "10000"},
				{Tag: "Tag4", ImageName: "lizrice/childimage", SHA: "10001"},
			}, result: []ImageVersion{
				{SHA: "10000", Tags: []Tag{{Tag: "latest"}}},
				{SHA: "10001", Tags: []Tag{{Tag: "specific"}}},
				{SHA: "10003", Tags: []Tag{{Tag: "Tag1"}}},
				{SHA: "10004", Tags: []Tag{{Tag: "Tag2"}}},
			}},

		// Delete a version
		{name: "Del-version", updateImage: changeTags,
			newTags:     []Tag{},
			changedTags: []Tag{},
			deletedTags: []Tag{
				{Tag: "Tag1", ImageName: "lizrice/childimage", SHA: "10003"}},
			result: []ImageVersion{
				{SHA: "10000", Tags: []Tag{{Tag: "latest"}}},
				{SHA: "10001", Tags: []Tag{{Tag: "specific"}}},
				{SHA: "10003", Tags: []Tag{}},
				{SHA: "10004", Tags: []Tag{{Tag: "Tag2"}}},
			}},

		// Add a new tag to an exsting version and add a new tag to a new version - two added tag changes
		{name: "Add-mixed", updateImage: changeTags,
			newTags: []Tag{
				{Tag: "Tag5", ImageName: "lizrice/childimage", SHA: "10001"},
				{Tag: "Tag6", ImageName: "lizrice/childimage", SHA: "10005"}},
			changedTags: []Tag{},
			deletedTags: []Tag{},
			result: []ImageVersion{
				{SHA: "10000", Tags: []Tag{{Tag: "latest"}}},
				{SHA: "10001", Tags: []Tag{{Tag: "specific"}, {Tag: "Tag5"}}},
				{SHA: "10003", Tags: []Tag{}},
				{SHA: "10004", Tags: []Tag{{Tag: "Tag2"}}},
				{SHA: "10005", Tags: []Tag{{Tag: "Tag6"}}},
			}},

		// Update the version for one tag
		{name: "Change", updateImage: changeTags,
			newTags: []Tag{},
			changedTags: []Tag{
				{Tag: "latest", ImageName: "lizrice/childimage", SHA: "10006"}},
			deletedTags: []Tag{},
			result: []ImageVersion{
				{SHA: "10000", Tags: []Tag{}},
				{SHA: "10001", Tags: []Tag{{Tag: "specific"}, {Tag: "Tag5"}}},
				{SHA: "10003", Tags: []Tag{}},
				{SHA: "10004", Tags: []Tag{{Tag: "Tag2"}}},
				{SHA: "10005", Tags: []Tag{{Tag: "Tag6"}}},
				{SHA: "10006", Tags: []Tag{{Tag: "latest"}}},
			}},

		// Delete a tag and add a new tag to an existing version - one delete, one add
		{name: "Change", updateImage: changeTags,
			newTags: []Tag{
				{Tag: "Tag7", ImageName: "lizrice/childimage", SHA: "10000"}},
			changedTags: []Tag{},
			deletedTags: []Tag{
				{Tag: "Tag5", ImageName: "lizrice/childimage", SHA: "10001"},
			}, result: []ImageVersion{
				{SHA: "10000", Tags: []Tag{{Tag: "Tag7"}}},
				{SHA: "10001", Tags: []Tag{{Tag: "specific"}}},
				{SHA: "10003", Tags: []Tag{}},
				{SHA: "10004", Tags: []Tag{{Tag: "Tag2"}}},
				{SHA: "10005", Tags: []Tag{{Tag: "Tag6"}}},
				{SHA: "10006", Tags: []Tag{{Tag: "latest"}}},
			}},

		// Change a tag and add a new tag to an existing version
		{name: "Change", updateImage: changeTags,
			changedTags: []Tag{
				{Tag: "Tag7", ImageName: "lizrice/childimage", SHA: "10001"}},
			newTags: []Tag{
				{Tag: "Tag8", ImageName: "lizrice/childimage", SHA: "10001"}},
			deletedTags: []Tag{},
			result: []ImageVersion{
				{SHA: "10000", Tags: []Tag{}},
				{SHA: "10001", Tags: []Tag{{Tag: "specific"}, {Tag: "Tag7"}, {Tag: "Tag8"}}},
				{SHA: "10003", Tags: []Tag{}},
				{SHA: "10004", Tags: []Tag{{Tag: "Tag2"}}},
				{SHA: "10005", Tags: []Tag{{Tag: "Tag6"}}},
				{SHA: "10006", Tags: []Tag{{Tag: "latest"}}},
			}},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf(tc.name), func(t *testing.T) {
			log.Debugf("=================Test %s==================", tc.name)
			img, err := tc.updateImage(db, tc.newTags, tc.changedTags, tc.deletedTags)
			if img.Name == "" || err != nil {
				t.Fatalf("Failed to update test case image for %s: %v", tc.name, err)
			}

			// We are testing that PutImage has the desired effect
			log.Debugf("-----------Running PutImage for %s-------", tc.name)
			nmc, err := db.PutImage(img)
			log.Debugf("-----------PutImage for %s done-------", tc.name)
			if err != nil {
				t.Fatalf("Failed to put image: %s", err)
			}

			if nmc.ImageName != "lizrice/childimage" {
				t.Errorf("Didn't include image name in NMC")
			}

			if len(nmc.NewTags) != len(tc.newTags) ||
				len(nmc.ChangedTags) != len(tc.changedTags) ||
				len(nmc.DeletedTags) != len(tc.deletedTags) {
				t.Errorf("Should have %d new, %d changed, %d deleted tags", len(tc.newTags), len(tc.changedTags), len(tc.deletedTags))
				t.Errorf("But got     %d new, %d changed, %d deleted tags", len(nmc.NewTags), len(nmc.ChangedTags), len(nmc.DeletedTags))
			}

			sort.Sort(Tags(nmc.NewTags))
			sort.Sort(Tags(nmc.ChangedTags))
			sort.Sort(Tags(nmc.DeletedTags))
			sort.Sort(Tags(tc.newTags))
			sort.Sort(Tags(tc.changedTags))
			sort.Sort(Tags(tc.deletedTags))

			if !reflect.DeepEqual(nmc.NewTags, tc.newTags) {
				t.Errorf("Unexpected new tags %v", nmc.NewTags)
			}

			if !reflect.DeepEqual(nmc.ChangedTags, tc.changedTags) {
				t.Errorf("Unexpected changed tags %v", nmc.ChangedTags)
			}

			if !reflect.DeepEqual(nmc.DeletedTags, tc.deletedTags) {
				t.Errorf("Unexpected deleted tags %v", nmc.DeletedTags)
			}

			// Get the updated image versions for this image from the DB so we can check the result is what we epected
			// Note that this includes the untagged ones (which we wouldn't return in the API)
			ivlist, err := db.GetAllImageVersions(img)
			if err != nil {
				t.Fatalf("Error getting the image versions: %v", err)
			}

			for i, iv := range ivlist {
				ivlist[i].Tags, err = db.GetTags(&iv)
				if err != nil {
					t.Fatalf("Error getting the versions' tags: %v", err)
				}

				// Get rid of things we can't really compare
				ivlist[i].Created = time.Time{}
				// TODO!! Is this really supposed to be {} for empty?
				ivlist[i].Labels = ""
			}

			// Put the imagename & SHA into the Tags in the expected result so that we can do a deep compare
			for i, ri := range tc.result {
				ri.ImageName = "lizrice/childimage"
				for j, rt := range ri.Tags {
					rt.ImageName = ri.ImageName
					rt.SHA = ri.SHA
					ri.Tags[j] = rt
				}
				tc.result[i] = ri
			}

			sort.Sort(ImageVersions(tc.result))
			sort.Sort(ImageVersions(ivlist))
			for i := range tc.result {
				sort.Sort(Tags(tc.result[i].Tags))
			}

			for i := range ivlist {
				sort.Sort(Tags(ivlist[i].Tags))
			}

			expectedBytes, _ := json.Marshal(tc.result)
			gotBytes, err := json.Marshal(ivlist)
			if err != nil {
				t.Fatalf("Error comparing json: %v", err)
			}

			// Can't directly compare the output because the ordering of tags might be different
			if len(expectedBytes) != len(gotBytes) {
				t.Errorf("Something is different")
				t.Errorf("Exp: %v", string(expectedBytes))
				t.Errorf("Got: %v", string(gotBytes))
			}

			if len(tc.result) != len(ivlist) {
				t.Fatalf("Wrong number of versions: got %d expected %d", len(ivlist), len(tc.result))
			}

			for i := range tc.result {
				if tc.result[i].SHA != ivlist[i].SHA {
					t.Errorf("SHAs don't match for expected %s", tc.result[i].SHA)
				}

				if len(tc.result[i].Tags) != len(ivlist[i].Tags) {
					t.Errorf("Tags are wrong for index %d", i)
					t.Errorf("%v", tc.result[i].Tags)
					t.Errorf("%v", ivlist[i].Tags)
				}
			}

			// Check that the image is still there in the database
			_, err = db.GetImage("lizrice/childimage")
			if err != nil {
				t.Fatalf("Couldn't get the image! %v", err)
			}
		})
	}
}

// For sorting
type ImageVersions []ImageVersion
type Tags []Tag

func (slice ImageVersions) Len() int {
	return len(slice)
}

func (slice ImageVersions) Less(i, j int) bool {
	return slice[i].SHA < slice[j].SHA
}

func (slice ImageVersions) Swap(i, j int) {
	slice[i], slice[j] = slice[j], slice[i]
}

func (slice Tags) Len() int {
	return len(slice)
}

func (slice Tags) Less(i, j int) bool {
	return slice[i].Tag < slice[j].Tag
}

func (slice Tags) Swap(i, j int) {
	slice[i], slice[j] = slice[j], slice[i]
}
