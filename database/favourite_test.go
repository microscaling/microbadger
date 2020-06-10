// +build dbrequired

package database

import (
	"testing"

	"github.com/markbates/goth"
)

func TestFavourites(t *testing.T) {
	var err error
	var db PgDB
	var isFav bool

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	u, err := db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "12345", Name: "myname", Email: "me@myaddress.com"})
	if err != nil {
		t.Errorf("Error creating user %v", err)
	}

	u2, err := db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "67890", Name: "anothername", Email: "another@theiraddress.com"})
	if err != nil {
		t.Errorf("Error creating user %v", err)
	}

	// Check there are no favourites
	favs := db.GetFavourites(u)
	if favs.Images != nil {
		t.Errorf("Unexpected favourites %v", favs)
	}

	favs = db.GetFavourites(u2)
	if favs.Images != nil {
		t.Errorf("Unexpected favourites %v", favs)
	}

	// Add, get and delete a favourite
	_, err = db.PutFavourite(u, "lizrice/childimage")
	if err != nil {
		t.Errorf("Failed to create favourite")
	}

	isFav, err = db.GetFavourite(u, "lizrice/childimage")
	if err != nil {
		t.Errorf("Error getting favourite")
	}
	if !isFav {
		t.Errorf("Added favourite doesn't exist")
	}

	favs = db.GetFavourites(u)
	if (len(favs.Images) != 1) || (favs.Images[0] != "lizrice/childimage") {
		t.Errorf("Unexpected favourites %v", favs)
	}

	err = db.DeleteFavourite(u, "lizrice/childimage")
	if err != nil {
		t.Errorf("Failed to create favourite")
	}

	// Check we can do this for a second user
	favs = db.GetFavourites(u2)
	if favs.Images != nil {
		t.Errorf("Unexpected favourites %v", favs)
	}

	_, err = db.PutFavourite(u2, "lizrice/childimage")
	if err != nil {
		t.Errorf("Failed to create favourite")
	}

	// Check this is now showing up as a favourite for u2 but not u
	favs = db.GetFavourites(u2)
	if (len(favs.Images) != 1) || (favs.Images[0] != "lizrice/childimage") {
		t.Errorf("Unexpected favourites %v", favs)
	}

	favs = db.GetFavourites(u)
	if favs.Images != nil {
		t.Errorf("Unexpected favourites %v", favs)
	}

	// Check that we can have more than one favourite for a user
	_, err = db.PutFavourite(u2, "lizrice/featured")
	if err != nil {
		t.Errorf("Failed to create favourite %v", err)
	}

	favs = db.GetFavourites(u2)
	if len(favs.Images) != 2 {
		t.Errorf("Unexpected favourites %v", favs)
	}

	// And check that deleting one leaves the other in place
	err = db.DeleteFavourite(u2, "lizrice/childimage")
	if err != nil {
		t.Errorf("Failed to delete favourite")
	}

	favs = db.GetFavourites(u2)
	if (len(favs.Images) != 1) || (favs.Images[0] != "lizrice/featured") {
		t.Errorf("Unexpected favourites %v", favs)
	}

	// this image should still be a favourite for u2
	isFav, err = db.GetFavourite(u2, "lizrice/featured")
	if err != nil {
		t.Errorf("Error getting favourite")
	}
	if !isFav {
		t.Errorf("Added favourite doesn't exist")
	}

	// but this image should no longer be a favourite for u2
	isFav, err = db.GetFavourite(u2, "lizrice/childimage")
	if err != nil {
		t.Errorf("Error getting favourite: %s", err)
	}
	if isFav {
		t.Errorf("Image is unexpectedly a favourite")
	}

	// Error cases
	_, err = db.PutFavourite(u, "missing")
	if err == nil {
		t.Errorf("Shouldn't have been able to create a favourite for an image that doesn't exist")
	}

	_, err = db.PutFavourite(User{}, "lizrice/childimage")
	if err == nil {
		t.Errorf("Shouldn't have been able to create a favourite for a user that doesn't exist")
	}
}
