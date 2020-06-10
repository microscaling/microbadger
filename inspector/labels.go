package inspector

import (
	"encoding/json"
	"io/ioutil"
	"net/url"
	"path"
	"strings"

	"github.com/microscaling/microbadger/database"
)

const (
	constLicenseCode        = "org.label-schema.license"
	constVersionControlType = "org.label-schema.vcs-type"
	constVersionControlURL  = "org.label-schema.vcs-url"
	constVersionControlRef  = "org.label-schema.vcs-ref"
	constGitHubSSH          = "git@github.com:"
	constGitHubHTTPS        = "https://github.com/"
	constLicenseFile        = "inspector/licenses.json"
)

var licenseCodeAltLabels = []string{
	"license",
}

var vcsTypeAltLabels = []string{
	"vcs-type",
}

var vcsRefAltLabels = []string{
	"vcs-ref",
}

var vcsUrlAltLabels = []string{
	"vcs-url",
}

var (
	licenseCodes map[string]string
)

func init() {
	// Load the license data
	licenses, err := getLicenses()
	if err != nil {
		log.Errorf("Error getting license list - %v", err)
		return
	}

	licenseCodes = make(map[string]string)

	for _, license := range licenses {
		licenseCodes[strings.ToLower(license.Code)] = license.URL
	}

	log.Debugf("License data initialized with %d licenses", len(licenseCodes))
}

// ParseLabels inspects Docker labels for those matching the label-schema.org schema.
// TODO Retire badgeCount as its no longer needed.
func ParseLabels(iv *database.ImageVersion) (badgeCount int, license *database.License, vcs *database.VersionControl) {
	var labels map[string]string

	err := json.Unmarshal([]byte(iv.Labels), &labels)
	if err != nil {
		log.Errorf("Error unmarshalling labels %s: %v", iv.Labels, err)
		return 0, nil, nil
	}

	license = parseLicense(labels)
	if license != nil {
		badgeCount += 1
	}

	vcs = parseVersionControl(labels)
	if vcs != nil {
		badgeCount += 1
	}

	return badgeCount, license, vcs
}

func parseVersionControl(labels map[string]string) *database.VersionControl {

	vcs := &database.VersionControl{
		Type:   getLabel(labels, constVersionControlType, vcsTypeAltLabels),
		URL:    getLabel(labels, constVersionControlURL, vcsUrlAltLabels),
		Commit: getLabel(labels, constVersionControlRef, vcsRefAltLabels),
	}

	if vcs.Type == "" || strings.ToLower(vcs.Type) == "git" {
		// Support for GitHub URLs
		if vcs.Commit != "" && strings.Contains(vcs.URL, "github.com") {
			// TODO Add support for BitBucket etc.
			return parseGitHubLabels(vcs)
		}
	}

	return nil
}

func parseLicense(labels map[string]string) *database.License {
	code := getLabel(labels, constLicenseCode, licenseCodeAltLabels)
	if code != "" {
		license := &database.License{
			Code: code,
			URL:  licenseCodes[strings.ToLower(code)],
		}

		return license
	}

	return nil
}

func parseGitHubLabels(vcs *database.VersionControl) *database.VersionControl {
	// Set type to Git
	vcs.Type = "git"

	// Convert from SSH to HTTPS URL
	vcs.URL = strings.Replace(vcs.URL, constGitHubSSH, constGitHubHTTPS, 1)

	// Remove .git suffix if present
	vcs.URL = strings.TrimSuffix(vcs.URL, ".git")

	commitURL, err := url.Parse(vcs.URL)
	if err != nil {
		log.Errorf("Error parsing GitHub URL - %v", err)
		return nil
	}

	// Link to exact commit
	commitURL.Path = path.Join(commitURL.Path, "tree", vcs.Commit)
	vcs.URL = commitURL.String()

	return vcs
}

func getLabel(labels map[string]string, main string, alternatives []string) string {

	value, ok := labels[main]
	if !ok {
		// Check for alternative versions
		for _, alternative := range alternatives {
			for key, value := range labels {
				if strings.Contains(key, alternative) {
					log.Debugf("Found alternative label format %s", alternative)
					return value
				}
			}
		}
	}

	return value
}

func getLicenses() (licenses []*database.License, err error) {
	raw, err := ioutil.ReadFile(constLicenseFile)
	if err != nil {
		log.Errorf("Error reading licenses.json - %v", err)
		return nil, err
	}

	err = json.Unmarshal(raw, &licenses)
	if err != nil {
		log.Errorf("Error unmarshalling licenses.json - %v", err)
		return nil, err
	}

	return
}
