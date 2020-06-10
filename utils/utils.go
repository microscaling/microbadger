package utils

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	logging "github.com/op/go-logging"
)

var (
	log                = logging.MustGetLogger("microbadger")
	loggingInitialized bool
)

// ParseDockerImage splits input into Docker org, image and tag portions. If no org is
// provided the default library org is returned. If no tag is provided the latest tag is
// returned.
// TODO Latest tag logic is probably incorrect but changing it needs careful testing.
func ParseDockerImage(imageInput string) (org string, image string, tag string) {
	// Separate image and tag for the Registry API.
	if strings.Contains(imageInput, ":") {
		image = strings.Split(imageInput, ":")[0]
		tag = strings.Split(imageInput, ":")[1]
	} else {
		image = imageInput
		tag = "latest"
	}

	// Separate organization and image name.
	if strings.Contains(image, "/") {
		org = strings.Split(image, "/")[0]
		image = strings.Split(image, "/")[1]
	} else {
		org = "library"
	}

	return org, image, tag
}

var badgeRe = regexp.MustCompile(`https?:\/\/images\.microbadger\.com\/badges(\/[a-z\-]*)(\/[a-z0-9\-\._]*)?\/[a-z0-9\-\._]*(:[a-zA-Z0-9\-\._]+)?\.svg`)

// BadgesInstalled counts the number of MicroBadger badges we can find in a string (thus far, this is the Full Description)
func BadgesInstalled(s string) int {
	matches := badgeRe.FindAllString(s, -1)
	return len(matches)
}

// GetEnvOrDefault returns an env var or the default value if it doesn't exist.
func GetEnvOrDefault(name string, defaultValue string) string {
	v := os.Getenv(name)
	if v == "" {
		v = defaultValue
	}

	return v
}

// GetArgOrLogError gets a command line argument or raises an error.
func GetArgOrLogError(name string, i int) string {
	v := ""
	if len(os.Args) >= i+1 {
		v = os.Args[i]
	} else {
		log.Errorf("No command line arg for %v", name)
	}

	return v
}

// InitLogging configures the logging settings.
func InitLogging() {
	if loggingInitialized {
		log.Infof("Logging already initialized")
		return
	}

	// The DH_LOG_DEBUG environment variable controls what logging is output
	// By default the log level is INFO for all components
	// Adding a component name to DH_LOG_DEBUG makes its logging level DEBUG
	// In addition, if "detail" is included in the environment variable details of the process ID and file name / line number are included in the logs
	// DH_LOG_DEBUG="all" - turn on DEBUG for all components
	// DH_LOG_DEBUG="dhinspect,detail" - turn on DEBUG for the api package, and use the detailed logging format
	basicLogFormat := logging.MustStringFormatter(`%{color}%{level:.4s} %{time:15:04:05.000}: %{color:reset} %{message}`)
	detailLogFormat := logging.MustStringFormatter(`%{color}%{level:.4s} %{time:15:04:05.000} %{pid} %{shortfile}: %{color:reset} %{message}`)

	logComponents := GetEnvOrDefault("MB_LOG_DEBUG", "none")
	if strings.Contains(logComponents, "detail") {
		logging.SetFormatter(detailLogFormat)
	} else {
		logging.SetFormatter(basicLogFormat)
	}

	fmt.Println("Init Logging")
	logBackend := logging.NewLogBackend(os.Stderr, "", 0)
	logging.SetBackend(logBackend)

	var components = []string{"microbadger", "mmapi", "mmauth", "mminspect", "mmdata", "mmhub", "mmnotify", "mmqueue", "mmslack", "mmenc"}

	for _, component := range components {
		if strings.Contains(logComponents, component) || strings.Contains(logComponents, "all") {
			logging.SetLevel(logging.DEBUG, component)
		} else {
			logging.SetLevel(logging.INFO, component)
		}
	}
}
