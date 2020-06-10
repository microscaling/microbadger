package inspector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/microscaling/microbadger/database"
	"github.com/microscaling/microbadger/registry"
)

func getVersionsFromRegistry(i registry.Image, rs *registry.Service) (versions map[string]database.ImageVersion, err error) {
	versions = make(map[string]database.ImageVersion, 1)

	t, err := registry.NewTokenAuth(i, rs)
	if err != nil {
		err = fmt.Errorf("Error getting Token Auth Client for %s: %v", i.Name, err)
		return
	}

	// Get all the tags
	tags, err := t.GetTags()
	if err != nil {
		err = fmt.Errorf("Error getting tags for %s: %v", i.Name, err)
		return
	}

	if len(tags) == 0 {
		err = fmt.Errorf("No tags exist for %s", i.Name)
		return
	}

	// Get the version info for all the tags, looking out for any that match latest
	log.Debugf("%d tags for %s", len(tags), i.Name)
	for _, tagName := range tags {

		manifest, manifestBytes, err := t.GetManifest(tagName)
		if err != nil {
			// Sometimes the registry returns not found for a tag eevn though that tag was included
			// in the list of tags - presumably this is the registry in a bit of a bad state.
			// We don't want to fail the whole image in this situation.
			err = fmt.Errorf("Error getting manifest for tag %s: %v", tagName, err)
			if strings.Contains(err.Error(), "404") {
				continue
			} else {
				return versions, err
			}
		}

		version, err := getVersionInfo(manifest)
		if err != nil {
			log.Errorf("Error getting version info for %s tag %s: %v", i.Name, tagName, err)
			return versions, err
		}

		// We might already know about this version under a different tag name
		if v, ok := versions[version.SHA]; ok {
			version = v
		}

		tag := database.Tag{
			Tag:       tagName,
			ImageName: i.Name,
			SHA:       version.SHA,
		}

		version.Tags = append(version.Tags, tag)
		version.Manifest = string(manifestBytes)
		versions[version.SHA] = version
	}

	return
}

func getVersionInfo(m registry.Manifest) (version database.ImageVersion, err error) {

	var v1c registry.V1Compatibility

	if len(m.History) == 0 {
		err = fmt.Errorf("No history for this image")
		return
	}

	// The first entry in the list is the one to look at
	h := m.History[0]
	err = json.Unmarshal([]byte(h.V1Compatibility), &v1c)
	if err != nil {
		err = fmt.Errorf("Error unmarshalling history: %v", err)
		return
	}

	created, err := time.Parse(time.RFC3339Nano, v1c.Created)
	if err != nil {
		log.Infof("Couldn't get created time for %s from string %s", m.Name, v1c.Created)
	}

	version = database.ImageVersion{
		SHA:        v1c.Id,
		ImageName:  m.Name,
		Author:     v1c.Author,
		Labels:     string(v1c.Config.Labels),
		Created:    created,
		LayerCount: len(m.FsLayers),
	}

	return
}

// formatHistory takes the raw command from the history in the manifest
// and makes it human readable.
func formatHistory(input []string) (cmd string) {

	// Get the raw command from the input array.
	cmd = strings.Join(input, " ")

	// Trim spaces just in case
	cmd = strings.TrimSpace(cmd)

	// Strip out the shell script prefix and strip whitespace again
	cmd = strings.TrimPrefix(cmd, "/bin/sh -c")
	cmd = strings.TrimSpace(cmd)

	// If the next word is #(nop) then the following word should be the Dockerfile directive
	if strings.HasPrefix(cmd, "#(nop)") {
		cmd = strings.TrimSpace(strings.TrimPrefix(cmd, "#(nop)"))

		// The exception is COPY where it's preceded by %s %s in %s
		cmd = strings.TrimPrefix(cmd, `%s %s in %s`)
	} else if cmd != "" {
		// If not, then it's a RUN
		cmd = "RUN " + strings.TrimSpace(cmd)
	}

	log.Debugf("Cmd is %s", cmd)
	return
}

func updateLatest(img *database.Image) {
	// See if there's a tag marked 'latest'; if not, find the most recent image
	var mostRecent time.Time

	// As we haven't stored into the database yet we need to go through all the versions
	for _, v := range img.Versions {
		for _, t := range v.Tags {
			if t.Tag == "latest" {
				img.Latest = t.SHA
				log.Debugf("Found version called latest for %s", img.Name)
				return
			}
		}

		if v.Created.After(mostRecent) {
			mostRecent = v.Created
			img.Latest = v.SHA
		}
	}

	log.Debugf("Latest version for %s is %s", img.Name, img.Latest)
}

// GetHashFromLayers hashes together all the layer commands
func GetHashFromLayers(layers []database.ImageLayer) string {
	// log.Debugf("Making hash from %d layers", len(layers))
	// This size is an approximation - we don't know how big the commands will really be, but it will do
	layerData := make([]byte, 0, len(layers)*sha256.Size)

	for _, layer := range layers {
		// Run all the cmd fields together to get our hashable string - or use the blobSum if there is no Command
		if layer.Command == "" {
			layerData = append(layerData, []byte(layer.BlobSum)...)
		} else {
			layerData = append(layerData, []byte(layer.Command)...)
		}
	}

	// log.Debugf("Layer data is %d bytes long", len(layerData))
	checksum := sha256.Sum256(layerData)
	return hex.EncodeToString(checksum[:])
}
