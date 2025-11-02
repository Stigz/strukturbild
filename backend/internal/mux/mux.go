package mux

import (
	"context"
	"net/http"
	"strings"
)

type Router struct {
	routes      []*route
	middlewares []func(http.Handler) http.Handler
}

type route struct {
	segments []segment
	handler  http.Handler
	methods  map[string]struct{}
	matchAll bool
}

type segment struct {
	literal string
	name    string
	isVar   bool
}

// context key used to stash path variables.
type contextKey string

const varsKey contextKey = "muxVars"

// NewRouter constructs a new Router instance.
func NewRouter() *Router {
	return &Router{}
}

// Use registers a middleware that wraps every matched handler.
func (r *Router) Use(mw func(http.Handler) http.Handler) {
	if mw == nil {
		return
	}
	r.middlewares = append(r.middlewares, mw)
}

// HandleFunc registers a handler for a pattern.
func (r *Router) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) *Route {
	return r.handle(pattern, http.HandlerFunc(handler))
}

// Methods registers a handler for any path with a method constraint.
func (r *Router) Methods(methods ...string) *Route {
	rt := r.handle("", nil)
	rt.data.matchAll = true
	return rt.Methods(methods...)
}

func (r *Router) handle(pattern string, handler http.Handler) *Route {
	rt := &route{handler: handler}
	if pattern == "" {
		rt.matchAll = true
	} else {
		rt.segments = parsePattern(pattern)
	}
	r.routes = append(r.routes, rt)
	return &Route{router: r, data: rt}
}

// Route allows configuring an individual route.
type Route struct {
	router *Router
	data   *route
}

// Methods constrains the HTTP methods accepted by the route.
func (rt *Route) Methods(methods ...string) *Route {
	if rt.data.methods == nil {
		rt.data.methods = make(map[string]struct{})
	}
	for _, method := range methods {
		if method == "" {
			continue
		}
		rt.data.methods[strings.ToUpper(method)] = struct{}{}
	}
	return rt
}

// HandlerFunc sets the handler function for the route.
func (rt *Route) HandlerFunc(handler func(http.ResponseWriter, *http.Request)) {
	if handler == nil {
		return
	}
	rt.data.handler = http.HandlerFunc(handler)
}

// ServeHTTP dispatches the request to the first matching route.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	for _, rt := range r.routes {
		match, vars := rt.match(req)
		if !match {
			continue
		}
		handler := rt.handler
		if handler == nil {
			continue
		}
		if len(vars) > 0 || rt.matchAll {
			req = req.WithContext(context.WithValue(req.Context(), varsKey, vars))
		}
		wrapped := handler
		for i := len(r.middlewares) - 1; i >= 0; i-- {
			wrapped = r.middlewares[i](wrapped)
		}
		wrapped.ServeHTTP(w, req)
		return
	}
	http.NotFound(w, req)
}

func (rt *route) match(req *http.Request) (bool, map[string]string) {
	method := strings.ToUpper(req.Method)
	if len(rt.methods) > 0 {
		if _, ok := rt.methods[method]; !ok {
			return false, nil
		}
	}
	if rt.matchAll {
		return true, map[string]string{}
	}

	pathSegs := splitPath(req.URL.Path)
	if len(pathSegs) != len(rt.segments) {
		return false, nil
	}
	vars := make(map[string]string)
	for i, seg := range rt.segments {
		value := pathSegs[i]
		if seg.isVar {
			vars[seg.name] = value
			continue
		}
		if seg.literal != value {
			return false, nil
		}
	}
	return true, vars
}

func parsePattern(pattern string) []segment {
	trimmed := strings.Trim(pattern, "/")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "/")
	segments := make([]segment, len(parts))
	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
			segments[i] = segment{name: name, isVar: true}
		} else {
			segments[i] = segment{literal: part}
		}
	}
	return segments
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return []string{}
	}
	return strings.Split(trimmed, "/")
}

// Vars returns the path variables captured for a request.
func Vars(r *http.Request) map[string]string {
	if r == nil {
		return map[string]string{}
	}
	if vars, ok := r.Context().Value(varsKey).(map[string]string); ok {
		return vars
	}
	return map[string]string{}
}
