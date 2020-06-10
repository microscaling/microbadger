package database

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jinzhu/gorm"
	logging "github.com/op/go-logging"
)

var (
	log = logging.MustGetLogger("mmdata")
)

// TableName for Registry
func (r Registry) TableName() string {
	return "registries"
}

// ImageList is used to send a list of images on the API.
// TODO We should probably move everything to use ImageInfoList
type ImageList struct {
	CurrentPage int      `json:",omitempty"`
	PageCount   int      `json:",omitempty"`
	ImageCount  int      `json:",omitempty"`
	Images      []string `json:",omitempty"`
}

// ImageInfo returns lists of images with pagination
type ImageInfoList struct {
	CurrentPage int
	PageCount   int
	ImageCount  int
	Images      []ImageInfo
}

// ImageInfo has summary info for display
type ImageInfo struct {
	ImageName string
	Status    string
	IsPrivate bool
}

// RegistryList lists the registries
type RegistryList struct {
	UserID            uint
	EnabledImageCount int
	Registries        []Registry
}

// Registry is a supported docker registry
type Registry struct {
	ID              string `gorm:"primary_key"`
	Name            string
	Url             string
	CredentialsName string `gorm:"-"`
}

// Image is an image
type Image struct {
	// Fields managed by gorm
	Name            string    `gorm:"primary_key" json:"-"`
	Status          string    `json:"-"`
	Featured        bool      `json:"-"`
	Latest          string    `sql:"REFERENCES image_versions(sha) ON DELETE RESTRICT" json:"LatestSHA"`
	BadgeCount      int       `json:"-"` // Number of badges we can generate for this image
	AuthToken       string    `json:"-"`
	WebhookURL      string    `gorm:"column:web_hook_url" json:",omitempty"`
	CreatedAt       time.Time `json:"-"`         // Auto-updated
	UpdatedAt       time.Time `json:"UpdatedAt"` // Auto-updated - This is used to show when we last inspected the image
	Description     string    `json:",omitempty"`
	IsPrivate       bool      `json:"-"`
	IsAutomated     bool      `json:"-"`
	LastUpdated     time.Time `json:",omitempty"` // This is what the hub API tells us
	BadgesInstalled int       `json:"-"`          // Number of badges for this image we have found (so far we only look on Docker Hub)
	PullCount       int
	StarCount       int

	// Gorm auto-updating doesn't work well as we have our own primary key, so we handle this ourselves
	Versions []ImageVersion `gorm:"-" json:",omitempty"`

	// These are json-only fields that are copied from the latest version
	// TODO We could move more of these to ImageVersion
	ID           string            `gorm:"-" json:"Id,omitempty"` // SHA from latest version
	ImageName    string            `gorm:"-" json:",omitempty"`   // Name but with library removed for base images
	ImageURL     string            `gorm:"-" json:",omitempty"`
	Author       string            `gorm:"-" json:",omitempty"`
	LayerCount   int               `gorm:"-" json:",omitempty"`
	DownloadSize int64             `gorm:"-" json:",omitempty"`
	Labels       map[string]string `gorm:"-" json:",omitempty"`
	LatestTag    string            `gorm:"-" json:"LatestVersion,omitempty"`
}

// ImageVersion is a version of an image
type ImageVersion struct {
	SHA            string            `gorm:"primary_key"`
	Tags           []Tag             `json:"Tags"`
	ImageName      string            `gorm:"primary_key" sql:"REFERENCES images(name) ON DELETE RESTRICT"`
	Author         string            ``
	Labels         string            `json:"-"`
	LabelMap       map[string]string `gorm:"-" json:"Labels,omitempty"`
	LayerCount     int               `sql:"DEFAULT:0"`
	DownloadSize   int64             `sql:"DEFAULT:0"`
	Created        time.Time         // From Registry, tells us when this image version was created
	Layers         string            `json:"-"`
	LayersArray    []ImageLayer      `gorm:"-" json:"Layers,omitempty"`
	Manifest       string            `json:"-"`
	Hash           string            `gorm:"index" json:"-"` // Hash of the layers in this image
	Parents        []ImageVersion    `gorm:"-" json:",omitempty"`
	Identical      []ImageVersion    `gorm:"-" json:",omitempty"`
	MicrobadgerURL string            `gorm:"-" json:",omitempty"`

	// JSON only fields for data parsed from the labels.
	License        *License        `gorm:"-" json:",omitempty"`
	VersionControl *VersionControl `gorm:"-" json:",omitempty"`
}

// Tag is assigned to an image version
type Tag struct {
	Tag       string `gorm:"primary_key" json:"tag"`
	ImageName string `gorm:"primary_key" json:"-" sql:"REFERENCES images(name) ON DELETE RESTRICT" `
	SHA       string `gorm:"index" json:",omitempty" sql:"REFERENCES image_versions(sha) on DELETE RESTRICT"`
}

// ImageLayer is the detail of layers that make up an image version. TODO!! Consider storing these so we can pull them and store the details
type ImageLayer struct {
	BlobSum      string `gorm:"-"` // TODO! We need this in API for calculating hashes, but we don't want it travelling on the API
	Command      string `gorm:"-" json:"Command"`
	DownloadSize int64  `gorm:"-" json:"DownloadSize"`
}

