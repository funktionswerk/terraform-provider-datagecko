package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

func TestClientCreatesAndListsAStream(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test-admin-key", nil)
	ctx := context.Background()

	created, err := client.CreateStream(ctx, "revenue", "Daily revenue", nil)
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}

	streams, err := client.ListStreams(ctx)
	if err != nil {
		t.Fatalf("ListStreams: %v", err)
	}
	if len(streams) != 1 || streams[0].ID != created.ID {
		t.Fatalf("expected the created stream back, got %+v", streams)
	}
}

func TestCreateStreamAlreadyExists(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, "revenue", "", nil); err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err := client.CreateStream(ctx, "revenue", "", nil)

	apiErr, ok := err.(*APIError)
	if !ok || !apiErr.IsAlreadyExists() {
		t.Fatalf("expected an already-exists APIError, got %v", err)
	}
}

func TestCreateStreamSameNameDifferentTagsIsNotAConflict(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)
	ctx := context.Background()

	online, err := client.CreateStream(ctx, "acme.com", "", map[string]string{"sales_channel": "online"})
	if err != nil {
		t.Fatalf("online create: %v", err)
	}
	offline, err := client.CreateStream(ctx, "acme.com", "", map[string]string{"sales_channel": "offline"})
	if err != nil {
		t.Fatalf("offline create: %v", err)
	}

	if online.ID == offline.ID {
		t.Fatal("expected two distinct streams, got the same id")
	}
	if online.Tags["sales_channel"] != "online" || offline.Tags["sales_channel"] != "offline" {
		t.Fatalf("expected each stream to keep its own tags, got %+v and %+v", online.Tags, offline.Tags)
	}
}

func TestCreateStreamSameNameAndTagsIsAConflict(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)
	ctx := context.Background()
	tags := map[string]string{"sales_channel": "online"}
	if _, err := client.CreateStream(ctx, "acme.com", "", tags); err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err := client.CreateStream(ctx, "acme.com", "", tags)

	apiErr, ok := err.(*APIError)
	if !ok || !apiErr.IsAlreadyExists() {
		t.Fatalf("expected an already-exists APIError, got %v", err)
	}
}

func TestUpdateStreamCanRetagInPlace(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)
	ctx := context.Background()
	stream, err := client.CreateStream(ctx, "acme.com", "", map[string]string{"sales_channel": "online"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	name := "acme.com"
	description := ""
	updated, err := client.UpdateStream(ctx, stream.ID, &name, &description, map[string]string{"sales_channel": "offline"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	if updated.Tags["sales_channel"] != "offline" {
		t.Fatalf("expected the tag to change, got %+v", updated.Tags)
	}
}

func TestDeleteStreamNotFound(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)

	err := client.DeleteStream(context.Background(), "nonexistent")

	apiErr, ok := err.(*APIError)
	if !ok || !apiErr.IsNotFound() {
		t.Fatalf("expected a not-found APIError, got %v", err)
	}
}

func TestClientRetriesUpToThreeTimesOn429(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	fake.RateLimitNextRequests(3)
	client := NewClient(fake.Server.URL, "test", nil)

	_, err := client.CreateStream(context.Background(), "revenue", "", nil)

	if err != nil {
		t.Fatalf("expected the third retry to succeed, got %v", err)
	}
}

func TestClientGivesUpAfterThreeRetries(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	fake.RateLimitNextRequests(4)
	client := NewClient(fake.Server.URL, "test", nil)

	_, err := client.CreateStream(context.Background(), "revenue", "", nil)

	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected a 429 APIError after three retries, got %v", err)
	}
}

func TestClientAuthenticatesWithTheAdminKey(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	fake.RequireAdminKey("correct-key")
	ctx := context.Background()

	wrong := NewClient(fake.Server.URL, "wrong-key", nil)
	_, err := wrong.ListStreams(ctx)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusUnauthorized || apiErr.Code != "invalid_key" {
		t.Fatalf("expected a 401 invalid_key APIError, got %v", err)
	}

	right := NewClient(fake.Server.URL, "correct-key", nil)
	if _, err := right.ListStreams(ctx); err != nil {
		t.Fatalf("expected the correct key to succeed, got %v", err)
	}
}

func TestSeriesAndKeyLifecycleAgainstTheFake(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)
	ctx := context.Background()

	stream, _ := client.CreateStream(ctx, "revenue", "", nil)
	series, err := client.CreateSeries(ctx, stream.ID, "delivery", "USD", "", nil)
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}
	newName := "delivered"
	if _, err := client.UpdateSeries(ctx, stream.ID, series.ID, &newName, nil, nil); err != nil {
		t.Fatalf("UpdateSeries: %v", err)
	}
	if err := client.DeleteSeries(ctx, stream.ID, series.ID); err != nil {
		t.Fatalf("DeleteSeries: %v", err)
	}

	key, err := client.CreateIngestKey(ctx, "feed")
	if err != nil {
		t.Fatalf("CreateIngestKey: %v", err)
	}
	if key.Secret == "" {
		t.Fatal("expected a secret on create")
	}
	if _, err := client.RenameKey(ctx, key.Prefix, "renamed-feed"); err != nil {
		t.Fatalf("RenameKey: %v", err)
	}
	if err := client.DeleteKey(ctx, key.Prefix); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	found, err := client.FindKey(ctx, key.Prefix)
	if err != nil {
		t.Fatalf("FindKey: %v", err)
	}
	if found != nil {
		t.Fatalf("expected a revoked key to be findable-none, got %+v", found)
	}
}

