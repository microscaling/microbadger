package api

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/github"
	logging "github.com/op/go-logging"
	"github.com/rs/cors"
	"github.com/urfave/negroni"

	"github.com/microscaling/microbadger/database"
	"github.com/microscaling/microbadger/encryption"
	"github.com/microscaling/microbadger/hub"
	"github.com/microscaling/microbadger/queue"
	"github.com/microscaling/microbadger/registry"
	"github.com/microscaling/microbadger/utils"
)

const (
	constHealthCheckMessage     = "HEALTH OK"
	constEllipsis               = "\u2026"
	constRefreshParam           = "refresh"
	constStatusNotFound         = "404 page not found"
	constStatusMethodNotAllowed = "405 method not allowed"

	gaProperty = "UA-62914780-6"
	gaHost     = "http://microbadger.com"

	imageURL = "hub.docker.com"
)

var (
	log              = logging.MustGetLogger("mmapi")
	db               database.PgDB
	ga               = utils.NewGoogleAnalytics(gaProperty, gaHost)
	qs               queue.Service
	rs               registry.Service
	hs               hub.InfoService
	es               encryption.Service
	refreshCodeValue string
	sessionStore     sessions.Store
	webhookURL       string
)

func init() {
	refreshCodeValue = os.Getenv("MB_REFRESH_CODE")
}

func muxRoutes() *mux.Router {
	r := mux.NewRouter()

	r.HandleFunc("/healthcheck.txt", handleHealthCheck).Methods("GET")
	r.HandleFunc("/images/{org}/{image}/{authToken}", handleImageWebhook).Methods("POST")
	r.HandleFunc("/images/{image}/{authToken}", handleImageWebhook).Methods("POST")

	r.HandleFunc("/v1/auth/github/callback", authCallbackHandler).Methods("GET")
	r.HandleFunc("/v1/auth/github", authHandler).Methods("GET").Queries("next", "{next}")

	// Badge image routes
	br := mux.NewRouter().PathPrefix("/badges").Subrouter().StrictSlash(true)
	br.HandleFunc("/{badgeType}/{org}/{image}:{tag}.svg", handleGetImageBadge).Methods("GET")
	br.HandleFunc("/{badgeType}/{image}:{tag}.svg", handleGetImageBadge).Methods("GET")
	br.HandleFunc("/{badgeType}/{org}/{image}.svg", handleGetImageBadge).Methods("GET")
	br.HandleFunc("/{badgeType}/{image}.svg", handleGetImageBadge).Methods("GET")

	r.PathPrefix("/badges").Handler(negroni.New(
		negroni.HandlerFunc(contentTypeSvgMw),
		negroni.Wrap(br)))

	// API routes
	ar := mux.NewRouter().PathPrefix("/v1").Subrouter().StrictSlash(true)
	ar.HandleFunc("/badges/counts", handleGetBadgeCounts).Methods("GET")
	ar.HandleFunc("/images/search/{term}/{term2}", handleImageSearch).Methods("GET")
	ar.HandleFunc("/images/search/{term}", handleImageSearch).Methods("GET")
	ar.HandleFunc("/images/{namespace}/{image}/version/{sha}", handleGetImageVersion).Methods("GET")
	ar.HandleFunc("/images/{image}/version/{sha}", handleGetImageVersion).Methods("GET")
	ar.HandleFunc("/images/{namespace}/{image}:{tag}", handleGetImage).Methods("GET")
	ar.HandleFunc("/images/{namespace}/{image}", handleGetImage).Methods("GET")
	ar.HandleFunc("/images/{image}:{tag}", handleGetImage).Methods("GET")
	ar.HandleFunc("/images/{image}", handleGetImage).Methods("GET")
	ar.HandleFunc("/images", handleGetImageList).Methods("GET").Queries("query", "{query}", "page", "{page}")
	ar.HandleFunc("/images", handleGetImageList).Methods("GET").Queries("query", "{query}")
	ar.HandleFunc("/logout", logoutHandler).Methods("GET").Queries("next", "{next}")
	ar.HandleFunc("/logout", logoutHandler).Methods("GET")
	ar.HandleFunc("/me", meHandler).Methods("GET")

	// Registries API requires OAuth
	rr := mux.NewRouter().PathPrefix("/v1/registries").Subrouter().StrictSlash(true)
	rr.HandleFunc("/", handleGetRegistries).Methods("GET")

	ar.PathPrefix("/registries").Handler(negroni.New(
		negroni.HandlerFunc(loginRequiredMw),
		negroni.Wrap(rr),
	))

	// Private Registries API requires OAuth
	pir := mux.NewRouter().PathPrefix("/v1/registry").Subrouter().StrictSlash(true)
	pir.HandleFunc("/{registry}", handleUserRegistryCredential).Methods("PUT")
	pir.HandleFunc("/{registry}", handleUserRegistryCredential).Methods("DELETE")
	pir.HandleFunc("/{registry}/namespaces/", handleGetUserNamespaces).Methods("GET")
	pir.HandleFunc("/{registry}/namespaces/{namespace}/images/", handleGetUserNamespaceImages).Methods("GET").Queries("page", "{page}")
	pir.HandleFunc("/{registry}/namespaces/{namespace}/images/", handleGetUserNamespaceImages).Methods("GET")
	pir.HandleFunc("/{registry}/images/{namespace}/{image}", handleUserImagePermissions).Methods("DELETE", "PUT")
	pir.HandleFunc("/{registry}/images/{namespace}/{image}:{tag}", handleGetImage).Methods("GET")
	pir.HandleFunc("/{registry}/images/{namespace}/{image}", handleGetImage).Methods("GET")
	pir.HandleFunc("/{registry}/images/{namespace}/{image}/version/{sha}", handleGetImageVersion).Methods("GET")

	ar.PathPrefix("/registry").Handler(negroni.New(
		negroni.HandlerFunc(loginRequiredMw),
		negroni.Wrap(pir),
	))

	// Favourites API requires OAuth
	fr := mux.NewRouter().PathPrefix("/v1/favourites").Subrouter().StrictSlash(true)
	fr.HandleFunc("/{org}/{image}", handleFavourite)
	fr.HandleFunc("/", handleGetAllFavourites)

	ar.PathPrefix("/favourites").Handler(negroni.New(
		negroni.HandlerFunc(loginRequiredMw),
		negroni.Wrap(fr),
	))

	// Notifications API requires OAuth
	nr := mux.NewRouter().PathPrefix("/v1/notifications").Subrouter().StrictSlash(true)
	nr.HandleFunc("/", handleGetAllNotifications).Methods("GET")
	nr.HandleFunc("/images/{org}/{image}", handleGetNotificationForUser).Methods("GET")
	nr.HandleFunc("/", handleCreateNotification).Methods("POST")
	nr.HandleFunc("/{id}/trigger", handleNotificationTrigger)
	nr.HandleFunc("/{id}", handleNotification)

	ar.PathPrefix("/notifications").Handler(negroni.New(
		negroni.HandlerFunc(loginRequiredMw),
		negroni.Wrap(nr),
	))

	debugCors := os.Getenv("MB_DEBUG_CORS")
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{os.Getenv("MB_CORS_ORIGIN")},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "Content-Length", "Accept-Encoding", "Accept-Language", "Cache-Control"},
		AllowCredentials: true,
		Debug:            (strings.ToLower(debugCors) == "true"),
	})

	r.PathPrefix("/v1").Handler(negroni.New(
		negroni.HandlerFunc(contentTypeJsMw),
		c,
		negroni.Wrap(ar)))

	r.HandleFunc("/__/test", loggedInTest).Methods("GET").Queries("next", "{next}")
	r.HandleFunc("/__/test", loggedInTest).Methods("GET")
	r.HandleFunc("/__/anotherpage", anotherPageTest).Methods("GET")

	return r
}

