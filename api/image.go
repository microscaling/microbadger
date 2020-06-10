package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/microscaling/microbadger/database"
	"github.com/microscaling/microbadger/inspector"
	"github.com/microscaling/microbadger/registry"
	"github.com/microscaling/microbadger/utils"
)

// BadgeCount includes the number of badges installed, and how many images they are installed across
type BadgeCount struct {
	Badges int `json:"badges"`
	Images int `json:"images"`
}

// BadgeCounts includes a BadgeCount for different sources (for now we only look at Docker Hub)
type BadgeCounts struct {
	DockerHub BadgeCount `json:"docker_hub"`
}

func handleGetImageList(w http.ResponseWriter, r *http.Request) {
	var imageList database.ImageList
	var err error

	vars := mux.Vars(r)
	queryType := vars["query"]
	pageString, ok := vars["page"]
	pageNum := 1
	if ok {
		pageNum, err = strconv.Atoi(pageString)
		if err != nil {
			pageNum = 1
		}
	}

	switch queryType {
	case "featured":
		imageList = db.GetFeaturedImages()
		log.Debugf("Featured images: %v", imageList)
	case "recent":
		imageList = db.GetRecentImages()
		log.Debugf("Recent images: %#v", imageList)
	case "labelschema":
		imageList = db.GetLabelSchemaImages(pageNum)
		log.Debugf("Label schema images: %#v", imageList)
	default:
		log.Errorf("If we get here, the mux is broken")
		return
	}

	bytes, err := json.Marshal(imageList)
	if err != nil {
		log.Errorf("Error: %v", err)
	}

	w.Write([]byte(bytes))
}

func getImageNameVars(r *http.Request) (ok bool, image string, registry string) {
	vars := mux.Vars(r)
	namespace := vars["namespace"]
	image = vars["image"]

	// Only the Docker registry is supported for the first release
	registry, regSpecified := vars["registry"]
	if regSpecified && registry != "docker" {
		return
	}

	// If a registry is specified there has to also be both namespace and image name
	if regSpecified {
		if namespace == "" || image == "" {
			return
		}
	} else {
		// IF it's a public official image the namespace doesn't get specified in the URL
		if namespace == "" {
			namespace = "library"
		}
	}

	image = namespace + "/" + image
	ok = true
	return
}

