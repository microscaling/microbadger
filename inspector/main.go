package inspector

import (
	"fmt"
	"os"
	"strings"
	"time"

	logging "github.com/op/go-logging"

	"github.com/microscaling/microbadger/database"
	"github.com/microscaling/microbadger/encryption"
	"github.com/microscaling/microbadger/hub"
	"github.com/microscaling/microbadger/queue"
	"github.com/microscaling/microbadger/registry"
	"github.com/microscaling/microbadger/utils"
)

var (
	webhookURL string
	log        = logging.MustGetLogger("mminspect")
)

func init() {
	webhookURL = os.Getenv("MB_WEBHOOK_URL")
}

// CheckImageExists checks if an image exists on DockerHub for this image.
// Uses V2 of the Docker Registry API. If it's a private image this is only going to
// work if you pass in the registry credentials as part of the image ()
func CheckImageExists(i registry.Image, rs *registry.Service) bool {
	log.Debugf("Checking image %s exists.", i.Name)

	t, err := registry.NewTokenAuth(i, rs)
	if err != nil {
		log.Debugf("Couldn't get auth for %s", i.Name)
		return false
	}

	// Getting the list of tags will fail if the image doesn't exist
	_, err = t.GetTags()
	if err != nil {
		err = fmt.Errorf("Error getting tags for %s: %v", i.Name, err)
		return false
	}

	return true
}

// Inspect creates a new database image record and populates it with data from the registry
func Inspect(imageName string, db *database.PgDB, rs *registry.Service, hs *hub.InfoService, qs queue.Service, es encryption.Service) (err error) {
	log.Debugf("Inspecting %s", imageName)

	var hasChanged bool
	image := registry.Image{Name: imageName}

	img, err := db.GetOrCreateImage(imageName)
	if err != nil {
		log.Errorf("GetImage error for %s - %v", imageName, err)
		return err
	}

	// Check if there are saved credentials for this image.
	// If this errors try anyway as the image may be public
	image, _ = getRegistryCredentials(imageName, db, es)

	// Get the information from the hub first
	hubInfo, err := hs.Info(image)
	if err != nil {
		// We still want to carry on in this case as we may be able to get registry info anyway
		log.Errorf("Failed to get hub info for %s", imageName)
	}

	// Update Docker Hub metadata and stop if the image hasn't changed
	hasChanged, img = setHubInfo(img, hubInfo)
	if !hasChanged {
		return nil
	}

	versions, err := getVersionsFromRegistry(image, rs)
	if err != nil {
		log.Errorf("Failed to get metadata using registry: %v", err)

		if strings.Contains(err.Error(), "401 Unauthorized") {
			img.Status = "MISSING"
		} else {
			img.Status = "FAILED_INSPECTION"
		}
		img.BadgeCount = 0

		// Save image status so its no longer displayed if it has been deleted or made private.
		err := db.PutImageOnly(img)
		if err != nil {
			log.Errorf("Failed to save image %v", err)
		}

		return err
	}

	log.Debugf("%d versions found for %s", len(versions), img.Name)
	img.Versions = make([]database.ImageVersion, len(versions))
	i := 0
	for _, v := range versions {
		img.Versions[i] = v
		i++
	}

	img.Status = "SIZE"
	updateLatest(&img)
	img.BadgeCount++ // One badge for the link to Docker Hub

	if img.AuthToken == "" {
		token, err := utils.GenerateAuthToken()
		if err == nil {
			img.AuthToken = token
		} else {
			log.Errorf("Error generating auth token for %s: %v", img.Name, err)
		}
	}

	// Generate webhook URL including the auth token.
	img.WebhookURL = fmt.Sprintf("%s/images/%s/%s", webhookURL, img.Name, img.AuthToken)

	// Save image to the database
	nmc, err := db.PutImage(img)
	if err != nil {
		log.Errorf("Couldn't put image: %v", err)
	}

	// If anything has changed we do extra processing.
	if len(nmc.NewTags) > 0 || len(nmc.ChangedTags) > 0 || len(nmc.DeletedTags) > 0 {
		// We may have some users to notify.
		buildNotifications(db, qs, img.Name, nmc)
	} else {
		// Since we're not going to do a size inspection we can skip straight to INSPECTED state
		img.Status = "INSPECTED"
		err = db.PutImageOnly(img)
		if err != nil {
			log.Errorf("Couldn't put image (only): %v", err)
		}
	}

	log.Infof("Name: %s", img.Name)
	log.Infof("Status: %s", img.Status)
	log.Infof("Badges installed: %d", img.BadgesInstalled)
	log.Infof("Latest SHA: %s", img.Latest)

	return
}

// Checks if the image has stored credentials and returns the first users.
// TODO Credentials can be revoked so on failure we should try the next user
func getRegistryCredentials(image string, db *database.PgDB, es encryption.Service) (registry.Image, error) {
	img := registry.Image{Name: image}

	rcl, err := db.GetRegistryCredentialsForImage(image)
	if err != nil {
		log.Errorf("Error getting registry creds for image %s - %v", image, err)
		return img, err
	}

	if len(rcl) >= 1 {
		rc := rcl[0]

		password, err := es.Decrypt(rc.EncryptedKey, rc.EncryptedPassword)
		if err != nil {
			log.Errorf("Error decrypting password - %v", err)
			return img, err
		}

		img.User = rc.User
		img.Password = password
	}

	return img, err
}

// For public images calls the Docker Hub API to check if the image has changed.
// Also sets the latest Docker Hub metadata.
func setHubInfo(img database.Image, hubInfo hub.Info) (bool, database.Image) {
	var lastUpdated time.Time

	// Handle null last updated dates in the API response.
	if hubInfo.LastUpdated != nil {
		lastUpdated = *hubInfo.LastUpdated
	}

	// We can stop if the image hasn't changed since we last looked
	if (img.Status == "INSPECTED") && lastUpdated.Equal(img.LastUpdated) {
		log.Infof("Image %s is unchanged since we last looked at %#v", img.Name, lastUpdated)
		return false, img
	}

	if lastUpdated.Before(img.LastUpdated) {
		// We'll update the info anyway
		log.Errorf("Image %s LastUpdated is older than our own record!", img.Name)
	}

	img.LastUpdated = lastUpdated
	img.IsPrivate = hubInfo.IsPrivate
	img.IsAutomated = hubInfo.IsAutomated
	img.Description = hubInfo.Description
	img.PullCount = hubInfo.PullCount
	img.StarCount = hubInfo.StarCount
	img.BadgesInstalled = utils.BadgesInstalled(hubInfo.FullDescription)

	return true, img
}
