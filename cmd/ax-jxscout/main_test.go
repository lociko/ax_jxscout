package main

import "testing"

func TestHeaderFlagsAreRepeatable(t *testing.T) {
	var headers headerFlags
	for _, value := range []string{"User-Agent: custom-agent", "X-Test: one", "X-Test: two"} {
		if err := headers.Set(value); err != nil {
			t.Fatal(err)
		}
	}
	got := headers.Header()
	if got.Get("User-Agent") != "custom-agent" {
		t.Fatalf("unexpected user agent: %q", got.Get("User-Agent"))
	}
	if values := got.Values("X-Test"); len(values) != 2 || values[0] != "one" || values[1] != "two" {
		t.Fatalf("unexpected repeated headers: %#v", values)
	}
}

func TestHeaderFlagsRejectMalformedAndManagedHeaders(t *testing.T) {
	for _, value := range []string{"missing-colon", "Bad Header: value", "Host: example.test", "X-Test: ok\r\nInjected: value"} {
		var headers headerFlags
		if err := headers.Set(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}
