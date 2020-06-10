// +build dbrequired

package database

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/markbates/goth"
)

func TestNotifications(t *testing.T) {
	var err error
	var db PgDB
	var imageName = "lizrice/childimage"
	var webhookURL = "https://hooks.slack.com/services/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"
	var secondWebhookURL = "https://hooks.example.com/test"
	var pageURL = "https://microbadger.com/images/lizrice/childimage"
	var count int

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	u, err := db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "12345", Name: "myname", Email: "me@myaddress.com"})
	if err != nil {
		t.Errorf("Error creating user %v", err)
	}

	// Check there are no notifications
	n, _ := db.GetNotifications(u)
	if len(n.Notifications) > 0 {
		t.Errorf("Unexpected notifications %v", n.Notifications)
	}

	count, _ = db.GetNotificationCount(u)
	if count != 0 {
		t.Errorf("Expected notification count to be 0 but was %d", count)
	}

	notify := Notification{UserID: u.ID,
		ImageName:  imageName,
		WebhookURL: webhookURL}

	// Add, get, update, list and delete a notification
	notify, err = db.CreateNotification(u, notify)
	if err != nil {
		t.Errorf("Failed to create notification %v", err)
	}

	count, _ = db.GetNotificationCount(u)
	if count != 1 {
		t.Errorf("Expected notification count to be 1 but was %d", count)
	}

	notify, err = db.GetNotification(u, int(notify.ID))
	if err != nil {
		t.Errorf("Error getting notification")
	}
	if notify.WebhookURL != webhookURL {
		t.Errorf("Added notification doesn't exist")
	}

	if notify.PageURL != pageURL {
		t.Errorf("Expected page URL to be %s but was %s", pageURL, notify.PageURL)
	}

	isPresent, _ := db.GetNotificationForUser(u, imageName)
	if !isPresent {
		t.Errorf("Expected notification was not found for this user")
	}

	isPresent, _ = db.GetNotificationForUser(u, "lizrice/featured")
	if isPresent {
		t.Errorf("Unexpected notification was found for this user")
	}

	notify.WebhookURL = secondWebhookURL
	notify, err = db.UpdateNotification(u, int(notify.ID), notify)
	if notify.WebhookURL != secondWebhookURL {
		t.Errorf("Notification not updated")
	}

	n, _ = db.GetNotifications(u)

	if (len(n.Notifications) != 1) || (n.Notifications[0].WebhookURL != secondWebhookURL) ||
		(n.Notifications[0].StatusCode != 0) {
		t.Errorf("Unexpected notifications %v", n.Notifications)
	}

	nmc := getNotificationMessageChanges()

	nmcAsJSON, err := json.Marshal(nmc)
	if err != nil {
		t.Fatalf("Failed to marshal NMC: %v", err)
	}

	msg := NotificationMessage{NotificationID: notify.ID,
		ImageName:  imageName,
		WebhookURL: webhookURL,
		Attempts:   1,
		StatusCode: 200,
		Response:   `{"errors":[]}"`,
		Message:    PostgresJSON{nmcAsJSON},
	}

	err = db.SaveNotificationMessage(&msg)
	if err != nil {
		t.Errorf("Error saving notification message %v", err)
	}

	n, _ = db.GetNotifications(u)

	if (len(n.Notifications) != 1) || (n.Notifications[0].WebhookURL != secondWebhookURL) ||
		(n.Notifications[0].StatusCode != 200) {
		t.Errorf("Unexpected notifications %v", n.Notifications)
	}

	err = db.DeleteNotification(u, int(notify.ID))
	if err != nil {
		t.Errorf("Failed to delete notification")
	}
}

func TestNotificationLimit(t *testing.T) {
	var err error
	var db PgDB

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	u, err := db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "12345", Name: "myname", Email: "me@myaddress.com"})
	if err != nil {
		t.Errorf("Error creating user %v", err)
	}

	us, _ := db.GetUserSetting(u)
	if us.NotificationLimit != 10 {
		t.Error("Error default notification limit should be 10")
	}

	// Create a notification
	notify := Notification{UserID: u.ID,
		ImageName:  "lizrice/childimage",
		WebhookURL: "http://example.com/webhook"}

	notify, err = db.CreateNotification(u, notify)
	if err != nil {
		t.Errorf("Failed to create notification")
	}

	// Set the notification limit to one so we can easily check what happens when we try to exceed it
	us.NotificationLimit = 1
	err = db.db.Save(us).Error
	if err != nil {
		log.Errorf("Failed to save UserSettings: %v", err)
	}

	notify, err = db.CreateNotification(u, notify)
	if err == nil {
		t.Errorf("Should have failed to create 2nd notification")
	}
}

func TestNotificationMessages(t *testing.T) {
	var err error
	var db PgDB
	var imageName = "lizrice/childimage"
	var webhookURL = "https://hooks.example.com/test"

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	u, err := db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "12345", Name: "myname", Email: "me@myaddress.com"})
	if err != nil {
		t.Errorf("Error creating user %v", err)
	}

	// Create a notification
	notify := Notification{UserID: u.ID,
		ImageName:  imageName,
		WebhookURL: webhookURL}

	notify, err = db.CreateNotification(u, notify)
	if err != nil {
		t.Errorf("Failed to create notification")
	}

	nmc := getNotificationMessageChanges()

	nmcAsJSON, err := json.Marshal(nmc)
	if err != nil {
		t.Fatalf("Failed to marshal NMC: %v", err)
	}

	msg := NotificationMessage{NotificationID: notify.ID,
		ImageName:  imageName,
		WebhookURL: webhookURL,
		Attempts:   1,
		StatusCode: 200,
		Response:   `{"errors":[]}"`,
		Message:    PostgresJSON{nmcAsJSON},
	}

	err = db.SaveNotificationMessage(&msg)
	if err != nil {
		t.Errorf("Error saving notification message %v", err)
	}

	// Test you can get it back out again
	msg2, err := db.GetNotificationMessage(msg.ID)
	if err != nil {
		t.Errorf("Error getting notification message %v", err)
	}

	if msg.ID != msg2.ID {
		log.Errorf("Failed to get notification message ID %d", msg.ID)
	}

	log.Debugf("Message is %s", string(msg2.Message.RawMessage))
	var nmc2 NotificationMessageChanges
	err = json.Unmarshal(msg2.Message.RawMessage, &nmc2)
	if err != nil {
		t.Fatalf("Failed to unmarshal into NMC: %v", err)
	}

	log.Debugf("Got NMC: %v", nmc2)
	if !reflect.DeepEqual(nmc, nmc2) {
		t.Fatalf("NMCs are not the same\n Got %v\n Exp %v", nmc2, nmc)
	}
}

func getNotificationMessageChanges() NotificationMessageChanges {
	nmc := NotificationMessageChanges{
		ImageName:   "lizrice/childimage",
		NewTags:     []Tag{},
		DeletedTags: []Tag{{Tag: "Tagx", SHA: "12345"}},
		ChangedTags: []Tag{},
	}

	return nmc
}
