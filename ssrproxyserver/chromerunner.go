package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	cdp "github.com/knq/chromedp"
	"github.com/knq/chromedp/client"
	"github.com/shibukawa/opengraph"
)

type Task struct {
	URL    *url.URL
	Route  *Route
	Result chan string
}

type Runner struct {
	queue   chan *Task
	config  *Config
	chrome  *cdp.CDP
	lock    sync.Mutex
	cache   *Cache
	profile *opengraph.Profile
}

func chromeWorker(runner *Runner) error {
	ctxt, cancel := context.WithCancel(context.Background())
	chrome, err := cdp.New(ctxt, cdp.WithTargets(client.New().WatchPageTargets(ctxt)), cdp.WithLog(log.Printf))
	if err != nil {
		return err
	}
	runner.chrome = chrome
	go func() {
		defer cancel()
		for task := range runner.queue {
			timeout, cancel := context.WithTimeout(ctxt, time.Second*5)
			route := runner.config.RoutesByPath[task.URL.Path]
			var html string
			tasks := cdp.Tasks{
				cdp.Navigate(task.URL.String()),
				cdp.Sleep(time.Second),
				cdp.WaitVisible(route.BodySelector, cdp.ByQuery),
				cdp.InnerHTML("html", &html),
			}
			err := chrome.Run(timeout, tasks)
			if err != nil {
				close(task.Result)
			} else {
				task.Result <- html
				close(task.Result)
			}
			cancel()
		}
	}()
	return nil
}

func NewRunner(config *Config) *Runner {
	runner := &Runner{
		config:  config,
		queue:   make(chan *Task, 10),
		cache:   NewCache(),
		profile: opengraph.NewProfile(config.Domain, config.SiteOwner, config.SiteOwner, config.SiteLogoURL, config.SiteName, config.TwitterID, config.FacebookAppID),
	}
	chromeWorker(runner)
	return runner
}

func (r *Runner) Request(request *http.Request, route *Route) {
	r.lock.Lock()
	defer r.lock.Unlock()
	cachedEntry := r.cache.Get(request)
	if cachedEntry != nil {
		return
	}
	cachedEntry = &CachedEntry{
		Wait: make(chan struct{}),
	}
	defer close(cachedEntry.Wait)

	r.cache.Set(request, cachedEntry)

	task := &Task{
		URL:    request.URL,
		Result: make(chan string),
		Route:  route,
	}
	r.queue <- task
	html := <-task.Result
	document, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return
	}
	main := document.Find(route.BodySelector)
	innerHTML, err := main.Html()
	if err != nil {
		return
	}
	description := main.Text()
	if len(description) > 160 {
		description = description[:160]
	}
	title := document.Find("title").Text()
	imagePath := r.config.SiteLogoURL
	image := document.Find("image")
	if image != nil {
		imagePath = image.AttrOr("src", r.config.SiteLogoURL)
	}
	article := r.profile.Article(request.URL.String(), title, description, imagePath, time.Now())
	cachedEntry.InnerHTML = innerHTML
	cachedEntry.OGP = article.Write()
	return
}

func (r *Runner) WaitResult(request *http.Request) *CachedEntry {
	return r.cache.Wait(request)
}
