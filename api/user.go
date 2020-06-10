package api

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/asaskevich/govalidator"
	"github.com/gorilla/mux"

	"github.com/microscaling/microbadger/database"
	"github.com/microscaling/microbadger/inspector"
	"github.com/microscaling/microbadger/registry"
)

func handleGetRegistries(w http.ResponseWriter, r *http.Request) {
	log.Debugf("handleGetRegistries")

	var list database.RegistryList

	u := userFromContext(r.Context())

	registries, err := db.GetUserRegistries(u.ID)
	if err != nil {
		log.Errorf("Failed to get registries for user %d - %v", u.ID, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	count, err := db.GetUserEnabledImageCount(u.ID)
	if err != nil {
		log.Errorf("Failed to get images enabled count for user %d - %v", u.ID, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	list = database.RegistryList{
		UserID:            u.ID,
		EnabledImageCount: count,
		Registries:        registries,
	}

	bytes, err := json.Marshal(list)
	if err != nil {
		log.Errorf("Error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(bytes))
	log.Debugf("Returning %s from handleGetRegistries", bytes)
}

func handleUserRegistryCredential(w http.ResponseWriter, r *http.Request) {
	log.Debugf("handleUserRegistryCredential")

	var i database.UserRegistryCredential

	u := userFromContext(r.Context())
	vars := mux.Vars(r)

	registryID, ok := vars["registry"]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	_, registryMissing := db.GetRegistry(registryID)
	if registryMissing != nil {
		log.Debugf("Registry %s does not exist", registryID)

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	switch r.Method {
	case "PUT":
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Errorf("Failed to get request body %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		err = json.Unmarshal(body, &i)
		if err != nil {
			log.Errorf("Error unmarshalling user registry cred %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Check we can log in with these credentials
		// TODO Will need extending to support non Docker Hub registries
		token, err := hs.Login(i.User, i.Password)
		if token == "" || err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}

		// Generate a data key and encrypt the password
		encKey, encPass, err := es.Encrypt(i.Password)
		if err != nil {
			log.Errorf("Error encrypting password - %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Clear password as soon as its no longer needed
		i.Password = ""

		urc, err := db.GetOrCreateUserRegistryCredential(registryID, u.ID)
		if err != nil {
			log.Errorf("Error saving user registry creds 1 -  %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Overwrite and save any existing credentials
		urc.User = i.User
		urc.EncryptedPassword = encPass
		urc.EncryptedKey = encKey

		err = db.PutUserRegistryCredential(urc)
		if err != nil {
			log.Errorf("Error saving user registry creds 2 - %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		log.Debugf("Saved credentials for registry %s user %d", registryID, u.ID)
		w.WriteHeader(http.StatusNoContent)
		return

	case "DELETE":
		err := db.DeleteUserRegistryCredential(registryID, u.ID)
		if err != nil {
			log.Errorf("Error deleting user registry creds - %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		log.Debugf("Deleted credentials for registry %s user %d", registryID, u.ID)
		w.WriteHeader(http.StatusNoContent)
		return

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(constStatusMethodNotAllowed))
	}
}

func handleGetUserNamespaces(w http.ResponseWriter, r *http.Request) {
	log.Debugf("handleGetUserNamespaces")

	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(constStatusMethodNotAllowed))
		return
	}

	u := userFromContext(r.Context())
	vars := mux.Vars(r)

	registryID, ok := vars["registry"]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	_, registryMissing := db.GetRegistry(registryID)
	if registryMissing != nil {
		log.Debugf("Registry %s does not exist", registryID)

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	user, password, err := getUserRegistryCreds(registryID, u.ID)
	if err != nil {
		log.Debugf("Cannot get credentials for registry %s user %d", registryID, u.ID)
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	// TODO Will need extending for non Docker Hub registries
	un, err := hs.UserNamespaces(user, password)
	if err != nil {
		log.Errorf("Error getting user namespaces - %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Clear password as soon as its no longer needed
	password = ""

	bytes, err := json.Marshal(un)
	if err != nil {
		log.Errorf("Error: %v", err)
	}

	w.Write([]byte(bytes))
	log.Debugf("Returning %s from handleGetUserNamespaces", bytes)
}

func handleGetUserNamespaceImages(w http.ResponseWriter, r *http.Request) {
	log.Debugf("handleGetUserNamespaceImages")

	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(constStatusMethodNotAllowed))
		return
	}

	u := userFromContext(r.Context())
	vars := mux.Vars(r)

	registryID, ok := vars["registry"]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	namespace, ok := vars["namespace"]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	page, err := strconv.Atoi(vars["page"])
	if err != nil {
		page = 1
	}

	_, registryMissing := db.GetRegistry(registryID)
	if registryMissing != nil {
		log.Debugf("Registry %s does not exist", registryID)

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	user, password, err := getUserRegistryCreds(registryID, u.ID)
	if err != nil {
		log.Debugf("Cannot get credentials for registry %s user %d", registryID, u.ID)
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	// TODO Will need extending for non Docker Hub registries
	ni, notfound, err := hs.UserNamespaceImages(user, password, namespace, page)
	if notfound {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}
	if err != nil {
		log.Errorf("Error getting user namespace images - %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Clear password as soon as its no longer needed
	password = ""

	images, err := db.CheckUserInspectionStatus(u.ID, namespace, ni.Images)
	if err != nil {
		log.Errorf("Error checking inspection status - %v", err)
	}

	for i, img := range images {
		if !img.IsPrivate && !img.IsInspected {
			err = inspectNewImage(img.ImageName)

			if err == nil {
				img.IsInspected = true
				images[i] = img
			}
		}
	}

	ni.Images = images

	bytes, err := json.Marshal(ni)
	if err != nil {
		log.Errorf("Error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	log.Debugf("Returning %s from handleGetUserNamespaceImages", bytes)
	w.Write([]byte(bytes))

}

func handleUserImagePermissions(w http.ResponseWriter, r *http.Request) {
	log.Debugf("handleUserImagePermissions")

	vars := mux.Vars(r)

	regID, ok := vars["registry"]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	namespace, ok := vars["namespace"]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	image, ok := vars["image"]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	u := userFromContext(r.Context())
	image = namespace + "/" + image

	switch r.Method {
	case "PUT":
		user, password, err := getUserRegistryCreds(regID, u.ID)
		if err != nil {
			log.Debugf("Cannot get credentials for registry %s user %d", regID, u.ID)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}

		i := registry.Image{
			Name:     image,
			User:     user,
			Password: password,
		}

		if inspector.CheckImageExists(i, &rs) == false {
			log.Debugf("Image %s not found for user %d in registry %s", i.Name, u.ID, regID)

			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(constStatusNotFound))
			return
		}

		// Clear password as soon as its no longer needed
		password = ""

		// Save the permission
		_, err = db.GetOrCreateUserImagePermission(u.ID, i.Name)
		if err == nil {
			log.Debugf("Saved permission for user %d image %s in registry %s", u.ID, i.Name, regID)
		} else {
			log.Errorf("Failed to save perm for user %d image %s in registry %s - %v", u.ID, i.Name, regID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Check if the image is in the database
		_, err = db.GetImage(i.Name)
		if err != nil {
			// Inspect the image if it doesn't exist
			err = inspectNewImage(i.Name)
			if err != nil {
				log.Errorf("Failed to inspect image %s in registry %s - %v", i.Name, regID, err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}

		w.WriteHeader(http.StatusNoContent)
		return

	case "DELETE":
		// Check if the permission exists
		// TODO!! Shouldn't need this extra query - this could all be dealt with inside db.DeleteUserImagePermission
		_, err := db.GetUserImagePermission(u.ID, image)
		if err != nil {
			log.Debugf("Failed to delete missing perm for user %d image %s in registry %s", u.ID, image, regID)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// TODO!! Do this inside DeleteUserImagePermission to reduce the number of SQL queries and make it all much safer
		// Check if there is a notification for this image
		exists, _ := db.GetNotificationForUser(*u, image)
		if exists {
			log.Debugf("Can't delete perm as a notfication exists for user %d image %s in registry - %v", u.ID, image, regID, err)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}

		// Delete it
		err = db.DeleteUserImagePermission(u.ID, image)
		if err != nil {
			log.Errorf("Failed to delete perm for user %d image %s in registry - %v", u.ID, image, regID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		log.Debugf("Removed access for user %d to image %s in registry", u.ID, image, regID)
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(constStatusMethodNotAllowed))
		return
	}
}

func handleGetAllFavourites(w http.ResponseWriter, r *http.Request) {
	log.Debugf("handleGetAllFavourites")

	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(constStatusMethodNotAllowed))
		return
	}

	u := userFromContext(r.Context())

	favs := db.GetFavourites(*u)
	bytes, err := json.Marshal(favs)
	if err != nil {
		log.Errorf("Error: %v", err)
	}

	w.Write([]byte(bytes))
	log.Debugf("Returning %s from handleGetAllFavourites", bytes)
}

func handleFavourite(w http.ResponseWriter, r *http.Request) {
	log.Debugf("handleFavourite")
	var isFav bool
	var err error

	vars := mux.Vars(r)
	u := userFromContext(r.Context())

	org, ok := vars["org"]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	image, ok := vars["image"]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	switch r.Method {
	case "GET":
		isFav, err = db.GetFavourite(*u, org+"/"+image)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(constStatusNotFound))
			return
		}
	case "POST":
		_, err = db.PutFavourite(*u, org+"/"+image)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(constStatusNotFound))
			return
		}
		isFav = true
		log.Debugf("%s is new favourite for %s", org+"/"+image, u.Name)
	case "DELETE":
		isFav, err = db.GetFavourite(*u, org+"/"+image)
		if !isFav || err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(constStatusNotFound))
			return
		}
		err = db.DeleteFavourite(*u, org+"/"+image)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		isFav = false
		log.Debugf("%s is no longer favourite for %s", org+"/"+image, u.Name)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(constStatusMethodNotAllowed))
		return
	}

	bytes, err := json.Marshal(database.IsFavourite{IsFavourite: isFav})
	if err != nil {
		log.Errorf("Error: %v", err)
	}

	log.Debugf("Completed OK, returning %s", bytes)
	w.Write([]byte(bytes))
}

func handleGetAllNotifications(w http.ResponseWriter, r *http.Request) {
	log.Debugf("handleGetAllNotifications")

	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(constStatusMethodNotAllowed))
		return
	}

	u := userFromContext(r.Context())

	notifications, err := db.GetNotifications(*u)
	if err != nil {
		log.Errorf("Error getting notifications - %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	bytes, err := json.Marshal(notifications)
	if err != nil {
		log.Errorf("Error marshalling notifications - %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(bytes))
	log.Debugf("Returning %s from handleGetAllNotifications", bytes)
}

func handleCreateNotification(w http.ResponseWriter, r *http.Request) {
	var notify database.Notification

	log.Debugf("handleCreateNotifications")

	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(constStatusMethodNotAllowed))
		return
	}

	u := userFromContext(r.Context())

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Errorf("Failed to get request body %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	err = json.Unmarshal(body, &notify)
	if err != nil {
		log.Errorf("Error unmarshalling notification %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	notify.UserID = u.ID

	if !govalidator.IsURL(notify.WebhookURL) {
		log.Infof("Invalid webhook URL %v", notify.WebhookURL)

		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte("Invalid webhook URL"))
		return
	}

	hasAccess, _ := db.CheckUserImagePermission(u, notify.ImageName)
	if !hasAccess {
		log.Debugf("User %d does not have permission to create notification for %s", u.ID, notify.ImageName)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	notify, err = db.CreateNotification(*u, notify)
	if err != nil {
		log.Errorf("Error creating notification %v", err)

		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to create notification"))
		return
	}

	bytes, err := json.Marshal(notify)
	if err != nil {
		log.Errorf("Error marshalling notification: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(bytes))
	log.Debugf("Returning %s from handleCreateNotification", bytes)
}

func handleNotification(w http.ResponseWriter, r *http.Request) {
	var notify database.Notification
	var err error

	log.Debugf("handleNotification")
	u := userFromContext(r.Context())

	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		log.Errorf("Failed to convert id to an int - %v", err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	switch r.Method {
	case "GET":
		notify, err = db.GetNotification(*u, id)
		if err != nil {
			log.Errorf("Failed to get notification %d - %v", id, err)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		history, err := db.GetNotificationHistory(id, notify.ImageName, 10)
		if err != nil {
			log.Errorf("Failed to get notification history %d - %v", id, err)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		notify.HistoryArray = history

	case "PUT":
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Errorf("Failed to get request body %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		err = json.Unmarshal(body, &notify)
		if err != nil {
			log.Errorf("Error unmarshalling notification %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		notify.UserID = u.ID

		if !govalidator.IsURL(notify.WebhookURL) {
			log.Infof("Invalid webhook URL %v", notify.WebhookURL)
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte("Invalid webhook URL"))
			return
		}

		hasAccess, _ := db.CheckUserImagePermission(u, notify.ImageName)
		if !hasAccess {
			log.Debugf("User %d does not have permission to update notification for %s", u.ID, notify.ImageName)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		notify, err = db.UpdateNotification(*u, id, notify)
		if err != nil {
			log.Errorf("Error updating notification %d - %v", id, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

	case "DELETE":
		err = db.DeleteNotification(*u, id)
		if err != nil {
			log.Errorf("Error deleting notification %d - %v", id, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
		return

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(constStatusMethodNotAllowed))
		return
	}

	bytes, err := json.Marshal(notify)
	if err != nil {
		log.Errorf("Error marshalling notification %d - %v", id, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(bytes))
	log.Debugf("Completed OK, returning %s", bytes)
}

func handleNotificationTrigger(w http.ResponseWriter, r *http.Request) {
	var notify database.Notification
	var err error

	log.Debugf("handleNotificationTrigger")
	u := userFromContext(r.Context())

	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		log.Errorf("Failed to convert id to an int - %v", err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	notify, err = db.GetNotification(*u, id)
	if err != nil {
		log.Infof("Failed to get notification %d - %v", id, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	imageName := strings.TrimPrefix(notify.ImageName, "library/")
	nmc := database.NotificationMessageChanges{
		Text:        fmt.Sprintf("Hi, it's MicroBadger, sending you a test notification for %s %s", imageName, notify.PageURL),
		ImageName:   imageName,
		NewTags:     []database.Tag{},
		ChangedTags: []database.Tag{},
		DeletedTags: []database.Tag{},
	}

	nmcAsJson, err := json.Marshal(nmc)
	if err != nil {
		log.Errorf("Failed to generate NMC message: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	nm := database.NotificationMessage{
		NotificationID: uint(id),
		ImageName:      notify.ImageName,
		WebhookURL:     notify.WebhookURL,
		Message:        database.PostgresJSON{nmcAsJson},
	}

	err = db.SaveNotificationMessage(&nm)
	if err != nil {
		log.Errorf("Failed to create triggered notification message for %s, id %d: %v", notify.ImageName, id, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	err = qs.SendNotification(nm.ID)
	if err != nil {
		log.Errorf("Failed to send triggered notification message for %s, id %d: %v", notify.ImageName, id, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func handleGetNotificationForUser(w http.ResponseWriter, r *http.Request) {
	log.Debugf("handleGetNotificationForUser")

	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(constStatusMethodNotAllowed))
		return
	}

	u := userFromContext(r.Context())
	vars := mux.Vars(r)

	org, ok := vars["org"]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	image, ok := vars["image"]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(constStatusNotFound))
		return
	}

	nc, err := db.GetNotificationCount(*u)
	if err != nil {
		log.Errorf("Error getting notification count: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	us, err := db.GetUserSetting(*u)
	if err != nil {
		log.Errorf("Error getting user settings: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	isn := database.IsNotification{
		NotificationCount: nc,
		NotificationLimit: us.NotificationLimit,
	}

	isPresent, n := db.GetNotificationForUser(*u, org+"/"+image)
	if isPresent {
		isn.Notification = n
	}

	bytes, err := json.Marshal(isn)
	if err != nil {
		log.Errorf("Error marshalling notification list - %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(bytes))
	log.Debugf("Returning %s from handleGetNotificationForUser", bytes)
}

// Gets the credentials and decrypts the password
func getUserRegistryCreds(registryID string, userID uint) (string, string, error) {
	urc, err := db.GetUserRegistryCredential(registryID, userID)
	if err != nil {
		return "", "", err
	}

	password, err := es.Decrypt(urc.EncryptedKey, urc.EncryptedPassword)
	if err != nil {
		log.Errorf("Error decrypting password - %v", err)
		return "", "", err
	}

	return urc.User, password, nil
}

// Send to queue and save to database if image was accepted
func inspectNewImage(image string) (err error) {
	err = qs.SendImage(image, "Sent")
	if err == nil {
		img, err := db.GetOrCreateImage(image)
		if err == nil {
			img.Status = "SUBMITTED"
			err = db.PutImageOnly(img)
		}
	}

	return err
}
