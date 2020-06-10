// +build dbrequired

package database

import (
	"strings"
	"testing"

	"github.com/markbates/goth"

	"github.com/microscaling/microbadger/hub"
)

func TestUsers(t *testing.T) {
	var err error
	var db PgDB

	db = getDatabase(t)
	emptyDatabase(db)

	email := "me@myaddress.com"
	name := "myname"
	avatarURL := "http://gravatar.com/my_avatar.png"

	u, err := db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "12345", Name: name, Email: email, AvatarURL: avatarURL})
	if err != nil {
		t.Errorf("Error creating user %v", err)
	}

	if u.Email != email {
		t.Errorf("Unxpected email %s", u.Email)
	}

	if u.Name != name {
		t.Errorf("Unxpected name %s", u.Name)
	}

	if u.AvatarURL != avatarURL {
		t.Errorf("Unxpected avatar URL %s", u.AvatarURL)
	}

	us, _ := db.GetUserSetting(u)
	if us.NotificationLimit != 10 {
		t.Errorf("Expected notification limit to be 10 but was %d", us.NotificationLimit)
	}

	// Check that if the auth name is different this doesn't change the user ID
	oldID := u.ID
	u, err = db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "12345", Name: "I changed my name", Email: email, AvatarURL: avatarURL})
	if err != nil {
		t.Errorf("Error updating user %v", err)
	}

	if u.ID != oldID {
		t.Errorf("Unexpected user ID %d (was %d)", u.ID, oldID)
	}

	// Check that if the auth nickname is different this doesn't change the user ID
	u, err = db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "12345", Name: "I changed my name", NickName: "boo", Email: email, AvatarURL: avatarURL})
	if err != nil {
		t.Errorf("Error updating user nickname %v", err)
	}

	if u.ID != oldID {
		t.Errorf("Unexpected user ID changing nickname %d (was %d)", u.ID, oldID)
	}
}

func TestUserRegistryCredential(t *testing.T) {
	var err error
	var db PgDB

	registry := "docker"
	user := "microbadgertest"
	encryptedPassword := "encrypted password"
	encryptedKey := "encrypted data key"
	imageName := "myuser/private"

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	u, err := db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "12345", Name: "myname", Email: "me@myaddress.com"})
	if err != nil {
		t.Errorf("Error creating user %v", err)
	}

	// Create a second user
	u2, err := db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "10000", Name: "othername", Email: "other@theiraddress.com"})
	if err != nil {
		t.Errorf("Error creating user %v", err)
	}

	_, err = db.GetUserRegistryCredential(registry, u.ID)
	if err == nil {
		t.Errorf("Expected error getting user registry cred not found %s %d", registry, u.ID)
	}

	urc, err := db.GetOrCreateUserRegistryCredential(registry, u.ID)
	if err != nil {
		t.Errorf("Error creating user registry cred %v", err)
	}

	urc.User = user
	urc.EncryptedPassword = encryptedPassword
	urc.EncryptedKey = encryptedKey

	err = db.PutUserRegistryCredential(urc)
	if err != nil {
		t.Errorf("Error saving user registry cred %v", err)
	}

	rc2, err := db.GetUserRegistryCredential(registry, u.ID)
	if err != nil {
		t.Errorf("Error getting user registry cred %v", err)
	}

	// Check second user still doesn't have credentials
	_, err = db.GetUserRegistryCredential(registry, u2.ID)
	if err == nil {
		t.Errorf("Expected error getting second user's registry cred: %s %d", registry, u2.ID)
	}

	// Check list of registries
	regs, err := db.GetUserRegistries(u.ID)
	if err != nil {
		t.Errorf("Error getting list of registries: %v", err)
	}

	if len(regs) != 1 {
		t.Errorf("First user has registry list length %d", len(regs))
	}

	if regs[0].CredentialsName != user {
		t.Errorf("Unexpected credentials name for first user: %s", regs[0].CredentialsName)
	}

	// List of registries should exist but credentials name should be empty
	regs, err = db.GetUserRegistries(u2.ID)
	if err != nil {
		t.Errorf("Error getting list of registries: %v", err)
	}

	if len(regs) != 1 {
		t.Errorf("Second user has registry list length %d: %v", len(regs), regs)
	}

	if len(regs[0].CredentialsName) != 0 {
		t.Errorf("Unexpected credentials name for second user: %s", regs[0].CredentialsName)
	}

	if rc2.User != user || rc2.EncryptedPassword != encryptedPassword ||
		rc2.EncryptedKey != encryptedKey {
		t.Errorf("Unexpected data found for user registry cred: %v", err)
	}

	_, err = db.GetOrCreateUserImagePermission(u.ID, imageName)
	if err != nil {
		t.Errorf("Error creating user image perm %v", err)
	}

	err = db.DeleteUserRegistryCredential(registry, u.ID)
	if err != nil {
		t.Errorf("Error deleting user registry cred %v", err)
	}

	_, err = db.GetUserImagePermission(u.ID, imageName)
	if err == nil {
		t.Errorf("Expected an error getting deleted user image permission %v", err)
	}
}

