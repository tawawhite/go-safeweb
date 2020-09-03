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

package safehttp

import (
	"net/http"
)

const (
	MethodConnect = "CONNECT" // RFC 7231, 4.3.6
	MethodDelete  = "DELETE"  // RFC 7231, 4.3.5
	MethodGet     = "GET"     // RFC 7231, 4.3.1
	MethodHead    = "HEAD"    // RFC 7231, 4.3.2
	MethodOptions = "OPTIONS" // RFC 7231, 4.3.7
	MethodPatch   = "PATCH"   // RFC 5789
	MethodPost    = "POST"    // RFC 7231, 4.3.3
	MethodPut     = "PUT"     // RFC 7231, 4.3.4
	MethodTrace   = "TRACE"   // RFC 7231, 4.3.8
)

// ServeMux is an HTTP request multiplexer. It matches the URL of each incoming
// request against a list of registered patterns and calls the handler for
// the pattern that most closely matches the URL.
//
// When creating the multiplexer, the user needs to specify a list of allowed
// domains. The server will only serve requests target to those domains and
// otherwise will reply with HTTP 404 Not Found.
// TODO(@mihalimara22, @mattiasgrenfeldt): add a link to docs/ explaining
// why this is done.
//
// Patterns names are fixed, rooted paths, like "/favicon.ico", or rooted
// subtrees like "/images/" (note the trailing slash). Longer patterns take
// precedence over shorter ones, so that if there are handlers registered for
// both "/images/" and "/images/thumbnails/", the latter handler will be called
// for paths beginning "/images/thumbnails/" and the former will receive
// requests for any other paths in the "/images/" subtree.
//
// Note that since a pattern ending in a slash names a rooted subtree, the
// pattern "/" matches all paths not matched by other registered patterns,
// not just the URL with Path == "/".
//
// If a subtree has been registered and a request is received naming the subtree
// root without its trailing slash, ServeMux redirects that request to
// the subtree root (adding the trailing slash). This behavior can be overridden
// with a separate registration for the path without the trailing slash. For
// example, registering "/images/" causes ServeMux to redirect a request for
// "/images" to "/images/", unless "/images" has been registered separately.
//
// Patterns may optionally begin with a host name, restricting matches to URLs
// on that host only. This host name must be in the list of allowed domains passed
// when creating the ServeMux. Host-specific patterns take precedence over general
// patterns, so that a handler might register for the two patterns "/codesearch"
// and "codesearch.google.com/" without also taking over requests for
// "http://www.google.com/".
//
// ServeMux also takes care of sanitizing the URL request path and the Host
// header, stripping the port number and redirecting any request containing . or
// .. elements or repeated slashes to an equivalent, cleaner URL.
//
// Multiple handlers can be registered for a single pattern, as long as they
// handle different HTTP methods.
type ServeMux struct {
	mux     *http.ServeMux
	domains map[string]bool
	disp    Dispatcher

	// Maps patterns to handlers supporting multiple HTTP methods.
	handlers map[string]methodHandler

	// Maps interceptor key to interceptor.
	interceps      map[string]Interceptor
	intercepsOrder []string
}

// NewServeMux allocates and returns a new ServeMux
func NewServeMux(d Dispatcher, domains ...string) *ServeMux {
	// TODO(@mattiasgrenfeldt, @mihalimara22): make domains a variadic of string **literals**.
	dm := map[string]bool{}
	for _, host := range domains {
		dm[host] = true
	}
	return &ServeMux{
		mux:       http.NewServeMux(),
		domains:   dm,
		disp:      d,
		handlers:  map[string]methodHandler{},
		interceps: map[string]Interceptor{},
	}
}

