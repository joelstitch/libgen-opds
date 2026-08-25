// Package mirrors discovers currently-reachable shadow-library bases by
// scraping the Shadow Library Uptime Monitor (https://open-slum.org). It
// holds a cached base URL for libgen and z-library lookups and falls
// back to legacy hardcoded defaults when the status page is unreachable
// or reports no UP mirrors.
package mirrors

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Fallback bases used when open-slum is unreachable or yields no UP
// mirrors. These match the URLs the server used before mirror discovery
// existed, so the server stays usable across the rollout.
const (
	DefaultLibGenBase = "https://libgen.is"
	DefaultZLibBase   = "https://usa1lib.org"
)

const (
	openSlumLibGenURL = "https://open-slum.org/libgen.html"
	openSlumZLibURL   = "https://open-slum.org/zlibrary.html"
	refreshTimeout    = 15 * time.Second
)

// Resolver caches the current best base URL for libgen and z-library
// lookups. It is safe for concurrent use after construction.
type Resolver struct {
	mu         sync.RWMutex
	libGenBase string
	zLibBase   string
	httpClient *http.Client
}

// New constructs a Resolver primed with fallback bases and immediately
// attempts one refresh. A failed refresh is logged and otherwise
// ignored — the resolver still returns the fallbacks.
func New() *Resolver {
	r := &Resolver{
		libGenBase: DefaultLibGenBase,
		zLibBase:   DefaultZLibBase,
		httpClient: &http.Client{Timeout: refreshTimeout},
	}
	if err := r.Refresh(); err != nil {
		log.Printf("mirrors: initial refresh failed, using fallback bases: %v", err)
	}
	return r
}

// LibGenBase returns the cached libgen base URL (e.g. "https://libgen.bz").
// May be a fallback value if no refresh has ever succeeded.
func (r *Resolver) LibGenBase() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.libGenBase
}

// ZLibBase returns the cached z-library base URL (e.g. "https://1lib.sk").
// May be a fallback value if no refresh has ever succeeded.
func (r *Resolver) ZLibBase() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.zLibBase
}

// Refresh re-fetches the open-slum status pages and updates the
// cached bases. Each upstream is independent: a failure to refresh one
// does not prevent the other from updating. Returns the first error
// encountered, or nil if neither fetch errored.
func (r *Resolver) Refresh() error {
	libGen, lerr := fetchUpMirror(r.httpClient, openSlumLibGenURL)
	if lerr != nil {
		log.Printf("mirrors: libgen refresh failed: %v", lerr)
	} else if libGen != "" {
		r.mu.Lock()
		r.libGenBase = libGen
		r.mu.Unlock()
		log.Printf("mirrors: libgen base now %s", libGen)
	} else {
		log.Printf("mirrors: libgen page parsed but no UP mirrors; keeping %s", r.libGenBase)
	}

	zlib, zerr := fetchUpMirror(r.httpClient, openSlumZLibURL)
	if zerr != nil {
		log.Printf("mirrors: zlib refresh failed: %v", zerr)
	} else if zlib != "" {
		r.mu.Lock()
		r.zLibBase = zlib
		r.mu.Unlock()
		log.Printf("mirrors: zlib base now %s", zlib)
	} else {
		log.Printf("mirrors: zlib page parsed but no UP mirrors; keeping %s", r.zLibBase)
	}

	if lerr != nil {
		return lerr
	}
	return zerr
}

// fetchUpMirror returns the first mirror on an open-slum page whose
// status badge reads "UP", or "" if none qualify. The result is a full
// https://domain URL ready to be used as a base. Returns an error only
// on a hard network or HTTP failure.
func fetchUpMirror(client *http.Client, pageURL string) (string, error) {
	resp, err := client.Get(pageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}

	var found string
	doc.Find("div.card").Each(func(_ int, card *goquery.Selection) {
		if found != "" {
			return
		}
		// Status badge classes on open-slum are "up", "protected",
		// "degraded", "down", "unknown". Only "up" mirrors are usable
		// from a default http.Get (no Cloudflare interstitial handling).
		if card.Find("span.status-badge.up").Length() == 0 {
			return
		}
		href, exists := card.Find("a.card-title").Attr("href")
		if !exists || href == "" {
			return
		}
		found = href
	})
	return found, nil
}
