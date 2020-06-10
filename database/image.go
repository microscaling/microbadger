package database

import (
	"fmt"
	"strings"
	"time"

	"github.com/jinzhu/gorm"
)

const (
	constDockerRegistryPath = "/registry/docker"
	constImagePagePath      = "/images/"
)

// GetImage gets an image from the database. Error if it's not there
// Use GetImageForUser unless you are very sure this is what you want
func (d *PgDB) GetImage(image string) (img Image, err error) {
	err = d.db.Where("name = ?", image).First(&img).Error
	if err != nil {
		log.Debugf("Error getting image name %s: %v", image, err)
	}
	return img, err
}

// GetImageForUser gets an image from the database, checking that the user has permission. Error if it's not there
func (d *PgDB) GetImageForUser(image string, u *User) (img Image, permission bool, err error) {
	var ok bool
	err = d.db.Where("name = ?", image).First(&img).Error
	if err != nil {
		// We don't know about this image yet, so we'll assume it's public until we know better
		log.Debugf("Error getting image name %s: %v", image, err)
		return img, true, err
	}

	// For a private image, check that the permissions are OK
	if img.IsPrivate {
		ok, err = d.CheckUserHasImagePermission(u, &img)
		if !ok {
			return Image{}, ok, err
		}
	}

	return img, true, err
}

// GetOrCreateImage makes sure it's there
func (d *PgDB) GetOrCreateImage(image string) (img Image, err error) {
	err = d.db.Where(Image{Name: image}).FirstOrCreate(&img).Error
	return img, err
}

// PutImage saves an image and its related image versions & tags
func (d *PgDB) PutImage(img Image) (nmc NotificationMessageChanges, err error) {
	var oldTags []Tag
	newTags := make([]Tag, 0)
	changedTags := make([]Tag, 0)
	deletedTags := make([]Tag, 0)

	if img.Name == "" {
		err = fmt.Errorf("Called PutImage with image that has no name")
		return
	}

	// Save everything within a transaction
	tx := d.db.Begin()

	err = tx.Table("tags").Where("image_name = ?", img.Name).Find(&oldTags).Error
	if err != nil && err.Error() != "record not found" {
		log.Debugf("Error retrieving tags for image %s: %v", img.Name, err)
		tx.Rollback()
		return
	}

	log.Debugf("There are currently %d tags", len(oldTags))
	for _, t := range oldTags {
		log.Debugf("%v", t)
	}

	err = tx.Save(&img).Error
	if err != nil {
		log.Debugf("Error saving image %s: %v", img.Name, err)
		tx.Rollback()
		return
	}

	// Don't use Gorm to handle saving the image version or the tags - we do this ourselves
	for _, v := range img.Versions {
		// Even if image versions are deleted from the registry (untagged?) we need to hold on to a record,
		// as people might be using them or have other containers built on top of them.

		// Find this version if it already exists, or create it
		log.Debugf("Saving version with SHA %s", v.SHA)
		var ev ImageVersion
		err = tx.Where(ImageVersion{ImageName: v.ImageName, SHA: v.SHA}).FirstOrCreate(&ev).Error
		if err != nil {
			log.Errorf("Error creating image version: %v", err)
			tx.Rollback()
			return
		}

		err = tx.Save(&v).Error
		if err != nil {
			log.Errorf("Error saving image version: %v", err)
			tx.Rollback()
			return
		}

		// Find changes in tags on this version
		log.Debugf("We now know about %d tags for version %s", len(v.Tags), v.SHA)
		for _, t := range v.Tags {
			log.Debugf("Looking for changes related to %s on version %s", t.Tag, t.SHA)

			// Does this tag already exist on this image? (doesn't have to be on the same SHA)
			found := false

			// Use a downward loop so we can delete items out of the slice
			// TODO!! There might be a nicer way to do this? Maybe have a map?
			for o := len(oldTags) - 1; o >= 0; o-- {
				ot := oldTags[o]
				if ot.Tag == t.Tag {
					found = true
					log.Debugf("Tag %s existed before", ot.Tag)

					// Remove this from the list of old tags
					oldTags = append(oldTags[:o], oldTags[o+1:]...)

					// But did it represent the same version?
					if ot.SHA != v.SHA {
						log.Debugf("Existing tag %s was on a different SHA %s", ot.Tag, ot.SHA)
						changedTags = append(changedTags, t)

						err = tx.Model(&t).Where(Tag{ImageName: v.ImageName, Tag: t.Tag}).Update("sha", v.SHA).Error
						if err != nil {
							log.Errorf("Error saving  for tag %s: %v", t.Tag, err)
							tx.Rollback()
							return
						}
					}
				}
			}

			if !found {
				// Tag didn't exist previously
				log.Debugf("Tag %s is new", t.Tag)
				newTags = append(newTags, t)

				err = tx.Create(&t).Error
				if err != nil {
					log.Errorf("Error saving  for tag %s: %v", t.Tag, err)
					tx.Rollback()
					return
				}
			}
		}
	}

	// If there's anything left in the old tags list, it must have been deleted
	log.Debugf("These tags were deleted: %v", oldTags)
	deletedTags = append(deletedTags, oldTags...)
	for _, ot := range oldTags {
		log.Debugf("Deleting tag %s for version %s of %s", ot.Tag, ot.SHA, ot.ImageName)
		err = tx.Where(ot).Delete(Tag{}).Error
		if err != nil {
			log.Errorf("Error deleting tag %s: %v", ot.Tag, err)
			tx.Rollback()
			return
		}
	}

	// I'm not totally happy that the text for this is hiding away in the database code, but there you are
	nmc = NotificationMessageChanges{
		ImageName:   strings.TrimPrefix(img.Name, "library/"),
		NewTags:     newTags,
		ChangedTags: changedTags,
		DeletedTags: deletedTags,
	}

	imageURL := d.GetPageURL(img)
	nmc.Text = fmt.Sprintf("MicroBadger: Docker Hub image %s has changed %s", nmc.ImageName, imageURL)

	tx.Commit()
	return
}

