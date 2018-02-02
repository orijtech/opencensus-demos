// Copyright 2017, OpenCensus Authors
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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"cloud.google.com/go/spanner"
	"golang.org/x/net/context"

	"go.opencensus.io/trace"

	"github.com/odeke-em/go-uuid"
)

var (
	addr string
	mux  = http.NewServeMux()
)

func main() {
	log.Printf("Serving on: %q\n", addr)
	mux.Handle("/", http.FileServer(http.Dir("./static")))
	mux.HandleFunc("/upload", byRawUploads)
	mux.HandleFunc("/url", byURL)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("listenAndServe: %v", err)
	}
}

func retrieveFile(ctx context.Context, req *http.Request) (io.Reader, error) {
	ctx, span := trace.StartSpan(ctx, "/retrieve-file")
	defer span.End()
	return retrieveBase64Upload(ctx, req)
}

var errNoFiles = errors.New(`no elements with key "file" found`)

func retrieveBase64Upload(ctx context.Context, req *http.Request) (io.Reader, error) {
	ctx, span := trace.StartSpan(ctx, "/retrieve-base64")
	defer span.End()
	base64Elements := req.MultipartForm.Value["file"]
	if len(base64Elements) == 0 {
		return nil, errNoFiles
	}
	splits := strings.Split(base64Elements[0], ";base64,")
	if len(splits) < 2 {
		return nil, errNoFiles
	}
	switch key := splits[0]; key {
	default:
		return nil, fmt.Errorf("unknown format %q", key)
	case "data:image/png":
		go recordStatsBase64UploadsCount(ctx, 1)
		rd := base64.NewDecoder(base64.StdEncoding, strings.NewReader(splits[1]))
		return rd, nil
	}
}

func byRawUploads(rw http.ResponseWriter, req *http.Request) {
	log.Printf("byRawUpload: %v\n", req)
	ctx, span := trace.StartSpan(req.Context(), "/upload")
	defer span.End()

	if err := req.ParseMultipartForm(1 << 40); err != nil {
		log.Printf("ParseMultipartForm err: %v", err)
		// TODO: break these down to record specifically parse errors
		recordStatsErrorCount(ctx, 1)
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	file, err := retrieveFile(ctx, req)
	if err != nil {
		recordStatsErrorCount(ctx, 1)
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	if c, ok := file.(io.Closer); ok {
		defer c.Close()
	}

	detectAndReply(file, ctx, rw, req)
}

type dbSaver struct {
	Referer   string `json:"referer" spanner:"referer"`
	NumFaces  int64  `json:"n_faces" spanner:"n_faces"`
	NumLabels int64  `json:"n_labels" spanner:"n_labels"`
	UserAgent string `json:"ua" spanner:"ua"`
	UserID    string `json:"user_id" spanner:"user_id"`
}

const databaseName = "projects/census-demos/instances/census-demos/databases/demo1"

func userIDOrGenerate(ctx context.Context, req *http.Request) string {
	_, span := trace.StartSpan(ctx, "/user-id-or-generate")
	defer span.End()

	reqUserID := strings.TrimSpace(req.Header.Get("X-VISION-USERID"))
	if reqUserID == "" {
		// TODO: Add a measure to count the number of new userIDs
		reqUserID = uuid.NewRandom().String()
	}
	return reqUserID
}

func saveToDBDetectionResults(ctx context.Context, res *DetectionResult, req *http.Request) {
	ctx, span := trace.StartSpan(ctx, "/save-to-db")
	defer span.End()

	spannerClient, err := spanner.NewClient(ctx, databaseName)
	if err != nil {
		recordStatsDBErrorCount(ctx, 1)
		return
	}
	defer spannerClient.Close()

	ds := &dbSaver{
		Referer:   req.Referer(),
		NumFaces:  int64(len(res.Faces)),
		NumLabels: int64(len(res.Labels)),
		UserAgent: req.UserAgent(),
		UserID:    userIDOrGenerate(ctx, req),
	}

	m, err := spanner.InsertStruct("CV", ds)
	if err != nil {
		log.Printf("spanner.InsertStruct: %v", err)
		recordStatsDBErrorCount(ctx, 1)
		return
	}
	if _, err := spannerClient.Apply(ctx, []*spanner.Mutation{m}); err != nil {
		log.Printf("spannerClient.Apply: %v", err)
		recordStatsDBErrorCount(ctx, 1)
	}
}

func detectAndReply(r io.Reader, ctx context.Context, rw http.ResponseWriter, req *http.Request) {
	ctx, span := trace.StartSpan(ctx, "/detect-and-reply")
	defer span.End()
	res, err := detectFacesAndLogos(r, ctx)
	if err != nil {
		recordStatsErrorCount(ctx, 1)
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	// log.Printf("res: %v\n", res)
	enc := json.NewEncoder(rw)
	if err := enc.Encode(res); err != nil {
		recordStatsErrorCount(ctx, 1)
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	saveToDBDetectionResults(ctx, res, req)
}

type urlIn struct {
	URL string `json:"url"`
}

func byURL(rw http.ResponseWriter, req *http.Request) {
	ctx, span := trace.StartSpan(req.Context(), "/url")
	defer span.End()

	defer req.Body.Close()

	blob, err := ioutil.ReadAll(req.Body)
	if err != nil {
		recordStatsErrorCount(ctx, 1)
		http.Error(rw, err.Error(), http.StatusBadRequest)
	}
	ui := new(urlIn)
	if err := json.Unmarshal(blob, ui); err != nil {
		recordStatsErrorCount(ctx, 1)
		http.Error(rw, err.Error(), http.StatusBadRequest)
	}
	body, err := fetchIt(ui.URL, ctx)
	if err := json.Unmarshal(blob, ui); err != nil {
		recordStatsErrorCount(ctx, 1)
		http.Error(rw, err.Error(), http.StatusBadRequest)
	}
	detectAndReply(body, ctx, rw, req)
}

func fetchIt(url string, ctx context.Context) (io.ReadCloser, error) {
	dlCtx, span := trace.StartSpan(ctx, "/url-get")
	defer span.End()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(dlCtx)
	res, err := http.DefaultClient.Do(req)
	if code := res.StatusCode; code < 200 || code > 299 {
		return res.Body, fmt.Errorf("%s %d", res.Status, code)
	}
	return res.Body, nil
}