func TestUserImagePermission(t *testing.T) {
	var db PgDB
	var imageName = "myuser/private"
	var publicImage = "lizrice/childimage"
	var missingImage = "notfound/image"

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	u, err := db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "12345", Name: "myname", Email: "me@myaddress.com"})
	if err != nil {
		t.Errorf("Error creating user %v", err)
	}

	// Create a second user
	u2, err := db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "10000", Name: "othername", Email: "other@theiraddress.com"})
	if err != nil {
		t.Errorf("Error creating second user %v", err)
	}

	// Users can't access private or missing images but can get to public ones
	result, _ := db.CheckUserImagePermission(&u, imageName)
	if result == true {
		t.Errorf("Expected user %d to not have access to image %s", u.ID, imageName)
	}

	result, _ = db.CheckUserImagePermission(&u, publicImage)
	if result == false {
		t.Errorf("Expected user %d to have access to public image %s", u.ID, publicImage)
	}

	result, _ = db.CheckUserImagePermission(&u, missingImage)
	if result == true {
		t.Errorf("Expected user %d to not have access to missing image %s", u.ID, missingImage)
	}

	i, _ := db.GetImage(imageName)
	result, _ = db.CheckUserHasImagePermission(&u, &i)
	if result == true {
		t.Errorf("Expected user %d to not have access to image %s", u.ID, imageName)
	}

	i, err = db.GetImage(publicImage)
	result, err = db.CheckUserHasImagePermission(&u, &i)
	if !result {
		t.Errorf("Expected user %d to have access to public image %s: %v", u.ID, publicImage, err)
	}

	i, _ = db.GetImage(missingImage)
	result, _ = db.CheckUserHasImagePermission(&u, &i)
	if result {
		t.Errorf("Expected user %d to not have access to missing image %s", u.ID, missingImage)
	}

	count, _ := db.GetUserEnabledImageCount(u.ID)
	if count != 0 {
		t.Errorf("Expected user image count to be 0 but was %d", count)
	}

	// Now add permission
	_, err = db.GetOrCreateUserImagePermission(u.ID, imageName)
	if err != nil {
		t.Errorf("Error creating user image perm %v", err)
	}

	result, err = db.CheckUserImagePermission(&u, imageName)
	if !result {
		t.Errorf("Expected user %d to have access to image %s", u.ID, imageName)
	}

	i, _ = db.GetImage(imageName)
	result, _ = db.CheckUserHasImagePermission(&u, &i)
	if !result {
		t.Errorf("Expected user %d to have access to image %s", u.ID, imageName)
	}

	count, _ = db.GetUserEnabledImageCount(u.ID)
	if count != 1 {
		t.Errorf("Expected user image count to be 1 but was %d", count)
	}

	i, permission, err := db.GetImageForUser(imageName, &u)
	if !permission {
		t.Errorf("Expected user to be able to access private image")
	}
	if i.Name != imageName {
		t.Errorf("Unexpected image name")
	}

	// Second user shouldn't be able to access it
	result, err = db.CheckUserImagePermission(&u2, imageName)
	if result {
		t.Errorf("Expected user %d to not have access to image %s", u.ID, imageName)
	}

	i, permission, err = db.GetImageForUser(imageName, &u2)
	if permission || i.ImageName == imageName {
		t.Errorf("didn't expect user %d to be able to get image %s", u2.ID, imageName)
	}

	result, _ = db.CheckUserHasImagePermission(&u2, &i)
	if result {
		t.Errorf("Expected user %d not to have access to image %s", u.ID, imageName)
	}

	uip2, err := db.GetUserImagePermission(u.ID, imageName)
	if err != nil {
		t.Errorf("Error getting user image perm %v", err)
	}

	if u.ID != uip2.UserID || imageName != uip2.ImageName {
		t.Errorf("Unexpected data for user image permission. Expected user %d image %s", u.ID, imageName)
	}

	// Now add permission for the second user
	_, err = db.GetOrCreateUserImagePermission(u2.ID, imageName)
	if err != nil {
		t.Errorf("Error creating user image perm %v", err)
	}

	// Delete the first user's permission
	err = db.DeleteUserImagePermission(u.ID, imageName)
	if err != nil {
		t.Errorf("Error deleting user image permission %v", err)
	}

	// First user can't access it but second should still be able to
	_, err = db.GetUserImagePermission(u.ID, imageName)
	if err == nil {
		t.Errorf("Expected an error getting deleted user image permission %v", err)
	}

	result, err = db.CheckUserImagePermission(&u2, imageName)
	if !result {
		t.Errorf("Expected user %d to have access to image %s", u.ID, imageName)
	}

	i, _ = db.GetImage(imageName)
	result, _ = db.CheckUserHasImagePermission(&u2, &i)
	if !result {
		t.Errorf("Expected user %d to have access to image %s", u.ID, imageName)
	}

	// Delete second user's permission
	err = db.DeleteUserImagePermission(u2.ID, imageName)
	if err != nil {
		t.Errorf("Error deleting user image permission %v", err)
	}

	// Image should be deleted now
	i, err = db.GetImage(imageName)
	if err == nil {
		t.Errorf("Expected private image to have been deleted after last UIP removed")
	}
}

