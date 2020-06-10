package queue

// TODO!! Find a better way of structuring this. Split of responsiblities between here and sender/receivers doesn't seem right
// and I don't like the asymmetry of messages we pass in and out of queues, this all seems a bit long-winded.
// Also, tests.

// ImageQueueMessage is sent to the queue to trigger an inspection.
type ImageQueueMessage struct {
	ImageName     string `json:"ImageName"`
	ReceiptHandle *string
}

// NotificationQueueMessage is sent to the queue to trigger a notification.
type NotificationQueueMessage struct {
	NotificationID uint `json:"NotificationID"`
	ReceiptHandle  *string
}

// Service interface so we can mock it out for tests
// TODO!! There must be a cleaner way that doesn't involve a different set of methods for each type of message
type Service interface {
	SendImage(imageName string, state string) (err error)
	ReceiveImage() *ImageQueueMessage
	DeleteImage(img *ImageQueueMessage) error
	SendNotification(notificationID uint) error
	ReceiveNotification() *NotificationQueueMessage
	DeleteNotification(notify *NotificationQueueMessage) error
}