func TestSeriesKindAndIntervalRoundTrip(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)
	ctx := context.Background()
	stream, _ := client.CreateStream(ctx, "revenue", "", nil)
	seven := int64(7)

	series, err := client.CreateSeries(ctx, stream.ID, "deployed", "", "marker", &seven)
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}
	if series.Kind != "marker" || series.IntervalDays == nil || *series.IntervalDays != 7 {
		t.Fatalf("expected kind marker and interval 7 back, got %+v", series)
	}

	cleared, err := client.UpdateSeries(ctx, stream.ID, series.ID, nil, nil, nil)
	if err != nil {
		t.Fatalf("UpdateSeries: %v", err)
	}
	if cleared.IntervalDays != nil {
		t.Fatalf("expected a null interval_days to clear the cadence, got %v", *cleared.IntervalDays)
	}
	if cleared.Kind != "marker" {
		t.Fatalf("expected the kind to be untouched by a PATCH, got %q", cleared.Kind)
	}
}

func TestMetricLifecycleAgainstTheFake(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)
	ctx := context.Background()

	created, err := client.CreateMetric(ctx, "freshness", "Freshness", json.RawMessage(`{"warning_after": 3}`))
	if err != nil {
		t.Fatalf("CreateMetric: %v", err)
	}
	if string(created.Effective) != `{"critical_after":7,"warning_after":3}` {
		t.Fatalf("expected defaults under params, got %s", created.Effective)
	}

	_, err = client.CreateMetric(ctx, "freshness", "FRESHNESS", nil)
	if apiErr, ok := err.(*APIError); !ok || !apiErr.IsAlreadyExists() {
		t.Fatalf("expected a case-insensitive name conflict, got %v", err)
	}

	updated, err := client.UpdateMetric(ctx, created.ID, "Fresh", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("UpdateMetric: %v", err)
	}
	if updated.Name != "Fresh" || string(updated.Params) != `{}` {
		t.Fatalf("expected the rename and params replaced whole, got %+v", updated)
	}
	if err := client.DeleteMetric(ctx, created.ID); err != nil {
		t.Fatalf("DeleteMetric: %v", err)
	}
	_, err = client.GetMetric(ctx, created.ID)
	if apiErr, ok := err.(*APIError); !ok || !apiErr.IsNotFound() {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestStreamMetricLifecycleAgainstTheFake(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)
	ctx := context.Background()
	stream, _ := client.CreateStream(ctx, "revenue", "", nil)
	series, _ := client.CreateSeries(ctx, stream.ID, "delivery", "", "", nil)
	coverage, _ := client.CreateMetric(ctx, "coverage", "Coverage", nil)

	row, err := client.CreateStreamMetric(ctx, stream.ID, coverage.ID, json.RawMessage(`{"target": "`+series.ID+`"}`))
	if err != nil {
		t.Fatalf("CreateStreamMetric: %v", err)
	}
	want := `{"critical_after":3,"markers":[],"target":"` + series.ID + `","warning_after":1,"window_days":30}`
	if string(row.Effective) != want {
		t.Fatalf("expected defaults, instance, row layered, got %s", row.Effective)
	}

	_, err = client.CreateStreamMetric(ctx, stream.ID, coverage.ID, nil)
	if apiErr, ok := err.(*APIError); !ok || !apiErr.IsAlreadyExists() {
		t.Fatalf("expected a second create to conflict, got %v", err)
	}

	replaced, err := client.ReplaceStreamMetric(ctx, stream.ID, coverage.ID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("ReplaceStreamMetric: %v", err)
	}
	if string(replaced.Params) != `{}` {
		t.Fatalf("expected params replaced whole, got %s", replaced.Params)
	}

	_, err = client.ReplaceStreamMetric(ctx, stream.ID, "metric-unknown", json.RawMessage(`{}`))
	if apiErr, ok := err.(*APIError); !ok || !apiErr.IsNotFound() {
		t.Fatalf("expected a replace of a missing row to be a 404, got %v", err)
	}

	if err := client.DeleteStreamMetric(ctx, stream.ID, coverage.ID); err != nil {
		t.Fatalf("DeleteStreamMetric: %v", err)
	}
	_, err = client.GetStreamMetric(ctx, stream.ID, coverage.ID)
	if apiErr, ok := err.(*APIError); !ok || !apiErr.IsNotFound() {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestChartsRoundTripOnlySetStyles(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)
	ctx := context.Background()
	stream, _ := client.CreateStream(ctx, "revenue", "", nil)
	delivery, _ := client.CreateSeries(ctx, stream.ID, "delivery", "", "", nil)
	baseline, _ := client.CreateSeries(ctx, stream.ID, "baseline", "", "", nil)
	other, _ := client.CreateStream(ctx, "orders", "", nil)
	foreign, _ := client.CreateSeries(ctx, other.ID, "count", "", "", nil)
	width, color := 2.5, "#f59e0b"

	_, err := client.ReplaceCharts(ctx, stream.ID, []Chart{{
		Title:  "Delivery vs baseline",
		Series: []ChartEntry{{ID: delivery.ID, Width: &width, Color: &color}, {ID: baseline.ID}},
	}})
	if err != nil {
		t.Fatalf("ReplaceCharts: %v", err)
	}
	charts, err := client.GetCharts(ctx, stream.ID)
	if err != nil {
		t.Fatalf("GetCharts: %v", err)
	}
	if len(charts) != 1 || charts[0].Series[0].Width == nil || *charts[0].Series[0].Width != 2.5 {
		t.Fatalf("expected the set width back, got %+v", charts)
	}
	if bare := charts[0].Series[1]; bare.Width != nil || bare.Color != nil {
		t.Fatalf("expected an unset style to stay absent, got %+v", bare)
	}

	_, err = client.ReplaceCharts(ctx, stream.ID, []Chart{{Series: []ChartEntry{{ID: foreign.ID}}}})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusBadRequest || !strings.Contains(apiErr.Message, "no series") {
		t.Fatalf("expected the server to refuse a foreign series, got %v", err)
	}
}
