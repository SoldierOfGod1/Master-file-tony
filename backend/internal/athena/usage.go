package athena

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UsageRow mirrors the columns returned by the CDR aggregation query.
// Usage is already in GB (converted in SQL) so the UI doesn't have to
// redo the byte math.
type UsageRow struct {
	Date           time.Time `json:"date"`
	AccountCode    string    `json:"account_code"`
	BillingAccount string    `json:"billing_account"`
	IMEI           string    `json:"imei"`
	IMSI           string    `json:"imsi"`
	MSISDN         string    `json:"msisdn"`
	UsageGB        float64   `json:"usage_gb"`
}

// cacheEntry stores a previous result + expiry.
type cacheEntry struct {
	at   time.Time
	rows []UsageRow
}

// UsageService owns the Athena client + in-memory result cache.
type UsageService struct {
	c       *Client
	mu      sync.RWMutex
	cache   map[string]cacheEntry
	ttl     time.Duration
	windowD time.Duration
}

// NewUsageService wraps a *Client (may be nil if Athena isn't
// configured — callers must check Available() before calling).
func NewUsageService(c *Client) *UsageService {
	return &UsageService{
		c:       c,
		cache:   map[string]cacheEntry{},
		ttl:     30 * time.Minute,
		windowD: 7 * 24 * time.Hour,
	}
}

// Available tells the caller whether Athena is wired at all.
func (s *UsageService) Available() bool {
	return s != nil && s.c != nil
}

// UsageSince runs the CDR aggregation for every IMSI across the
// service's default 7-day lookback window. Results are cached for
// 30 min per unique IMSI-set + date window. Returns empty (not
// error) when not configured so callers render a "no rows" chip
// without branching.
func (s *UsageService) UsageSince(ctx context.Context, imsis []int64) ([]UsageRow, error) {
	if !s.Available() || len(imsis) == 0 {
		return []UsageRow{}, nil
	}
	end := time.Now().UTC()
	start := end.Add(-s.windowD)
	key := cacheKey(imsis, start, end)

	s.mu.RLock()
	if e, ok := s.cache[key]; ok && time.Since(e.at) < s.ttl {
		s.mu.RUnlock()
		return e.rows, nil
	}
	s.mu.RUnlock()

	sql := buildUsageSQL(imsis, start, end)
	raw, err := s.c.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	rows, err := parseUsageRows(raw)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[key] = cacheEntry{at: time.Now(), rows: rows}
	// Bound cache size to prevent unbounded growth from a loop of
	// distinct lookups — drop oldest half when over 256.
	if len(s.cache) > 256 {
		type kv struct {
			k string
			t time.Time
		}
		all := make([]kv, 0, len(s.cache))
		for k, e := range s.cache {
			all = append(all, kv{k, e.at})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
		for i := 0; i < len(all)/2; i++ {
			delete(s.cache, all[i].k)
		}
	}
	s.mu.Unlock()
	return rows, nil
}

// buildUsageSQL composes a safe literal SQL string. IMSIs are
// bigints so string concat is safe; dates are ISO-formatted.
func buildUsageSQL(imsis []int64, start, end time.Time) string {
	quoted := make([]string, 0, len(imsis))
	for _, m := range imsis {
		// IMSI column is varchar; quote the formatted int.
		quoted = append(quoted, "'"+strconv.FormatInt(m, 10)+"'")
	}
	return fmt.Sprintf(`
SELECT
    CAST(date_trunc('day', inserted_at) AS date) AS dt,
    account_code,
    regexp_replace(customer_code, 'BA', '') AS billing_account,
    imei,
    calling_party_imsi,
    calling_party_number,
    ROUND(SUM(CAST(total_volume AS double) / 1024.0 / 1024.0 / 1024.0), 3) AS usage_gb
  FROM AwsDataCatalog.usage.iv_usage_cdr_detail
 WHERE inserted_at BETWEEN timestamp '%s' AND timestamp '%s'
   AND calling_party_imsi IN (%s)
   AND LOWER(service_type) LIKE '%%gprs%%'
 GROUP BY 1, 2, 3, 4, 5, 6
 ORDER BY 1 DESC, 2`,
		start.Format("2006-01-02 15:04:05"),
		end.Format("2006-01-02 15:04:05"),
		strings.Join(quoted, ","),
	)
}

func parseUsageRows(raw [][]string) ([]UsageRow, error) {
	if len(raw) <= 1 {
		return []UsageRow{}, nil
	}
	out := make([]UsageRow, 0, len(raw)-1)
	for _, r := range raw[1:] {
		if len(r) < 7 {
			continue
		}
		var row UsageRow
		if t, err := time.Parse("2006-01-02", r[0]); err == nil {
			row.Date = t
		}
		row.AccountCode = r[1]
		row.BillingAccount = r[2]
		row.IMEI = r[3]
		row.IMSI = r[4]
		row.MSISDN = r[5]
		if v, err := strconv.ParseFloat(r[6], 64); err == nil {
			row.UsageGB = v
		}
		out = append(out, row)
	}
	return out, nil
}

// cacheKey is a deterministic fingerprint of query inputs. IMSIs
// are sorted so the same set in any order collapses to one entry.
func cacheKey(imsis []int64, start, end time.Time) string {
	cp := make([]int64, len(imsis))
	copy(cp, imsis)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	parts := make([]string, 0, len(cp)+2)
	for _, m := range cp {
		parts = append(parts, strconv.FormatInt(m, 10))
	}
	parts = append(parts, start.Format("2006-01-02"), end.Format("2006-01-02"))
	return strings.Join(parts, "|")
}
