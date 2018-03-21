// Copyright 2018, OpenCensus Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"golang.org/x/net/context"

	"github.com/dgraph-io/badger"

	xray "github.com/census-instrumentation/opencensus-go-exporter-aws"
	"go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/plugin/ochttp/propagation/b3"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"

	"github.com/orijtech/youtube"
	gat "google.golang.org/api/googleapi/transport"
)

var yc *youtube.Client
var db *badger.DB

func init() {
	trace.SetDefaultSampler(trace.AlwaysSample())
	xe, err := xray.NewExporter(xray.WithVersion("latest"))
	if err != nil {
		log.Fatalf("X-Ray newExporter: %v", err)
	}
	trace.RegisterExporter(xe)
	se, err := stackdriver.NewExporter(stackdriver.Options{ProjectID: "census-demos"})
	if err != nil {
		log.Fatalf("Stackdriver newExporter: %v", err)
	}
	trace.RegisterExporter(se)
	view.RegisterExporter(se)
	if err := view.Subscribe(ochttp.DefaultServerViews...); err != nil {
		log.Fatalf("Failed to subscribe to views: %v", err)
	}
	log.Printf("Finished exporter registration")

	envAPIKey := os.Getenv("YOUTUBE_API_KEY")
	yc, err = youtube.NewWithHTTPClient(&http.Client{
		Transport: &ochttp.Transport{Base: &gat.APIKey{Key: envAPIKey}},
	})
	if err != nil {
		log.Fatalf("Failed to create youtube API client: %v", err)
	}
}

func main() {
	dir, err := ioutil.TempDir("", "badger")
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	defer os.RemoveAll(dir)
	opts := badger.DefaultOptions
	opts.Dir = dir
	opts.ValueDir = dir
	db, err = badger.Open(ctx, opts)
	if err != nil {
		log.Fatal(err)
	}

	addr := ":9778"
	mux := http.NewServeMux()
	mux.HandleFunc("/search", search)

	h := &ochttp.Handler{
		Handler:     mux,
		Propagation: &b3.HTTPFormat{},
	}
	log.Printf("Serving on %q", addr)
	if err := http.ListenAndServe(addr, h); err != nil {
		log.Fatalf("ListenAndServe err: %v", err)
	}
}

type query struct {
	Keywords   string `json:"keywords"`
	MaxPerPage int64  `json:"max_per_page"`
	MaxPages   int64  `json:"max_pages"`
}

func search(w http.ResponseWriter, r *http.Request) {
	sc := trace.FromContext(r.Context()).SpanContext()
	log.Printf("search here: %+v\n", sc)
	ctx, span := trace.StartSpan(r.Context(), "/search")
	defer span.End()

	q := new(query)
	if err := parseJSON(ctx, r.Body, q); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	q.setDefaultLimits()

	keywords := q.Keywords
	var outBlob []byte
	// 1. Firstly check if this has been cached before
	err := db.View(ctx, func(cctx context.Context, txn *badger.Txn) error {
		item, err := txn.Get(cctx, []byte(keywords))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil
			}
			return err
		}
		log.Printf("item: %+v\n", item)
		outBlob, err = item.Value()
		return err
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(outBlob) > 0 {
		// Cache hit!
		w.Write(outBlob)
		return
	}

	// 2. Otherwise that was a cache-miss, now retrieve it then save it
	pagesChan, err := yc.Search(ctx, &youtube.SearchParam{
		Query:             keywords,
		MaxPage:           uint64(q.MaxPages),
		MaxResultsPerPage: uint64(q.MaxPerPage),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var pages []*youtube.SearchPage
	for page := range pagesChan {
		pages = append(pages, page)
	}
	outBlob, err = json.Marshal(pages)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Now cache it so that next time it'll be a hit.
	txn := db.NewTransaction(ctx, true)
	txn.Set(ctx, []byte(keywords), outBlob)
	_ = txn.Commit(ctx, func(err error) {})

	_, _ = w.Write(outBlob)
}

func parseJSON(ctx context.Context, r io.Reader, recv interface{}) error {
	ctx, span := trace.StartSpan(ctx, "/parse-json")
	span.End()

	blob, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	return json.Unmarshal(blob, recv)
}

func (q *query) setDefaultLimits() {
	if q.MaxPerPage <= 0 {
		q.MaxPerPage = 5
	}
	if q.MaxPages <= 0 {
		q.MaxPages = 1
	}
}
