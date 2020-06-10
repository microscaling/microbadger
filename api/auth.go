package api

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/markbates/goth/gothic"
	"github.com/op/go-logging"

	"github.com/microscaling/microbadger/database"
)

var (
	// Special log setting so we can debug the auth details if we need them
	logauth = logging.MustGetLogger("mmauth")
)

func init() {
	gob.Register(database.User{})
}

func authHandler(w http.ResponseWriter, r *http.Request) {
	var next string
	vars := mux.Vars(r)
	next = vars["next"]

	if next != "" {
		session, err := sessionStore.Get(r, "session-name")
		if err != nil {
			log.Errorf("Failed to get session from session store %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		session.Values["next"] = next
		session.Save(r, w)
		if err != nil {
			log.Errorf("Failed to save session info in authCallback %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logauth.Debugf("Session: %#v", session)

	}

	gothic.BeginAuthHandler(w, r)
}

func authCallbackHandler(w http.ResponseWriter, r *http.Request) {
	var u database.User

	user, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		log.Errorf("Failed authorization %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	session, err := sessionStore.Get(r, "session-name")
	if err != nil {
		log.Errorf("Failed to get session from session store %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	val := session.Values["user"]
	logauth.Debugf("Pre-existing session user %#v ", val)

	// If you're already logged in, this is adding another auth to an existing user, which we need to retrieve
	if val != nil {
		var ok bool
		if u, ok = val.(database.User); !ok {
			log.Errorf("Unexpectedly got a user from the session that failed type assertion")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	// We can't store goth.User in our database so we make sure we have our own user with the salient information
	u, err = db.GetOrCreateUser(u, user)
	if err != nil {
		log.Errorf("Failed to get or create user %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logauth.Debugf("Now logged in user is %#v ", u)
	next := session.Values["next"].(string)

	session.Values["user"] = u
	session.Values["next"] = ""

	err = session.Save(r, w)
	if err != nil {
		log.Errorf("Failed to save session info in authCallback %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if next != "" {
		log.Debugf("redirecting to %s", next)
		http.Redirect(w, r, next, http.StatusFound)
	}
}

func isLoggedIn(r *http.Request) (isLoggedIn bool, user database.User, err error) {

	session, err := sessionStore.Get(r, "session-name")
	if err != nil {
		log.Errorf("Failed to get session from session store: %v", err)
		return false, user, err
	}

	val := session.Values["user"]
	logauth.Debugf("isLoggedIn? session user %#v ", val)

	if val != nil {
		if user, isLoggedIn = val.(database.User); !isLoggedIn {
			err = fmt.Errorf("Unexpectedly got a user from the session that failed type assertion")
		}
	}

	return isLoggedIn, user, err
}

func meHandler(w http.ResponseWriter, r *http.Request) {

	isLoggedIn, user, err := isLoggedIn(r)
	if err != nil {
		log.Errorf("Failed to get session info %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Debugf("Logged in %t", isLoggedIn)
	bytes, err := json.Marshal(user)
	if err != nil {
		log.Errorf("Error: %v", err)
	}

	w.Write([]byte(bytes))
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	session, err := sessionStore.Get(r, "session-name")
	if err != nil {
		log.Errorf("Failed to get session info in logout%v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	session.Values["user"] = nil

	err = session.Save(r, w)
	if err != nil {
		log.Errorf("Failed to save session info in logout %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(r)
	next := vars["next"]
	if next != "" {
		log.Debugf("redirecting to %s", next)
		http.Redirect(w, r, next, http.StatusFound)
	}
}

func loggedInTest(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	next := vars["next"]
	if next != "" {
		log.Debugf("Log in redirecting to %s", next)
	} else {
		next = "/__/anotherpage"
	}

	isLoggedIn, user, err := isLoggedIn(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	if isLoggedIn {
		t, _ := template.New("loggedIn").Parse(`<p>Hello {{.Name}} <a href="/v1/logout?next=/__/test">Logout</a></p>`)
		t.Execute(w, user)
	} else {
		t, _ := template.New("loggedOut").Parse(`<p>Come on in - <a href="/v1/auth/github?next=` + next + `">Log in with GitHub</a></p>`)
		t.Execute(w, "")
	}
}

func anotherPageTest(w http.ResponseWriter, r *http.Request) {
	isLoggedIn, user, err := isLoggedIn(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	if isLoggedIn {
		t, _ := template.New("another").Parse(`<p>Hello {{.Name}} This is another page.  <a href="/v1/logout?next=/__/anotherpage">Logout</a></p></a></p>`)
		t.Execute(w, user)
	} else {
		t, _ := template.New("anotherOut").Parse(`<p>You're not logged in, you should go <a href="/__/test">here</a></p>`)
		t.Execute(w, "")
	}
}
