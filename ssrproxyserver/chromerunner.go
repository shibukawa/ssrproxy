package main

import (
	"context"
	"log"
	//"runtime"
	"sync"

	cdp "github.com/knq/chromedp"
	//cdptypes "github.com/knq/chromedp/cdp"
	"github.com/knq/chromedp/client"
	"fmt"
	"net/url"
	"time"
)

type Task struct {
	URL   *url.URL
	Result *Result
}

type Runner struct {
	queue   chan *Task
	config  *Config
	chrome  *cdp.CDP
	lock    sync.Mutex
	results map[string]*Result
}

type Result struct {
	InnerHTML string
	Error     error
	Wait      chan struct{}
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
			timeout, cancel := context.WithTimeout(ctxt, time.Second * 5)
			result := task.Result
			route := runner.config.RoutesByPath[task.URL.Path]
			fmt.Println(task.URL.Path, runner.config.Routes)
			fmt.Println("task.URL.String()", task.URL.String())
			fmt.Println("route.BodySelector", route.BodySelector)
			tasks := cdp.Tasks{
				cdp.Navigate(task.URL.String()),
				cdp.Sleep(time.Second),
				cdp.WaitVisible(route.BodySelector, cdp.ByQuery),
				cdp.InnerHTML(route.BodySelector, &result.InnerHTML),
			}
			err := chrome.Run(timeout, tasks)
			if err != nil {
				result.Error = err
			} else {
				log.Println("result.InnerHTML", result.InnerHTML)
			}
			cancel()
			close(result.Wait)
		}
	}()
	return nil
}

func NewRunner(config *Config) *Runner {
	runner := &Runner{
		config:  config,
		queue:   make(chan *Task, 10),
		results: make(map[string]*Result),
	}
	chromeWorker(runner)
	return runner
}

func (r *Runner) Request(url *url.URL) *Result {
	r.lock.Lock()
	defer r.lock.Unlock()
	result, ok := r.results[url.String()]
	if ok {
		return result
	}
	task := &Task{
		URL: url,
		Result: &Result{
			Wait: make(chan struct{}),
		},
	}
	r.results[url.String()] = task.Result
	go func() {
		r.queue <- task
	}()
	return task.Result
}

func (r *Runner) Wait(url *url.URL) *Result {
	result := r.Request(url)
	<-result.Wait
	return result
}