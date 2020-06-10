package inspector

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/microscaling/microbadger/database"
	"github.com/microscaling/microbadger/encryption"
	"github.com/microscaling/microbadger/registry"
)

// InspectSize inspects the size of an image
func InspectSize(imgName string, db *database.PgDB, rs *registry.Service, es encryption.Service) (err error) {
	log.Debugf("Inspecting size of %s", imgName)
	var m registry.Manifest

	img, err := db.GetImage(imgName)
	if err != nil || img.Status == "MISSING" {
		// We make the error nil so that we don't put this back on the queue for reinspection.
		// That means we can stop doing size inspection for a wrong'un simply by deleting the image from the DB.
		log.Infof("Image %s no longer available: %v", imgName, err)
		err = nil
		return
	}

	// We already have the manifests for each version
	versions, err := db.GetAllImageVersionsWithManifests(img)
	if err != nil {
		// Stay in SIZE state because we will want to reinspect it
		log.Errorf("Failed to get all versions for %s: %v", imgName, err)
		return err
	}

	// Check if there are saved credentials for this image.
	// If this errors try anyway as the image may be public
	image, _ := getRegistryCredentials(imgName, db, es)

	t, err := registry.NewTokenAuth(image, rs)
	if err != nil {
		log.Errorf("Error getting Token Auth Client for %s: %v", imgName, err)
		return err
	}

	log.Debugf("Image is %v and has %d versions", img, len(versions))
	for _, iv := range versions {
		// No need to get the image size again if we already have it stored in a database
		if db.ImageVersionNeedsSizeOrLayers(&iv) {

			// Manifest is stored as a string in the database
			err = json.Unmarshal([]byte(iv.Manifest), &m)
			if err != nil {
				err = fmt.Errorf("Error unmarshalling manifest for %s: %v", img.Name, err)
				return err
			}

			// Get the total download size and the sizes of each individual layer.
			downloadSize, layerSizes, err := t.GetImageDownloadSize(m)
			if err != nil {
				log.Infof("Couldn't get download size for %s: %v", img.Name, err)
				if strings.Contains(err.Error(), "Rate limited") {
					// No point immediately trying to get other versions if we're rate limited
					break
				}
				// Other errors could be specific to this particular version, so we may still be able to
				// get information about other versions.
				continue
			}

			iv.DownloadSize = downloadSize

			// Check we have the layer sizes and that they match the history length.
			// Otherwise wait until the next time the size data is inspected.
			if layerSizes != nil && len(layerSizes) == len(m.History) {
				err = getLayerInfoFromManifest(&iv, m, layerSizes)
				if err != nil {
					err = fmt.Errorf("Error getting layer info from manifest for %s: %v", img.Name, err)
					return err
				}
			}

			// We have got all the information we could need from this manifest string, so we can
			// delete it and free up some space in the database
			iv.Manifest = ""

			log.Debugf("Updating image %s version %s with size %d", iv.ImageName, iv.SHA, iv.DownloadSize)
			db.PutImageVersion(iv)
		}
	}

	// Don't move this image to inspected if we got rate-limited. It should stay in SIZE state so we'll
	// try again another time.
	if err == nil {
		img.Status = "INSPECTED"
		err = db.PutImageOnly(img)
	}

	return err
}

func getLayerInfoFromManifest(v *database.ImageVersion, m registry.Manifest, layerSizes []int64) (err error) {
	var v1c registry.V1Compatibility
	layers := make([]database.ImageLayer, len(m.History))

	// Process the history commands stored in the manifest.
	for i, h := range m.History {

		// Data is in the V1 compatibility section.
		err = json.Unmarshal([]byte(h.V1Compatibility), &v1c)
		if err != nil {
			err = fmt.Errorf("Error unmarshalling history: %v", err)
			return err
		}

		// Format the raw command for display.
		cmd := formatHistory(v1c.ContainerConfig.Cmd)

		// Add the command and the corresponding layer size.
		layers[i] = database.ImageLayer{
			BlobSum:      m.FsLayers[i].BlobSum,
			Command:      cmd,
			DownloadSize: layerSizes[i],
		}
	}

	// Reverse the layers to make them human readable.
	for i, j := 0, len(layers)-1; i < j; i, j = i+1, j-1 {
		layers[i], layers[j] = layers[j], layers[i]
	}

	// Hash the layers together to identify this image
	v.Hash = GetHashFromLayers(layers)
	log.Debugf("Got hash %s", v.Hash)

	// Serialize the layers as JSON for storage.
	layersJSON, err := json.Marshal(layers)
	if err != nil {
		log.Infof("Couldn't convert layers for %s to string: %v", m.Name, err)
	}

	v.Layers = string(layersJSON)

	return err
}
