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
	"flag"
	"fmt"
	"log"
	"reflect"
	"strings"
	"unicode"

	"cloud.google.com/go/spanner"
	"github.com/google/uuid"
	ss "go.opencensus.io/exporter/stats/stackdriver"
	ts "go.opencensus.io/exporter/trace/stackdriver"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
)

var sc *spanner.Client

func main() {
	var projectID string
	flag.StringVar(&projectID, "project-id", "census-demo", "the Spanner and GCP project-id")
	flag.Parse()

	ctx := context.Background()
	client, err := spanner.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("Creating New Spanner client: err: %v", err)
	}
	sc = client

	sse, err := ss.NewExporter(ss.Options{
		ProjectID: projectID,
	})
	if err != nil {
		log.Fatalf("trace/StackDriver err: %v", err)
	}
	ste, err := ts.NewExporter(ts.Options{
		ProjectID: projectID,
	})
	if err != nil {
		log.Fatal("stats/StackDriver err: %v", err)
	}
	trace.RegisterExporter(ste)
	stats.RegisterExporter(sse)

	defer trace.UnregisterExporter(ste)
	defer stats.UnregisterExporter(sse)
}

var (
	newOrderCount, _              = stats.NewMeasureInt64("new-order", "the number of new orders", "order")
	timespentLookup, _            = stats.NewMeasureFloat64("time-spent", "the amount of time spent per order", "second")
	genericErrorCount, _          = stats.NewMeasureInt64("generic-error-count", "the number of generic errors encountered", "error")
	reservationNotFoundCount, _   = stats.NewMeasureInt64("reservation-not-found", "the number of reservations that weren't found", "error")
	attemptedReservationCount, _  = stats.NewMeasureInt64("attempted-reservations", "the number of attempted reservations", "reservation")
	successfulReservationCount, _ = stats.NewMeasureInt64("successful-reservations", "the number of successful reservations", "reservation")
	successfulRemovalCount, _     = stats.NewMeasureInt64("successful-removals", "the number of successful reservation deletions", "reservation")
	attemptedRemovalErrorCount , _     = stats.NewMeasureInt64("attempted-removals", "the number of attempted reservation deletions", "reservation")
)

func setupViews() {
	_ = viewNoErr(stats.NewView(
		"attempted reservations", "The number of attempted reservations", nil,
		attemptedReservationCount, stats.CountAggregation{}, stats.Cumulative{},
	))
	_ = viewNoErr(stats.NewView(
		"generic errors", "The number of errors encountered", nil,
		genericErrorCount, stats.CountAggregation{}, stats.Cumulative{},
	))
	_ = viewNoErr(stats.NewView(
		"not found reservations", "The number of reservations that we couldn't find", nil,
		reservationNotFoundCount, stats.CountAggregation{}, stats.Cumulative{},
	))
	_ = viewNoErr(stats.NewView(
		"successful reservations", "The number of successfully placed reservations", nil,
		successfulReservationCount, stats.CountAggregation{}, stats.Cumulative{},
	))
	_ = viewNoErr(stats.NewView("lookup time spent", "The time spent per lookup",
		[]tag.Key{newKey("time"), newKey("processing-time")},
		timespentLookup,
		stats.DistributionAggregation{
			1e-6, // 1μ
			1e-5, // 10μ
			1e-4, // 100μ
			1e-3, // 1ms
			1e-2, // 10ms
			1e-1, // 100ms
			1,    // 1s
			1e1,  // 10s
			1e2,  // 100s
			1e3,  // 1000s
		},
		stats.Cumulative{},
	))
}

func newKey(name string) tag.Key {
	k, err := tag.NewKey(name)
	if err != nil {
		log.Fatalf("creating NewKey: %v", err)
	}
	return k
}

func viewNoErr(v *stats.View, err error) *stats.View {
	if err != nil {
		log.Fatalf("Error creating view: %v", err)
	}
	if err := stats.RegisterView(v); err != nil {
		log.Fatalf("Registering view %v err: %v", v, err)
	}
	return v
}

