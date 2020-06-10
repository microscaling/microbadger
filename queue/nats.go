package queue

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	nats "github.com/nats-io/nats.go"
)

// NatsService for sending and receiving messages to Nats.
type NatsService struct {
	nc *nats.Conn
	// Image send & receive queues are different for inspector & size processes,
	// so that's why we need both send & receive
	imageSendQueueName    string
	imageReceiveQueueName string
	notificationQueueName string
}

// NewNatsService opens a new session with Nats.
func NewNatsService() NatsService {
	baseURL := os.Getenv("NATS_BASE_URL")

	nc, err := nats.Connect(baseURL)
	if err != nil {
		log.Errorf("Unable to connect to queue at %s: %v", nats.DefaultURL, err)
	}

	receiveQueueURL := os.Getenv("NATS_RECEIVE_QUEUE_NAME")

	return NatsService{
		nc:                    nc,
		imageSendQueueName:    os.Getenv("NATS_SEND_QUEUE_NAME"),
		imageReceiveQueueName: receiveQueueURL,
		notificationQueueName: os.Getenv("MB_NOTIFY_QUEUE_NAME"),
	}
}

// SendImage to the Nats queue for processing by the Inspector.
func (q NatsService) SendImage(imageName string, state string) (err error) {
	log.Debugf("Sending image %s to queue", imageName)

	msg := ImageQueueMessage{
		ImageName: imageName,
	}

	bytes, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Error: %v", err)
		return err
	}

	err = q.natsSend(q.imageSendQueueName, bytes)
	if err != nil {
		log.Errorf("Failed to send image %s: %v", imageName, err)
		return
	}

	log.Infof("%s image %s to queue", state, imageName)
	return err
}

// ReceiveImage from the Nats queue for processing by the inspector.
func (q NatsService) ReceiveImage() *ImageQueueMessage {
	log.Debugf("Receiving on queue %s", q.imageReceiveQueueName)

	msg, err := q.natsReceive(q.imageReceiveQueueName)
	if err != nil {
		log.Errorf("Error receiving image from queue: %v", err)
		return nil
	}

	if msg != nil {
		var img ImageQueueMessage

		err = json.Unmarshal(msg, &img)
		if err != nil {
			log.Errorf("Error unmarshaling from %v, error is %v", img, err)
			return nil
		}

		log.Infof("Received image %s from queue.", img.ImageName)

		return &ImageQueueMessage{
			ImageName: img.ImageName,
		}
	}

	return nil
}

// DeleteImage from the queue once it has successfully been inspected.
func (q NatsService) DeleteImage(img *ImageQueueMessage) error {
	log.Infof("Deleting image %s not implemented for NATS.", img.ImageName)
	return nil
}

// SendNotification  to the SQS queue for processing by the Notifier.
func (q NatsService) SendNotification(notificationID uint) (err error) {
	log.Debugf("Sending notification %s to queue", notificationID)

	msg := NotificationQueueMessage{
		NotificationID: notificationID,
	}

	bytes, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Error: %v", err)
		return err
	}

	q.natsSend(q.notificationQueueName, bytes)

	return err
}

// ReceiveNotification from the SQS queue for sending by the notifier
func (q NatsService) ReceiveNotification() *NotificationQueueMessage {
	log.Debug("Checking queue for notification messages.")

	msg, err := q.natsReceive(q.notificationQueueName)
	if err != nil {
		log.Errorf("Error receiving notification from queue: %v", err)
		return nil
	}

	if msg != nil {
		var notification NotificationQueueMessage

		err = json.Unmarshal(msg, &notification)
		if err != nil {
			log.Errorf("Error unmarshaling from %v, error is %v", notification, err)
			return nil
		}

		log.Infof("Received message for notification %d from queue.", notification.NotificationID)

		return &notification
	}

	return nil
}

// DeleteNotification from the queue once it has been sent.
func (q NatsService) DeleteNotification(notification *NotificationQueueMessage) error {
	log.Infof("Deleting notification %d not implemented for NATS.", notification.NotificationID)
	return nil
}

func (q NatsService) natsSend(queueName string, message []byte) (err error) {
	log.Debugf("Sending on queue %s", queueName)

	err = q.nc.Publish(queueName, message)
	if err != nil {
		log.Errorf("Nats send Error: %v", err)
	}

	return err
}

func (q NatsService) natsReceive(queueName string) (message []byte, err error) {
	sub, err := q.nc.SubscribeSync(queueName)
	if err != nil {
		log.Errorf("NATS subscribe error: %v", err)
		return
	}

	msg, err := sub.NextMsg(30 * time.Second)
	if errors.Is(err, nats.ErrTimeout) {
		// Waiting for a message timed out. We return nil and will retry.
		return nil, nil
	} else if err != nil {
		log.Errorf("NATS next message error: %v", err)
		return
	}

	return msg.Data, nil
}
