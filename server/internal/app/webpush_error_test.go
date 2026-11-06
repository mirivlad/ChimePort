package app

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"testing"
)

func TestWebPushErrorClass(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"unknown_ca", &url.Error{Op: "Post", URL: "https://updates.push.services.mozilla.com/wpush/v2/SECRET", Err: x509.UnknownAuthorityError{}}, "tls_unknown_authority"},
		{"dns", &url.Error{Op: "Post", Err: &net.DNSError{Err: "not found", Name: "updates.push.services.mozilla.com"}}, "dns_error"},
		{"deadline", context.DeadlineExceeded, "deadline_exceeded"},
		{"unknown", errors.New("misc failure"), "other_error"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := webPushErrorClass(test.err)
			if got != test.want {
				t.Fatalf("webPushErrorClass() = %q, want %q", got, test.want)
			}
		})
	}
}
