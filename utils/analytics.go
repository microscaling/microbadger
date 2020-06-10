package utils

import (
	// "fmt"
	// "io/ioutil"
	"net/http"
	"net/url"
)

const gaUrl = "https://www.google-analytics.com"
const fixedCid = "555"

// See https://developers.google.com/analytics/devguides/collection/protocol/v1/devguide

type GoogleAnalytics struct {
	property string
	host     string
}

func NewGoogleAnalytics(property string, host string) GoogleAnalytics {
	return GoogleAnalytics{
		property: property,
		host:     host,
	}
}

func post(postUrl string, params map[string]string) (err error) {
	vals := make(url.Values, len(params)+1)
	vals.Add("v", "1") // Measurement protocol version
	for key, val := range params {
		vals.Add(key, val)
	}

	_, err = http.PostForm(gaUrl+postUrl, vals)

	// This commented out code lets you view Google Analytics debug response
	// see https://developers.google.com/analytics/devguides/collection/protocol/v1/validating-hits

	// fmt.Printf("Sending analytics event")
	// resp, err := http.PostForm(gaUrl+postUrl, vals)

	// if err != nil {
	// 	fmt.Printf("Error: %v", err)
	// } else {
	// 	htmlData, err := ioutil.ReadAll(resp.Body)
	// 	if err != nil {
	// 		fmt.Printf("Error: %v", err)
	// 	} else {
	// 		fmt.Printf(string(htmlData))
	// 		resp.Body.Close()
	// 	}
	// }

	return err
}

// BadgeImageView records a page view for an image.
// imageURl is the URL without the host e.g. /badges/image/microscaling/microscaling.svg
func (a *GoogleAnalytics) ImageView(dataSource string, campaignSource string, campaignMedium string, campaignName string, r *http.Request) error {

	// GA seems to show these as source / medium / campaign
	params := map[string]string{
		"tid": a.property,
		"ds":  dataSource,
		"cid": fixedCid,
		"t":   "pageview",
		"dh":  a.host,
		"dp":  r.URL.Path,
		"cs":  campaignSource,
		"cm":  campaignMedium,
		"cn":  campaignName,
		"dr":  r.Referer(),
	}

	// Use the /debug/collect version if we want to see Google Analytics debug response
	// return post("/debug/collect", params)
	return post("/collect", params)
}