// PutImageOnly saves just the image, none of its related image versions or tags
func (d *PgDB) PutImageOnly(img Image) error {
	return d.db.Save(&img).Error
}

// DeleteImage deletes it including all versions and tags
func (d *PgDB) DeleteImage(image string) (err error) {
	tx := d.db.Begin()
	err = deleteImage(image, tx)
	if err != nil {
		tx.Rollback()
		return err
	}

	tx.Commit()
	log.Debugf("Deleted image %s", image)
	return
}

// deleteImage does the deletes inside a transaction
func deleteImage(image string, tx *gorm.DB) (err error) {
	var versions []ImageVersion
	var tags []Tag

	err = tx.Where("image_name = ?", image).Find(&versions).Error
	if err != nil {
		log.Errorf("Error getting versions for image %s - %v", image, err)
		return
	}

	for _, v := range versions {
		err = tx.Delete(ImageVersion{SHA: v.SHA, ImageName: v.ImageName}).Error
		if err != nil {
			log.Errorf("Error deleting image version %v", err)
			return
		}
	}

	err = tx.Where("image_name = ?", image).Find(&tags).Error
	if err != nil {
		log.Errorf("Error getting tags for image %s - %v", image, err)
		return
	}

	for _, t := range tags {
		err = tx.Where(t).Delete(Tag{}).Error
		if err != nil {
			log.Errorf("Error deleting tag %v", err)
			return
		}
	}

	err = tx.Delete(Image{Name: image}).Error
	if err != nil {
		log.Errorf("Error deleting image %v", err)
	}

	return
}

// GetFeaturedImages returns a list of images with the featured flag set
func (d *PgDB) GetFeaturedImages() ImageList {
	log.Debug("Getting featured images")
	var images []string
	var pageCount int

	err := d.db.Table("images").
		Where("featured = true and is_private is not true and status = 'INSPECTED'").
		Select("name").Pluck("name", &images).Error

	if err != nil {
		log.Errorf("Error getting featured images: %v", err)
	}

	log.Debugf("Database: Featured images: %v", images)

	if len(images) > constImagesPerPage {
		log.Errorf("We have %d featured images, which is more than one page", len(images))
	}

	if len(images) > 0 {
		pageCount = 1
	}

	return ImageList{
		CurrentPage: 1,
		PageCount:   pageCount,
		ImageCount:  len(images),
		Images:      images,
	}
}

// GetRecentImages returns a list of public images with badges created in the last so-many days
func (d *PgDB) GetRecentImages() ImageList {
	log.Debug("Getting recent images")
	var name []string
	var pageCount int

	// Calculate the start date for the scan.
	dur := constRecentImagesDays * -24 * time.Hour
	since := time.Now().UTC().Add(dur)
	err := d.db.Table("images").
		Where("created_at > ? and badge_count > 1 and is_private is not True and status = 'INSPECTED'", since).
		Order("created_at DESC").Limit(constDisplayMaxImages).
		Select("name").Pluck("name", &name).Error

	if err != nil {
		log.Errorf("Error getting recent images: %v", err)
	}

	log.Debugf("Database: Recent images: %v", name)

	if len(name) > constImagesPerPage {
		log.Errorf("We have %d recent images, which is more than one page", len(name))
	}

	if len(name) > 0 {
		pageCount = 1
	}

	return ImageList{
		CurrentPage: 1,
		PageCount:   pageCount,
		ImageCount:  len(name),
		Images:      name,
	}
}

