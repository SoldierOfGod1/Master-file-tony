package axiom

import "testing"

// TestIsSensitive_RedactsSIMIdentifiers is the POPIA regression test for
// the Phase 0 redaction fix. Before this fix, /api/v1/axiom/peek returned
// raw IMSI / MSISDN / ICCID / IMEI values — a direct PII leak to any user
// who could reach the Axiom Explorer. These columns must be redacted.
//
// The column names come straight from docs/axiom/axiom-prod-columns.json
// (the catalogue) — they are the exact column names that exist in
// resource.resource.sim and the downstream rollup views. If any of these
// regresses to returning false, /axiom/peek leaks PII again.
func TestIsSensitive_RedactsSIMIdentifiers(t *testing.T) {
	t.Parallel()

	mustRedact := []string{
		// resource.resource.sim
		"imsi", "msisdn", "iccid", "imei", "cmi_imsi",
		// resource.resource.batches
		"first_imsi",
		// audit + recon views
		"current_imsi", "udm_imsi", "ib_imsi",
		// mixed case should also trip the lowercase match
		"IMSI", "Msisdn", "ICCID",
	}
	for _, col := range mustRedact {
		if !isSensitive(col) {
			t.Errorf("POPIA regression: column %q must be redacted but isSensitive returned false", col)
		}
	}
}

// TestIsSensitive_PreexistingFragments pins the prior redaction list so a
// future refactor can't silently drop a rule.
func TestIsSensitive_PreexistingFragments(t *testing.T) {
	t.Parallel()

	cases := []string{
		"password", "user_password", "passwd",
		"api_secret", "secret_key", "refresh_token", "access_token",
		"api_key", "id_number", "id_no",
		"passport_number", "ssn",
		"pin", "otp_code", "cvv",
	}
	for _, col := range cases {
		if !isSensitive(col) {
			t.Errorf("column %q should be redacted", col)
		}
	}
}

// TestIsSensitive_AllowsNonPII ensures the redaction is precise — routine
// catalogue / relational columns must pass through. A trigger-happy
// redaction list would make the Axiom Explorer useless.
func TestIsSensitive_AllowsNonPII(t *testing.T) {
	t.Parallel()

	cases := []string{
		"id", "inserted_at", "updated_at", "status",
		"billing_account_id", "product_id", "customer_id",
		"activated_at", "name", "description",
		"financial_account_id", "plmn", "provnr",
	}
	for _, col := range cases {
		if isSensitive(col) {
			t.Errorf("column %q should NOT be redacted — blocks legitimate debugging", col)
		}
	}
}
