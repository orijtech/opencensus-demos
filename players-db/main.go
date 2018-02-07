package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/spanner"
	"golang.org/x/net/context"

	"go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/plugin/grpc/grpcstats"
	"go.opencensus.io/plugin/http/httptrace"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
)

type Player struct {
	FirstName string `spanner:"first_name"`
	LastName  string `spanner:"last_name"`
	UUID      string `spanner:"uuid"`
	Email     string `spanner:"email"`
}

func main() {
	var projectID, spannerDBName string
	flag.StringVar(&projectID, "gcp-id", "census-demos", "the projectID of the GCP project")
	flag.StringVar(&spannerDBName, "spanner-db", "demo1", "the name of the Cloud Spanner Database")
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

	// Enable the trace sampler.
	// We are using always sample for demo purposes only: it is very high
	// depending on the QPS, but sufficient for the purpose of this quick demo.
	// More realistically perhaps tracing 1 in 10,000 might be more useful
	trace.SetDefaultSampler(trace.AlwaysSample())

	ctx, err := tag.New(
		context.Background(),
		tag.Insert(tagKeyMust("team"), "demos"),
		tag.Insert(tagKeyMust("promo"), "jumbo"),
	)
	if err != nil {
		log.Fatalf("Creating tags err: %v", err)
	}

	ctx, span := trace.StartSpan(ctx, "players-db")
	defer span.End()

	// The database must exist
	databaseName := "projects/census-demos/instances/census-demos/databases/" + spannerDBName
	client, err := spanner.NewClient(ctx, databaseName)
	if err != nil {
		log.Fatalf("SpannerClient err: %v", err)
	}
	defer client.Close()

	// Warm up the client to create a session
	_, _ = client.Single().ReadRow(ctx, "Players", spanner.Key{"foo@gmail.com"}, []string{"email"})

	players := []*Player{
		{FirstName: "Poke", LastName: "Mon", Email: "poke.mon@example.org"},
		{FirstName: "Go", LastName: "Census", Email: "go.census@census.io"},
		{FirstName: "Quick", LastName: "Sort", Email: "q.sort@gmail.com"},
	}
	// A UUID for each player
	ctx = trace.WithSpan(ctx, span)
	uidl, err := nUUIDs(ctx, int64(len(players)))
	if err != nil {
		log.Fatalf("Creating UUIDs err: %v", err)
	}

	// Ensure that the primary key is always unique to ease with demo speed
	uniqPrefix := fmt.Sprintf("%d_", int64(time.Now().Unix()))
	for i, player := range players {
		player.UUID = uidl[i]
		player.Email = uniqPrefix + player.Email
	}

	if err := newPlayers(ctx, client, players...); err != nil {
		log.Printf("newPlayers err: %v", err)
	}
}

func tagKeyMust(key string) tag.Key {
	tagKey, err := tag.NewKey(key)
	if err != nil {
		log.Fatalf("Creating tag key: %v", err)
	}
	return tagKey
}

func newPlayers(ctx context.Context, client *spanner.Client, players ...*Player) error {
	ctx, span := trace.StartSpan(ctx, "new-player")
	defer span.End()

	var ml []*spanner.Mutation
	for _, player := range players {
		m, err := spanner.InsertStruct("Players", player)
		if err != nil {
			return err
		}
		ml = append(ml, m)
	}
	_, err := client.Apply(ctx, ml)
	return err
}

var httpClient = http.Client{
	Transport: httptrace.NewTransport(),
}

func nUUIDs(ctx context.Context, count int64) ([]string, error) {
	ctx, span := trace.StartSpan(ctx, "new-uuid")
	defer span.End()
	fullURL := fmt.Sprintf("http://localhost:8989/uuids?c=%d", count)
	req, err := http.NewRequest("POST", fullURL, nil)
	if err != nil {
		return nil, err
	}
	sc := span.SpanContext()
	log.Printf("Invoking /uuids with spanContext: %+v\n", sc)

	req = req.WithContext(ctx)
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	return deserializeUUIDs(ctx, res.Body)
}

func deserializeUUIDs(ctx context.Context, rc io.ReadCloser) ([]string, error) {
	ctx, span := trace.StartSpan(ctx, "deserialize-uuids")
	defer span.End()
	defer rc.Close()
	blob, err := ioutil.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	var udl []string
	if err := json.Unmarshal(blob, &udl); err != nil {
		return nil, err
	}
	return udl, nil
}
