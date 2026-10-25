package main

import (
	"context"
	"testing"
)

func TestPeekSubscriptionMieruNoHTTP(t *testing.T) {
	t.Parallel()
	p, err := peekSubscription(context.Background(), "mierus://u:p@192.0.2.2:9002", currentSubscriptionClient())
	if err != nil {
		t.Fatal(err)
	}
	if p.HTTPStatus != 200 || p.SuggestedName == "" {
		t.Fatalf("peek: %+v", p)
	}
}