func contentTypeJsMw(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	w.Header().Set("Content-Type", "application/javascript")
	next(w, r)
}

func contentTypeSvgMw(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	w.Header().Set("Content-Type", "image/svg+xml")
	next(w, r)
}

func loginRequiredMw(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	log.Debugf("Checking user is logged in")

	isLoggedIn, user, err := isLoggedIn(r)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !isLoggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		log.Debugf("No user info so this is unauthorized")
		return
	}

	ctx := context.WithValue(r.Context(), "user", user)
	next(w, r.WithContext(ctx))
}

func userFromContext(ctx context.Context) *database.User {
	user, ok := ctx.Value("user").(database.User)
	if !ok {
		return nil
	}
	return &user
}

func basicAuthRequiredMw(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	log.Debugf("Checking for basic auth credentials")

	user, pass, _ := r.BasicAuth()

	if user != os.Getenv("MB_API_USER") || pass != os.Getenv("MB_API_PASSWORD") {
		log.Debugf("Basic auth failed for user %s", user)

		http.Error(w, "Unauthorized.", 401)
		return
	}

	log.Debugf("Basic auth credentials were correct")
	next(w, r)
}

// StartServer starts the REST API.
func StartServer(dbpg database.PgDB, queueService queue.Service, registryService registry.Service, hubService hub.InfoService, encryptionService encryption.Service) {
	qs = queueService
	rs = registryService
	hs = hubService
	es = encryptionService
	db = dbpg
	gothic.Store = db.SessionStore
	sessionStore = db.SessionStore
	webhookURL = os.Getenv("MB_WEBHOOK_URL")

	// Right now we only use github as a provider
	gothic.GetProviderName = func(*http.Request) (string, error) { return "github", nil }
	log.Debugf("Redirect callback is %s", os.Getenv("MB_API_URL")+"/v1/auth/github/callback")
	goth.UseProviders(github.New(os.Getenv("MB_GITHUB_KEY"), os.Getenv("MB_GITHUB_SECRET"), os.Getenv("MB_API_URL")+"/v1/auth/github/callback"))

	r := muxRoutes()

	n := negroni.Classic()
	n.UseHandler(r)
	http.ListenAndServe(":8080", n)
}

// Where a version has several tags, we use the longest one
func getLongestTag(iv *database.ImageVersion) string {
	var longestTag string
	var latestExists bool
	tags, _ := db.GetTags(iv)
	for _, tag := range tags {
		if tag.Tag == "latest" {
			// We use "latest" as a last resort
			latestExists = true
		} else {
			if len(tag.Tag) > len(longestTag) {
				longestTag = tag.Tag
			}
		}
	}

	if longestTag == "" && latestExists {
		longestTag = "latest"
	}

	return longestTag
}

func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(constHealthCheckMessage))
}

func handleImageWebhook(w http.ResponseWriter, r *http.Request) {
	var org, image, authToken string
	var ok bool

	vars := mux.Vars(r)
	authToken = vars["authToken"]
	image = vars["image"]
	if org, ok = vars["org"]; !ok {
		org = "library"
	}

	img, err := db.GetImage(org + "/" + image)
	var msg string

	if err == nil {
		if img.AuthToken == authToken {
			w.WriteHeader(http.StatusOK)
			qs.SendImage(img.Name, "Webhook resent")
			msg = "OK"
		} else {
			msg = "Bad token"
			w.WriteHeader(http.StatusForbidden)
		}
	} else {
		msg = "Image not found"
		w.WriteHeader(http.StatusNotFound)
	}

	w.Write([]byte(msg))
}
