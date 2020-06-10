package database

import (
	"fmt"
	"reflect"

	"github.com/jinzhu/gorm"
	"github.com/markbates/goth"

	"github.com/microscaling/microbadger/hub"
)

type userImageAccess struct {
	Name          string
	Status        string
	IsPrivate     bool
	HasPermission bool
}

// GetOrCreateUser adds a user if they don't already exist
// TODO!! Finish and test adding multiple providers to the same user ID
func (d *PgDB) GetOrCreateUser(existingUser User, gothUser goth.User) (u User, err error) {
	var ua UserAuth
	var us UserSetting

	err = d.db.First(&ua, UserAuth{
		Provider:   gothUser.Provider,
		IDFromAuth: gothUser.UserID}).Error

	if err == nil {
		// It is possible that the user auth contains some new info that we need to update
		ua.NameFromAuth = gothUser.Name
		ua.NicknameFromAuth = gothUser.NickName
		err = d.db.Save(&ua).Error
		if err != nil {
			log.Errorf("Failed to update pre-existing user_auth (%v): %v", ua, err)
			return
		}

		// This auth exists already, so it should have a related user
		err = d.db.Model(ua).Related(&u).Error

		if err == nil {
			us, err = d.GetUserSetting(u)
			u.UserSetting = &us
		}

		// And that related user should be the same as the one passed in, if there is one
		if !reflect.DeepEqual(existingUser, User{}) && (existingUser.ID != u.ID) {
			log.Errorf("Found a duplicate user with the same auth: existing %v\n new auth %v", existingUser, u)
		}

		return u, err
	}

	// The userauth didn't exist already. If we've been passed a user, this is a new auth to attach to the same user account
	if !reflect.DeepEqual(existingUser, User{}) {
		log.Errorf("This isn't supported yet!!")
		return u, fmt.Errorf("Not yet implemented")

		// log.Infof("Adding a new auth from provider %s to user ID %d", gothUser.Provider, existingUser.ID)
		// err = d.db.FirstOrCreate(&ua, UserAuth{
		// 	UserID:       existingUser.ID,
		// 	Provider:     gothUser.Provider,
		// 	IDFromAuth:   gothUser.UserID,
		// 	NameFromAuth: gothUser.Name}).Error

		// if err != nil {
		// 	log.Errorf("Error creating user auth %v", err)
		// 	return u, err
		// }

		// err = d.db.Model(ua).Related(&u).Error
		// log.Debugf("User %#v", u)
		// if u.ID != existingUser.ID {
		// 	log.Errorf("Related user ID %v doesn't match existing user ID %v", u.ID, existingUser.ID)
		// 	err = fmt.Errorf("Related user ID %v doesn't match existing user ID %v", u.ID, existingUser.ID)
		// }

		// return u, err
	}

	u = User{
		Email:     gothUser.Email,
		Name:      gothUser.Name,
		AvatarURL: gothUser.AvatarURL,
	}

	tx := d.db.Begin()

	err = tx.Create(&u).Error
	if err != nil {
		log.Errorf("Failed to create user %#v", u)
		tx.Rollback()
		return u, err
	}

	ua = UserAuth{
		UserID:           u.ID,
		Provider:         gothUser.Provider,
		NameFromAuth:     gothUser.Name,
		IDFromAuth:       gothUser.UserID,
		NicknameFromAuth: gothUser.NickName,
	}

	err = tx.Create(&ua).Error
	if err != nil {
		log.Errorf("Failed to create user auth %#v", ua)
		tx.Rollback()
		return u, err
	}

	us = UserSetting{
		UserID:            u.ID,
		NotificationLimit: 10, // All users get ten free notifications when they register.
	}

	err = tx.Create(&us).Error
	if err != nil {
		log.Errorf("Failed to create user setting %#v", us)
		tx.Rollback()
		return u, err
	}

	log.Infof("Created new user %d with auth from %s", u.ID, gothUser.Provider)

	tx.Commit()

	return u, err
}

// GetUserSetting returns extra user data that is not stored on the session.
func (d *PgDB) GetUserSetting(user User) (us UserSetting, err error) {
	err = d.db.Table("user_settings").
		Where(`"user_id" = ?`, user.ID).
		First(&us).Error
	if err != nil {
		log.Errorf("Failed to get user settings %v", err)
	}

	return us, err
}

// PutUserSetting saves the extra user data
func (d *PgDB) PutUserSetting(us UserSetting) (err error) {
	return d.db.Save(&us).Error
}

