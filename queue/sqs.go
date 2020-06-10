package queue

import (
	"encoding/json"
	"os"
	"strconv"

	"github.com/op/go-logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
)

const (
	constImagePagePath    = "/images/"
	constSentStateMessage = "Sent"
)

var (
	log = logging.MustGetLogger("mmqueue")
)

// SqsService for sending and receiving messages on SQS
type SqsService struct {
	svc *sqs.SQS
	// Image send & receive queues are different for inspector & size processes,
	// so that's why we need both send & receive
	imageSendQueueURL    string
	imageReceiveQueueURL string
	notificationQueueURL string
}

// make sure it satisfies the interface
var _ Service = (*SqsService)(nil)

// NewSqsService opens a new session with SQS
func NewSqsService() SqsService {
	svc := sqs.New(session.New())
	receiveQueueURL := os.Getenv("SQS_RECEIVE_QUEUE_URL")

	// Enable long polling of up to 5 seconds on the queue we're receiving on
	_, err := svc.SetQueueAttributes(&sqs.SetQueueAttributesInput{
		QueueUrl: aws.String(receiveQueueURL),
		Attributes: aws.StringMap(map[string]string{
			"ReceiveMessageWaitTimeSeconds": strconv.Itoa(5),
		}),
	})
	if err != nil {
		log.Errorf("Unable to update queue at %s: %v", receiveQueueURL, err)
	}

	return SqsService{
		svc:                  svc,
		imageSendQueueURL:    os.Getenv("SQS_SEND_QUEUE_URL"),
		imageReceiveQueueURL: receiveQueueURL,
		notificationQueueURL: os.Getenv("SQS_NOTIFY_QUEUE_URL"),
	}
}

// SendImage to the SQS queue for processing by the Inspector.
func (q SqsService) SendImage(imageName string, state string) (err error) {
	log.Debugf("Sending image %s to queue", imageName)

	msg := ImageQueueMessage{
		ImageName: imageName,
	}

	bytes, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Error: %v", err)
		return err
	}

	err = q.sqsSend(q.imageSendQueueURL, string(bytes))
	if err != nil {
		log.Errorf("Failed to send image %s: %v", imageName, err)
		return
	}

	log.Infof("%s image %s to queue", state, imageName)
	return err
}

// ReceiveImage from the SQS queue for processing by the inspector.
func (q SqsService) ReceiveImage() *ImageQueueMessage {
	log.Debugf("Receiving on queue %s", q.imageReceiveQueueURL)

	msg, err := q.sqsReceive(q.imageReceiveQueueURL)
	if err != nil {
		log.Errorf("Error receiving image from queue: %v", err)
		return nil
	}

	if msg != nil {
		var img ImageQueueMessage

		err = json.Unmarshal([]byte(*msg.Body), &img)
		if err != nil {
			log.Errorf("Error unmarshaling from %v, error is %v", img, err)
			return nil
		}

		log.Infof("Received image %s from queue.", img.ImageName)

		img.ReceiptHandle = msg.ReceiptHandle

		return &img
	}

	return nil
}

// DeleteImage from the queue once it has successfully been inspected.
func (q SqsService) DeleteImage(img *ImageQueueMessage) error {
	log.Debugf("Deleting image %s from queue.", img.ImageName)

	err := q.sqsDelete(q.imageReceiveQueueURL, img.ReceiptHandle)
	if err == nil {
		log.Infof("Deleted image %s from queue.", img.ImageName)
	}

	return err
}

// SendNotification  to the SQS queue for processing by the Notifier.
func (q SqsService) SendNotification(notificationID uint) (err error) {
	log.Debugf("Sending notification %s to queue", notificationID)

	msg := NotificationQueueMessage{
		NotificationID: notificationID,
	}

	bytes, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Error: %v", err)
		return err
	}

	q.sqsSend(q.notificationQueueURL, string(bytes))

	return err
}

// ReceiveNotification from the SQS queue for sending by the notifier
func (q SqsService) ReceiveNotification() *NotificationQueueMessage {
	log.Debug("Checking queue for notification messages.")

	msg, err := q.sqsReceive(q.notificationQueueURL)
	if err != nil {
		log.Errorf("Error receiving notification from queue: %v", err)
		return nil
	}

	if msg != nil {
		var notification NotificationQueueMessage

		err = json.Unmarshal([]byte(*msg.Body), &notification)
		if err != nil {
			log.Errorf("Error unmarshaling from %v, error is %v", notification, err)
			return nil
		}

		log.Infof("Received message for notification %d from queue.", notification.NotificationID)

		notification.ReceiptHandle = msg.ReceiptHandle

		return &notification
	}

	return nil
}

// DeleteNotification from the queue once it has been sent.
func (q SqsService) DeleteNotification(notification *NotificationQueueMessage) error {
	log.Debugf("Deleting message %s from queue.", notification.NotificationID)

	err := q.sqsDelete(q.notificationQueueURL, notification.ReceiptHandle)
	if err == nil {
		log.Infof("Deleted message %s from queue.", notification.NotificationID)
	}

	return err
}

func (q SqsService) sqsSend(queueURL string, message string) (err error) {
	log.Debugf("Sending on queue %s", queueURL)

	params := &sqs.SendMessageInput{
		MessageBody: aws.String(message),
		QueueUrl:    aws.String(queueURL),
	}

	_, err = q.svc.SendMessage(params)
	if err != nil {
		log.Errorf("SQS send Error: %v", err)
	}

	return err
}

func (q SqsService) sqsReceive(queueURL string) (msg *sqs.Message, err error) {
	params := &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(queueURL),
		MaxNumberOfMessages: aws.Int64(1),
		WaitTimeSeconds:     aws.Int64(5),
	}

	resp, err := q.svc.ReceiveMessage(params)
	if err != nil {
		log.Errorf("SQS receive error: %v", err)
		return
	}

	if len(resp.Messages) > 0 {
		msg = resp.Messages[0]
	}

	return
}

func (q SqsService) sqsDelete(queueURL string, receiptHandle *string) error {
	params := &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(queueURL),
		ReceiptHandle: aws.String(*receiptHandle),
	}

	_, err := q.svc.DeleteMessage(params)
	if err != nil {
		log.Errorf("Error deleting SQS message: %v", err)
	}
	return err
}
