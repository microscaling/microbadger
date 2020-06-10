package main

import (
	"os"

	"github.com/op/go-logging"

	"github.com/microscaling/microbadger/api"
	"github.com/microscaling/microbadger/database"
	"github.com/microscaling/microbadger/encryption"
	"github.com/microscaling/microbadger/hub"
	"github.com/microscaling/microbadger/inspector"
	"github.com/microscaling/microbadger/queue"
	"github.com/microscaling/microbadger/registry"
	"github.com/microscaling/microbadger/utils"
)

var (
	log = logging.MustGetLogger("microbadger")
)

const constPollQueueTimeout = 250 // milliseconds - how often to check the queue for images.

func init() {
	utils.InitLogging()
}

// For this prototype either inspects the image and saves the metadata or shows
// the stored metadata.
func main() {
	var err error

	var image string
	var db database.PgDB
	var qs queue.Service

	if os.Getenv("MB_QUEUE_TYPE") == "nats" {
		qs = queue.NewNatsService()
	} else {
		qs = queue.NewSqsService()
	}

	cmd := utils.GetArgOrLogError("cmd", 1)

	if cmd != "api" && cmd != "inspector" && cmd != "size" {
		image = utils.GetArgOrLogError("image", 2)
	}

	db, err = database.GetDB()
	if err != nil {
		log.Errorf("Failed to get DB: %v", err)
		return
	}

	switch cmd {
	case "api":
		log.Info("starting microbadger api")
		rs := registry.NewService()
		hs := hub.NewService()
		es := encryption.NewService()
		api.StartServer(db, qs, rs, hs, es)
	case "inspector":
		log.Info("starting inspector")
		hs := hub.NewService()
		rs := registry.NewService()
		es := encryption.NewService()
		startInspector(db, qs, hs, rs, es)
	case "size":
		log.Info("starting size inspector")
		rs := registry.NewService()
		es := encryption.NewService()
		startSizeInspector(db, qs, rs, es)
	case "feature":
		log.Infof("Feature image %s", image)
		err := db.FeatureImage(image, true)
		if err != nil {
			log.Error("Failed to feature %s: %v", image, err)
		}
	case "unfeature":
		log.Infof("Unfeature image %s", image)
		err := db.FeatureImage(image, false)
		if err != nil {
			log.Error("Failed to unfeature %s: %v", image, err)
		}
	default:
		log.Errorf("Unrecognised command: %v", cmd)
	}
}

func startInspector(db database.PgDB, qs queue.Service, hs hub.InfoService, rs registry.Service, es encryption.Service) {
	for {
		img := qs.ReceiveImage()
		if img != nil && img.ImageName != "" {
			log.Infof("Received Image: %v", img.ImageName)
			err := inspector.Inspect(img.ImageName, &db, &rs, &hs, qs, es)

			// If we failed to inspect this item, it might well be because Docker Hub has behaved badly.
			// By not deleting it, it will get resent again at some point in the future.
			if err == nil {
				qs.DeleteImage(img)

				// Getting the size information can take a while so we do this asynchronously.
				// We have different environment variables for deciding which queue to send & receive on.
				qs.SendImage(img.ImageName, "Sent for size inspection")
			} else {
				log.Errorf("Failed to inspect %s", img.ImageName)
			}
		}
	}
}

func startSizeInspector(db database.PgDB, qs queue.Service, rs registry.Service, es encryption.Service) {
	for {
		img := qs.ReceiveImage()
		if img != nil {
			log.Debugf("Received Image for size processing: %v", img.ImageName)
			err := inspector.InspectSize(img.ImageName, &db, &rs, es)
			if err == nil {
				qs.DeleteImage(img)
			} else {
				log.Errorf("size inspection of %s: %v", img.ImageName, err)
			}
		}
	}
}