// GetUserRegistries returns all registries and whether the user has saved credentials
func (d *PgDB) GetUserRegistries(userID uint) (registries []Registry, err error) {
	regJoin := "LEFT OUTER JOIN user_registry_credentials urc ON r.id = urc.registry_id AND urc.user_id = ?"
	regSelect := "r.id, r.name, r.url, urc.user AS credentials_name"

	err = d.db.Table("registries r").Joins(regJoin, userID).Select(regSelect).
		Find(&registries).Error
	return
}

// GetUserRegistryCredential gets it
func (d *PgDB) GetUserRegistryCredential(registryID string, userID uint) (urc UserRegistryCredential, err error) {
	err = d.db.Where(UserRegistryCredential{RegistryID: registryID, UserID: userID}).First(&urc).Error
	return
}

// GetOrCreateUserRegistryCredential makes sure it's there
func (d *PgDB) GetOrCreateUserRegistryCredential(registryID string, userID uint) (urc UserRegistryCredential, err error) {
	err = d.db.Where(UserRegistryCredential{RegistryID: registryID, UserID: userID}).FirstOrCreate(&urc).Error
	return
}

// PutUserRegistryCredential saves it
func (d *PgDB) PutUserRegistryCredential(urc UserRegistryCredential) (err error) {
	return d.db.Save(&urc).Error
}

// DeleteUserRegistryCredential deletes it and removes permission to all images
// TODO Will need updating when registry is added to images
func (d *PgDB) DeleteUserRegistryCredential(registryID string, userID uint) (err error) {
	tx := d.db.Begin()
	err = deleteUserRegistryCredential(registryID, userID, tx)
	if err != nil {
		log.Errorf("Failed to delete user credentials: %v", err)
		tx.Rollback()
		return
	}

	tx.Commit()
	log.Debugf("Deleted creds for registry %s user %d", registryID, userID)
	return
}

// deleteUserRegistryCredential does the deletes inside a transaction
// TODO!! I think this is only called from one place, why is it in a separate function?
func deleteUserRegistryCredential(registryID string, userID uint, tx *gorm.DB) (err error) {
	var perms []UserImagePermission

	// Get all permissions for user
	err = tx.Where("user_id = ?", userID).Find(&perms).Error
	if err != nil {
		return fmt.Errorf("Error getting user image permissions for %s user %d: %v", registryID, userID, err)
	}

	// Delete permissions and any images that are no longer needed
	for _, p := range perms {
		err = deleteUserImagePermission(userID, p.ImageName, tx)
		if err != nil {
			return fmt.Errorf("Error deleting user image permission %v: %v", p, err)
		}
	}

	// Delete the registry creds
	err = tx.Delete(UserRegistryCredential{RegistryID: registryID, UserID: userID}).Error
	if err != nil {
		return fmt.Errorf("Error deleting registry creds for %s user %d: %v", registryID, userID, err)
	}

	return
}

// GetRegistryCredentialsForImage gets auth creds for this image so it can be inspected
func (d *PgDB) GetRegistryCredentialsForImage(imageName string) (registryCreds []UserRegistryCredential, err error) {
	permsJoin := "JOIN user_image_permissions uip ON uip.user_id = urc.user_id"

	err = d.db.Table("user_registry_credentials urc").
		Joins(permsJoin).Where("uip.image_name = ?", imageName).
		Select("urc.*").Find(&registryCreds).Error
	return
}

// GetOrCreateUserImagePermission makes sure it's there
func (d *PgDB) GetOrCreateUserImagePermission(userID uint, imageName string) (uip UserImagePermission, err error) {
	err = d.db.Where(UserImagePermission{UserID: userID, ImageName: imageName}).FirstOrCreate(&uip).Error
	return
}

// GetUserImagePermission gets it
func (d *PgDB) GetUserImagePermission(userID uint, imageName string) (uip UserImagePermission, err error) {
	err = d.db.Where(UserImagePermission{UserID: userID, ImageName: imageName}).First(&uip).Error
	return
}

// TODO!! Can we get to not needing this?
// CheckUserImagePermission checks whether the user has access
func (d *PgDB) CheckUserImagePermission(u *User, imageName string) (result bool, err error) {
	// Check if the image is public
	img, err := d.GetImage(imageName)
	if err == nil && !img.IsPrivate {
		return true, err
	}

	if img.IsPrivate && u == nil {
		return false, fmt.Errorf("User must be signed in to view private images")
	}

	_, err = d.GetUserImagePermission(u.ID, imageName)
	if err == nil {
		result = true
	}

	return
}

