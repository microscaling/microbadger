// (c) 2016 Force12io Ltd

package api

import "time"

// Image is an image
type Image struct {
	ImageName     string // namespace/name
	Author        string
	Description   string
	DownloadSize  int64
	ImageURL      string // URL on Docker Hub
	Labels        map[string]string
	LastUpdated   time.Time // When the image was last pushed
	LayerCount    int
	LatestSHA     string
	LatestVersion string
	UpdatedAt     time.Time // When we last inspected the image
	Versions      []ImageVersion
	WebhookURL    string
}

// ImageVersion is a version of an image
type ImageVersion struct {
	SHA            string
	Tags           []Tag
	Author         string
	Labels         map[string]string
	LayerCount     int
	DownloadSize   int64
	Created        time.Time
	MicrobadgerURL string
}

// Tag is assigned to an image version
type Tag struct {
	Tag string
}
