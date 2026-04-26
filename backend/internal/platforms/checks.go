package platforms

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// probe runs every enabled check for one target and folds them into
// a single Status. Ordering matters: DNS first (cheapest, catches
// the biggest root cause early), then HTTP (reuses resolved hosts),
// then content validation (only if HTTP succeeded), then TLS (cached
// for 6h — certs don't change between polls).
func (m *Monitor) probe(ctx context.Context, t Target) Status {
	st := Status{
		ID:          t.ID,
		Name:        t.Name,
		Group:       t.Group,
		URL:         t.URL,
		DocsURL:     t.DocsURL,
		Criticality: t.Criticality,
		Environment: t.Environment,
		Owner:       t.Owner,
		State:       "unknown",
		CheckedAt:   time.Now().UTC(),
	}

	host := hostOf(t.URL)
	if host != "" {
		st.DNS = m.checkDNS(ctx, host)
	}

	// HTTPS-only policy violation — fail fast without a network call.
	if t.Expect != nil && t.Expect.MustBeHTTPS && !strings.HasPrefix(t.URL, "https://") {
		st.State = "down"
		st.Error = "MustBeHTTPS violated — URL is not HTTPS"
		return st
	}

	// HTTP probe.
	method := t.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, t.URL, nil)
	if err != nil {
		st.State = "down"
		st.Error = err.Error()
		return st
	}
	req.Header.Set("User-Agent", "rain-cc-monitor/1.0")
	start := time.Now()
	resp, err := m.client.Do(req)
	st.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		st.State = "down"
		st.Error = err.Error()
		return st
	}
	// Read up to 256 KB of body for content validation. Always close.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	_ = resp.Body.Close()
	st.HTTPCode = resp.StatusCode

	// Reachability classification. 5xx + network errors → down;
	// 4xx is still "up" since DNS/TLS/network are all fine. 2xx/3xx
	// is the happy path. Content-validation below can still demote
	// a 2xx to "degraded".
	switch {
	case resp.StatusCode >= 500:
		st.State = "down"
		st.Error = resp.Status
	default:
		st.State = "up"
	}

	// Content validation only runs when Expect is set and HTTP succeeded.
	if t.Expect != nil && resp.StatusCode < 400 {
		st.Content = checkContent(body, t.Expect)
		if st.Content.Checked && (!st.Content.TitleOK || !st.Content.BodyOK) {
			// Soft failure — page loaded but is broken. The user's
			// spec says degraded/critical depending on service.
			// We mark "degraded" here and let the alert engine
			// decide severity based on target criticality.
			st.State = "degraded"
			if st.Content.Error != "" {
				st.Error = st.Content.Error
			}
		}
	}

	// TLS check (HTTPS only). Cached 6h — certs don't change between
	// polls, and an in-band TLS handshake per tick is wasteful.
	if strings.HasPrefix(t.URL, "https://") && host != "" {
		st.TLS = m.cachedTLS(ctx, host)
	}

	return st
}

// checkDNS resolves host via the default resolver with a tight
// timeout. Latency is the wall-clock of the resolve call.
func (m *Monitor) checkDNS(ctx context.Context, host string) DNSCheck {
	dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	start := time.Now()
	ips, err := net.DefaultResolver.LookupHost(dctx, host)
	out := DNSCheck{LatencyMS: time.Since(start).Milliseconds()}
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Resolved = true
	out.IPs = ips
	return out
}

// cachedTLS returns the TLS cert info for host, refreshing at most
// every 6h to avoid opening a new TLS handshake on every probe.
func (m *Monitor) cachedTLS(ctx context.Context, host string) TLSCheck {
	m.mu.RLock()
	last, okTime := m.tlsCheckedAt[host]
	cached, okCache := m.tlsCache[host]
	m.mu.RUnlock()
	if okTime && okCache && time.Since(last) < 6*time.Hour {
		// Recompute DaysToExpiry from the stable ExpiresAt so the
		// tile doesn't go stale mid-week.
		if !cached.ExpiresAt.IsZero() {
			cached.DaysToExpiry = int(time.Until(cached.ExpiresAt).Hours() / 24)
		}
		return cached
	}
	tlsCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	check := checkTLS(tlsCtx, host)
	m.mu.Lock()
	m.tlsCache[host] = check
	m.tlsCheckedAt[host] = time.Now()
	m.mu.Unlock()
	return check
}

// checkTLS dials host:443, captures the leaf cert, returns a summary.
func checkTLS(ctx context.Context, host string) TLSCheck {
	dialer := &net.Dialer{Timeout: 4 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", host+":443", &tls.Config{
		ServerName: host,
	})
	if err != nil {
		return TLSCheck{Error: err.Error()}
	}
	defer conn.Close()
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return TLSCheck{Error: "no peer certs"}
	}
	leaf := state.PeerCertificates[0]
	return TLSCheck{
		Valid:        time.Now().Before(leaf.NotAfter) && time.Now().After(leaf.NotBefore),
		Issuer:       leaf.Issuer.CommonName,
		Subject:      leaf.Subject.CommonName,
		ExpiresAt:    leaf.NotAfter,
		DaysToExpiry: int(time.Until(leaf.NotAfter).Hours() / 24),
	}
}

// checkContent validates page body + title against the Expectation
// rules. Empty fields in Expect default to "pass".
func checkContent(body []byte, e *Expectation) ContentCheck {
	out := ContentCheck{Checked: true, TitleOK: true, BodyOK: true}
	txt := string(body)
	lower := strings.ToLower(txt)

	if e.TitleContains != "" {
		title := extractTitle(txt)
		if !strings.Contains(strings.ToLower(title), strings.ToLower(e.TitleContains)) {
			out.TitleOK = false
			out.Error = fmt.Sprintf("title missing %q (got %q)", e.TitleContains, title)
		}
	}
	if e.BodyContains != "" {
		if !strings.Contains(lower, strings.ToLower(e.BodyContains)) {
			out.BodyOK = false
			if out.Error == "" {
				out.Error = fmt.Sprintf("body missing %q", e.BodyContains)
			}
		}
	}
	return out
}

var titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func extractTitle(html string) string {
	m := titleRe.FindStringSubmatch(html)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// hostOf extracts the host (no port) from a URL. Returns empty
// string on parse error so callers can skip host-based checks.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	h := u.Hostname()
	return h
}
