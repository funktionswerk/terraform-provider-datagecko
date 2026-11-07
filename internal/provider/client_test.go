package provider

import (
	"context"
	"net/http"
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
	series, err := client.CreateSeries(ctx, stream.ID, "delivery", "USD")
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}
	newName := "delivered"
	if _, err := client.UpdateSeries(ctx, stream.ID, series.ID, &newName, nil); err != nil {
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
