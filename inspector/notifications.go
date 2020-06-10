package inspector

import (
	"encoding/json"

	"github.com/microscaling/microbadger/database"
	"github.com/microscaling/microbadger/queue"
)

// buildNotifications sends an SQS message to the notifier for each user who needs to be notified about this change
func buildNotifications(db *database.PgDB, qs queue.Service, imageName string, nmc database.NotificationMessageChanges) {
	var nm database.NotificationMessage

	if len(nmc.NewTags) == 0 && len(nmc.ChangedTags) == 0 && len(nmc.DeletedTags) == 0 {
		return
	}

	log.Debugf("%s has changes that may need to generate notifications", imageName)
	notifications, err := db.GetNotificationsForImage(imageName)
	if err != nil {
		log.Errorf("Failed to generate notifications for %s", imageName)
		return
	}

	nmcAsJson, err := json.Marshal(nmc)
	if err != nil {
		log.Errorf("Failed to generate NMC message: %v", err)
		return
	}

	log.Infof("Generating %d notifications for image %s", len(notifications), imageName)

	// TODO!! We could consider having one SQS message per image, and have the notifier generate all the webhooks
	// We'll need to send a notification message for all the notifications for this image
	for _, n := range notifications {
		// Save an unsent notification message
		nm = database.NotificationMessage{
			NotificationID: n.ID,
			ImageName:      n.ImageName,
			WebhookURL:     n.WebhookURL,
			Message:        database.PostgresJSON{nmcAsJson},
		}

		err := db.SaveNotificationMessage(&nm)
		if err != nil {
			log.Errorf("Failed to create notification message for %s, id %d: %v", imageName, n.ID, err)
			return
		}

		err = qs.SendNotification(nm.ID)
		if err != nil {
			log.Errorf("Failed to send notification message for %s, id %d: %v", imageName, n.ID, err)
		}
	}
}
