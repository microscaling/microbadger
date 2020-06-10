package database

import (
	"fmt"
	"os"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	logging "github.com/op/go-logging"
	"github.com/wader/gormstore"

	"github.com/microscaling/microbadger/utils"
)

func init() {
	utils.InitLogging()
}

const (
	constRecentImagesDays = 14
	constDisplayMaxImages = 5
	constImagesPerPage    = 100
	constDBAttempts       = 5
)

// PgDB is our postgres database
type PgDB struct {
	db           *gorm.DB
	SessionStore *gormstore.Store
	SiteURL      string
}

// Exec does a raw SQL command on the database
func (d *PgDB) Exec(cmd string, params ...interface{}) {
	d.db.Exec(cmd, params...)
}

// GetDB returns a database connection.
func GetDB() (db PgDB, err error) {
	host := utils.GetEnvOrDefault("MB_DB_HOST", "postgres")
	user := utils.GetEnvOrDefault("MB_DB_USER", "microbadger")
	dbname := utils.GetEnvOrDefault("MB_DB_NAME", "microbadger")
	password := utils.GetEnvOrDefault("MB_DB_PASSWORD", "microbadger")

	dbLogLevel := logging.GetLevel("mmdata")
	db, err = GetPostgres(host, user, dbname, password, (dbLogLevel == logging.DEBUG))
	return db, err
}

// GetPostgres opens a database connection
func GetPostgres(host string, user string, dbname string, password string, debug bool) (db PgDB, err error) {
	params := fmt.Sprintf("host=%s user=%s dbname=%s sslmode=disable password=%s", host, user, dbname, password)
	log.Debugf("Opening postgres with params %s", params)
	db = PgDB{}
	var gormDb *gorm.DB

	attempts := 0
	for {
		gormDb, err = gorm.Open("postgres", params)
		if err == nil {
			break
		}

		time.Sleep(1 * time.Second)
		attempts++
		if attempts <= constDBAttempts {
			log.Debugf("Sleep %d trying to connect to database: %v", attempts, err)
		} else {
			log.Errorf("Error connecting to database: %v", err)
			log.Debugf("%s", params)
			return db, err
		}
	}

	if debug {
		// log.Info("Database logging on")
		db.db = gormDb.Debug()
	} else {
		// log.Info("Database logging off")
		db.db = gormDb
	}

	db.db.AutoMigrate(&Registry{}, &Image{}, &ImageVersion{}, &Tag{}, &Favourite{}, &User{}, &UserAuth{}, &UserSetting{}, &UserImagePermission{}, &UserRegistryCredential{}, &Notification{}, &NotificationMessage{})

	// Session store
	db.SessionStore = gormstore.New(db.db, []byte(os.Getenv("MB_SESSION_SECRET")))
	// db cleanup every hour - close quit channel to stop cleanup (We don't do this)
	quit := make(chan struct{})
	go db.SessionStore.PeriodicCleanup(1*time.Hour, quit)

	// Initialize the Docker Hub registry if it's not already there
	_, dockerMissing := db.GetRegistry("docker")
	if dockerMissing != nil {
		db.PutRegistry(&Registry{ID: "docker", Name: "Docker Hub", Url: "https://hub.docker.com"})
	}

	// For building URLs
	db.SiteURL = utils.GetEnvOrDefault("MB_SITE_URL", "https://microbadger.com")

	return db, err
}
