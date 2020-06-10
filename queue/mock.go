package queue

// MockService for tests
type MockService struct{}

// make sure it satisfies the interface
var _ Service = (*MockService)(nil)

func NewMockService() MockService {
	return MockService{}
}

// SendImage on mock queue always succeeds
func (q MockService) SendImage(imageName string, state string) (err error) {
	log.Infof("Sending %s image %s to queue", state, imageName)
	return nil
}

// ReceiveImage on mock queue always succeeds
func (q MockService) ReceiveImage() *ImageQueueMessage {
	log.Infof("Received image with no name from mock queue.")
	return &ImageQueueMessage{}
}

// DeleteImage on mock queue always succeeds
func (q MockService) DeleteImage(img *ImageQueueMessage) error {
	log.Infof("Deleted image %s from queue.", img.ImageName)
	return nil
}

// SendNotification on mock queue always succeeds
func (q MockService) SendNotification(id uint) (err error) {
	log.Infof("Sending notification message %d to queue", id)
	return nil
}

// ReceiveNotification on mock queue always succeeds
func (q MockService) ReceiveNotification() *NotificationQueueMessage {
	log.Infof("Received notification from mock queue.")
	return &NotificationQueueMessage{}
}

// DeleteNotification on mock queue always succeeds
func (q MockService) DeleteNotification(notify *NotificationQueueMessage) error {
	log.Info("Deleted notification from queue.")
	return nil
}