func findReservationByCode(ctx context.Context, code string) (*Reservation, error) {
	ctx = trace.StartSpan(ctx, "/find-reservation-by-code")
	defer trace.EndSpan(ctx)

	row, err := sc.Single().ReadRow(ctx, "Reservations", spanner.Key{code}, []string{"code"})
	if err != nil {
		if spanner.ErrCode(err) == codes.NotFound {
			stats.Record(ctx, reservationNotFoundCount.M(1))
		} else {
			stats.Record(ctx, genericErrorCount.M(1))
		}
		return nil, err
	}

	recv := new(Reservation)
	if err := row.ToStruct(recv); err != nil {
		stats.Record(ctx, genericErrorCount.M(1))
		return nil, err
	}
	return recv, nil
}

func removeReservationByCode(ctx context.Context, code string) error {
	ctx = trace.StartSpan(ctx, "/remove-reservation-by-code")
	defer trace.EndSpan(ctx)

	go stats.Record(ctx, attemptedRemovalErrorCount.M(1))

	_, err := sc.Apply(ctx, []*spanner.Mutation{
		spanner.Delete("Reservation", spanner.Key{code}),
	})

	switch {
	case err == nil:
		stats.Record(ctx, successfulRemovalCount.M(1))
	case spanner.ErrCode(err) == codes.NotFound:
		stats.Record(ctx, reservationNotFoundCount.M(1))
	default:
		stats.Record(ctx, genericErrorCount.M(1))
	}

	return err
}

func findReservationsForEmail(ctx context.Context, email string) ([]*Reservation, error) {
	ctx = trace.StartSpan(ctx, "/find-reservation-for-email")
	defer trace.EndSpan(ctx)

	iter := sc.Single().Read(ctx, "Reservations", spanner.Key{email}, []string{"email"})
	var rsrvl []*Reservation
	iter.Do(func(row *spanner.Row) error {
		recv := new(Reservation)
		if err := row.ToStruct(recv); err != nil {
			stats.Record(ctx, genericErrorCount.M(1))
		} else {
			rsrvl = append(rsrvl, recv)
		}
		return nil
	})
	return rsrvl, nil
}

func (rsv *Reservation) toRowsAndValues() ([]string, []interface{}, error) {
	rv := reflect.ValueOf(rsv).Elem()
	nf := rv.NumField()
	names := make([]string, 0, nf)
	values := make([]interface{}, 0, nf)
	typ := rv.Type()
	for i := 0; i < nf; i++ {
		f := typ.Field(i)
		if f.Name == "" || !unicode.IsUpper(rune(f.Name[0])) { // Don't deal with unexported names
			continue
		}
		names = append(names, strings.ToLower(f.Name))
		vf := rv.Field(i)
		values = append(values, vf.Interface())
	}
	return names, values, nil
}

func addReservation(ctx context.Context, rsv *Reservation) (*Reservation, error) {
	ctx = trace.StartSpan(ctx, "/new-reservation")
	defer trace.EndSpan(ctx)

	go stats.Record(ctx, attemptedReservationCount.M(1))

	// Issue them a new reservation
	rsv.Code = fmt.Sprintf("%x", uuid.New())
	tableNames, fieldValues, err := rsv.toRowsAndValues()
	if err != nil {
		stats.Record(ctx, genericErrorCount.M(1))
		return nil, err
	}

	_, err = sc.Apply(ctx, []*spanner.Mutation{
		spanner.Insert("reservations", tableNames, fieldValues),
	})
	if err != nil {
		stats.Record(ctx, genericErrorCount.M(1))
		return nil, err
	}

	stats.Record(ctx, successfulReservationCount.M(1))
	// Now lookup the reservation by the code.
	return findReservationByCode(ctx, rsv.Code)
}
