// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package csp

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/go-safeweb/safehttp"
	"github.com/google/go-safeweb/safehttp/safehttptest"
)

type dummyReader struct{}

func (dummyReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 41
	}
	return len(b), nil
}

func TestMain(m *testing.M) {
	randReader = dummyReader{}
	os.Exit(m.Run())
}

func TestSerialize(t *testing.T) {
	tests := []struct {
		name       string
		policy     Policy
		wantString string
	}{
		{
			name:       "StrictCSP",
			policy:     StrictCSPBuilder{}.Build(),
			wantString: "object-src 'none'; script-src 'unsafe-inline' https: http: 'nonce-super-secret'; base-uri 'none'",
		},
		{
			name:       "StrictCSP with strict-dynamic",
			policy:     StrictCSPBuilder{StrictDynamic: true}.Build(),
			wantString: "object-src 'none'; script-src 'unsafe-inline' https: http: 'nonce-super-secret' 'strict-dynamic'; base-uri 'none'",
		},
		{
			name:       "StrictCSP with unsafe-eval",
			policy:     StrictCSPBuilder{UnsafeEval: true}.Build(),
			wantString: "object-src 'none'; script-src 'unsafe-inline' https: http: 'nonce-super-secret' 'unsafe-eval'; base-uri 'none'",
		},
		{
			name:       "StrictCSP with set base-uri",
			policy:     StrictCSPBuilder{BaseURI: "https://example.com"}.Build(),
			wantString: "object-src 'none'; script-src 'unsafe-inline' https: http: 'nonce-super-secret'; base-uri https://example.com",
		},
		{
			name:       "StrictCSP with report-uri",
			policy:     StrictCSPBuilder{ReportURI: "https://example.com/collector"}.Build(),
			wantString: "object-src 'none'; script-src 'unsafe-inline' https: http: 'nonce-super-secret'; base-uri 'none'; report-uri https://example.com/collector",
		},
		{
			name:       "FramingCSP",
			policy:     FramingPolicy(false, ""),
			wantString: "frame-ancestors 'self'",
		},
		{
			name:       "FramingCSP with report-uri",
			policy:     FramingPolicy(false, "httsp://example.com/collector"),
			wantString: "frame-ancestors 'self'; report-uri httsp://example.com/collector",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.policy.serialize("super-secret")

			if s != tt.wantString {
				t.Errorf("tt.policy.serialize() got: %q want: %q", s, tt.wantString)
			}
		})
	}
}

