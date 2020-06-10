// +build dbrequire

package inspector

import (
	"encoding/json"
	"testing"

	"github.com/microscaling/microbadger/database"
)

func addNotificationThings(db database.PgDB) {

}

func TestBuildNotifications(t *testing.T) {
	db := getDatabase(t)
	emptyDatabase(db)
	addNotificationThings(db)

}

func TestNotificationMessageChanges(t *testing.T) {
	nmc := database.NotificationMessageChanges{
		NewTags: []database.Tag{{
			Tag: "Tag1", SHA: "10000", ImageName: "image/name"}},
	}

	msg, err := json.Marshal(nmc)
	if err != nil {
		t.Fatalf("Error with NMC %v", err)
	}

	log.Infof("%s", msg)
}