// CheckUserHasImagePermission checks whether the user has access
func (d *PgDB) CheckUserHasImagePermission(u *User, i *Image) (bool, error) {
	var uip UserImagePermission

	if i == nil {
		return false, fmt.Errorf("Can't have permission for an image that doesn't exist")
	}

	if i.Name == "" {
		return false, fmt.Errorf("Can't have permission for an image with no name")
	}

	if !i.IsPrivate {
		return true, nil
	}

	if u == nil {
		return false, fmt.Errorf("Forbidden: User must be signed in to view private images")
	}

	err := d.db.Where(UserImagePermission{UserID: u.ID, ImageName: i.Name}).First(&uip).Error
	if err != nil {
		return false, fmt.Errorf("Forbidden: User does not have permissions for this image")
	}

	return true, nil
}

// DeleteUserImagePermission deletes it and the image if its not enabled by any other users
// TODO Will need updating when registry is added to images
func (d *PgDB) DeleteUserImagePermission(userID uint, image string) (err error) {
	tx := d.db.Begin()
	err = deleteUserImagePermission(userID, image, tx)
	if err != nil {
		log.Errorf("Failed to delete UIP: %v", err)
		tx.Rollback()
		return
	}

	tx.Commit()
	log.Debugf("Deleted permission for user %d image %s", userID, image)
	return
}

// deleteUserImagePermission does the deletes inside a transaction
func deleteUserImagePermission(userID uint, image string, tx *gorm.DB) (err error) {
	var count int
	var img Image

	err = tx.Where("name = ?", image).First(&img).Error
	if err != nil {
		return fmt.Errorf("Error getting image name %s: %v", image, err)
	}

	// Remove the permission
	err = tx.Delete(UserImagePermission{UserID: userID, ImageName: image}).Error
	if err != nil {
		return fmt.Errorf("Failed to delete permission for user %d image %s - %v", userID, image, err)
	}

	// TODO!! I'm not sure if we should ever come through here for a public image anyway, what would we be doing
	// with UIP for a public image?
	if !img.IsPrivate {
		return
	}

	// Check how many users have access
	err = tx.Table("user_image_permissions").
		Where("image_name = ?", image).
		Count(&count).Error
	if err != nil {
		return fmt.Errorf("Failed to check for other users of image %s - %v", image, err)
	}

	// No other users so also delete the image if it's private
	if count == 0 {
		err = deleteImage(image, tx)
		if err != nil {
			return fmt.Errorf("Failed to delete private image %s - %v", image, err)
		}
		log.Debugf("Deleted private image %s", image)
	}

	return
}

// CheckUserInspectionStatus checks whether the images in this namespace have been inspected
// TODO!! Needs renaming as inspected is a bad name. Changed to enabled /disabled in the UI
func (d *PgDB) CheckUserInspectionStatus(userID uint, namespace string, images []hub.ImageInfo) ([]hub.ImageInfo, error) {
	var ua []userImageAccess

	inspectSelect := `
	i.name, i.status, i.is_private,
	CASE WHEN uip.user_id IS NOT NULL THEN true
		ELSE false END AS has_permission`

	inspectJoin := `
	LEFT OUTER JOIN user_image_permissions uip
		ON i.name = uip.image_name AND uip.user_id = ?`

	filter := namespace + "/%"
	err := d.db.Table("images i").
		Joins(inspectJoin, userID).
		Select(inspectSelect).
		Where("i.name LIKE ?", filter).
		Find(&ua).Error

	// Populate map for lookups by image name
	lookups := make(map[string]userImageAccess, len(ua))
	for _, uia := range ua {
		lookups[uia.Name] = uia
	}

	result := make([]hub.ImageInfo, len(images))

	for i, img := range images {
		if uia, ok := lookups[img.ImageName]; ok {
			// Private images only return "IsInspected" if this user has permission
			// TODO!! IsInspected is not a great name
			if img.IsPrivate {
				img.IsInspected = uia.HasPermission
			} else {
				img.IsInspected = ((uia.Status != "SITEMAP") && (uia.Status != "MISSING"))
			}
		}

		result[i] = img
	}

	return result, err
}

// GetUserEnabledImageCount returns how many enabled private images the user has
func (d *PgDB) GetUserEnabledImageCount(userID uint) (count int, err error) {
	err = d.db.Table("user_image_permissions").Where("user_id = ?", userID).Count(&count).Error
	return
}