func TestRegistryCredentialsForImage(t *testing.T) {
	var db PgDB
	var registry = "docker"
	var imageName = "microbadgertest/alpine"

	user := "microbadgertest"
	encryptedPassword := "encrypted docker password"
	encryptedKey := "encrypted data key"

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	u, err := db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "12345", Name: "myname", Email: "me@myaddress.com"})
	if err != nil {
		t.Errorf("Error creating user %v", err)
	}

	_, err = db.GetOrCreateUserImagePermission(u.ID, imageName)
	if err != nil {
		t.Errorf("Error creating user image perm %v", err)
	}

	urc, err := db.GetOrCreateUserRegistryCredential(registry, u.ID)
	if err != nil {
		t.Errorf("Error creating user registry auth %v", err)
	}

	urc.User = user
	urc.EncryptedPassword = encryptedPassword
	urc.EncryptedKey = encryptedKey

	err = db.PutUserRegistryCredential(urc)
	if err != nil {
		t.Errorf("Error saving user registry creds %v", err)
	}

	u2, err := db.GetOrCreateUser(User{}, goth.User{Provider: "yourprov", UserID: "23456", Name: "yourname", Email: "you@youraddress.com"})
	if err != nil {
		t.Errorf("Error creating user %v", err)
	}

	_, err = db.GetOrCreateUserImagePermission(u2.ID, imageName)
	if err != nil {
		t.Errorf("Error creating user image perm %v", err)
	}

	urc2, err := db.GetOrCreateUserRegistryCredential(registry, u2.ID)
	if err != nil {
		t.Errorf("Error creating user registry auth %v", err)
	}

	urc2.User = user
	urc2.EncryptedPassword = encryptedPassword
	urc2.EncryptedKey = encryptedKey

	err = db.PutUserRegistryCredential(urc2)
	if err != nil {
		t.Errorf("Error saving user registry auth %v", err)
	}

	rc, err := db.GetRegistryCredentialsForImage(imageName)
	if err != nil {
		t.Errorf("Error getting registry auths for image %v", err)
	}

	if len(rc) != 2 {
		t.Errorf("Expected 2 registry credentials for image but found %v", len(rc))
	}

	if rc[0].User != user || rc[0].EncryptedPassword != encryptedPassword || rc[0].EncryptedKey != encryptedKey {
		t.Errorf("Unexpected data found getting registry credentials for image")
	}
}

