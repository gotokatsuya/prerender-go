package prerender

import (
	"compress/gzip"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type Options struct {
	PrerenderURL *url.URL
	Token        string
}

func NewOptions() *Options {
	var (
		prerenderRawURL string
	)
	value, ok := os.LookupEnv("PRERENDER_SERVICE_URL")
	switch {
	case ok:
		prerenderRawURL = value
	default:
		prerenderRawURL = "https://service.prerender.io/"
	}
	prerenderURL, err := url.Parse(prerenderRawURL)
	if err != nil {
		panic(err)
	}
	return &Options{
		PrerenderURL: prerenderURL,
		Token:        os.Getenv("PRERENDER_TOKEN"),
	}
}

type Prerender struct {
	Options *Options
}

func New(o *Options) *Prerender {
	return &Prerender{Options: o}
}

func (p *Prerender) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.ShouldPrerender(r) {
			next.ServeHTTP(w, r)
			return
		}
		if err := p.PrerenderHandler(w, r); err != nil {
			log.Printf("PrerenderHandler err:%v", err)
			next.ServeHTTP(w, r)
		}
	})
}

func (p *Prerender) ShouldPrerender(r *http.Request) bool {
	userAgent := strings.ToLower(r.Header.Get("User-Agent"))
	if userAgent == "" {
		return false
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		return false
	}
	if v := r.Header.Get("X-Prerender"); v != "" {
		return false
	}

	isRequestingPrerenderedPage := false

	// if it contains _escaped_fragment_, show prerendered page
	if v := r.URL.Query().Get("_escaped_fragment_"); v != "" {
		isRequestingPrerenderedPage = true
	}

	// if it is a bot...show prerendered page
	for _, crawlerUserAgent := range crawlerUserAgents {
		if strings.Contains(strings.ToLower(userAgent), strings.ToLower(crawlerUserAgent)) {
			isRequestingPrerenderedPage = true
			break
		}
	}

	// if it is BufferBot...show prerendered page
	if r.Header.Get("X-Bufferbot") != "" {
		isRequestingPrerenderedPage = true
	}

	// if it is a bot and is requesting a resource...dont prerender
	for _, extension := range extensionsToIgnore {
		if strings.HasSuffix(strings.ToLower(r.URL.String()), strings.ToLower(extension)) {
			return false
		}
	}

	return isRequestingPrerenderedPage
}

func (p *Prerender) PrerenderHandler(w http.ResponseWriter, r *http.Request) error {
	req, err := http.NewRequest("GET", p.buildAPIURL(r), nil)
	if err != nil {
		return err
	}

	if p.Options.Token != "" {
		req.Header.Set("X-Prerender-Token", p.Options.Token)
	}
	req.Header.Set("User-Agent", r.Header.Get("User-Agent"))
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	req.Header.Set("Accept-Encoding", "gzip")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// prerender server error
	if res.StatusCode >= 500 && res.StatusCode <= 511 {
		return err
	}

	w.Header().Set("Content-Type", res.Header.Get("Content-Type"))

	isGzipAcceptEncoding := strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
	isGzipContentEncoding := strings.Contains(res.Header.Get("Content-Encoding"), "gzip")
	switch {
	case isGzipAcceptEncoding && !isGzipContentEncoding:
		// gzip raw response
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(res.StatusCode)
		gz := gzip.NewWriter(w)
		defer gz.Close()
		if _, err := io.Copy(gz, res.Body); err != nil {
			return err
		}
		if err := gz.Flush(); err != nil {
			return err
		}
	case !isGzipAcceptEncoding && isGzipContentEncoding:
		// gunzip response
		w.WriteHeader(res.StatusCode)
		gz, err := gzip.NewReader(res.Body)
		if err != nil {
			return err
		}
		defer gz.Close()
		if _, err := io.Copy(w, gz); err != nil {
			return err
		}
	default:
		w.Header().Set("Content-Encoding", res.Header.Get("Content-Encoding"))
		w.WriteHeader(res.StatusCode)
		if _, err := io.Copy(w, res.Body); err != nil {
			return err
		}
	}
	return nil
}

var (
	cfSchemeRegex = regexp.MustCompile("\"scheme\":\"(http|https)\"")
)

func (p *Prerender) buildAPIURL(r *http.Request) string {
	prerenderURL := p.Options.PrerenderURL

	if !strings.HasSuffix(prerenderURL.String(), "/") {
		prerenderURL.Path = prerenderURL.Path + "/"
	}

	var protocol = r.URL.Scheme

	if cf := r.Header.Get("CF-Visitor"); cf != "" {
		match := cfSchemeRegex.FindStringSubmatch(cf)
		if len(match) > 1 {
			protocol = match[1]
		}
	}

	if len(protocol) == 0 {
		protocol = "http"
	}

	if fp := r.Header.Get("X-Forwarded-Proto"); fp != "" {
		protocol = strings.Split(fp, ",")[0]
	}

	return prerenderURL.String() + protocol + "://" + r.Host + r.URL.Path + "?" + r.URL.RawQuery
}
