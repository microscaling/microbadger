package main

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/op/go-logging"

	"github.com/microscaling/microbadger/database"
	"github.com/microscaling/microbadger/queue"
	"github.com/microscaling/microbadger/utils"
)

var (
	log = logging.MustGetLogger("mbnotify")
)

const constPollQueueTimeout = 250 // milliseconds - how often to check the queue for messages.
const constNotificationRetries = 5

func init() {
	utils.InitLogging()
}

func main() {
	var err error

	var db database.PgDB
	var qs queue.Service

	db, err = database.GetDB()
	if err != nil {
		log.Errorf("Failed to get DB: %v", err)
		return
	}

	if os.Getenv("MB_QUEUE_TYPE") == "nats" {
		qs = queue.NewNatsService()
	} else {
		qs = queue.NewSqsService()
	}

	log.Info("starting notifier")
	startNotifier(db, qs)
}

// Polls an SQS queue for notifications that need to be sent.
func startNotifier(db database.PgDB, qs queue.Service) {
	pollQueueTimeout := time.NewTicker(constPollQueueTimeout * time.Millisecond)
	for range pollQueueTimeout.C {
		msg := qs.ReceiveNotification()
		if msg != nil {
			notifyMsgID := msg.NotificationID

			log.Infof("Sending notification for: %d", notifyMsgID)
			success, attempts, err := sendNotification(db, notifyMsgID)
			if err != nil {
				log.Errorf("Error sending notification for %d: %v", notifyMsgID, err)
			}

			// Don't retry more than a certain number of times
			if success || (attempts >= constNotificationRetries) {
				log.Infof("Notification %d stopping after %d attempts", notifyMsgID, attempts)
				qs.DeleteNotification(msg)
			}
		}
	}
}

// Sends a notification that an image has changed.
func sendNotification(db database.PgDB, notifyMsgID uint) (success bool, attempts int, err error) {
	nm, err := db.GetNotificationMessage(notifyMsgID)
	if err != nil {
		return false, 0, err
	}

	// Call the webhook to send the notification.
	statusCode, resp, err := postMessage(nm.WebhookURL, nm.Message.RawMessage)
	if err != nil {
		log.Errorf("Error sending notification %v", err)
	}

	nm.Attempts++
	nm.StatusCode = statusCode
	nm.Response = string(resp)
	nm.SentAt = time.Now()

	// If the webhook returns a response in the 200s its counted as a success.
	if statusCode >= 200 && statusCode <= 299 {
		success = true
	} else {
		log.Infof("Notification response %d for ID %d", statusCode, notifyMsgID)
	}

	err = db.SaveNotificationMessage(&nm)
	return success, nm.Attempts, err
}

// Post a message to a webhook.
func postMessage(url string, request []byte) (status int, response []byte, err error) {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(request))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return status, response, err
	}
	defer resp.Body.Close()

	response, err = ioutil.ReadAll(resp.Body)

	return resp.StatusCode, response, err
}
