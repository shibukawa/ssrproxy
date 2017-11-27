package main

import (
	"bytes"
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/PuerkitoBio/goquery"
	"github.com/julienschmidt/httprouter"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
)

var (
	runner *Runner
)

type Config struct {
	Domain        string            `toml:"domain"`
	ProxyAddress  string            `toml:"proxy_address"`
	BackendServer string            `toml:"backend_server"`
	SiteName      string            `toml:"site_name"`
	SiteOwner     string            `toml:"site_owner"`
	SiteLogoURL   string            `toml:"site_logo_url"`
	TwitterID     string            `tmol:"twitter_id"'`
	FacebookAppID string            `tmol:"facebook_app_id"'`
	Routes        map[string]*Route `toml:"route""`
	RoutesByPath  map[string]*Route
}

type Route struct {
	Name         string
	Path         string `toml:"path"`
	BodySelector string `toml:"body_selector"`
	OGP          bool   `toml:"ogp"`
	SSR          bool   `toml:"ssr"`
}

func (r *Route) Init(name string) {
	r.Name = name
	if r.BodySelector == "" {
		r.BodySelector = "main"
	}
}

func makeReverseProxyDirector(config *Config, route *Route) (func(request *http.Request), error) {
	backendURL, err := url.Parse(config.BackendServer)
	if err != nil {
		return nil, err
	}
	return func(request *http.Request) {
		log.Println("receive request:", request.URL.String())
		url := *request.URL
		url.Scheme = backendURL.Scheme
		url.Host = backendURL.Host
		var body io.Reader
		if body != nil {
			buffer, err := ioutil.ReadAll(request.Body)
			if err != nil {
				log.Fatal(err.Error())
			}
			body = bytes.NewBuffer(buffer)
		}
		req, err := http.NewRequest(request.Method, url.String(), body)
		if err != nil {
			log.Fatal(err.Error())
		}
		req.Header = request.Header
		*request = *req
		if route != nil && (route.OGP || route.SSR) {
			go runner.Request(request, route)
		}
	}, nil
}

func makeCustomReverseProxy(config *Config, route *Route, director func(request *http.Request)) http.Handler {
	rp := &httputil.ReverseProxy{
		Director: director,
	}
	if route.OGP || route.SSR {
		rp.ModifyResponse = func(res *http.Response) error {
			log.Println("modifyResponse", route.Name, res.Request.URL.String())
			result := runner.WaitResult(res.Request)
			document, err := goquery.NewDocumentFromReader(res.Body)
			if err != nil {
				return err
			}
			document.Find("head").AppendHtml(result.OGP)
			body := document.Find(route.BodySelector)
			body.SetHtml("")
			body.AppendHtml(result.InnerHTML)
			html, err := document.Html()
			if err != nil {
				return err
			}
			rawHtml := []byte(html)
			res.Header.Set("Content-Length", strconv.Itoa(len(rawHtml)))
			res.Body = ioutil.NopCloser(bytes.NewReader(rawHtml))
			return nil
		}
	}
	return rp
}

func main() {
	config := Config{
		RoutesByPath: make(map[string]*Route),
	}

	_, err := toml.DecodeFile("config.toml", &config)
	if err != nil {
		panic(err)
	}
	router := httprouter.New()
	hasRoute := false
	for name, route := range config.Routes {
		route.Init(name)
		if route.Path == "/" {
			hasRoute = true
		}
		director, err := makeReverseProxyDirector(&config, route)
		if err != nil {
			panic(err)
		}
		config.RoutesByPath[route.Path] = route
		router.Handler(http.MethodGet, route.Path, makeCustomReverseProxy(&config, route, director))
	}
	runner = NewRunner(&config)
	methods := []string{
		http.MethodConnect,
		http.MethodDelete,
		http.MethodGet,
		http.MethodOptions,
		http.MethodPatch,
		http.MethodPost,
		http.MethodPut,
		http.MethodTrace,
	}
	director, err := makeReverseProxyDirector(&config, nil)
	if err != nil {
		panic(err)
	}
	for _, method := range methods {
		if method == http.MethodGet && hasRoute {
			continue
		}
		router.Handler(method, "/", &httputil.ReverseProxy{Director: director})
	}
	fmt.Printf("Start listening at %s\n", config.ProxyAddress)
	log.Fatal(http.ListenAndServe(config.ProxyAddress, router))
}