func handleGetImage(w http.ResponseWriter, r *http.Request) {
	var err error
	var bytes []byte
	var version database.ImageVersion
	vars := mux.Vars(r)

	ok, image, registry := getImageNameVars(r)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	u := userFromContext(r.Context())
	tag := vars["tag"]
	log.Debugf("Image name %s", image)
	log.Debugf("Tag %s", tag)

	// This could fail if the image doesn't exist, or if the user doesn't have permission
	// for which we return a 404 to prevent leaking that this image exists.
	// We're glossing over the possibility of other sorts of failures by always returning 404 on error.
	img, permission, err := db.GetImageForUser(image, u)
	if !permission {
		log.Debugf("User %v does not have access to image %s, if it exists", u, image)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	// If the image doesn't exist and it's public, we will go looking for it, but not if it's private
	if err != nil && registry != "" {
		log.Debugf("User %v does not have access to image %s, if it exists", u, image)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	// If the image doesn't exist yet, and we're looking for a public one we can go ahead and create it
	if err != nil {
		log.Debugf("Create a new image")
		img, err = db.GetOrCreateImage(image)
		if err != nil {
			log.Errorf("Failed to get or create image %s: %v", image, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		img.Status = "MISSING"
		log.Debugf("Image %s is missing", img.Name)
	}

	// As well as sending this response we may also need to re-inspect
	refreshCode := r.URL.Query().Get(constRefreshParam)
	log.Debugf("Refresh code %s", refreshCode)
	if refreshCode == refreshCodeValue || img.Status == "MISSING" || img.Status == "FAILED_INSPECTION" || img.Status == "SITEMAP" || (img.Status == "INSPECTED" && img.Latest == "") {
		submitImage(&img, u)
	}

	// We will return an imageName that doesn't include library/ for official images
	img.ImageName = strings.TrimPrefix(img.Name, "library/")
	log.Debugf("Image name is %s, status is %s", img.ImageName, img.Status)

	// We make sure the image always has an auth token so it can be refreshed.
	if img.AuthToken == "" {
		token, err := utils.GenerateAuthToken()
		if err == nil {
			img.AuthToken = token
		} else {
			log.Errorf("Error generating auth token for %s: %v", img.Name, err)
		}

		// Generate webhook URL including the auth token.
		img.WebhookURL = fmt.Sprintf("%s/images/%s/%s", webhookURL, img.Name, img.AuthToken)

		err = db.PutImageOnly(img)
		if err != nil {
			log.Errorf("Failed to save images %s: %v", image, err)
		}
	}

	switch img.Status {
	case "INSPECTED", "SIZE":
		if tag == "" {
			// No tag so most recent version is returned. Only gets private versions
			version, err = db.GetImageVersionBySHA(img.Latest, img.Name, img.IsPrivate)
		} else {
			// Get the version matching this tag.
			version, err = db.GetImageVersionByTag(img.Name, tag)
		}

		if err != nil {
			log.Debugf("No version found: %v", err)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(constStatusNotFound))
			return
		}

		img, err = updateInspectedImage(img, version)
		if err != nil {
			log.Debugf("Error updating inspected image: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

	case "SUBMITTED", "FAILED_INSPECTION":
		w.WriteHeader(http.StatusAccepted)
	case "MISSING":
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	default:
		w.WriteHeader(http.StatusInternalServerError)
		err = json.NewEncoder(w).Encode(fmt.Sprintf("Image status is %s", img.Status))
		if err != nil {
			log.Error("failed to encode json")
		}
		return
	}

	bytes, err = json.Marshal(img)
	if err != nil {
		log.Errorf("Error: %v", err)
	}

	w.Write([]byte(bytes))
}

func handleGetImageVersion(w http.ResponseWriter, r *http.Request) {
	var bytes []byte
	vars := mux.Vars(r)
	u := userFromContext(r.Context())

	ok, image, _ := getImageNameVars(r)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	sha := vars["sha"]
	if sha == "" {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	img, permission, err := db.GetImageForUser(image, u)
	if !permission {
		log.Debugf("Error for user %d getting access to image %s", u.ID, image)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	// Are we still inspecting the layers?
	if img.Status == "SIZE" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	iv, err := getImageVersionBySHA(sha, img.Name, img.IsPrivate, u)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// We will return an imageName that doesn't include library/ for official images
	iv.ImageName = strings.TrimPrefix(iv.ImageName, "library/")

	bytes, err = json.Marshal(iv)
	if err != nil {
		log.Errorf("Error: %v", err)
	}

	w.Write([]byte(bytes))
}

func handleImageSearch(w http.ResponseWriter, r *http.Request) {
	var list database.ImageList

	vars := mux.Vars(r)
	term := vars["term"]
	term2 := vars["term2"]

	if term2 != "" {
		term += "/" + term2
	}

	images, err := db.ImageSearch(term)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	list.Images = images

	bytes, err := json.Marshal(list)
	if err != nil {
		log.Errorf("Error: %v", err)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(bytes))
}

func getParentsFromLayers(layers *[]database.ImageLayer, SHA string, imageName string, u *database.User) []database.ImageVersion {

	// Go through the layers looking for images it's built on, starting at the longest
	for ii := len(*layers) - 1; ii > 0; ii-- {
		parents := getIdenticalImageVersions((*layers)[:ii], SHA, imageName, u)
		if len(parents) > 0 {
			// Strip out these layers - we'll use the ones from the parent instead
			*layers = (*layers)[ii:]
			return parents
		}
	}

	return []database.ImageVersion{}
}
func getIdenticalImageVersions(layers []database.ImageLayer, SHA string, imageName string, u *database.User) []database.ImageVersion {

	hash := inspector.GetHashFromLayers(layers)

	// Get image versions that match this hash but not this SHA or image name
	matches, err := db.GetImageVersionsByHash(hash, SHA, imageName)
	if err != nil {
		return []database.ImageVersion{}
	}

	// If one or more is an official image, use that and remove any clones
	var foundOfficial bool
	for mm := 0; mm < len(matches); mm++ {
		if strings.HasPrefix(matches[mm].ImageName, "library/") {
			// Remove any up to this point
			if !foundOfficial && mm > 0 {
				log.Debugf("Discarding %d unofficial matched images", mm)
				matches = matches[mm:]
				mm = 0
			}
			foundOfficial = true
		} else {
			if foundOfficial {
				log.Debugf("Discarding an unofficial matched image")
				matches = append(matches[:mm], matches[mm+1:]...)
			}
		}
	}

	for mm := 0; mm < len(matches); mm++ {
		parent, permission, err := db.GetImageForUser(matches[mm].ImageName, u)
		if !permission || err != nil {
			// Drop this match as it's not permitted, or because something has gone wrong
			if err != nil {
				log.Errorf("Error looking up parent image: %v", err)
			}
			log.Debugf("Discarding a matching image that user doesn't have permission to access")
			matches = append(matches[:mm], matches[mm+1:]...)
		} else {
			matches[mm].Tags, _ = db.GetTagsList(&matches[mm])
			matches[mm].LayersArray = layers
			// TODO Could add specific tag into this URL
			matches[mm].MicrobadgerURL = db.GetPageURL(parent)
			log.Debugf("URL is %s", matches[mm].MicrobadgerURL)
		}
	}
	return matches
}

// Submit image for inspection if it exists
func submitImage(img *database.Image, u *database.User) {
	i := registry.Image{Name: img.Name}
	if img.IsPrivate {
		if u == nil {
			// If it's a private image but we don't have a user, just ignore this submission request
			log.Infof("Ignoring anonymous request to submit private image")
			return
		}
		user, password, err := getUserRegistryCreds(img.Name, u.ID)
		if err != nil {
			log.Debugf("Got user %d credentials for %s", u.ID, img.Name)
			i.User = user
			i.Password = password
		}
	}

	if inspector.CheckImageExists(i, &rs) {
		// Reset UpdatedAt so the processing time is accurate.
		if img.Status == "SITEMAP" {
			img.UpdatedAt = time.Now()
		}

		// Send image to the queue.
		err := qs.SendImage(img.Name, "Sent")
		if err == nil {
			img.Status = "SUBMITTED"
			log.Debugf("Image %s now submitteed ", img.Name)
		}

	} else {
		// Image does not exist on DockerHub.
		log.Debugf("Image %s doesn't exist on Dockerhub", img.Name)
		img.Status = "MISSING"
	}

	// Save to the database.
	db.PutImageOnly(*img)

	return
}

func getLabelMapFromLabels(iv *database.ImageVersion) (labels map[string]string, err error) {
	if len(iv.Labels) > 0 {
		labels = make(map[string]string)

		err = json.Unmarshal([]byte(iv.Labels), &labels)
		if err != nil {
			log.Errorf("Error unmarshalling labels %s: %v", iv.Labels, err)
		}
	}
	return
}

func getLatestFromVersion(img *database.Image, iv *database.ImageVersion) (err error) {

	if iv != nil && iv.SHA != "" {
		img.ID = iv.SHA
		img.Author = iv.Author
		img.LayerCount = iv.LayerCount
		img.DownloadSize = iv.DownloadSize
		img.Labels, err = getLabelMapFromLabels(iv)
		if err != nil {
			log.Errorf("Error ping labels %s: %v", iv.Labels, err)
			return
		}

		img.LatestTag = getLongestTag(iv)
	}
	return
}

func updateInspectedImage(img database.Image, version database.ImageVersion) (database.Image, error) {
	var err error

	// Set Docker Hub URL. Use the version that has library in it for official images
	img.ImageURL = fmt.Sprintf("https://%s/r/%s/", imageURL, img.Name)

	// Get all versions for this image
	img.Versions, err = db.GetImageVersions(img)
	if err != nil {
		log.Errorf("Failed to get image versions: %v", err)
		return img, err
	}

	// Convert labels to the map version for the interface
	for key, iv := range img.Versions {
		tags, err := db.GetTagsList(&iv)
		img.Versions[key].Tags = tags
		if err != nil {
			log.Errorf("Error getting tags for %s: %v", img.Name, err)
			return img, err
		}

		img.Versions[key].LabelMap, err = getLabelMapFromLabels(&iv)
		if err != nil {
			log.Errorf("Error prepping labels %s: %v", iv.Labels, err)
			return img, err
		}

		// We want to consistently return image names that don't include library/ for official images
		if iv.ImageName != img.Name {
			log.Errorf("Bizarrely image version name %s doesn't match image name %s", iv.ImageName, img.Name)
		}
		img.Versions[key].ImageName = img.ImageName

		// Parse JSON only fields from the labels data
		_, lic, vcs := inspector.ParseLabels(&iv)
		img.Versions[key].License = lic
		img.Versions[key].VersionControl = vcs
	}

	// Populate image JSON from either the latest or the tagged version
	err = getLatestFromVersion(&img, &version)
	if err != nil {
		log.Errorf("Couldn't update Json fields for %s: %v", img.Name, err)
	}

	return img, err
}

func getImageVersionBySHA(sha string, image string, isPrivate bool, u *database.User) (database.ImageVersion, error) {
	var layers []database.ImageLayer

	iv, err := db.GetImageVersionBySHA(sha, image, isPrivate)
	if err != nil {
		return iv, err
	}

	// Convert labels to the map version for the interface
	// Convert layers to the array version for the interface
	// Get parents and any identical versions
	log.Debugf("Version with %d layers", iv.LayerCount)

	iv.LabelMap, err = getLabelMapFromLabels(&iv)
	if err != nil {
		log.Errorf("Error getting labelmap %s: %v", iv.Labels, err)
		return iv, err
	}

	// TODO Maybe we should be storing the layers in a separate table
	// TODO Can we avoid so much unmarshalling and remarshalling?
	if len(iv.Layers) > 0 {
		layers = make([]database.ImageLayer, iv.LayerCount)
		err = json.Unmarshal([]byte(iv.Layers), &layers)
		if err != nil {
			log.Errorf("Error unmarshalling layers %s: %v", iv.Layers, err)
			return iv, err
		}

		iv.Parents = getParentsFromLayers(&layers, iv.SHA, iv.ImageName, u)
		iv.Identical = getIdenticalImageVersions(layers, iv.SHA, iv.ImageName, u)
		iv.LayersArray = layers

		// It's possible there could be multiple image versions that are identical
		if len(iv.Parents) > 1 {
			log.Infof("More than one identical parent for %s version %s", iv.ImageName, iv.SHA)
		}

		if len(iv.Identical) > 0 {
			log.Infof("Identical images found for %s version %s", iv.ImageName, iv.SHA)
		}
	}

	return iv, err
}

// What does it mean for a base image to 'change'?
// Definite change:
// 1. My image is based on image:specific_tag, and specific_tag is moved from image version X to version Y. This
// also covers 'latest'
//    => "Version <specific_tag> of <image> has changed since you built this image"
// 2. My image is based on image:specific_tag, and specific_tag is deleted
//    => "Version <specific_tag> of <image> has been deleted"
// 3. My image is based on image, but no tag is specified. A new version of that image is built.
//    => "There is a newer version of <image> available"
// Not a change:
// A. My image is based on image:specific_tag. A new version of image is created, but specific_tag is left where it is

// When we get a tag
// If this tag existed before, and if the image version it referred to is not the same
//    This version has changed
//    Find any images who were based on the old one
//    For each image, mark that it changed
//        Previous version that it matched has a date
//        Add 'deleted timestamp' to table of 'tags'

// We can't know about any changes to tags that happened before we started looking at any given image