// License is parsed from the org.label-schema.license label.
type License struct {
	Code string `json:"Code,omitempty"`
	URL  string `json:"URL,omitempty"`
}

// VersionControl is parsed from the org.label-schema.vcs-* labels.
type VersionControl struct {
	Type   string
	URL    string
	Commit string
}

// User is a user, and has to refer to potentially multiple authorizations
type User struct {
	gorm.Model  `json:"-"`
	Name        string       `json:",omitempty"`
	Email       string       `json:",omitempty"`
	AvatarURL   string       `json:",omitempty"`
	Auths       []UserAuth   `json:"-" gorm:"ForeignKey:UserID"`
	UserSetting *UserSetting `gorm:"ForeignKey:UserID" json:",omitempty"`
}

// UserAuth is the identify of this user for an OAuth provider
type UserAuth struct {
	UserID uint `gorm:"primary_key"`

	Provider         string `gorm:"primary_key"`
	NameFromAuth     string
	IDFromAuth       string
	NicknameFromAuth string
}

// UserImagePermission holds permissions for user access to private images
type UserImagePermission struct {
	UserID    uint   `gorm:"primary_key"`
	ImageName string `gorm:"primary_key"`

	CreatedAt time.Time `json:"-"` // Auto-updated
	UpdatedAt time.Time `json:"-"` // Auto-updated
}

// UserRegistryCredential holds credentials for accessing private images
type UserRegistryCredential struct {
	RegistryID string `gorm:"primary_key"`
	UserID     uint   `gorm:"primary_key"`

	User              string
	Password          string `gorm:"-"`
	EncryptedPassword string `json:"-"`
	EncryptedKey      string `json:"-"`

	CreatedAt time.Time `json:"-"` // Auto-updated
	UpdatedAt time.Time `json:"-"` // Auto-updated
}

// UserSetting holds additional data that is not stored on the session.
type UserSetting struct {
	gorm.Model `json:"-"`
	UserID     uint `gorm:"primary_key" json:"-"`

	NotificationLimit         int  `json:",omitempty"`
	HasPrivateRegistrySupport bool `json:",omitempty"`
}

// Favourite is an image that a user wanted to keep track of
type Favourite struct {
	User   User
	UserID uint `gorm:"primary_key"`

	ImageName string `gorm:"primary_key" sql:"REFERENCES images(name) ON DELETE RESTRICT"`
}

type IsFavourite struct {
	IsFavourite bool
}

// Notification is an image that a user wants to be notified when it changes
type Notification struct {
	ID           uint                  `json:",omitempty", gorm:"primary_key"`
	UserID       uint                  `json:"-" gorm:"ForeignKey:UserID" sql:"REFERENCES users(id) ON DELETE RESTRICT"`
	ImageName    string                `json:",omitempty" gorm:"ForeignKey:ImageName" sql:"REFERENCES images(name) ON DELETE RESTRICT"`
	WebhookURL   string                `json:",omitempty"`
	PageURL      string                `json:",omitempty" gorm: "-"`
	HistoryArray []NotificationMessage `json:"History,omitempty", gorm:"-"`
}

// NotificationMessage is a message sent to a webhook
type NotificationMessage struct {
	gorm.Model     `json:"-"`
	NotificationID uint   `json:"-" gorm:"ForeignKey:NotificationID" sql:"REFERENCES notifications(id) ON DELETE RESTRICT"`
	ImageName      string `json:"-"`
	WebhookURL     string
	Message        PostgresJSON `gorm:"type:jsonb"`
	Attempts       int
	StatusCode     int
	Response       string
	SentAt         time.Time
}

type NotificationMessageChanges struct {
	Text        string `json:"text"`
	ImageName   string `json:"image_name"`
	NewTags     []Tag  `json:"new_tags"`
	ChangedTags []Tag  `json:"changed_tags"`
	DeletedTags []Tag  `json:"deleted_tags"`
}

// NotificationList is a list of notifications with the count and limit for this user
type NotificationList struct {
	NotificationCount int                  `gorm:"-"`
	NotificationLimit int                  `gorm:"-"`
	Notifications     []NotificationStatus `gorm:"-"`
}

// NotificationStatus is a notification with its most recently sent message
type NotificationStatus struct {
	ID         int
	ImageName  string
	WebhookURL string
	Message    PostgresJSON
	StatusCode int
	Response   string
	SentAt     time.Time
}

// IsNotification returns whether a notification exists as well as the current count and limit
type IsNotification struct {
	NotificationCount int
	NotificationLimit int
	Notification      Notification
}

// Implement sql.Scanner interface to save a json.RawMessage to the database
type PostgresJSON struct {
	json.RawMessage
}

func (j PostgresJSON) Value() (driver.Value, error) {
	return j.MarshalJSON()
}

func (j *PostgresJSON) Scan(src interface{}) error {
	if data, ok := src.([]byte); ok {
		return j.UnmarshalJSON(data)
	}
	return fmt.Errorf("Type assertion failed - src is type %T", src)
}
