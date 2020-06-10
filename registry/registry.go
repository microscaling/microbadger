package registry

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	logging "github.com/op/go-logging"

	"github.com/microscaling/microbadger/utils"
)

const registryURL = "https://registry.hub.docker.com"
const authURL = "https://auth.docker.io"
const serviceURL = "registry.docker.io"

var log = logging.MustGetLogger("mminspect")

// Service connects to the Docker Hub over the internet
type Service struct {
	client      *http.Client
	authURL     string
	serviceURL  string
	registryURL string
}

// NewService is a real info service
func NewService() Service {
	return Service{
		client: &http.Client{
			// TODO Make timeout configurable.
			Timeout: 10 * time.Second,
		},
		authURL:     authURL,
		registryURL: registryURL,
		serviceURL:  serviceURL,
	}
}

// NewMockService is for testing
func NewMockService(transport *http.Transport, aurl string, rurl string, surl string) Service {

	return Service{
		client: &http.Client{
			Transport: transport,
		},
		authURL:     aurl,
		registryURL: rurl,
		serviceURL:  surl,
	}
}

type Image struct {
	Name     string
	User     string
	Password string
}

type registryTagsList struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

type registryConfig struct {
	Labels json.RawMessage
}

type containerConfig struct {
	Cmd []string `json:"Cmd,omitempty"`
}

type V1Compatibility struct {
	Id              string          `json:"id"`
	Throwaway       bool            `json:"throwaway"`
	Config          registryConfig  `json:"config,omitempty"`
	ContainerConfig containerConfig `json:"container_config,omitempty"`
	Created         string          `json:"created,omitempty"`
	Author          string          `json:"author,omitempty"`
}

type registryHistory struct {
	V1Compatibility string `json:"v1Compatibility"`
}

type registryFsLayers struct {
	BlobSum string `json:"blobSum"`
}

type Manifest struct {
	SchemaVersion int                `json:"schemaVersion"`
	Name          string             `json:"name"`
	History       []registryHistory  `json:"history"`
	FsLayers      []registryFsLayers `json:"fsLayers"`
}

// DockerAuth is returned by the Docker Auth API.
type dockerAuth struct {
	Token string `json:"token"`
}

// TokenAuthClient is associated with a particular org / image
type TokenAuthClient struct {
	org      string
	image    string
	user     string
	password string
	token    string
	tokenURL string

	service *Service

	rl             sync.RWMutex
	rateLimited    bool
	rateLimitDelay int
}

func NewTokenAuth(i Image, rs *Service) (t *TokenAuthClient, err error) {
	org, image, _ := utils.ParseDockerImage(i.Name)

	t = &TokenAuthClient{
		org:            org,
		image:          image,
		user:           i.User,
		password:       i.Password,
		rateLimitDelay: 10,
	}

	t.tokenURL = fmt.Sprintf("%s/token?service=%s&scope=repository:%s/%s:pull", rs.authURL, rs.serviceURL, org, image)
	t.service = rs

	err = t.getToken()
	if err != nil {
		log.Errorf("Failed to get auth token for %s/%s", org, image)
	}

	return t, err
}

func (t *TokenAuthClient) getToken() (err error) {
	var auth dockerAuth

	log.Debugf("Getting Auth Token URL for %s", t.tokenURL)

	req, err := http.NewRequest("GET", t.tokenURL, nil)
	if err != nil {
		log.Errorf("Failed to build API GET request err %v", err)
		return
	}

	if t.user != "" && t.password != "" {
		log.Debugf("Setting authorization for user %s", t.user)
		req.SetBasicAuth(t.user, t.password)
	}

	resp, err := t.service.client.Do(req)
	if err != nil {
		log.Errorf("Error getting auth token from %s: %v", t.tokenURL, err)
		return
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Errorf("Error getting auth token %d: %s", resp.StatusCode, resp.Status)
		return
	}

	body, err := ioutil.ReadAll(resp.Body)

	err = json.Unmarshal(body, &auth)
	if err != nil {
		log.Errorf("Error getting auth token for image %s/%s - %v", t.org, t.image, err)
		return
	}

	log.Debug("Got new auth token")
	t.token = auth.Token

	return
}

// ReqWithAuth sets the Authorization header on the request to use the provided token,
func (t *TokenAuthClient) reqWithAuth(reqType string, reqURL string) (resp *http.Response, err error) {
	req, err := http.NewRequest(reqType, reqURL, nil)
	if err != nil {
		log.Errorf("Failed to build API GET request err %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", t.token))

	resp, err = t.service.client.Do(req)
	if err != nil {
		log.Errorf("Error sending request: %v", err)
		return
	}

	if resp.StatusCode == http.StatusUnauthorized {
		log.Debug("Unauthorized on first attempt")

		// Perhaps this token has expired - try getting a new one and retrying the request
		err = t.getToken()
		if err != nil {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", t.token))
			resp, err = t.service.client.Do(req)
			if err != nil {
				log.Errorf("Error sending request the second time: %v", err)
				return
			}
		}
	}

	if resp.StatusCode != http.StatusOK {
		log.Debugf("Req failed: %d %s", resp.StatusCode, resp.Status)
		err = fmt.Errorf("Req failed: %d %s", resp.StatusCode, resp.Status)
	}

	return
}

