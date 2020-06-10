package hub

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/microscaling/microbadger/registry"
	"github.com/microscaling/microbadger/utils"
	"github.com/op/go-logging"
)

const constHubURL = "hub.docker.com"
const constImagesPerPage = 10

var log = logging.MustGetLogger("mmhub")

// Info is returned from the hub.docker.com/v2/repositories API
type Info struct {
	Name            string     `json:"name"`
	Namespace       string     `json:"namespace"`
	Description     string     `json:"description"`
	FullDescription string     `json:"full_description"`
	IsAutomated     bool       `json:"is_automated"`
	IsPrivate       bool       `json:"is_private"`
	LastUpdated     *time.Time `json:"last_updated"`
	PullCount       int        `json:"pull_count"`
	StarCount       int        `json:"star_count"`
}

type infoResponse struct {
	Count   int    `json:"count"`
	Results []Info `json:"results"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

type namespacesResponse struct {
	Namespaces []string `json:"namespaces"`
}

type NamespaceList struct {
	Namespaces []string
}

type orgsResponse struct {
	Orgs []org `json:"results"`
}

type org struct {
	OrgName string `json:"orgname"`
}

type ImageList struct {
	CurrentPage int
	PageCount   int
	ImageCount  int
	Images      []ImageInfo
}

type ImageInfo struct {
	ImageName   string
	IsInspected bool
	IsPrivate   bool
}

// InfoService connects to the Docker Hub over the internet
type InfoService struct {
	client  *http.Client
	baseURL string
}

// NewService is a real info service
func NewService() InfoService {
	return InfoService{
		client: &http.Client{
			// TODO Make timeout configurable.
			Timeout: 10 * time.Second,
		},
		baseURL: "https://" + constHubURL,
	}
}

// NewMockService is for testing
func NewMockService(transport *http.Transport) InfoService {

	return InfoService{
		client: &http.Client{
			Transport: transport,
		},
		baseURL: "http://fakehub",
	}
}

// Login logs in to get an auth token from Docker Hub
func (hub *InfoService) Login(user string, password string) (token string, err error) {
	var lr loginResponse

	hubLoginURL := fmt.Sprintf("%s/v2/users/login/", hub.baseURL)
	log.Debugf("Logging into %s", hubLoginURL)

	l := loginRequest{
		Username: user,
		Password: password,
	}

	b, err := json.Marshal(l)
	if err != nil {
		log.Errorf("Error marshalling login request -  %v", err)
		return
	}

	buf := bytes.NewBuffer(b)
	req, _ := http.NewRequest("POST", hubLoginURL, buf)
	req.Header.Add("Content-Type", "application/json")

	resp, err := hub.client.Do(req)
	if err != nil {
		log.Errorf("Error making login request - %v", err)
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Debugf("Failed to get login token %d: %s", resp.StatusCode, resp.Status)
		return "", errors.New("Incorrect credentials")
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Error reading login response - %v", err)
		return
	}

	err = json.Unmarshal(body, &lr)
	if err != nil {
		log.Errorf("Error unmarshalling login response - %v", err)
	}

	token = lr.Token
	log.Debug("Got login token")

	return
}

// UserNamespaces gets the namespaces the user belongs to including any orgs they are a member of
func (hub *InfoService) UserNamespaces(user string, password string) (namespaceList NamespaceList, err error) {
	dedupe := make(map[string]bool)

	loginToken, err := hub.Login(user, password)
	if err != nil {
		log.Errorf("Error logging in to docker hub %v", err)
		return
	}

	namespaces, err := hub.getNamespaces(loginToken)
	if err != nil {
		log.Errorf("Error getting namespaces %v", err)
	}

	for _, ns := range namespaces {
		dedupe[ns] = true
	}

	orgs, err := hub.getOrgs(loginToken)
	if err != nil {
		log.Errorf("Error getting orgs %v", err)
	}

	// Add any orgs that aren't in the namespaces list
	for _, org := range orgs {
		if _, ok := dedupe[org]; !ok {
			namespaces = append(namespaces, org)
		}
	}

	// Return a sorted slice
	namespaceList.Namespaces = namespaces
	sort.Strings(namespaceList.Namespaces)

	return
}

// UserNamespaceImages gets the list of images within a namespace
func (hub *InfoService) UserNamespaceImages(user string, password string, namespace string, page int) (imageList ImageList, notfound bool, err error) {
	var ir infoResponse

	loginToken, err := hub.Login(user, password)
	if err != nil {
		log.Errorf("Error logging in to docker hub %v", err)
		return
	}

	hubURL := fmt.Sprintf("%s/v2/repositories/%s/?page=%d", hub.baseURL, namespace, page)
	log.Debugf("Getting namespace info %s", hubURL)

	req, _ := http.NewRequest("GET", hubURL, nil)
	req.Header.Add("Authorization", fmt.Sprintf("JWT %s", loginToken))
	req.Header.Add("Content-Type", "application/json")

	resp, err := hub.client.Do(req)
	if err != nil {
		log.Errorf("Error getting hub info from %s: %v", hubURL, err)
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		log.Debugf("Not found")
		notfound = true
		return
	}

	if resp.StatusCode != http.StatusOK {
		log.Errorf("Failed to get namespace images %d: %s", resp.StatusCode, resp.Status)
		err = fmt.Errorf("Failed to get namespace images: %d %s", resp.StatusCode, resp.Status)
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Error reading namespace images response - %v", err)
		return
	}

	err = json.Unmarshal(body, &ir)
	if err != nil {
		log.Errorf("Error unmarshalling namespace images - %v", err)
	}

	images := make([]ImageInfo, len(ir.Results))

	for i, info := range ir.Results {
		images[i] = ImageInfo{
			ImageName: info.Namespace + "/" + info.Name,
			IsPrivate: info.IsPrivate,
		}
	}

	pageCount := ir.Count / constImagesPerPage
	if (ir.Count % constImagesPerPage) > 0 {
		pageCount += 1
	}

	imageList = ImageList{
		CurrentPage: page,
		PageCount:   pageCount,
		ImageCount:  ir.Count,
		Images:      images,
	}

	return
}

// Info gets information about this image
func (hub *InfoService) Info(image registry.Image) (hubInfo Info, err error) {
	org, imageName, _ := utils.ParseDockerImage(image.Name)

	hubRepoURL := fmt.Sprintf("%s/v2/repositories/%s/%s/", hub.baseURL, org, imageName)

	log.Debugf("Getting hub info from %s", hubRepoURL)
	req, err := http.NewRequest("GET", hubRepoURL, nil)
	if err != nil {
		log.Errorf("Failed to build API GET request err %v", err)
		return
	}

	if image.User != "" && image.Password != "" {
		token, err := hub.Login(image.User, image.Password)
		if err != nil {
			log.Errorf("Failed to login to Docker Hub API %v", err)
			return hubInfo, err
		}

		req.Header.Add("Authorization", fmt.Sprintf("JWT %s", token))
	}

	resp, err := hub.client.Do(req)
	if err != nil {
		log.Errorf("Error getting hub info from %s: %v", hubRepoURL, err)
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Errorf("Error getting hub info %d: %s", resp.StatusCode, resp.Status)
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	// It's possible to get 200 OK but with json that represents nothing, rather than the hub info
	// We can check whether it's right by confirming that the name is correct
	if !strings.Contains(string(body), "name") && !strings.Contains(string(body), imageName) {
		err = fmt.Errorf("Failed to get hub info for %s/%s", org, imageName)
		return
	}

	err = json.Unmarshal(body, &hubInfo)
	if err != nil {
		log.Errorf("Error unmarshalling hub info for image %s/%s - %v", org, imageName, err)
	}

	log.Debugf("Got hub info %v", hubInfo)
	return
}

// getNamespaces returns namespaces where the user is an owner
func (hub *InfoService) getNamespaces(loginToken string) (namespaces []string, err error) {
	var nr namespacesResponse

	hubURL := fmt.Sprintf("%s/v2/repositories/namespaces/", hub.baseURL)
	log.Debugf("Getting namespaces %s", hubURL)

	body, err := hub.getDockerHubData(hubURL, loginToken)
	if err != nil {
		log.Errorf("Error getting namespaces - %v", err)
		return
	}

	err = json.Unmarshal(body, &nr)
	if err != nil {
		log.Errorf("Error unmarshalling namespaces - %v", err)
		return
	}

	namespaces = nr.Namespaces
	log.Debugf("Got namespaces %v", namespaces)

	return
}

// getOrgs returns orgs where the user is a member
func (hub *InfoService) getOrgs(loginToken string) (orgs []string, err error) {
	var or orgsResponse

	// Match large page size used by Docker Hub UI
	orgURL := fmt.Sprintf("%s/v2/user/orgs/?page_size=250", hub.baseURL)
	log.Debugf("Getting orgs %s", orgURL)

	orgBody, err := hub.getDockerHubData(orgURL, loginToken)
	if err != nil {
		log.Errorf("Error getting orgs - %v", err)
		return
	}

	err = json.Unmarshal(orgBody, &or)
	if err != nil {
		log.Errorf("Error unmarshalling orgs - %v", err)
		return
	}

	orgs = make([]string, len(or.Orgs))

	for i, o := range or.Orgs {
		orgs[i] = o.OrgName
	}

	log.Debugf("Got orgs %v", orgs)

	return
}

func (hub *InfoService) getDockerHubData(hubURL string, loginToken string) (body []byte, err error) {
	req, _ := http.NewRequest("GET", hubURL, nil)
	req.Header.Add("Authorization", fmt.Sprintf("JWT %s", loginToken))
	req.Header.Add("Content-Type", "application/json")

	resp, err := hub.client.Do(req)
	if err != nil {
		log.Errorf("Error getting hub data from %s: %v", hubURL, err)
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Errorf("Failed to get %s %d: %s", hubURL, resp.StatusCode, resp.Status)
		return
	}

	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Error reading hub response %s: %v", hubURL, err)
	}

	return
}
