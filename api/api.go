// (c) 2016 Force12io Ltd

package api

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	logging "github.com/op/go-logging"
)

var mbURL string
var log *logging.Logger

func init() {
	mbURL = os.Getenv("MB_API_URL")
	if mbURL == "" {
		mbURL = "https://api.microbadger.com/v1"
	}

	log = logging.MustGetLogger("mbapi")
	logging.SetLevel(logging.INFO, "mbapi")
}

// GetLabels queries Microbadger for the label information associated with this image
func GetLabels(imageName string) (labels map[string]string, err error) {
	i, err := getImage(imageName)
	if err != nil {
		return
	}

	labels = i.Labels
	log.Debugf("Image %s has labels %v", imageName, labels)
	return
}

func getImage(imageName string) (i Image, err error) {
	url := mbURL + "/images/" + imageName
	resp, err := http.Get(url)
	if err != nil {
		log.Errorf("Failed to build API GET request err %v", err)
		return
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("GET request failed %s %d: %s", url, resp.StatusCode, resp.Status)
		log.Errorf("%v", err)
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Failed to read from response to %s", url)
		return
	}

	err = json.Unmarshal(body, &i)
	return
}
