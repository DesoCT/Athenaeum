package security

import (
	"net/http"
	"net/url"
	"slices"
)

// OriginPolicy enforces the same-origin requirement that spec 03 section 10
// places on state-mutating requests, and the origin allow-list that section 11
// requires in remote mode.
type OriginPolicy struct {
	// allowed holds exact scheme://host[:port] origins. In local mode this is
	// derived from the listen address; in remote mode it is configured.
	allowed []string
}

// NewOriginPolicy builds a policy from an explicit allow-list.
func NewOriginPolicy(allowed []string) *OriginPolicy {
	return &OriginPolicy{allowed: slices.Clone(allowed)}
}

// Allows reports whether a request may mutate state. Requests with no Origin
// header are permitted only when they are not cross-origin capable; browsers
// always send Origin on the fetch/XHR paths Athenaeum uses, so a missing
// Origin on a mutating request is treated as untrusted.
func (p *OriginPolicy) Allows(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Fall back to Referer, which older clients still send.
		if ref := r.Header.Get("Referer"); ref != "" {
			u, err := url.Parse(ref)
			if err != nil {
				return false
			}
			origin = u.Scheme + "://" + u.Host
		}
	}
	if origin == "" {
		return false
	}
	return slices.Contains(p.allowed, origin)
}

// IsMutating reports whether a method changes state and therefore requires the
// origin check in addition to a valid session.
func IsMutating(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}
