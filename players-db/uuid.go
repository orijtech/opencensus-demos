package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"strconv"

	"golang.org/x/net/context"

	"go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/plugin/grpc/grpcstats"
	"go.opencensus.io/plugin/http/httptrace"
	"go.opencensus.io/stats"
	"go.opencensus.io/trace"

	"github.com/odeke-em/go-uuid"
)

func main() {
	var projectID string
	flag.StringVar(&projectID, "gcp-id", "census-demos", "the projectID of the GCP project")
	flag.Parse()

	se, err := stackdriver.NewExporter(stackdriver.Options{ProjectID: projectID})
	if err != nil {
		log.Fatalf("StatsExporter err: %v", err)
	}
	// Let's ensure that data is uploaded before the program exits
	defer se.Flush()

	// Enable tracing on the exporter
	trace.RegisterExporter(se)

	// Enable metrics collection
	stats.RegisterExporter(se)

	// Stats that we are interested in
	views := []*stats.View{
		grpcstats.RPCClientErrorCountMinuteView,
		grpcstats.RPCClientRoundTripLatencyView,
		grpcstats.RPCClientRequestBytesView,
	}
	for i, v := range views {
		if err := v.Subscribe(); err != nil {
			log.Printf("Views.Subscribe (#%d) err: %v", i, err)
		}
		defer v.Unsubscribe()
	}

	trace.SetDefaultSampler(trace.AlwaysSample())

	http.Handle("/uuids", httptrace.NewHandler(http.HandlerFunc(handleCreateUUIDs)))
	if err := http.ListenAndServe(":8989", nil); err != nil {
		log.Fatalf("http.ListenAndServe: %v", err)
	}
}

func nUUIDs(ctx context.Context, n int64) []string {
	ctx, span := trace.StartSpan(ctx, "n-new-uuids")
	defer span.End()

	var udl []string
	for i := int64(0); i < n; i++ {
		_, span := trace.StartSpan(ctx, "invoke-uuid-new-random")
		udl = append(udl, uuid.NewRandom().String())
		span.End()
	}
	return udl
}

func handleCreateUUIDs(rw http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	ctx, span := trace.StartSpan(req.Context(), "create-uuids")
	defer span.End()

	sc := span.SpanContext()
	log.Printf("handleCreateUUIDs invoked with span: %+v spanContext: %+v\n", span, sc)

	n, err := strconv.ParseInt(q.Get("c"), 10, 64)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	uuids := nUUIDs(ctx, n)
	if err := jsonSerializeUUIDs(ctx, rw, uuids); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
}

func jsonSerializeUUIDs(ctx context.Context, w io.Writer, data []string) error {
	ctx, span := trace.StartSpan(ctx, "serialize-uuids")
	defer span.End()

	blob, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = w.Write(blob)
	return err
}
