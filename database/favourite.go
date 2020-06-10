package database

import ()

// GetFavourite returns true if this user favourited this image
func (d *PgDB) GetFavourite(user User, image string) (bool, error) {
	var fav Favourite

	_, err := d.GetImage(image)
	if err != nil {
		return false, err
	}

	err = d.db.Where(`"user_id" = ? AND image_name = ?`, user.ID, image).First(&fav).Error
	if err != nil {
		// TODO!! We're kind of rashly assuming that an error here simply means the row doesn't exist
		// Return no error, but this just isn't a favourite for this user
		return false, nil
	}
	return true, nil
}

// PutFavourite saves it
func (d *PgDB) PutFavourite(user User, image string) (fav Favourite, err error) {

	_, err = d.GetImage(image)
	if err != nil {
		return fav, err
	}

	err = d.db.Where("id = ?", user.ID).Find(&user).Error
	if err != nil {
		return fav, err
	}

	fav = Favourite{UserID: user.ID, ImageName: image}
	err = d.db.Where(fav).FirstOrCreate(&fav).Error
	if err != nil {
		log.Debugf("Put Favourite error %v", err)
		return fav, err
	}

	err = d.db.Save(&fav).Error
	if err != nil {
		log.Debugf("Put Favourite 2 error %v", err)
	}

	return fav, err
}

// GetFavourites returns a slice of image names that are favourites for this user
func (d *PgDB) GetFavourites(user User) ImageList {
	var images []string

	err := d.db.Table("favourites").
		Where(`"user_id" = ?`, user.ID).
		Pluck("image_name", &images).Error
	if err != nil {
		log.Errorf("Error getting favourites images: %v", err)
	}

	return ImageList{Images: images}
}

// DeleteFavourite deletes a favourite, returning an error if it doesn't exist
func (d *PgDB) DeleteFavourite(user User, image string) error {

	err := d.db.Where(`"user_id" = ? and "image_name" = ?`, user.ID, image).Delete(Favourite{}).Error
	if err != nil {
		log.Debugf("Error Deleting favourites images: %v", err)
	}

	return err
}