func TestBefore(t *testing.T) {
	tests := []struct {
		name                  string
		interceptor           Interceptor
		wantEnforcementPolicy []string
		wantReportOnlyPolicy  []string
		wantNonce             string
	}{
		{
			name:        "No policies",
			interceptor: Interceptor{},
			wantNonce:   "KSkpKSkpKSkpKSkpKSkpKSkpKSk=",
		},
		{
			name:        "Default policies",
			interceptor: Default(""),
			wantEnforcementPolicy: []string{
				"object-src 'none'; script-src 'unsafe-inline' https: http: 'nonce-KSkpKSkpKSkpKSkpKSkpKSkpKSk='; base-uri 'none'",
				"frame-ancestors 'self'",
			},
			wantNonce: "KSkpKSkpKSkpKSkpKSkpKSkpKSk=",
		},
		{
			name:        "Default policies with reporting URI",
			interceptor: Default("https://example.com/collector"),
			wantEnforcementPolicy: []string{
				"object-src 'none'; script-src 'unsafe-inline' https: http: 'nonce-KSkpKSkpKSkpKSkpKSkpKSkpKSk='; base-uri 'none'; report-uri https://example.com/collector",
				"frame-ancestors 'self'; report-uri https://example.com/collector",
			},
			wantNonce: "KSkpKSkpKSkpKSkpKSkpKSkpKSk=",
		},
		{
			name:        "StrictCSP reportonly",
			interceptor: NewInterceptor(StrictCSPBuilder{ReportOnly: true, ReportURI: "https://example.com/collector"}.Build()),
			wantReportOnlyPolicy: []string{
				"object-src 'none'; script-src 'unsafe-inline' https: http: 'nonce-KSkpKSkpKSkpKSkpKSkpKSkpKSk='; base-uri 'none'; report-uri https://example.com/collector",
			},
			wantNonce: "KSkpKSkpKSkpKSkpKSkpKSkpKSk=",
		},
		{
			name:                 "FramingCSP reportonly",
			interceptor:          NewInterceptor(FramingPolicy(true, "https://example.com/collector")),
			wantReportOnlyPolicy: []string{"frame-ancestors 'self'; report-uri https://example.com/collector"},
			wantNonce:            "KSkpKSkpKSkpKSkpKSkpKSkpKSk=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := safehttptest.NewResponseRecorder()
			req := safehttptest.NewRequest(safehttp.MethodGet, "/", nil)

			tt.interceptor.Before(rr.ResponseWriter, req)

			h := rr.Header()
			if diff := cmp.Diff(tt.wantEnforcementPolicy, h.Values("Content-Security-Policy"), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("h.Values(\"Content-Security-Policy\") mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tt.wantReportOnlyPolicy, h.Values("Content-Security-Policy-Report-Only"), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("h.Values(\"Content-Security-Policy-Report-Only\") mismatch (-want +got):\n%s", diff)
			}

			ctx := req.Context()
			v := ctx.Value(ctxKey{})
			if v == nil {
				v = ""
			}
			if got := v.(string); got != tt.wantNonce {
				t.Errorf("ctx.Value(ctxKey{}) got: %q want: %q", got, tt.wantNonce)
			}
		})
	}
}

func TestAlreadyClaimed(t *testing.T) {
	headers := []string{
		"Content-Security-Policy",
		"Content-Security-Policy-Report-Only",
	}

	for _, h := range headers {
		t.Run(h, func(t *testing.T) {
			rr := safehttptest.NewResponseRecorder()
			if _, err := rr.ResponseWriter.Header().Claim(h); err != nil {
				t.Fatalf("rr.ResponseWriter.Header().Claim(h) got err: %v want: nil", err)
			}
			req := safehttptest.NewRequest(safehttp.MethodGet, "/", nil)

			it := Interceptor{}
			it.Before(rr.ResponseWriter, req)

			if got, want := rr.Status(), int(safehttp.StatusInternalServerError); got != want {
				t.Errorf("rr.Status() got: %v want: %v", got, want)
			}

			wantHeaders := map[string][]string{
				"Content-Type":           {"text/plain; charset=utf-8"},
				"X-Content-Type-Options": {"nosniff"},
			}
			if diff := cmp.Diff(wantHeaders, map[string][]string(rr.Header())); diff != "" {
				t.Errorf("rr.Header() mismatch (-want +got):\n%s", diff)
			}

			if got, want := rr.Body(), "Internal Server Error\n"; got != want {
				t.Errorf("rr.Body() got: %q want: %q", got, want)
			}
		})
	}
}

type errorReader struct{}

func (errorReader) Read(b []byte) (int, error) {
	return 0, errors.New("bad")
}

func TestPanicWhileGeneratingNonce(t *testing.T) {
	randReader = errorReader{}
	defer func() {
		if r := recover(); r == nil {
			t.Error("generateNonce() expected panic")
		}
	}()
	generateNonce()
}

func TestNonce(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKey{}, "nonce")
	if got, want := Nonce(ctx), "nonce"; got != want {
		t.Errorf("Nonce(ctx) got: %v want: %v", got, want)
	}
}

func TestNonceEmptyContext(t *testing.T) {
	ctx := context.Background()
	if got, want := Nonce(ctx), ""; got != want {
		t.Errorf("Nonce(ctx) got: %v want: %v", got, want)
	}
}