// GetImageVersions only returns versions that have a tag
func (d *PgDB) GetImageVersions(img Image) (ivlist []ImageVersion, err error) {
	if len(img.Name) == 0 {
		err = fmt.Errorf("Can't get image versions if there's no image name")
		return
	}
	err = d.db.Where("image_name = ? and exists(select * from tags where image_name = ? and sha = image_versions.sha)",
		img.Name, img.Name).Find(&ivlist).Error
	if err != nil {
		log.Errorf("Error getting image versions: %v", err)
	}

	return ivlist, err
}

// GetAllImageVersions returns all versions for this image, tagged or not
func (d *PgDB) GetAllImageVersions(img Image) (ivlist []ImageVersion, err error) {
	err = d.db.Where("image_name = ?", img.Name).
		Find(&ivlist).Error
	if err != nil {
		log.Errorf("Error getting image versions: %v", err)
	}

	return ivlist, err
}

// GetAllImageVersionsWithManifests returns all versions for this image, tagged
// or not if they have manifest data. The manifest data is cleared once it has
// been processed.
func (d *PgDB) GetAllImageVersionsWithManifests(img Image) (ivlist []ImageVersion, err error) {
	err = d.db.Where("image_name = ? and manifest != ''", img.Name).
		Find(&ivlist).Error
	if err != nil {
		log.Errorf("Error getting image versions: %v", err)
	}

	return ivlist, err
}

func (d *PgDB) GetImageVersionBySHA(SHA string, image string, isPrivate bool) (iv ImageVersion, err error) {
	// TODO!! Need to query by registry and image name when registry is added to the images table
	// TODO!! Checking for "isPrivate" is really just shorthand for this
	// TODO!! Permissions check should be moved from the API handler to here
	// as it would be more robust
	err = d.db.Table("image_versions").
		Joins("JOIN images ON image_versions.image_name = images.name").
		Where("image_versions.sha = ? and image_versions.image_name = ? and images.is_private = ?", SHA, image, isPrivate).
		First(&iv).Error
	if err != nil {
		log.Errorf("Error getting image versions: %v", err)
	}

	return iv, err
}

// GetImageVersionsByHash returns image versions that match this hash but not this SHA or image name
func (d *PgDB) GetImageVersionsByHash(hash string, SHA string, imageName string) (ivs []ImageVersion, err error) {
	err = d.db.Where("hash = ? AND sha <> ? AND image_name <> ?", hash, SHA, imageName).
		Find(&ivs).Error
	if err != nil {
		log.Errorf("Error getting image versions: %v", err)
	}

	log.Debugf("Found %d other image versions with matching hash", len(ivs))
	return ivs, err
}

func (d *PgDB) PutImageVersion(iv ImageVersion) (err error) {
	return d.db.Save(&iv).Error
}

func (d *PgDB) FeatureImage(image string, featured bool) (err error) {
	log.Debugf("Changing feature status to %v for image %s", featured, image)
	img, err := d.GetImage(image)
	if err != nil {
		return
	}

	if img.IsPrivate {
		return fmt.Errorf("Can't feature private image %s", img.Name)
	}

	err = d.db.Model(&img).Update("featured", featured).Error
	return
}

func (d *PgDB) GetTags(iv *ImageVersion) (tags []Tag, err error) {
	log.Debugf("Getting tags for version %s of %s", iv.SHA, iv.ImageName)

	err = d.db.Where("sha = ? and image_name = ?", iv.SHA, iv.ImageName).
		Find(&tags).Error

	log.Debugf("Found %d tags for version %s of %s", len(tags), iv.SHA, iv.ImageName)

	return tags, err
}

// GetTagsList only fills in the tag name, so that when we pass the structure on the API it only includes the tags
func (d *PgDB) GetTagsList(iv *ImageVersion) (tags []Tag, err error) {
	log.Debugf("Getting tags list for version %s of %s", iv.SHA, iv.ImageName)

	err = d.db.Table("tags").
		Where("sha = ? and image_name = ?", iv.SHA, iv.ImageName).
		Select("tag").
		Scan(&tags).Error

	log.Debugf("Found %d tags for version %s of %s", len(tags), iv.SHA, iv.ImageName)

	return tags, err
}

// Return true if either download size is 0 or layers is '' or the hash has not yet been calculated
func (d *PgDB) ImageVersionNeedsSizeOrLayers(iv *ImageVersion) bool {
	row := d.db.
		Raw("SELECT download_size=0 as dd, layers='' as ll, hash as hh from image_versions where sha = ? and image_name = ?", iv.SHA, iv.ImageName).
		Row()

	var dd bool
	var ll bool
	var hh []byte

	row.Scan(&dd, &ll, &hh)
	log.Debugf("Need download size? %v  Need layers? %v, Length of hash? %d", dd, ll, len(hh))
	return dd || ll || (len(hh) == 0)
}

