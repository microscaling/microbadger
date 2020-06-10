package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"code.cloudfoundry.org/bytefmt"
	"github.com/gorilla/mux"

	"github.com/microscaling/microbadger/database"
	"github.com/microscaling/microbadger/inspector"
)

func handleGetImageBadge(w http.ResponseWriter, r *http.Request) {
	var labelValue, badgeSVG string
	var imageVersion database.ImageVersion

	var image, org, tag, badgeType string
	var ok bool
	var license *database.License
	var vcs *database.VersionControl

	vars := mux.Vars(r)
	image = vars["image"]
	if org, ok = vars["org"]; !ok {
		org = "library"
	}

	badgeType = vars["badgeType"]
	tag = vars["tag"]

	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Expires", time.Now().Format(http.TimeFormat))

	log.Debugf("Badge type: %s - image: %s - tag: %s", badgeType, org+"/"+image, tag)

	img, err := db.GetImage(org + "/" + image)
	if err != nil || img.Status == "MISSING" || img.IsPrivate {
		log.Infof("Image %s missing for badge", img.Name)
		badgeType = "imagemissing"
	} else {
		if tag == "" {
			imageVersion, err = db.GetImageVersionBySHA(img.Latest, img.Name, false)
			if err != nil {
				// Error as there should always be a latest SHA.
				log.Errorf("Missing latest version for %s: %v", img.Name, err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		} else {
			imageVersion, err = db.GetImageVersionByTag(img.Name, tag)
			if err != nil {
				log.Infof("Missing image version for %s tag %s : %v", img.Name, tag, err)
				badgeType = "tagmissing"
			}
		}

		if !strings.Contains(badgeType, "missing") {
			_, license, vcs = inspector.ParseLabels(&imageVersion)
		}
	}

	switch badgeType {
	case "image":
		badgeSVG = generateImageBadge(imageVersion)

	case "commit":
		if vcs == nil {
			labelValue = "not given"
		} else {
			labelValue = formatLabel(vcs.Commit, 7)
		}
		badgeSVG = generateBadge(badgeType, labelValue)

	case "version":
		// Use a tag if it was specified
		if tag != "" {
			log.Debugf("Specified tag is %s", tag)
			labelValue = formatLabel(tag, 10)
		} else {
			labelValue = formatLabel(getLongestTag(&imageVersion), 10)
		}
		badgeSVG = generateBadge(badgeType, labelValue)

	case "license":
		if license == nil {
			labelValue = "not given"
		} else {
			labelValue = formatLabel(license.Code, 10)
		}
		badgeSVG = generateBadge(badgeType, labelValue)

	case "imagemissing":
		badgeSVG = generateBadge("Image", "not found")

	case "tagmissing":
		badgeSVG = generateBadge(formatLabel(tag, 7), "not found")

	default:
		log.Infof("Bad badge type for badge %s", badgeType)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.Write([]byte(badgeSVG))

	ga.ImageView("imageView", "imageView", "badge", badgeType, r)
}

func formatLabel(input string, length int) string {
	var result string

	if len(input) > length {
		lastChar := length - 1

		result = input[0:lastChar] + constEllipsis
	} else {
		result = input
	}

	return result
}

func generateImageBadge(latest database.ImageVersion) (badgeSVG string) {
	size := fmt.Sprintf("%sB", bytefmt.ByteSize(uint64(latest.DownloadSize)))
	layers := fmt.Sprintf("%d layers", latest.LayerCount)

	badgeSVG = "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"130\" height=\"20\"><linearGradient id=\"b\" x2=\"0\" y2=\"100%\"><stop offset=\"0\" stop-color=\"#bbb\" stop-opacity=\".1\"/><stop offset=\"1\" stop-opacity=\".1\"/></linearGradient><mask id=\"a\"><rect width=\"130\" height=\"20\" rx=\"3\" fill=\"#fff\"/></mask><g mask=\"url(#a)\"><path fill=\"#555\" d=\"M0 0h61v20H0z\"/><path fill=\"#007ec6\" d=\"M61 0h69v20H61z\"/><path fill=\"url(#b)\" d=\"M0 0h130v20H0z\"/></g><g fill=\"#fff\" text-anchor=\"middle\" font-family=\"DejaVu Sans,Verdana,Geneva,sans-serif\" font-size=\"11\"><text x=\"30.5\" y=\"15\" fill=\"#010101\" fill-opacity=\".3\">SIZE</text><text x=\"30.5\" y=\"14\">SIZE</text><text x=\"94.5\" y=\"15\" fill=\"#010101\" fill-opacity=\".3\">LAYERS</text><text x=\"94.5\" y=\"14\">LAYERS</text></g></svg>"

	badgeSVG = strings.Replace(badgeSVG, "SIZE", size, 2)
	badgeSVG = strings.Replace(badgeSVG, "LAYERS", layers, 2)

	return badgeSVG
}

func generateBadge(label string, value string) (badgeSVG string) {
	if len(value) > 7 {
		badgeSVG = "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"122\" height=\"20\"><linearGradient id=\"b\" x2=\"0\" y2=\"100%\"><stop offset=\"0\" stop-color=\"#bbb\" stop-opacity=\".1\"/><stop offset=\"1\" stop-opacity=\".1\"/></linearGradient><mask id=\"a\"><rect width=\"122\" height=\"20\" rx=\"3\" fill=\"#fff\"/></mask><g mask=\"url(#a)\"><path fill=\"#555\" d=\"M0 0h47v20H0z\"/><path fill=\"#007ec6\" d=\"M47 0h75v20H47z\"/><path fill=\"url(#b)\" d=\"M0 0h122v20H0z\"/></g><g fill=\"#fff\" text-anchor=\"middle\" font-family=\"DejaVu Sans,Verdana,Geneva,sans-serif\" font-size=\"11\"><text x=\"23.5\" y=\"15\" fill=\"#010101\" fill-opacity=\".3\">LABEL</text><text x=\"23.5\" y=\"14\">LABEL</text><text x=\"83.5\" y=\"15\" fill=\"#010101\" fill-opacity=\".3\">VALUE</text><text x=\"83.5\" y=\"14\">VALUE</text></g></svg>"
	} else {
		badgeSVG = "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"116\" height=\"20\"><linearGradient id=\"b\" x2=\"0\" y2=\"100%\"><stop offset=\"0\" stop-color=\"#bbb\" stop-opacity=\".1\"/><stop offset=\"1\" stop-opacity=\".1\"/></linearGradient><mask id=\"a\"><rect width=\"116\" height=\"20\" rx=\"3\" fill=\"#fff\"/></mask><g mask=\"url(#a)\"><path fill=\"#555\" d=\"M0 0h51v20H0z\"/><path fill=\"#007ec6\" d=\"M51 0h65v20H51z\"/><path fill=\"url(#b)\" d=\"M0 0h116v20H0z\"/></g><g fill=\"#fff\" text-anchor=\"middle\" font-family=\"DejaVu Sans,Verdana,Geneva,sans-serif\" font-size=\"11\"><text x=\"25.5\" y=\"15\" fill=\"#010101\" fill-opacity=\".3\">LABEL</text><text x=\"25.5\" y=\"14\">LABEL</text><text x=\"82.5\" y=\"15\" fill=\"#010101\" fill-opacity=\".3\">VALUE</text><text x=\"82.5\" y=\"14\">VALUE</text></g></svg>"
	}

	badgeSVG = strings.Replace(badgeSVG, "LABEL", label, 2)
	badgeSVG = strings.Replace(badgeSVG, "VALUE", value, 2)

	return badgeSVG
}

func handleGetBadgeCounts(w http.ResponseWriter, r *http.Request) {
	var badgeCounts BadgeCounts
	badges, images, err := db.GetBadgesInstalledCount()
	if err != nil {
		log.Errorf("Couldn't retrieve badge count: %v", err)
	}

	badgeCounts.DockerHub.Badges = badges
	badgeCounts.DockerHub.Images = images

	bytes, err := json.Marshal(badgeCounts)
	if err != nil {
		log.Errorf("Error: %v", err)
	}

	w.Write([]byte(bytes))
}
