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

// ResponseWriter TODO
type ResponseWriter struct {
	d  Dispatcher
	rw http.ResponseWriter

	// Having this field unexported is essential for security. Otherwise one can
	// easily overwrite the struct bypassing all our safety guarantees.
	header       Header
	muxInterceps map[string]Interceptor
	written      bool
}

// NewResponseWriter creates a ResponseWriter from a safehttp.Dispatcher, an
// http.ResponseWriter and a list of interceptors associated with a ServeMux.
func NewResponseWriter(d Dispatcher, rw http.ResponseWriter, muxInterceps map[string]Interceptor) *ResponseWriter {
	header := newHeader(rw.Header())
	return &ResponseWriter{
		d:            d,
		rw:           rw,
		header:       header,
		muxInterceps: muxInterceps,
	}
}

// Interceptor returns the interceptor associated with the given key.
func (w *ResponseWriter) Interceptor(key string) Interceptor {
	mp, ok := w.muxInterceps[key]
	if !ok {
		return nil
	}
	return mp
}

// Result TODO
type Result struct {
	err error
}

// NotWritten returns a Result which indicates that nothing has been written yet. It
// can be used in all functions that return a Result, such as in the ServeHTTP method
// of a Handler or in the Before method of an Interceptor. When returned, NotWritten
// indicates that the writing of the response should take place later. When this
// is returned by the Before method in Interceptors the next Interceptor in line
// is run. When this is returned by a Handler, a 204 No Content response is written.
func NotWritten() Result {
	return Result{}
}

// Write dispatches the response to the Dispatcher, setting the Content-Type to
// text/html; charset=utf-8 and response status to 200 OK if resp is a safe
// response. The Dispatcher will then write the response to the underlying
// Response Writer.
//
// TODO: replace panics with proper error handling when getting the response
// Content-Type or writing the response fails.
func (w *ResponseWriter) Write(resp Response) Result {
	ct, err := w.d.ContentType(resp)
	if err != nil {
		panic(err)
	}
	w.rw.WriteHeader(int(StatusOK))
	w.rw.Header().Set("Content-Type", ct)
	if err := w.d.Write(w.rw, resp); err != nil {
		panic(err)
	}
	w.markWritten()
	return Result{}
}

// WriteJSON encapsulates data into a JSON response and dispatches it to the
// Dispatcher, to be serialised and written to the ResponseWriter. It will also
// set the Content-Type to application/json; charset=utf-8 and the response
// status to 200 OK.
//
// TODO: replace panics with proper error handling when getting the response
// Content-Type or writing the response fails.
func (w *ResponseWriter) WriteJSON(data interface{}) Result {
	resp := JSONResponse{Data: data}
	ct, err := w.d.ContentType(resp)
	if err != nil {
		panic(err)
	}
	w.rw.WriteHeader(int(StatusOK))
	w.rw.Header().Set("Content-Type", ct)
	if err := w.d.WriteJSON(w.rw, resp); err != nil {
		panic(err)
	}
	w.markWritten()
	return Result{}
}

// WriteTemplate dispatches a parsed template and a data object to the
// Dispatcher to be executed and written to the underlying Response Writer, in
// case the template is a safe HTML template. If t is a safe HTML Template, the
// Content-Type will also be set to text/html; charset=utf-8 and the response
// status to 200 OK.
//
// TODO: replace panics with proper error handling when getting the response
// Content-Type or writing the response fails.
func (w *ResponseWriter) WriteTemplate(t Template, data interface{}) Result {
	ct, err := w.d.ContentType(t)
	if err != nil {
		panic(err)
	}
	w.rw.WriteHeader(int(StatusOK))
	w.rw.Header().Set("Content-Type", ct)
	if err := w.d.ExecuteTemplate(w.rw, t, data); err != nil {
		panic(err)
	}
	w.markWritten()
	return Result{}
}

// NoContent responds with a 204 No Content response.
func (w *ResponseWriter) NoContent() Result {
	w.markWritten()
	w.rw.WriteHeader(int(StatusNoContent))
	return Result{}
}

// WriteError writes an error response (400-599) according to the provided status
// code.
func (w *ResponseWriter) WriteError(code StatusCode) Result {
	w.markWritten()
	http.Error(w.rw, http.StatusText(int(code)), int(code))
	return Result{}
}

// Redirect responds with a redirect to a given url, using code as the status code.
func (w *ResponseWriter) Redirect(r *IncomingRequest, url string, code StatusCode) Result {
	w.markWritten()
	if code < 300 || code >= 400 {
		panic("wrong method called")
	}
	http.Redirect(w.rw, r.req, url, int(code))
	return Result{}
}

// markWritten ensures that the ResponseWriter is only written to once by panicking
// if it is written more than once.
func (w *ResponseWriter) markWritten() {
	if w.written {
		panic("ResponseWriter was already written to")
	}
	w.written = true
}

// Header returns the collection of headers that will be set
// on the response. Headers must be set before writing a
// response (e.g. Write, WriteTemplate).
func (w ResponseWriter) Header() Header {
	return w.header
}

// SetCookie adds a Set-Cookie header to the provided ResponseWriter's headers.
// The provided cookie must have a valid Name. Otherwise an error will be
// returned.
func (w *ResponseWriter) SetCookie(c *Cookie) error {
	return w.header.addCookie(c)
}

// Dispatcher TODO
type Dispatcher interface {
	Write(rw http.ResponseWriter, resp Response) error
	WriteJSON(rw http.ResponseWriter, resp JSONResponse) error
	ExecuteTemplate(rw http.ResponseWriter, t Template, data interface{}) error
	ContentType(resp Response) (string, error)
}