// Handle registers a handler for the given pattern and method. If another
// handler is already registered for the same pattern and method, Handle panics.
//
// Configs can be optionally passed in order to modify the behavior of the
// interceptors on a registered handler. Passing a Config whose corresponding
// Interceptor was not installed will produce no effect.
func (m *ServeMux) Handle(pattern string, method string, h Handler, cfgs ...Config) {
	cfgMap := map[string]Config{}
	for _, cfg := range cfgs {
		for k, it := range m.interceps {
			if cfg.Match(it) {
				cfgMap[k] = cfg
			}
		}
	}
	hi := handlerWithInterceptors{
		handler:        h,
		interceps:      m.interceps,
		disp:           m.disp,
		configs:        cfgMap,
		intercepsOrder: m.intercepsOrder,
	}

	mh, ok := m.handlers[pattern]
	if !ok {
		mh := methodHandler{
			handlers: map[string]handlerWithInterceptors{method: hi},
			domains:  m.domains,
		}

		m.handlers[pattern] = mh
		m.mux.Handle(pattern, mh)
		return
	}

	if _, ok := mh.handlers[method]; ok {
		panic("method already registered")
	}
	mh.handlers[method] = hi
}

// Install installs an Interceptor. Interceptor keys need to be unique. If an
// Interceptor with the same key has already been installed, Install panics.
//
// TODO(@empijei, @grenfeldt, @kele, @mihalimara22): Right now you could install
// the same interceptor twice with different keys, we need to figure out how
// exactly we want to avoid that and how we define key uniqueness.
func (m *ServeMux) Install(key string, i Interceptor) {
	if _, exists := m.interceps[key]; exists {
		panic("interceptor with same key already installed")
	}
	m.interceps[key] = i
	m.intercepsOrder = append(m.intercepsOrder, key)
}

// ServeHTTP dispatches the request to the handler whose method matches the
// incoming request and whose pattern most closely matches the request URL.
func (m *ServeMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mux.ServeHTTP(w, r)
}

// methodHandler is a collection of handlerWithInterceptors based on the request method.
type methodHandler struct {
	// Maps an HTTP method to its handlerWithInterceptors
	handlers map[string]handlerWithInterceptors
	domains  map[string]bool
}

// ServeHTTP dispatches the request to the handlerWithInterceptors associated
// with the IncomingRequest method.
func (m methodHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !m.domains[r.Host] {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	h, ok := m.handlers[r.Method]
	if !ok {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	h.ServeHTTP(w, r)
}

// handlerWithInterceptors encapsulates a handler and its corresponding
// interceptors.
type handlerWithInterceptors struct {
	handler        Handler
	configs        map[string]Config
	interceps      map[string]Interceptor
	intercepsOrder []string
	disp           Dispatcher
}

// ServeHTTP calls the Before method of all the interceptors and then calls the
// underlying handler.
func (h handlerWithInterceptors) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ir := NewIncomingRequest(r)
	cp := &CommitPhase{
		Req:            ir,
		Interceps:      h.interceps,
		Configs:        h.configs,
		IntercepsOrder: h.intercepsOrder,
	}
	rw := NewResponseWriter(h.disp, w, h.interceps, cp)

	// The `net/http` package recovers handler panics, but we cannot rely on that behavior here.
	// The reason is, we might need to run After/Commit stages of the interceptors before we
	// respond with a 500 Internal Server Error.
	defer func() {
		if r := recover(); r != nil {
			rw.WriteError(StatusInternalServerError)
		}
	}()

	for _, key := range h.intercepsOrder {
		h.interceps[key].Before(rw, ir, h.configs[key])
		if rw.written {
			return
		}
	}

	h.handler.ServeHTTP(rw, ir)
	if !rw.written {
		rw.NoContent()
	}
}

// CommitPhase contains the configured interceptors that are associated with
// handler and the incoming request.
type CommitPhase struct {
	Req            *IncomingRequest
	Configs        map[string]Config
	Interceps      map[string]Interceptor
	IntercepsOrder []string
}

func (c *CommitPhase) commit(w *CommitResponseWriter, resp Response) {
	for i := len(c.IntercepsOrder) - 1; i >= 0; i-- {
		key := c.IntercepsOrder[i]
		it := c.Interceps[key]
		cfg := c.Configs[key]
		it.Commit(w, c.Req, resp, cfg)
		if w.rw.written {
			return
		}
	}
}
