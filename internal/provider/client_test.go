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

	created, err := client.CreateStream(ctx, "revenue", "Daily revenue")
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
	if _, err := client.CreateStream(ctx, "revenue", ""); err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err := client.CreateStream(ctx, "revenue", "")

	apiErr, ok := err.(*APIError)
	if !ok || !apiErr.IsAlreadyExists() {
		t.Fatalf("expected an already-exists APIError, got %v", err)
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

func TestClientRetriesOnceOn429(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	fake.RateLimitNextRequests(1)
	client := NewClient(fake.Server.URL, "test", nil)

	_, err := client.CreateStream(context.Background(), "revenue", "")

	if err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
}

func TestClientGivesUpAfterOneRetry(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	fake.RateLimitNextRequests(2)
	client := NewClient(fake.Server.URL, "test", nil)

	_, err := client.CreateStream(context.Background(), "revenue", "")

	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected a 429 APIError after the one retry, got %v", err)
	}
}

func TestSeriesAndKeyLifecycleAgainstTheFake(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)
	ctx := context.Background()

	stream, _ := client.CreateStream(ctx, "revenue", "")
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
