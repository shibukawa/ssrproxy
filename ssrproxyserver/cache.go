package main

import (
	"github.com/robfig/go-cache"
	"net/http"
	"time"
)

type Cache struct {
	cache *cache.Cache
}

type CachedEntry struct {
	InnerHTML string
	OGP       string
	Wait      chan struct{}
}

func NewCache() *Cache {
	return &Cache{
		cache: cache.New(time.Hour, time.Hour),
	}
}

func (c Cache) Get(r *http.Request) *CachedEntry {
	cachedEntry, found := c.cache.Get(r.URL.String())
	if !found {
		return nil
	}
	return cachedEntry.(*CachedEntry)
}

func (c Cache) Set(r *http.Request, entry *CachedEntry) {
	c.cache.Set(r.URL.String(), entry, 0)
}

func (c Cache) Wait(r *http.Request) *CachedEntry {
	cachedEntry := c.Get(r)
	if cachedEntry == nil {
		return nil
	}
	<-cachedEntry.Wait
	return cachedEntry
}
