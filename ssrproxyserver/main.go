package main

import (
	//"os/exec"
	//"fmt"
	"bytes"
	"github.com/BurntSushi/toml"
	"github.com/julienschmidt/httprouter"
	//"github.com/k0kubun/pp"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	Routes        map[string]*Route `toml:"route""`
	RoutesByPath  map[string]*Route
}

type Route struct {
	Name         string
	Path         string `toml:"path"`
	BodySelector string `toml:"body_selector"`
	OGP          bool   `toml:"ogp"`
	SSR          bool   `toml:"ssr"`
	AMP          bool   `toml:"amp"`
}

func (r *Route) Init(name string) {
	r.Name = name
	if r.BodySelector == "" {
		r.BodySelector = "main"
	}
}

var sampleToml = `
domain = "https://example.com"
proxy_address = ":8080"
backend_server = "http://192.168.1.3:8000"
site_name = "shibu.jp"
site_owner = "@shibu_jp"
default_image = ""
document_selectors = ["main"]

[route.top]
    path = "/"
	body_selector = "#root"
    ogp = true
    ssr = true
    amp = true

[route.test]
    path = "/test"
	body_selector = "#main"
    ogp = true
    ssr = true
    amp = true
`

func makeReverseProxyDirector(config *Config) (func(request *http.Request), error) {
	backendURL, err := url.Parse(config.BackendServer)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Forward to %s\n", backendURL.String())
	return func(request *http.Request) {
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
		runner.Request(&url)
	}, nil
}

func makeCustomReverseProxy(config *Config, route *Route, director func(request *http.Request)) http.Handler {
	rp := &httputil.ReverseProxy{
		Director: director,
	}
	if route.AMP || route.OGP || route.SSR {
		rp.ModifyResponse = func(res *http.Response) error {
			log.Println("modifyResponse", route.Name, res.Request.URL.String())
			return nil
		}
	}
	return rp
}

func main() {
	config := Config{
		RoutesByPath: make(map[string]*Route),
	}
	_, err := toml.Decode(sampleToml, &config)
	if err != nil {
		panic(err)
	}
	director, err := makeReverseProxyDirector(&config)
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
	for _, method := range methods {
		if method == http.MethodGet && hasRoute {
			continue
		}
		router.Handler(method, "/", &httputil.ReverseProxy{Director: director})
	}
	fmt.Printf("Start listening at %s\n", config.ProxyAddress)
	log.Fatal(http.ListenAndServe(config.ProxyAddress, router))
}
