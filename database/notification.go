package database

import (
	"errors"
)

const (
	constNotificationStatusesSQL = `
SELECT n.id, n.image_name, n.webhook_url,
	COALESCE(nm.message, '{}') AS message, nm.sent_at, nm.response, nm.status_code 
FROM notifications n
LEFT OUTER JOIN (
	SELECT notification_id, MAX(id) AS max_id
	FROM notification_messages
	GROUP BY notification_id
) nmax ON n.id = nmax.notification_id
LEFT OUTER JOIN notification_messages nm
	ON nmax.notification_id = nm.notification_id
		AND nmax.max_id = nm.id
		AND n.image_name = nm.image_name
WHERE n.user_id = ? ORDER BY n.image_name`
)

// GetNotifications gets all image notifications for a user along with the most
// recently sent message
func (d *PgDB) GetNotifications(user User) (list NotificationList, err error) {
	var notifications []NotificationStatus
	err = d.db.Raw(constNotificationStatusesSQL, user.ID).
		Find(&notifications).Error
	if err != nil {
		log.Errorf("Error getting notifications: %v", err)
	}

	us, err := d.GetUserSetting(user)
	if err != nil {
		log.Errorf("Error getting user settings: %v", err)
	}

	list = NotificationList{
		NotificationCount: len(notifications),
		NotificationLimit: us.NotificationLimit,
		Notifications:     notifications,
	}

	return list, err
}

// GetNotification returns an image notification for this user
func (d *PgDB) GetNotification(user User, id int) (notify Notification, err error) {
	err = d.db.Table("notifications").
		Where(`"id" = ? AND "user_id" = ?`, id, user.ID).
		First(&notify).Error
	if err != nil {
		log.Errorf("Error getting notification %d for user %d: %v", id, user.ID, err)
	}

	img, err := d.GetImage(notify.ImageName)
	if err != nil {
		log.Errorf("Error getting image %s for notification %d: %v", notify.ImageName, id, err)
	}

	// Set URL depending on whether image is public or private
	notify.PageURL = d.GetPageURL(img)

	return notify, err
}

// GetNotificationCount returns the number of notifications for a a user
func (d *PgDB) GetNotificationCount(user User) (count int, err error) {
	err = d.db.Table("notifications").
		Where("user_id = ?", user.ID).Count(&count).Error
	if err != nil {
		log.Errorf("Error getting notifications count: %v", err)
	}

	return count, err
}

// GetNotificationHistory returns a slice of the most recent notification messages
func (d *PgDB) GetNotificationHistory(id int, image string, count int) (history []NotificationMessage, err error) {
	err = d.db.Table("notification_messages").
		Where(`"notification_id" = ? AND image_name = ?`, id, image).
		Order("created_at DESC").
		Limit(count).
		Find(&history).Error
	if err != nil {
		log.Errorf("Error getting history for notification %d - %v", id, err)
	}

	return history, err
}

// CreateNotification creates it
func (d *PgDB) CreateNotification(user User, notify Notification) (Notification, error) {
	_, err := d.GetImage(notify.ImageName)
	if err != nil {
		log.Errorf("Error getting image %s - %v", notify.ImageName, err)
		return notify, err
	}

	count, err := d.GetNotificationCount(user)
	if err != nil {
		log.Errorf("Error getting notification count for user - %v", err)
		return notify, err
	}

	us, err := d.GetUserSetting(user)
	if err != nil {
		log.Errorf("Error getting user settings: %v", err)
		return notify, err
	}

	if count >= us.NotificationLimit {
		err = errors.New("Failed to create notification as limit is exceeded")
		return notify, err
	}

	err = d.db.Table("notifications").
		Where(`"user_id" = ? AND "image_name" = ?`, notify.UserID, notify.ImageName).
		FirstOrCreate(&notify).Error
	if err != nil {
		log.Errorf("Create Notification error %v", err)
		return notify, err
	}

	err = d.db.Save(&notify).Error
	if err != nil {
		log.Errorf("Create Notification error 2: %v", err)
	}

	return notify, err
}

// UpdateNotification updates it
func (d *PgDB) UpdateNotification(user User, id int, input Notification) (Notification, error) {
	_, err := d.GetImage(input.ImageName)
	if err != nil {
		log.Errorf("Error getting image %s - %v", input.ImageName, err)
		return input, err
	}

	// Get the saved notification.
	notify, err := d.GetNotification(user, id)
	if err == nil {
		// Set fields that need to be updated.
		notify.ImageName = input.ImageName
		notify.WebhookURL = input.WebhookURL

		err = d.db.Save(&notify).Error
		if err != nil {
			log.Errorf("Update Notification error: %v", err)
		}

	} else {
		log.Errorf("Update Notification error 2: %v", err)
	}

	return notify, err
}

// DeleteNotification deletes a notification, returning an error if it doesn't exist
func (d *PgDB) DeleteNotification(user User, id int) error {
	notify := Notification{}
	err := d.db.Where(`"id" = ? AND "user_id" = ?`, id, user.ID).Delete(&notify).Error
	if err != nil {
		log.Debugf("Error Deleting notification: %v", err)
	}

	return err
}

// CreateNotificationMessage saves whether a notification was sent successfully
func (d *PgDB) CreateNotificationMessage(msg *NotificationMessage) error {
	err := d.db.Table("notification_messages").
		Create(&msg).Error
	if err != nil {
		log.Errorf("Save Notification Message error %v", err)
	}

	return err
}

// GetNotificationMessage saves whether a notification was sent successfully
func (d *PgDB) GetNotificationMessage(id uint) (NotificationMessage, error) {
	var nm NotificationMessage
	err := d.db.First(&nm, id).Error
	if err != nil {
		log.Errorf("Failed to get NotificationMessage: %v", err)
	}

	return nm, err
}

// SaveNotificationMessage updates an existing notification message
func (d *PgDB) SaveNotificationMessage(nm *NotificationMessage) error {
	err := d.db.Save(nm).Error
	if err != nil {
		log.Errorf("Failed to save NotificationMessage: %v", err)
	}

	return err
}

// GetNotificationsForImage gets a list of notifications we need to make for this image
func (d *PgDB) GetNotificationsForImage(imageName string) (n []Notification, err error) {
	err = d.db.Where("image_name = ?", imageName).Find(&n).Error
	return n, err
}

// GetNotificationForUser returns true and the notification if it exists for this user and image
func (d *PgDB) GetNotificationForUser(user User, image string) (bool, Notification) {
	var n Notification

	err := d.db.Where(`"user_id" = ? AND "image_name" = ?`, user.ID, image).
		First(&n).Error
	return (err == nil), n
}
