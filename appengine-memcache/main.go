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
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"google.golang.org/appengine"
	"google.golang.org/appengine/memcache"
	"google.golang.org/grpc/codes"

	xray "github.com/census-instrumentation/opencensus-go-exporter-aws"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"

	"github.com/orijtech/otils"
)

var (
	httpClient = &http.Client{Transport: new(ochttp.Transport)}
)

func main() {
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
	xe, err := xray.NewExporter(xray.WithVersion("latest"))
	if err != nil {
		log.Fatalf("X-Ray newExporter error: %v", err)
	}

	se, err := stackdriver.NewExporter(stackdriver.Options{
		ProjectID:    otils.EnvOrAlternates("OPENCENSUS_GCP_PROJECTID", "census-demos"),
		MetricPrefix: "grandcache",
	})
	if err != nil {
		log.Fatalf("Stackdriver newExporter error: %v", err)
	}
	defer se.Flush()

	pe, err := prometheus.NewExporter(prometheus.Options{Namespace: "memcache_demo"})
	if err != nil {
		log.Fatalf("Prometheus newExporter error: %v", err)
	}

	// Now register the exporters
	trace.RegisterExporter(xe)
	trace.RegisterExporter(se)
	view.RegisterExporter(se)
	view.RegisterExporter(pe)

	// Register all the views
	if err := view.Register(memcache.AllViews...); err != nil {
		log.Fatalf("Failed to register memcache.DefaultStats: %v", err)
	}
	if err := view.Register(ochttp.DefaultServerViews...); err != nil {
		log.Fatalf("Failed to register ochttp.DefaultServerViews: %v", err)
	}
	if err := view.Register(ochttp.DefaultClientViews...); err != nil {
		log.Fatalf("Failed to register ochttp.DefaultClientViews: %v", err)
	}
	view.SetReportingPeriod(3 * time.Second)

	go func() {
		peMux := http.NewServeMux()
		peMux.Handle("/metrics", pe)
		if err := http.ListenAndServe(":9988", peMux); err != nil {
			log.Fatalf("Prometheus handler ListenAndServe error: %v", err)
		}
	}()

	addr := ":8778"
	mux := http.NewServeMux()
	mux.HandleFunc("/fetch", fetchIt)
	log.Printf("Serving at: %s", addr)
	h := &ochttp.Handler{Handler: mux}
	if err := http.ListenAndServe(addr, h); err != nil {
		log.Fatalf("Serve error: %v", err)
	}
}

type request struct {
	URL string `json:"url"`
}

func fetchIt(w http.ResponseWriter, r *http.Request) {
	ctx, span := trace.StartSpan(appengine.NewContext(r), "fetchIt")
	defer span.End()

	defer r.Body.Close()
	span.Annotate(nil, "Decoding JSON from request body")
	dec := json.NewDecoder(r.Body)

	rq := new(request)
	if err := dec.Decode(rq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	span.Annotate(nil, "Finished decoding JSON from request body")

	blob, err := fetch(ctx, httpClient, rq.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, _ = w.Write(blob)
}

func fetch(ctx context.Context, httpClient *http.Client, url string) ([]byte, error) {
	ctx, span := trace.StartSpan(ctx, "fetch")
	defer span.End()

	memoized, err := memcache.Get(ctx, url)
	if err == nil && memoized != nil && len(memoized.Value) > 0 {
		span.Annotate(nil, "Cache hit!")
		return memoized.Value, nil
	}

	if err != nil {
		span.Annotate([]trace.Attribute{
			trace.StringAttribute("error", err.Error()),
		}, "Get error")
	}

	span.Annotate(nil, "Cache miss")
	span.SetStatus(trace.Status{Code: int32(codes.NotFound), Message: "Cache miss"})

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		span.SetStatus(trace.Status{Code: int32(codes.Unknown), Message: err.Error()})
		return nil, err
	}
	req = req.WithContext(ctx)
	res, err := httpClient.Do(req)
	if err != nil {
		span.SetStatus(trace.Status{Code: int32(codes.Internal), Message: err.Error()})
		return nil, err
	}
	if !otils.StatusOK(res.StatusCode) {
		span.SetStatus(trace.Status{Code: int32(codes.Unknown), Message: res.Status})
		return nil, errors.New(res.Status)
	}
	blob, err := ioutil.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		span.SetStatus(trace.Status{Code: int32(codes.Unknown), Message: err.Error()})
		return nil, err
	}
	_ = memcache.Set(ctx, &memcache.Item{Key: url, Value: blob})
	return blob, nil
}