func (t *TokenAuthClient) GetTags() (tags []string, err error) {
	var tagList registryTagsList

	tagsURL := fmt.Sprintf("%s/v2/%s/%s/tags/list", t.service.registryURL, t.org, t.image)
	log.Debugf("Getting tags at URL %s", tagsURL)

	resp, err := t.reqWithAuth("GET", tagsURL)
	if err != nil {
		log.Errorf("Failed to get tags: %v", err)
		return
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	err = json.Unmarshal(body, &tagList)
	if err != nil {
		log.Errorf("Error unmarshalling tags: %v", err)
		return
	}

	tags = tagList.Tags

	return
}

func (t *TokenAuthClient) GetManifest(tag string) (manifest Manifest, body []byte, err error) {

	manifestURL := fmt.Sprintf("%s/v2/%s/%s/manifests/%s", t.service.registryURL, t.org, t.image, tag)
	log.Debugf("Getting manifest at URL %s", manifestURL)

	resp, err := t.reqWithAuth("GET", manifestURL)
	if err != nil {
		log.Errorf("Failed to get manifest: %v", err)
		return
	}

	defer resp.Body.Close()

	body, err = ioutil.ReadAll(resp.Body)
	err = json.Unmarshal(body, &manifest)
	if err != nil {
		log.Errorf("Error unmarshalling manifest: %v", err)
		return
	}

	return
}

func (t *TokenAuthClient) getBlobDownloadSize(blobSum string) (size int64, err error) {
	blobURL := fmt.Sprintf("%s/v2/%s/%s/blobs/%s", t.service.registryURL, t.org, t.image, blobSum)
	log.Debugf("Getting blob at URL %s", blobURL)

	// Make a HEAD request because only the response headers are needed.
	resp, err := t.reqWithAuth("HEAD", blobURL)
	if err != nil {
		log.Errorf("Failed to get blob: %v", err)
		return
	}

	defer resp.Body.Close()

	size, err = strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		log.Errorf("Failed to convert content length to int: %v", err)
		return
	}

	return
}

// GetImageDownloadSize gets the download size of the image. The total size is returned as well as an array
// with the size of each layer. Only layers that affect the filesystem generate a new blob. If the blob already
// exists for the image the layer size will be 0. The blobs are gzip compressed so the size on disk will be larger.
func (t *TokenAuthClient) GetImageDownloadSize(m Manifest) (size int64, layerSizes []int64, err error) {

	if t.getRateLimited() {
		return 0, nil, fmt.Errorf("Rate limited")
	}

	blobs := make(map[string]int64)
	layerSizes = make([]int64, len(m.FsLayers))

	// Get the download size for each layer.
	for i, l := range m.FsLayers {

		// Blob already exists so this layer is empty.
		if _, ok := blobs[l.BlobSum]; ok {
			layerSizes[i] = 0
		} else {
			// Blob is new so get the download size from the Registry API.
			blobSize, err := t.getBlobDownloadSize(l.BlobSum)
			if err != nil {
				log.Infof("Error getting blob size for image %s/%s blob %s: %v", t.org, t.image, l.BlobSum, err)

				if strings.Contains(err.Error(), "HAP429") {
					log.Info("Rate limited")
					t.setRateLimited()
				}

				return 0, nil, err
			}

			// Add blob to the map of known blobs.
			blobs[l.BlobSum] = blobSize

			// Set the download size for the layer and add to the total size.
			layerSizes[i] = blobSize
			size += blobSize
		}
	}

	return
}

func (t *TokenAuthClient) getRateLimited() bool {
	t.rl.RLock()
	defer t.rl.RUnlock()

	return t.rateLimited
}

func (t *TokenAuthClient) setRateLimited() {
	t.rl.Lock()
	defer t.rl.Unlock()

	log.Debug("Setting rate limiter")
	if t.rateLimited {
		log.Fatal("Shouldn't call setRateLimited when already rate limited!!")
	}

	t.rateLimited = true
	time.AfterFunc(time.Duration(t.rateLimitDelay)*time.Second, func() {
		log.Debug("Unsetting rate limiter")
		t.rl.Lock()
		t.rateLimited = false
		t.rl.Unlock()
	})
}