func checkResult(t *testing.T, testcase string, img hub.ImageInfo, expPrivate bool, expInsp bool) {
	if img.IsPrivate != expPrivate {
		t.Errorf("%s: Expected image %s to have IsPrivate set %t", testcase, img.ImageName, expPrivate)
	}

	if img.IsInspected != expInsp {
		t.Errorf("%s: Expected image %s to have IsInspected set %t", testcase, img.ImageName, expInsp)
	}
}

func TestCheckUserInspectionStatus(t *testing.T) {

	var db PgDB
	var privateImage = "lizrice/private"
	var publicImage = "lizrice/featured"

	db = getDatabase(t)
	emptyDatabase(db)
	addThings(db)

	// Need additional images in the same namespace
	db.Exec("INSERT INTO images (name, status, is_private) VALUES('lizrice/private', 'INSPECTED', True)")

	u, err := db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "12345", Name: "myname", Email: "me@myaddress.com"})
	if err != nil {
		t.Errorf("Error creating user %v", err)
	}

	u2, err := db.GetOrCreateUser(User{}, goth.User{Provider: "myprov", UserID: "10000", Name: "othername", Email: "other@theiraddress.com"})
	if err != nil {
		t.Errorf("Error creating second user %v", err)
	}

	images := make([]hub.ImageInfo, 2)
	images[0] = hub.ImageInfo{ImageName: privateImage, IsPrivate: true}
	images[1] = hub.ImageInfo{ImageName: publicImage}

	// Initially we expect private images to be uninspected
	checkedimgs, err := db.CheckUserInspectionStatus(u.ID, "lizrice", images)
	t.Logf("checked images gives %#v", checkedimgs)
	if len(checkedimgs) != 2 {
		t.Errorf("Unexpected mismatch in image length %d", len(checkedimgs))
	}

	for _, v := range checkedimgs {
		priv := strings.Contains(v.ImageName, "private")
		checkResult(t, "u", v, priv, !priv)
	}

	// Add user permission for the private image
	_, err = db.GetOrCreateUserImagePermission(u.ID, privateImage)
	if err != nil {
		t.Errorf("Error creating user image perm %v", err)
	}

	// Now this user should see both images as inspected
	checkedimgs, err = db.CheckUserInspectionStatus(u.ID, "lizrice", images)
	if len(checkedimgs) != 2 {
		t.Errorf("Unexpected mismatch in image length %d", len(checkedimgs))
	}

	for _, v := range checkedimgs {
		priv := strings.Contains(v.ImageName, "private")
		checkResult(t, "u with perm", v, priv, true)
	}

	// But the second user should only see the public one as inspected
	checkedimgs, err = db.CheckUserInspectionStatus(u2.ID, "lizrice", images)
	if len(checkedimgs) != 2 {
		t.Errorf("Unexpected mismatch in image length %d", len(checkedimgs))
	}

	for _, v := range checkedimgs {
		priv := strings.Contains(v.ImageName, "private")
		checkResult(t, "u2", v, priv, !priv)
	}
}
