package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/xml"
	"log"
	"net/http"
	"net/url"
	"reichard.io/libgen-opds/client"
	"reichard.io/libgen-opds/opds"
	"time"
)

func (api *API) basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if ok {
			usernameHash := sha256.Sum256([]byte(username))
			passwordHash := sha256.Sum256([]byte(password))
			expectedUsernameHash := sha256.Sum256([]byte(api.Username))
			expectedPasswordHash := sha256.Sum256([]byte(api.Password))

			usernameMatch := (subtle.ConstantTimeCompare(usernameHash[:], expectedUsernameHash[:]) == 1)
			passwordMatch := (subtle.ConstantTimeCompare(passwordHash[:], expectedPasswordHash[:]) == 1)

			if usernameMatch && passwordMatch {
				next.ServeHTTP(w, r)
				return
			}
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

func (api *API) RootHandler(w http.ResponseWriter, r *http.Request) {

	// Headers
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	w.Header().Set("Content-Type", "application/xml")

	if r.Method == http.MethodHead {
		return
	}

	rootFeed := opds.Feed{
		Title:   "LibGen OPDS Bridge",
		Updated: time.Now().UTC(),
		Links: []opds.Link{
			opds.Link{
				Title:    "Search LibGen",
				Rel:      "search",
				TypeLink: "application/opensearchdescription+xml",
				Href:     "search.xml",
			},
		},
		Entries: []opds.Entry{
			opds.Entry{
				Title: "Goodreads - Most Read This Month",
				Content: &opds.Content{
					Content:     "Goodreads - Most Read This Month",
					ContentType: "text",
				},
				Links: []opds.Link{
					opds.Link{
						Href:     "./most-read?cadence=month",
						TypeLink: "application/atom+xml;type=feed;profile=opds-catalog",
					},
				},
			},
			opds.Entry{
				Title: "Goodreads - Most Read This Year",
				Content: &opds.Content{
					Content:     "Goodreads - Most Read This Year",
					ContentType: "text",
				},
				Links: []opds.Link{
					opds.Link{
						Href:     "./most-read?cadence=year",
						TypeLink: "application/atom+xml;type=feed;profile=opds-catalog",
					},
				},
			},
			// opds.Entry{
			// 	Title: "Check for Updates",
			// 	Content: &opds.Content{
			// 		Content:     "Check for Updates",
			// 		ContentType: "text",
			// 	},
			// 	Links: []opds.Link{
			// 		opds.Link{
			// 			Href:     "./update-check",
			// 			TypeLink: "application/atom+xml;type=feed;profile=opds-catalog",
			// 		},
			// 	},
			// },
		},
	}

	feedXML, _ := xml.Marshal(rootFeed)
	w.Write(feedXML)

}

func (api *API) SearchDescriptionHandler(w http.ResponseWriter, r *http.Request) {

	// Headers
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	w.Header().Set("Content-Type", "application/xml")

	if r.Method == http.MethodHead {
		return
	}

	w.Write([]byte(`
		<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/">
			<ShortName>Search LibGen</ShortName>
			<Description>Search LibGen</Description>
			<Url type="application/atom+xml;profile=opds-catalog;kind=acquisition" template="./search?query={searchTerms}"/>
		</OpenSearchDescription>`))

}

func (api *API) DownloadHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	downloadType := r.URL.Query().Get("type")

	// Derive Info URL
	var infoURL string
	switch downloadType {
	case "fiction":
		infoURL = api.Resolver.LibGenBase() + "/fiction/" + id
	case "non-fiction":
		infoURL = api.Resolver.LibGenBase() + "/main/" + id
	case "zlib":
		infoURL = api.Resolver.ZLibBase() + "/book/" + id
	default:
		http.Error(w, "unknown download type", http.StatusBadRequest)
		return
	}

	// Acquire info page
	body, err := client.GetPage(infoURL)
	if err != nil {
		log.Printf("upstream fetch failed for %s: %v", infoURL, err)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer body.Close()

	// Pick parser per source
	var downloadURL string
	if downloadType == "zlib" {
		downloadURL = client.ParseZLibDownloadURL(body)
	} else {
		downloadURL = client.ParseLibGenDownloadURL(body)
	}

	if downloadURL == "" {
		http.Error(w, "download URL not found", http.StatusNotFound)
		return
	}

	// Redirect
	http.Redirect(w, r, downloadURL, 301)
}

func (api *API) GoodReadsMostReadHandler(w http.ResponseWriter, r *http.Request) {

	// Headers
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	w.Header().Set("Content-Type", "application/xml")

	if r.Method == http.MethodHead {
		return
	}

	// Derive Duration
	var duration string
	if r.URL.Query().Get("cadence") == "month" {
		duration = "m"
	} else {
		duration = "y"
	}

	// Acquire & Parse Page Source
	mostReadURL := "https://www.goodreads.com/book/most_read?category=all&country=US&duration=" + duration
	body, err := client.GetPage(mostReadURL)
	if err != nil {
		log.Printf("upstream fetch failed for %s: %v", mostReadURL, err)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer body.Close()
	allEntries := client.ParseGoodReads(body)

	// Build XML
	mostReadFeed := &opds.Feed{
		Title:   "GoodReads Most Read",
		Updated: time.Now().UTC(),
		Entries: allEntries,
	}
	feedXML, _ := xml.Marshal(mostReadFeed)

	// Serve
	w.Write(feedXML)

}

func (api *API) LibZMostPopularHandler(w http.ResponseWriter, r *http.Request) {
	// Headers
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	w.Header().Set("Content-Type", "application/xml")

	if r.Method == http.MethodHead {
		return
	}

	// Acquire & Parse Page Source
	popURL := api.Resolver.ZLibBase() + "/popular.php"
	body, err := client.GetPage(popURL)
	if err != nil {
		log.Printf("upstream fetch failed for %s: %v", popURL, err)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer body.Close()
	allEntries := client.ParseZLibPopular(body)

	// Build XML
	mostReadFeed := &opds.Feed{
		Title:   "ZLib Most Popular",
		Updated: time.Now().UTC(),
		Entries: allEntries,
	}
	feedXML, _ := xml.Marshal(mostReadFeed)

	// Serve
	w.Write(feedXML)
}

func (api *API) SearchHandler(w http.ResponseWriter, r *http.Request) {

	// Headers
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	w.Header().Set("Content-Type", "application/xml")

	if r.Method == http.MethodHead {
		return
	}

	// Acquire Params
	query := r.URL.Query().Get("query")
	searchType := r.URL.Query().Get("type")

	var allEntries []opds.Entry
	feedTitle := "Search Results"

	if searchType == "fiction" {
		// Search Fiction
		url := api.Resolver.LibGenBase() + "/fiction/?q=" + url.QueryEscape(query) + "&language=English"
		body, err := client.GetPage(url)
		if err != nil {
			log.Printf("upstream fetch failed for %s: %v", url, err)
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer body.Close()
		allEntries = client.ParseLibGenFiction(body)
	} else if searchType == "non-fiction" {
		// Search NonFiction
		url := api.Resolver.LibGenBase() + "/search.php?req=" + url.QueryEscape(query)
		body, err := client.GetPage(url)
		if err != nil {
			log.Printf("upstream fetch failed for %s: %v", url, err)
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer body.Close()
		allEntries = client.ParseLibGenNonFiction(body)
	} else {
		// Offer Options
		feedTitle = "Select Search Type"
		allEntries = []opds.Entry{
			opds.Entry{
				Title: "Search Fiction",
				Content: &opds.Content{
					Content:     "Search Fiction",
					ContentType: "text",
				},
				Links: []opds.Link{
					opds.Link{
						Href:     "./search?type=fiction&query=" + url.QueryEscape(query),
						TypeLink: "application/atom+xml;type=feed;profile=opds-catalog",
					},
				},
			},
			opds.Entry{
				Title: "Search Non-Fiction",
				Content: &opds.Content{
					Content:     "Search Non-Fiction",
					ContentType: "text",
				},
				Links: []opds.Link{
					opds.Link{
						Href:     "./search?type=non-fiction&query=" + url.QueryEscape(query),
						TypeLink: "application/atom+xml;type=feed;profile=opds-catalog",
					},
				},
			},
		}
	}

	// Build XML
	searchFeed := &opds.Feed{
		Title:   feedTitle,
		Updated: time.Now().UTC(),
		Entries: allEntries,
	}
	feedXML, _ := xml.Marshal(searchFeed)

	// Serve
	w.Write(feedXML)

}
