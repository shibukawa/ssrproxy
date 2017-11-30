package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/PuerkitoBio/goquery"
	"github.com/julienschmidt/httprouter"
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
	return func(request *http.Request) {
		log.Println("receive request:", request.URL.String())
		var urlstr string
		if strings.HasSuffix(config.BackendServer, "/") {
			urlstr = config.BackendServer + request.URL.String()[1:]
		} else {
			urlstr = config.BackendServer + request.URL.String()
		}
		newURL, err := url.Parse(urlstr)
		if err != nil {
			log.Fatal(err)
		}
		request.URL = newURL
		var body io.Reader
		if body != nil {
			buffer, err := ioutil.ReadAll(request.Body)
			if err != nil {
				log.Fatal(err.Error())
			}
			body = bytes.NewBuffer(buffer)
		}
		req, err := http.NewRequest(request.Method, urlstr, body)
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
			b := []byte(html)
			res.Body = ioutil.NopCloser(bytes.NewReader(b))
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
	proxyURL, err := url.Parse(config.ProxyAddress)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Start listening at %s\n", config.ProxyAddress)
	var port string
	if proxyURL.Port() == "" {
		port = ":80"
	} else {
		port = ":" + proxyURL.Port()
	}
	log.Fatal(http.ListenAndServe(port, router))
}