func (d *PgDB) GetImageVersionByTag(image string, tag string) (iv ImageVersion, err error) {
	err = d.db.Table("image_versions").
		Joins("JOIN tags ON image_versions.image_name = tags.image_name AND image_versions.sha = tags.sha").
		Where("image_versions.image_name = ? AND tags.tag = ?", image, tag).
		First(&iv).Error
	return iv, err
}

// GetBadgesInstalledCount tells us how many badges are installed, and how many images have badges installed.
// At the moment these are only taken from the full descriptions in Docker Hub. Badges for automated images
// are counted twice because they also appear in the user's GitHub readme.
func (d *PgDB) GetBadgesInstalledCount() (badges int, images int, err error) {
	badges, images, err = d.getBadgesInstalledCount(false)
	if err != nil {
		log.Errorf("Failed to get badge count for non automated images - %v", err)
		return badges, images, err
	}

	b, i, err := d.getBadgesInstalledCount(true)
	if err != nil {
		log.Errorf("Failed to get badge count for automated images - %v", err)
		return badges, images, err
	}

	// Count automated images twice for GitHub.
	badges += (b * 2)
	images += i

	return badges, images, err
}

func (d *PgDB) getBadgesInstalledCount(isAutomated bool) (badges int, images int, err error) {
	type Result struct {
		Badges int
		Images int
	}

	var results Result
	err = d.db.Table("images").
		Select("sum(badges_installed) as badges, count(badges_installed) as images").
		Where("badges_installed > 0 AND is_automated = ?", isAutomated).
		Scan(&results).Error

	return results.Badges, results.Images, err
}

func (d *PgDB) GetLabelSchemaImages(pageNum int) ImageList {
	log.Debug("Getting label schema images")
	var name []string

	offset := (pageNum - 1) * constImagesPerPage
	log.Debugf("Getting page %d with offset %d", pageNum, offset)

	// We are only returning public images on this query
	err := d.db.Table("image_versions").
		Where("labels LIKE '%org.label-schema.%' AND is_private is not True AND status = 'INSPECTED'").
		Joins("JOIN images ON image_versions.image_name = images.name AND image_versions.sha = images.latest").
		Order("image_name").Limit(constDisplayMaxImages).
		Select("image_name").
		Group("image_name").
		Limit(constImagesPerPage).
		Offset(offset).
		Pluck("image_name", &name).Error

	if err != nil {
		log.Errorf("Error getting label schema images: %v", err)
	}

	imageCount := d.GetLabelSchemaImageCount()
	pageCount := imageCount / constImagesPerPage

	if (imageCount % constImagesPerPage) > 0 {
		pageCount += 1
	}

	log.Debugf("Database: Label schema images: %v", name)
	return ImageList{ImageCount: imageCount,
		CurrentPage: pageNum,
		PageCount:   pageCount,
		Images:      name}
}

func (d *PgDB) GetLabelSchemaImageCount() int {
	var results []int

	log.Debug("Getting label schema image count")

	err := d.db.Raw(`SELECT COUNT(DISTINCT v.image_name) AS image_count FROM image_versions v
									 JOIN images i ON v.image_name = i.name AND v.sha = i.latest
									 WHERE v.labels LIKE '%org.label-schema.%' AND i.is_private IS NOT True
									 AND i.status = 'INSPECTED'`).
		Pluck("image_count", &results).Error

	if err != nil {
		log.Errorf("Error getting label schema image count: %v", err)
	}

	log.Debugf("Database: Label schema image count: %d", results)
	return results[0]
}

// ImageSearch returns the first 10 matching images ordered by number of pulls.
func (d *PgDB) ImageSearch(search string) (images []string, err error) {
	query := "%" + search + "%"
	err = d.db.Table("images").
		Where("status IN ('INSPECTED', 'SITEMAP', 'SIZE') AND is_private is not True AND name LIKE ?", query).
		Order("pull_count DESC").Limit(10).
		Pluck("name", &images).Error
	if err != nil {
		log.Errorf("Failed to get search results for %s: %v", search, err)
	}

	return images, nil
}

// GetPageURL returns the URL for the image on the MicroBadger site.
// TODO Support non Docker Hub registries
func (d *PgDB) GetPageURL(image Image) (pageURL string) {
	imageName := strings.TrimPrefix(image.Name, "library/")

	if image.IsPrivate {
		pageURL = d.SiteURL + constDockerRegistryPath + constImagePagePath + imageName
	} else {
		pageURL = d.SiteURL + constImagePagePath + imageName
	}

	return
}
