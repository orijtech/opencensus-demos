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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	xray "github.com/census-instrumentation/opencensus-go-exporter-aws"
	"go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
)

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
	if err := view.Subscribe(ochttp.DefaultClientViews...); err != nil {
		log.Fatalf("Failed to subscribe to views: %v", err)
	}
	log.Printf("Finished exporter registration")
}

func main() {
	client := &http.Client{Transport: &ochttp.Transport{}}
	br := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("Content to search$ ")
		input, _, err := br.ReadLine()
		if err != nil {
			log.Fatalf("Failed to read input: %v", err)
		}
		inBlob, err := json.Marshal(map[string]string{
			"keywords": string(input),
		})
		if err != nil {
			log.Fatalf("Failed to json.Marshal input blob: %v", err)
		}
		req, err := http.NewRequest("POST", "http://localhost:9778/search", bytes.NewReader(inBlob))
		if err != nil {
			log.Fatalf("Failed to build POST request: %v", err)
		}
		ctx, span := trace.StartSpan(context.Background(), "go-search")
		span.Annotate(
			[]trace.Attribute{
				trace.StringAttribute("client", "go"),
			}, "identifiers")
		req = req.WithContext(ctx)
		res, err := client.Do(req)
		span.End()
		if err != nil {
			log.Fatalf("Failed to POST: %v", err)
		}
		outBlob, err := ioutil.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			log.Fatalf("Failed to read res.Body: %v", err)
		}
		fmt.Printf("%s\n\nsc: %+v\n\n", outBlob, span.SpanContext())
	}
}
