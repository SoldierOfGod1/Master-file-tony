package customer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// productRootCategoryID is the root node in service.service.service_category
// whose direct children are the customer-facing product lines:
// Rain Home, Rain Work, Rain Mobile, Rain Loop, 101, etc. Traversing one
// level deeper gives the individual SKUs. The ID was provided by the
// product team and is stable across SIT and prod.
const productRootCategoryID = "Bi4-NgkENagt6KVe_sg"

// fetchProductsProd reads the customer's owned products from
// `product.product.product` — rain's one-row-per-instance table keyed
// by the 8-digit `billing_account_id` (same value as
// `account.account.billing_account.id`).
//
// Each row represents one product the customer currently has: loop
// devices, mobile SIMs, 101 handsets, rainOne Home bundle, care
// packages, etc. The hierarchy is modelled via `parent_product_id`
// (bundles contain sub-products).
//
// When a non-nil `servicePool` is supplied we enrich each row with
// the human-readable product line resolved from
// `service.service_category` (children of productRootCategoryID) and
// the matching product image. A nil servicePool means "product DB
// only" — callers that don't have the service cluster configured
// still get sensible data, just with name-pattern classification.
func fetchProductsProd(ctx context.Context, pool *pgxpool.Pool,
	servicePool *pgxpool.Pool,
	finAccts []string, accountNums []string,
) ([]Product, error) {
	_ = finAccts
	if len(accountNums) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT id,
		       COALESCE(name,''),
		       COALESCE(description,''),
		       COALESCE(status,''),
		       COALESCE(product_status_type,''),
		       COALESCE(product_offering_id,''),
		       COALESCE(parent_product_id,''),
		       COALESCE(is_bundle,false),
		       COALESCE(iccid,''),
		       COALESCE(imei,''),
		       COALESCE(billing_account_id,''),
		       start_date, termination_date
		  FROM product.product
		 WHERE billing_account_id = ANY($1::text[])
		 ORDER BY is_bundle DESC, updated_at DESC
		 LIMIT 80`, accountNums)
	if err != nil {
		return nil, fmt.Errorf("product.product.product: %w", err)
	}
	defer rows.Close()

	var out []Product
	for rows.Next() {
		var (
			id, name, description, status, statusType string
			offeringID, parentID, billingAcctID       string
			iccid, imei                               string
			isBundle                                  bool
			startDate, endDate                        *time.Time
		)
		if err := rows.Scan(
			&id, &name, &description, &status, &statusType,
			&offeringID, &parentID, &isBundle,
			&iccid, &imei, &billingAcctID,
			&startDate, &endDate,
		); err != nil {
			continue
		}
		p := Product{
			ID:            id,
			Name:          name,
			Category:      description,
			ServiceType:   offeringID,
			State:         status,
			AccountNumber: billingAcctID,
			ICCID:         iccid,
			IMEI:          imei,
			MasterPolicy:  statusType,
			IsBundle:      isBundle,
			ParentID:      parentID,
		}
		if startDate != nil {
			p.StartDate = *startDate
		}
		if endDate != nil {
			p.EndDate = *endDate
		}
		p.HasStarted = !p.StartDate.IsZero() && p.StartDate.Before(time.Now())
		p.Family = classifyProductName(name)
		p.ColourVariant = extractColour(name)
		out = append(out, p)
	}

	// Resolve human-readable product lines from the service_category
	// taxonomy (children + grandchildren of productRootCategoryID).
	// The lookup is best-effort: if the service pool is missing or the
	// query fails, we fall back to the name-pattern guess already
	// stored in Name/Category.
	lines := fetchProductLineMap(ctx, servicePool)

	for i := range out {
		out[i].ProductLine = resolveProductLine(out[i], lines)
		out[i].ImageURL = imageForProduct(out[i])
	}
	return out, nil
}

// fetchProductLineMap queries service.service_category for the two
// levels under productRootCategoryID. Returns a map keyed by both the
// category id and the lowercase category name so callers can match
// against whatever they have (a product_offering_id or a free-text
// product name). A nil/errored pool yields an empty map — callers
// treat that as "no enrichment available".
func fetchProductLineMap(ctx context.Context, pool *pgxpool.Pool) map[string]string {
	out := map[string]string{}
	if pool == nil {
		return out
	}
	rows, err := pool.Query(ctx, `
		WITH RECURSIVE tree AS (
		  SELECT id, name, parent_id, 1 AS depth
		    FROM service.service_category
		   WHERE parent_id = $1
		  UNION ALL
		  SELECT c.id, c.name, c.parent_id, t.depth + 1
		    FROM service.service_category c
		    JOIN tree t ON c.parent_id = t.id
		   WHERE t.depth < 3
		)
		SELECT id, name FROM tree`, productRootCategoryID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[id] = name
		out["name:"+strings.ToLower(name)] = name
	}
	return out
}

// resolveProductLine picks the best friendly label for a product.
// Priority: direct id match on product_offering_id → explicit keyword
// rules for the Pro/Home split on 101 → name-contains match on any
// category name → branded fallback derived from Family.
func resolveProductLine(p Product, lines map[string]string) string {
	if p.ServiceType != "" {
		if v, ok := lines[p.ServiceType]; ok {
			return v
		}
	}
	lower := strings.ToLower(p.Name + " " + p.Category)

	// The 101 family splits into two SKUs — Pro and Home. Always
	// classify explicitly; falling through to generic "101" hides
	// which model the customer actually owns.
	if strings.Contains(lower, "101") {
		switch {
		case strings.Contains(lower, "pro"):
			return "101 Pro"
		case strings.Contains(lower, "home"), strings.Contains(lower, "101a"), strings.Contains(lower, "101 a"):
			return "101 Home"
		}
		// Unknown 101 variant — still surface it, just without the
		// specific SKU.
		return "101"
	}

	for key, label := range lines {
		if !strings.HasPrefix(key, "name:") {
			continue
		}
		needle := strings.TrimPrefix(key, "name:")
		if needle == "" {
			continue
		}
		if strings.Contains(lower, needle) {
			return label
		}
	}
	switch p.Family {
	case "mobile":
		return "rain Mobile"
	case "loop":
		return "rain Loop"
	}
	return ""
}

// extractColour parses "loop - Orange", "loop_pink", "Studio Muti
// Green" etc. into a single colour token so the UI can render the
// matching image variant without re-parsing the raw name. Returns an
// empty string when no known colour is present.
func extractColour(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "orange"):
		return "orange"
	case strings.Contains(n, "pink"):
		return "pink"
	case strings.Contains(n, "green"):
		return "green"
	case strings.Contains(n, "white"):
		return "white"
	}
	return ""
}

// imageForProduct picks the product shot that best represents the row.
// The mapping is intentionally simple: look at the name/product-line
// string and fall through known keywords. Unknown products return an
// empty string so the frontend can render a neutral placeholder.
//
// Available static assets (served by the SPA at /products/):
//   sim-3.png, xtender-blue.png, 101pro-blue.png, 101a-blue.png,
//   loop-white.png, loop-pink.png, loop-orange.png, loop-green.png
func imageForProduct(p Product) string {
	blob := strings.ToLower(p.ProductLine + " " + p.Name + " " + p.Category + " " + p.ServiceType)

	// 101 family — explicit Pro/Home split, default "unknown 101"
	// to the Home artwork rather than collapsing to Pro.
	if strings.Contains(blob, "101") {
		switch {
		case strings.Contains(blob, "pro"):
			return "/products/101pro-blue.png"
		default:
			return "/products/101a-blue.png"
		}
	}

	// Loop family — prefer the explicit colour variant the backend
	// parsed; fall back to pattern match on the blob for legacy
	// rows that didn't carry a colour token.
	if p.Family == "loop" || strings.Contains(blob, "loop") ||
		strings.Contains(blob, "rain home") || strings.Contains(blob, "rainone home") ||
		strings.Contains(blob, "rain work") || strings.Contains(blob, "home level") ||
		strings.Contains(blob, "device_group_home") {
		switch p.ColourVariant {
		case "orange":
			return "/products/loop-orange.png"
		case "pink":
			return "/products/loop-pink.png"
		case "green":
			return "/products/loop-green.png"
		}
		switch {
		case strings.Contains(blob, "orange"):
			return "/products/loop-orange.png"
		case strings.Contains(blob, "pink"):
			return "/products/loop-pink.png"
		case strings.Contains(blob, "green"):
			return "/products/loop-green.png"
		case strings.Contains(blob, "extender"), strings.Contains(blob, "xtender"):
			return "/products/xtender-blue.png"
		}
		return "/products/loop-white.png"
	}

	// Mobile family — single SIM image for every SKU (we don't
	// differentiate by colour here).
	if p.Family == "mobile" || strings.Contains(blob, "sim") ||
		strings.Contains(blob, "mobile") || strings.Contains(blob, "msisdn") {
		return "/products/sim-3.png"
	}

	if strings.Contains(blob, "extender") || strings.Contains(blob, "xtender") {
		return "/products/xtender-blue.png"
	}
	return ""
}

// classifyProductName maps rain's real product names into the three UI
// families (mobile / loop / 101) + "other" for edge cases. Based on the
// 21 products observed live on baptista's billing account:
//
//   rainone Home, Device_Group_Home, Home Level        → loop (home internet)
//   101 pro, rainOne 101 Level 0 Bundle                → 101
//   My SIM, My SIM1, My SIM2                           → mobile
//   rain Loop, loop devices, loop - Orange, loopcare,
//   loop care package, loop primary sim group, my loop,
//   1 unli loopzone, loop secondary sim                → loop
//   Pro Skins Group, Studio Muti Green, 5G Speed …     → other (accessories/addons)
func classifyProductName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(n, "101"):
		return "101"
	case strings.Contains(n, "loop"),
		strings.Contains(n, "rainone home"),
		strings.Contains(n, "rain home"),
		strings.Contains(n, "device_group_home"),
		strings.Contains(n, "home level"),
		strings.Contains(n, "extender"):
		return "loop"
	case strings.Contains(n, "sim"),
		strings.Contains(n, "mobile"),
		strings.Contains(n, "4g"),
		strings.Contains(n, "msisdn"):
		return "mobile"
	}
	return "other"
}
